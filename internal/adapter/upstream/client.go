package upstream

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const (
	minWAVBytes            = 46
	healthReplyBytes       = 4096
	multipartFramingBytes  = 64 << 10
	privateProtocolVersion = "1"
)

type Options struct {
	URL, Fingerprint, Model    string
	OperationTimeout           time.Duration
	MaxWAVBytes, MaxReplyBytes int64
	PromptBytes                int
	Client                     *http.Client
}

type Client struct {
	opts Options
	http *http.Client
}

func New(opts Options) (*Client, error) {
	u, err := url.Parse(opts.URL)
	digest, digestErr := hex.DecodeString(opts.Fingerprint)
	if err != nil ||
		u.Hostname() == "" ||
		u.Scheme != "http" && u.Scheme != "https" ||
		u.User != nil ||
		u.RawQuery != "" ||
		u.Fragment != "" ||
		u.Path != "" && u.Path != "/" ||
		digestErr != nil ||
		len(digest) != sha256.Size ||
		!logging.ValidRequestID(opts.Model) ||
		opts.OperationTimeout < time.Millisecond ||
		opts.MaxWAVBytes < minWAVBytes ||
		opts.MaxReplyBytes < 1 ||
		opts.MaxReplyBytes == math.MaxInt64 ||
		opts.PromptBytes <= 0 ||
		opts.MaxWAVBytes > math.MaxInt64-int64(opts.PromptBytes)-multipartFramingBytes {
		return nil, &domain.Failure{Kind: domain.InvalidInput}
	}

	client := http.Client{}
	if opts.Client != nil {
		client = *opts.Client
	}
	if client.Transport == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client.Transport = transport
	}

	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	opts.URL = strings.TrimSuffix(opts.URL, "/")
	return &Client{opts: opts, http: &client}, nil
}

func (c *Client) Close() {
	c.http.CloseIdleConnections()
}

