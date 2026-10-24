package httpapi

import (
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const (
	multipartFramingBytes  = 64 << 10
	multipartBoundaryBytes = 70
	multipartPartLimit     = 7
	multipartHeaderBytes   = 32 << 10
	multipartFieldBytes    = 128
)

type uploadedRequest struct {
	transcription.Input
	Format string
}

// ReadMultipart owns staging until cleanup; it never accepts a client path.
func (h *Handler) ReadMultipart(
	parent context.Context,
	w http.ResponseWriter,
	r *http.Request,
) (input uploadedRequest, cleanup func() error, err error) {
	ctx, cancel := context.WithTimeout(parent, h.opts.OperationTimeout)
	defer cancel()

	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	_ = controller.SetReadDeadline(deadline)
	readStopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = controller.SetReadDeadline(time.Now())
		close(readStopped)
	})
	defer func() {
		if !stop() {
			<-readStopped
		}
	}()
	defer func() {
		if ctx.Err() != nil {
			err = &domain.Failure{Kind: domain.DeadlineExceeded, Cause: ctx.Err()}
		}
	}()

	kind, params, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if parseErr != nil ||
		kind != "multipart/form-data" ||
		len(params["boundary"]) == 0 ||
		len(params["boundary"]) > multipartBoundaryBytes {
		return input, nil, NewHTTPFailure("invalid_request", "")
	}

	limit := h.opts.AudioBytes + int64(h.opts.PromptBytes) + multipartFramingBytes
	if r.ContentLength > limit {
		return input, nil, NewHTTPFailure("payload_too_large", "file")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	reader, parseErr := r.MultipartReader()
	if parseErr != nil {
		return input, nil, NewHTTPFailure("invalid_request", "")
	}

	dir, makeErr := h.uploadDirectory()
	if makeErr != nil {
		return input, nil, &domain.Failure{Kind: domain.Internal, Cause: makeErr}
	}

	var once sync.Once
	var cleanErr error
	cleanup = func() error {
		once.Do(func() {
			if removeErr := h.root.RemoveAll(dir); removeErr != nil {
				cleanErr = &domain.Failure{Kind: domain.Internal, Cause: removeErr}
			}
		})
		return cleanErr
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = cleanup()
			panic(recovered)
		}
		if err != nil {
			err = errors.Join(err, cleanup())
		}
	}()

	input.RequestID = logging.RequestID(parent)
	input.Fields.Language = "auto"
	input.Format = "json"
	if err := h.readParts(reader, dir, &input); err != nil {
		return input, cleanup, err
	}
	if _, readErr := io.Copy(io.Discard, r.Body); readErr != nil {
		var capErr *http.MaxBytesError
		if errors.As(readErr, &capErr) {
			return input, cleanup, NewHTTPFailure("payload_too_large", "file")
		}
		return input, cleanup, NewHTTPFailure("invalid_request", "")
	}

	err = h.validateUpload(&input)
	return input, cleanup, err
}

func (h *Handler) readParts(reader *multipart.Reader, dir string, input *uploadedRequest) error {
	seen := map[string]bool{}
	headerTotal := 0
	for count := 0; ; count++ {
		part, readErr := reader.NextRawPart()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			var capErr *http.MaxBytesError
			if errors.As(readErr, &capErr) {
				return NewHTTPFailure("payload_too_large", "file")
			}
			return NewHTTPFailure("invalid_request", "")
		}

		key := part.FormName()
		if count >= multipartPartLimit ||
			seen[key] ||
			len(part.Header.Values("Content-Disposition")) != 1 ||
			part.Header.Get("Content-Transfer-Encoding") != "" {
			return NewHTTPFailure("invalid_request", "")
		}
		seen[key] = true

		headerBytes := 0
		for k, values := range part.Header {
			headerBytes += len(k)
			for _, value := range values {
				headerBytes += len(value)
			}
		}
		headerTotal += headerBytes
		if headerTotal > multipartHeaderBytes {
			return NewHTTPFailure("invalid_request", "")
		}

		if key == "file" {
			if err := h.readUpload(part, dir, input); err != nil {
				return err
			}
			continue
		}

		bound := int64(multipartFieldBytes)
		if key == "prompt" {
			bound = int64(h.opts.PromptBytes)
		}
		value, readErr := io.ReadAll(io.LimitReader(part, bound))
		if readErr != nil {
			return NewHTTPFailure("invalid_request", key)
		}

		var extra [1]byte
		if _, readErr = io.ReadFull(part, extra[:]); readErr != io.EOF {
			return NewHTTPFailure("invalid_request", key)
		}
		if err := input.setField(key, string(value)); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) readUpload(part *multipart.Part, dir string, input *uploadedRequest) error {
	name := filepath.Join(dir, "upload")
	file, openErr := h.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if openErr != nil {
		return &domain.Failure{Kind: domain.Internal, Cause: openErr}
	}
	defer file.Close()

	var readErr error
	input.Size, readErr = io.Copy(file, io.LimitReader(part, h.opts.AudioBytes))
	closeErr := file.Close()
	if closeErr != nil {
		return &domain.Failure{Kind: domain.Internal, Cause: closeErr}
	}
	if readErr != nil {
		return NewHTTPFailure("invalid_request", "file")
	}

	var extra [1]byte
	if _, readErr = io.ReadFull(part, extra[:]); readErr != io.EOF {
		return NewHTTPFailure("payload_too_large", "file")
	}

	input.UploadPath = filepath.Join(h.opts.WorkRoot, name)
	return nil
}

func (input *uploadedRequest) setField(key, value string) error {
	switch key {
	case "model":
		input.Model = value
	case "language":
		input.Fields.Language = value
	case "prompt":
		input.Fields.Prompt = value
	case "temperature":
		var readErr error
		input.Fields.Temperature, readErr = strconv.ParseFloat(value, 64)
		if readErr != nil {
			return NewHTTPFailure("invalid_request", key)
		}
	case "response_format":
		input.Format = value
		if input.Format != "json" && input.Format != "text" {
			return NewHTTPFailure("invalid_request", key)
		}
	case "stream":
		if value != "false" {
			return NewHTTPFailure("invalid_request", key)
		}
	default:
		return NewHTTPFailure("invalid_request", "")
	}
	return nil
}

func (h *Handler) validateUpload(input *uploadedRequest) error {
	if input.UploadPath == "" || input.Size == 0 {
		return NewHTTPFailure("invalid_request", "file")
	}
	if !logging.ValidRequestID(input.Model) {
		return NewHTTPFailure("invalid_request", "model")
	}
	if input.Model != h.opts.Model {
		return NewHTTPFailure("model_not_found", "model")
	}

	language, ok := domain.CanonicalLanguage(input.Fields.Language)
	if !ok {
		return NewHTTPFailure("invalid_request", "language")
	}
	input.Fields.Language = language
	if !utf8.ValidString(input.Fields.Prompt) {
		return NewHTTPFailure("invalid_request", "prompt")
	}
	if math.IsNaN(input.Fields.Temperature) || math.IsInf(input.Fields.Temperature, 0) {
		return NewHTTPFailure("invalid_request", "temperature")
	}
	return nil
}
