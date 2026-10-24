package backend

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/httpapi"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const (
	maxMultipartBoundaryBytes = 70
	maxMultipartParts         = 5
	maxMultipartFieldBytes    = 64
)

func (s *Supervisor) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Supervisor) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Keep body cleanup bounded even when routing or private headers reject first.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(s.opts.OperationTimeout))
	defer r.Body.Close()

	id := ""
	if values := r.Header.Values("X-Request-ID"); len(values) == 1 && logging.ValidRequestID(values[0]) {
		id = values[0]
	} else {
		id = rand.Text()
	}

	ctx := logging.WithRequestID(r.Context(), id)
	w.Header().Set("X-Request-ID", id)

	start := time.Now()
	status := 0
	code := ""
	route := ""
	responded := false
	writeFailure := func(err error) {
		if responded || ctx.Err() != nil {
			return
		}
		mapped := httpapi.MapFailure(err)
		status = mapped.Status
		code = mapped.Code

		responded = true
		_ = httpapi.WriteFailure(ctx, w, err)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			writeFailure(failure(domain.Internal, errNative))
		}
		if ctx.Err() != nil {
			status = 0
		}
		logging.Emit(
			s.opts.Logger,
			ctx,
			slog.LevelInfo,
			logging.Event{
				Name:       logging.RequestCompleted,
				Method:     r.Method,
				Route:      route,
				Status:     status,
				DurationMS: time.Since(start).Milliseconds(),
				ErrorCode:  code,
			},
		)
		if !responded && ctx.Err() != nil {
			// A read deadline can cancel the request while TCP still permits writes.
			// Abort instead of letting net/http fabricate an implicit 200 response.
			panic(http.ErrAbortHandler)
		}
	}()

	if r.URL.EscapedPath() != r.URL.Path {
		writeFailure(httpapi.NewHTTPFailure("route_not_found", ""))
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		route = "/health"
		health, err := s.Health(ctx)
		if err != nil {
			writeFailure(err)
			return
		}

		status = http.StatusServiceUnavailable
		if health.CUDAReady {
			status = http.StatusOK
		}

		responded = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(struct {
			Status          domain.BackendState `json:"status"`
			ProtocolVersion string              `json:"protocol_version"`
			Fingerprint     string              `json:"configuration_fingerprint"`
			Generation      string              `json:"process_generation"`
			Model           string              `json:"model"`
			Multilingual    bool                `json:"multilingual"`
			CUDAReady       bool                `json:"cuda_ready"`
		}{
			health.Status,
			health.ProtocolVersion,
			health.ConfigurationFingerprint,
			health.ProcessGeneration,
			health.Model,
			health.Multilingual,
			health.CUDAReady,
		})
	case r.Method == http.MethodPost && r.URL.Path == "/inference":
		route = "/inference"
		compat, timeout, err := s.privateHeaders(r)
		if err != nil {
			writeFailure(err)
			return
		}

		operationCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if deadline, ok := operationCtx.Deadline(); ok {
			_ = http.NewResponseController(w).SetReadDeadline(deadline)
			_ = http.NewResponseController(w).SetWriteDeadline(deadline.Add(s.opts.CleanupTimeout + time.Second))
			// net/http resets deadlines after body cleanup and response flushing.
		}

		result, err := s.handleInference(operationCtx, w, r, compat)
		if err != nil {
			writeFailure(err)
			return
		}
		if ctx.Err() != nil {
			return
		}

		responded = true
		status = http.StatusOK
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Text string `json:"text"`
		}{result.Text})
	default:
		writeFailure(httpapi.NewHTTPFailure("route_not_found", ""))
	}
}

func (s *Supervisor) privateHeaders(r *http.Request) (domain.BackendCompatibility, time.Duration, error) {
	var values [4]string
	for i, name := range []string{
		"X-Whisper-Protocol",
		"X-Whisper-Configuration",
		"X-Whisper-Generation",
		"X-Whisper-Timeout-Ms",
	} {
		header := r.Header.Values(name)
		if len(header) != 1 || header[0] == "" {
			return domain.BackendCompatibility{}, 0, failure(domain.InvalidInput, errNative)
		}
		values[i] = header[0]
	}

	ms, err := strconv.ParseInt(values[3], 10, 64)
	if err != nil || ms <= 0 || ms > int64(s.opts.OperationTimeout/time.Millisecond) {
		return domain.BackendCompatibility{}, 0, failure(domain.InvalidInput, errNative)
	}
	return domain.BackendCompatibility{
		ProtocolVersion:          values[0],
		ConfigurationFingerprint: values[1],
		ProcessGeneration:        values[2],
	}, time.Duration(ms) * time.Millisecond, nil
}