func (c *Client) Health(parent context.Context) (transcription.Health, error) {
	ctx, cancel := context.WithTimeout(parent, c.opts.OperationTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.URL+"/health", nil)
	if err != nil {
		return transcription.Health{}, portFailure(ctx, err)
	}
	request.Header.Set("X-Request-ID", requestID(ctx, ""))

	response, err := c.http.Do(request)
	if err != nil {
		return transcription.Health{}, portFailure(ctx, err)
	}
	defer response.Body.Close()

	data, err := readBounded(response.Body, healthReplyBytes)
	if err != nil {
		return transcription.Health{}, portFailure(ctx, err)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusServiceUnavailable {
		return transcription.Health{}, privateFailure(response.StatusCode, data)
	}

	var wire struct {
		Status       *domain.BackendState `json:"status"`
		Protocol     *string              `json:"protocol_version"`
		Fingerprint  *string              `json:"configuration_fingerprint"`
		Generation   *string              `json:"process_generation"`
		Model        *string              `json:"model"`
		Multilingual *bool                `json:"multilingual"`
		CUDA         *bool                `json:"cuda_ready"`
	}
	if decode(data, &wire) != nil ||
		wire.Status == nil ||
		wire.Protocol == nil ||
		wire.Fingerprint == nil ||
		wire.Generation == nil ||
		wire.Model == nil ||
		wire.Multilingual == nil ||
		wire.CUDA == nil {
		return transcription.Health{}, portFailure(ctx, errPrivate)
	}
	if *wire.Protocol != privateProtocolVersion || *wire.Fingerprint != c.opts.Fingerprint || *wire.Model != c.opts.Model {
		return transcription.Health{}, &domain.Failure{Kind: domain.NotReady, Cause: errPrivate}
	}
	switch *wire.Status {
	case domain.BackendStarting,
		domain.BackendReady,
		domain.BackendBusy,
		domain.BackendRecovering,
		domain.BackendStopping,
		domain.BackendFailed:
	default:
		return transcription.Health{}, portFailure(ctx, errPrivate)
	}

	ready := *wire.Status == domain.BackendReady || *wire.Status == domain.BackendBusy
	if *wire.CUDA != ready ||
		ready && (response.StatusCode != http.StatusOK ||
			!logging.ValidRequestID(*wire.Generation)) ||
		!ready && response.StatusCode != http.StatusServiceUnavailable ||
		*wire.Generation != "" && !logging.ValidRequestID(*wire.Generation) {
		return transcription.Health{}, portFailure(ctx, errPrivate)
	}
	return transcription.Health{
		BackendCompatibility: domain.BackendCompatibility{
			ProtocolVersion:          *wire.Protocol,
			ConfigurationFingerprint: *wire.Fingerprint,
			ProcessGeneration:        *wire.Generation,
		},
		Status:       *wire.Status,
		Model:        *wire.Model,
		Multilingual: *wire.Multilingual,
		CUDAReady:    *wire.CUDA,
	}, nil
}

func (c *Client) Infer(parent context.Context, input transcription.Normalized) (transcription.Result, error) {
	ctx, cancel := context.WithTimeout(parent, c.opts.OperationTimeout)
	defer cancel()
	ctx = logging.WithRequestID(ctx, requestID(ctx, input.RequestID))

	if input.Audio == nil ||
		input.Size < minWAVBytes ||
		input.Size > c.opts.MaxWAVBytes ||
		!utf8.ValidString(input.Fields.Prompt) ||
		len(input.Fields.Prompt) > c.opts.PromptBytes ||
		math.IsNaN(input.Fields.Temperature) ||
		math.IsInf(input.Fields.Temperature, 0) ||
		input.Fields.Temperature < 0 ||
		input.Fields.Temperature > 1 {
		return transcription.Result{}, &domain.Failure{Kind: domain.InvalidInput}
	}

	health, err := c.Health(ctx)
	if err != nil {
		return transcription.Result{}, err
	}
	if !health.CUDAReady || health.Status != domain.BackendReady {
		return transcription.Result{}, &domain.Failure{Kind: domain.NotReady}
	}

	language := input.Fields.Language
	if language == "" {
		language = "auto"
	}
	if language == "auto" && !health.Multilingual {
		language = "en"
	}
	if !domain.WhisperLanguage(language) || !health.Multilingual && language != "en" {
		return transcription.Result{}, &domain.Failure{Kind: domain.InvalidInput, Field: "language"}
	}

	prefix, suffix, contentType, err := inferenceFraming(input.Fields, language)
	if err != nil {
		return transcription.Result{}, portFailure(ctx, err)
	}

	body := &borrowedBody{
		ctx:    ctx,
		source: io.MultiReader(bytes.NewReader(prefix), io.LimitReader(input.Audio, input.Size), bytes.NewReader(suffix)),
	}
	defer body.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.URL+"/inference", body)
	if err != nil {
		return transcription.Result{}, portFailure(ctx, err)
	}

	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline) / time.Millisecond
	if remaining <= 0 {
		return transcription.Result{}, &domain.Failure{Kind: domain.DeadlineExceeded, Cause: context.DeadlineExceeded}
	}

	request.ContentLength = int64(len(prefix)) + input.Size + int64(len(suffix))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-Request-ID", logging.RequestID(ctx))
	request.Header.Set("X-Whisper-Protocol", health.ProtocolVersion)
	request.Header.Set("X-Whisper-Configuration", health.ConfigurationFingerprint)
	request.Header.Set("X-Whisper-Generation", health.ProcessGeneration)
	request.Header.Set("X-Whisper-Timeout-Ms", strconv.FormatInt(int64(remaining), 10))

	response, err := c.http.Do(request)
	if err != nil {
		return transcription.Result{}, portFailure(ctx, err)
	}
	defer response.Body.Close()

	data, err := readBounded(response.Body, c.opts.MaxReplyBytes)
	if err != nil {
		return transcription.Result{}, portFailure(ctx, err)
	}
	if response.StatusCode != http.StatusOK {
		return transcription.Result{}, privateFailure(response.StatusCode, data)
	}

	var reply struct {
		Text *string `json:"text"`
	}
	if decode(data, &reply) != nil || reply.Text == nil {
		return transcription.Result{}, portFailure(ctx, errPrivate)
	}
	if ctx.Err() != nil {
		return transcription.Result{}, portFailure(ctx, ctx.Err())
	}
	return transcription.Result{Text: *reply.Text}, nil
}

