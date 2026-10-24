package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

const (
	canonicalWAVHeaderBytes  = 44
	canonicalWAVSampleBytes  = 2
	canonicalWAVSampleRate   = 16000
	canonicalWAVMinimumBytes = canonicalWAVHeaderBytes + canonicalWAVSampleBytes
)

type operation struct {
	ctx, parent context.Context
	cancel      context.CancelFunc
	unlink      func() bool
	child       *nativeChild
	finished    chan struct{}
	submitted   bool
}

func (s *Supervisor) begin(parent context.Context, compat domain.BackendCompatibility) (*operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := parent.Err(); err != nil {
		return nil, failure(domain.DeadlineExceeded, err)
	}
	if s.stopping {
		return nil, failure(domain.ShuttingDown, errNative)
	}
	if s.status != domain.BackendReady || s.current == nil || s.active != nil {
		return nil, failure(domain.NotReady, errNative)
	}
	if compat.ProtocolVersion != backendProtocolVersion || compat.ConfigurationFingerprint != s.opts.Fingerprint ||
		compat.ProcessGeneration != s.current.generation {
		return nil, failure(domain.NotReady, errNative)
	}

	ctx, cancel := context.WithTimeout(parent, s.opts.OperationTimeout)
	op := &operation{ctx: ctx, parent: parent, cancel: cancel, child: s.current, finished: make(chan struct{})}
	op.unlink = context.AfterFunc(op.child.ctx, cancel)
	s.active = op
	s.status = domain.BackendBusy

	return op, nil
}

// Infer borrows an already bounded canonical WAV stream. Its owner keeps the
// stream readable until this blocking call returns, including cancellation.
func (s *Supervisor) Infer(ctx context.Context, request Request) (result domain.TranscriptionResult, err error) {
	op, err := s.begin(ctx, request.Compatibility)
	if err != nil {
		return result, err
	}

	defer func() {
		err = s.finish(op, err, nil)
	}()

	return s.infer(op, request)
}

func (s *Supervisor) infer(op *operation, request Request) (domain.TranscriptionResult, error) {
	fields, err := s.inferenceFields(request.Fields)
	if err != nil {
		return domain.TranscriptionResult{}, err
	}
	if request.Audio == nil || request.Size < canonicalWAVMinimumBytes || request.Size > s.opts.MaxWAVBytes {
		return domain.TranscriptionResult{}, failure(domain.InvalidInput, errNative)
	}

	var header [canonicalWAVHeaderBytes]byte
	if _, err := io.ReadFull(contextInput{op.ctx, request.Audio}, header[:]); err != nil {
		return domain.TranscriptionResult{}, failure(domain.InvalidInput, err)
	}
	if !canonicalWAV(header[:], request.Size) {
		return domain.TranscriptionResult{}, failure(domain.UnsupportedAudio, errNative)
	}

	request.Audio = io.MultiReader(bytes.NewReader(header[:]), request.Audio)
	request.Fields = fields
	if err := op.ctx.Err(); err != nil {
		return domain.TranscriptionResult{}, failure(domain.DeadlineExceeded, err)
	}

	op.submitted = true
	return s.callNative(op, request)
}

func (s *Supervisor) inferenceFields(fields domain.InferenceFields) (domain.InferenceFields, error) {
	multilingual := s.opts.Artifact.Metadata.Multilingual
	if fields.Language == "" || fields.Language == "auto" && !multilingual {
		if multilingual {
			fields.Language = "auto"
		} else {
			fields.Language = "en"
		}
	}

	if !domain.WhisperLanguage(fields.Language) || !multilingual && fields.Language != "en" {
		return domain.InferenceFields{}, failure(domain.InvalidInput, errNative)
	}
	if !utf8.ValidString(fields.Prompt) || len(fields.Prompt) > s.opts.PromptBytes {
		return domain.InferenceFields{}, failure(domain.InvalidInput, errNative)
	}
	if math.IsNaN(fields.Temperature) || math.IsInf(fields.Temperature, 0) || fields.Temperature < 0 ||
		fields.Temperature > 1 {
		return domain.InferenceFields{}, failure(domain.InvalidInput, errNative)
	}

	return fields, nil
}

func canonicalWAV(h []byte, size int64) bool {
	if string(h[:4]) != "RIFF" || string(h[8:16]) != "WAVEfmt " {
		return false
	}
	if binary.LittleEndian.Uint32(h[4:]) != uint32(size-8) || size-8 > math.MaxUint32 {
		return false
	}
	if binary.LittleEndian.Uint32(h[16:]) != 16 || binary.LittleEndian.Uint16(h[20:]) != 1 ||
		binary.LittleEndian.Uint16(h[22:]) != 1 {
		return false
	}
	if binary.LittleEndian.Uint32(h[24:]) != canonicalWAVSampleRate ||
		binary.LittleEndian.Uint32(h[28:]) != canonicalWAVSampleRate*canonicalWAVSampleBytes {
		return false
	}
	if binary.LittleEndian.Uint16(h[32:]) != canonicalWAVSampleBytes || binary.LittleEndian.Uint16(h[34:]) != 16 ||
		string(h[36:40]) != "data" {
		return false
	}

	dataBytes := size - canonicalWAVHeaderBytes
	return int64(binary.LittleEndian.Uint32(h[40:])) == dataBytes && dataBytes%canonicalWAVSampleBytes == 0
}