type stagedUpload struct {
	file *os.File
	name string
}

func (s *Supervisor) handleInference(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	compat domain.BackendCompatibility,
) (result domain.TranscriptionResult, err error) {
	op, err := s.begin(ctx, compat)
	if err != nil {
		return result, err
	}

	readInterrupted := make(chan struct{})
	stopRead := context.AfterFunc(op.ctx, func() {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now())
		close(readInterrupted)
	})

	upload := stagedUpload{}
	defer func() {
		recovered := recover()
		if recovered != nil {
			err = failure(domain.Internal, errNative)
		}
		// Join the deadline callback before request-body cleanup returns to net/http.
		if !stopRead() {
			<-readInterrupted
		}
		// Retirement must complete before disposing of request resources.
		err = s.finish(op, err, func() error {
			if upload.file == nil {
				return nil
			}
			return errors.Join(upload.file.Close(), s.area.root.Remove(upload.name))
		})
		if recovered == http.ErrAbortHandler {
			panic(recovered)
		}
	}()

	fields, size, err := s.readMultipart(op.ctx, w, r, &upload)
	if err != nil {
		return result, err
	}
	return s.infer(op, Request{Audio: upload.file, Size: size, Fields: fields, Compatibility: compat})
}

func (s *Supervisor) readMultipart(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	upload *stagedUpload,
) (fields domain.InferenceFields, size int64, err error) {
	kind, params, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if parseErr != nil || kind != "multipart/form-data" || len(params["boundary"]) == 0 ||
		len(params["boundary"]) > maxMultipartBoundaryBytes {
		err = failure(domain.InvalidInput, errNative)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxWAVBytes+int64(s.opts.PromptBytes)+multipartOverheadBytes)
	reader, parseErr := r.MultipartReader()
	if parseErr != nil {
		err = failure(domain.InvalidInput, parseErr)
		return
	}

	seen := map[string]bool{}
	for count := 0; ; count++ {
		part, readErr := reader.NextPart()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			err = failure(domain.InvalidInput, readErr)
			return
		}

		key := part.FormName()
		if count >= maxMultipartParts || seen[key] {
			err = failure(domain.InvalidInput, errNative)
			return
		}
		seen[key] = true

		switch key {
		case "file":
			upload.name = filepath.Join(s.area.name, "request-"+rand.Text()+".wav")
			upload.file, err = s.area.root.OpenFile(upload.name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				err = failure(domain.Internal, err)
				return
			}

			size, err = io.Copy(upload.file, io.LimitReader(contextInput{ctx, part}, s.opts.MaxWAVBytes))
			if err != nil {
				err = failure(domain.InvalidInput, err)
				return
			}

			var extra [1]byte
			if _, extraErr := io.ReadFull(contextInput{ctx, part}, extra[:]); extraErr != io.EOF {
				err = httpapi.NewHTTPFailure("payload_too_large", "file")
				return
			}
		case "language", "prompt", "temperature", "response_format":
			limit := int64(maxMultipartFieldBytes)
			if key == "prompt" {
				limit = int64(s.opts.PromptBytes)
			}

			value, readErr := io.ReadAll(io.LimitReader(contextInput{ctx, part}, limit))
			if readErr != nil {
				err = failure(domain.InvalidInput, readErr)
				return
			}

			var extra [1]byte
			if _, extraErr := io.ReadFull(contextInput{ctx, part}, extra[:]); extraErr != io.EOF {
				err = failure(domain.InvalidInput, errNative)
				return
			}

			switch key {
			case "language":
				fields.Language = string(value)
			case "prompt":
				fields.Prompt = string(value)
			case "temperature":
				fields.Temperature, err = strconv.ParseFloat(string(value), 64)
				if err != nil {
					err = failure(domain.InvalidInput, err)
					return
				}
			case "response_format":
				if string(value) != "json" {
					err = failure(domain.InvalidInput, errNative)
					return
				}
			}
		default:
			err = failure(domain.InvalidInput, errNative)
			return
		}
	}

	if upload.file == nil {
		err = failure(domain.InvalidInput, errNative)
		return
	}
	if _, seekErr := upload.file.Seek(0, io.SeekStart); seekErr != nil {
		err = failure(domain.Internal, seekErr)
	}
	return
}