func inferenceFraming(fields domain.InferenceFields, language string) (prefix, suffix []byte, contentType string, err error) {
	var framing bytes.Buffer
	form := multipart.NewWriter(&framing)
	for _, field := range [][2]string{
		{"language", language},
		{"prompt", fields.Prompt},
		{"temperature", strconv.FormatFloat(fields.Temperature, 'f', -1, 64)},
		{"response_format", "json"},
	} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return nil, nil, "", err
		}
	}
	if _, err := form.CreateFormFile("file", "audio.wav"); err != nil {
		return nil, nil, "", err
	}

	prefix = bytes.Clone(framing.Bytes())
	if err := form.Close(); err != nil {
		return nil, nil, "", err
	}
	suffix = bytes.Clone(framing.Bytes()[len(prefix):])
	return prefix, suffix, form.FormDataContentType(), nil
}

var errPrivate = errors.New("private backend response unavailable")

func portFailure(ctx context.Context, cause error) error {
	if ctx.Err() != nil {
		return &domain.Failure{Kind: domain.DeadlineExceeded, Cause: ctx.Err()}
	}
	return &domain.Failure{Kind: domain.BackendUnavailable, Cause: cause}
}

func requestID(ctx context.Context, id string) string {
	if logging.ValidRequestID(id) {
		return id
	}
	if id = logging.RequestID(ctx); logging.ValidRequestID(id) {
		return id
	}
	return rand.Text()
}

func readBounded(reader io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errPrivate
	}
	return data, nil
}

func decode(data []byte, value any) error {
	if !utf8.Valid(data) {
		return errPrivate
	}

	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}

	var extra any
	if d.Decode(&extra) != io.EOF {
		return errPrivate
	}
	return nil
}

func privateFailure(status int, data []byte) error {
	var wire struct {
		Error struct {
			Code    string  `json:"code"`
			Param   *string `json:"param"`
			Message string  `json:"message"`
			Type    string  `json:"type"`
		} `json:"error"`
	}

	kind := domain.BackendUnavailable
	field := ""
	if json.Unmarshal(data, &wire) == nil {
		if wire.Error.Param != nil {
			switch *wire.Error.Param {
			case "file", "model", "language", "prompt", "temperature", "response_format", "stream":
				field = *wire.Error.Param
			}
		}
		if failure, ok := privateCodes[wire.Error.Code]; ok && failure.status == status {
			kind = failure.kind
		}
	}
	return &domain.Failure{Kind: kind, Field: field, Cause: errPrivate}
}

var privateCodes = map[string]struct {
	kind   domain.FailureKind
	status int
}{
	"invalid_request":      {domain.InvalidInput, http.StatusBadRequest},
	"unsupported_audio":    {domain.UnsupportedAudio, http.StatusBadRequest},
	"audio_duration_limit": {domain.AudioTooLong, http.StatusBadRequest},
	"model_not_found":      {domain.UnknownModel, http.StatusNotFound},
	"operation_timeout":    {domain.DeadlineExceeded, http.StatusRequestTimeout},
	"not_ready":            {domain.NotReady, http.StatusServiceUnavailable},
	"backend_unavailable":  {domain.BackendUnavailable, http.StatusServiceUnavailable},
	"gpu_unavailable":      {domain.GPUUnavailable, http.StatusServiceUnavailable},
	"provisioning_failed":  {domain.ProvisioningFailed, http.StatusServiceUnavailable},
	"shutting_down":        {domain.ShuttingDown, http.StatusServiceUnavailable},
	"internal_error":       {domain.Internal, http.StatusInternalServerError},
}

// Close joins transport reads without closing the borrowed normalized resource.
type borrowedBody struct {
	mu     sync.Mutex
	ctx    context.Context
	source io.Reader
	closed bool
}

func (b *borrowedBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, io.ErrClosedPipe
	}
	if b.ctx.Err() != nil {
		return 0, b.ctx.Err()
	}
	return b.source.Read(p)
}

func (b *borrowedBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.source = nil
	return nil
}