func (s *Supervisor) callNative(op *operation, request Request) (domain.TranscriptionResult, error) {
	var framing bytes.Buffer
	form := multipart.NewWriter(&framing)
	for _, field := range [][2]string{
		{"language", request.Fields.Language},
		{"prompt", request.Fields.Prompt},
		{"temperature", strconv.FormatFloat(request.Fields.Temperature, 'f', -1, 64)},
		{"carry_initial_prompt", "false"},
		{"response_format", "json"},
	} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return domain.TranscriptionResult{}, failure(domain.Internal, err)
		}
	}

	if _, err := form.CreateFormFile("file", "audio.wav"); err != nil {
		return domain.TranscriptionResult{}, failure(domain.Internal, err)
	}

	prefix := bytes.Clone(framing.Bytes())
	if err := form.Close(); err != nil {
		return domain.TranscriptionResult{}, failure(domain.Internal, err)
	}

	suffix := bytes.Clone(framing.Bytes()[len(prefix):])
	body := &requestBody{
		ctx:    op.ctx,
		source: io.MultiReader(bytes.NewReader(prefix), io.LimitReader(request.Audio, request.Size), bytes.NewReader(suffix)),
	}
	defer body.Close()

	native, err := http.NewRequestWithContext(op.ctx, http.MethodPost, "http://"+s.nativeAddress()+"/inference", body)
	if err != nil {
		return domain.TranscriptionResult{}, failure(domain.BackendUnavailable, err)
	}
	native.ContentLength = int64(len(prefix)) + request.Size + int64(len(suffix))
	native.Header.Set("Content-Type", form.FormDataContentType())

	response, err := s.client.Do(native)
	if err != nil {
		return domain.TranscriptionResult{}, failure(domain.BackendUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return domain.TranscriptionResult{}, failure(domain.BackendUnavailable, errNative)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, s.opts.MaxReplyBytes))
	if err != nil {
		return domain.TranscriptionResult{}, failure(domain.BackendUnavailable, err)
	}

	var extra [1]byte
	if _, err := io.ReadFull(response.Body, extra[:]); err != io.EOF {
		return domain.TranscriptionResult{}, failure(domain.BackendUnavailable, errNative)
	}

	var reply struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(data, &reply) != nil || reply.Text == nil || !utf8.Valid(data) {
		return domain.TranscriptionResult{}, failure(domain.BackendUnavailable, errNative)
	}

	return domain.TranscriptionResult{Text: *reply.Text}, nil
}

func (s *Supervisor) finish(op *operation, result error, cleanup func() error) error {
	contextErr := op.ctx.Err()
	if deadline, ok := op.ctx.Deadline(); contextErr == nil && ok && !time.Now().Before(deadline) {
		contextErr = context.DeadlineExceeded
	}
	if err := contextErr; err != nil {
		kind := domain.BackendUnavailable
		if errors.Is(err, context.DeadlineExceeded) || op.parent.Err() != nil {
			kind = domain.DeadlineExceeded
		}
		result = failure(kind, err)
	}

	if result != nil && op.submitted {
		s.requestRecovery(op.child)
		s.mu.Lock()
		remaining := time.Until(op.child.retireAt)
		s.mu.Unlock()
		timer := time.NewTimer(max(0, remaining))
		select {
		case <-op.child.retired:
		case <-timer.C:
			result = errors.Join(result, failure(domain.BackendUnavailable, context.DeadlineExceeded))
		}
		timer.Stop()
	}

	op.unlink()
	op.cancel()
	if cleanup != nil {
		if err := cleanup(); err != nil {
			result = errors.Join(result, failure(domain.Internal, err))
			s.requestRecovery(op.child)
		}
	}

	s.mu.Lock()
	if s.active == op {
		s.active = nil
		if s.current == op.child && s.status == domain.BackendBusy && !s.stopping {
			s.status = domain.BackendReady
		}
	}
	close(op.finished)
	s.mu.Unlock()

	return result
}

type contextInput struct {
	ctx    context.Context
	source io.Reader
}

func (r contextInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}

// Close synchronizes with the final reader and prevents future transport reads;
// it never closes the caller-owned WAV resource.
type requestBody struct {
	mu     sync.Mutex
	ctx    context.Context
	source io.Reader
	closed bool
}

func (b *requestBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, io.ErrClosedPipe
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.source.Read(p)
}

func (b *requestBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	b.source = nil
	return nil
}
