package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v5"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const transcriptionsPath = "/v1/audio/transcriptions"

type Transcriber interface {
	Admit(context.Context, transcription.Input) (transcription.Result, error)
	Readiness(context.Context) (transcription.Health, error)
}

type Options struct {
	Model            string
	WorkRoot         string
	AudioBytes       int64
	PromptBytes      int
	WaitingCapacity  int
	OperationTimeout time.Duration
}

type Handler struct {
	opts    Options
	service Transcriber
	root    *os.Root
	lock    *os.File
	slots   chan struct{}
	mu      sync.Mutex
	closed  bool
}

func New(opts Options, service Transcriber) (*Handler, error) {
	if service == nil ||
		!logging.ValidRequestID(opts.Model) ||
		!filepath.IsAbs(opts.WorkRoot) ||
		opts.AudioBytes <= 0 ||
		opts.PromptBytes <= 0 ||
		opts.WaitingCapacity < 0 ||
		opts.WaitingCapacity == math.MaxInt ||
		opts.OperationTimeout <= 0 ||
		opts.AudioBytes > math.MaxInt64-int64(opts.PromptBytes)-multipartFramingBytes {
		return nil, &domain.Failure{Kind: domain.InvalidInput}
	}

	root, lock, err := openUploads(opts.WorkRoot)
	if err != nil {
		return nil, &domain.Failure{Kind: domain.ProvisioningFailed, Cause: err}
	}
	return &Handler{
		opts:    opts,
		service: service,
		root:    root,
		lock:    lock,
		slots:   make(chan struct{}, opts.WaitingCapacity+1),
	}, nil
}

type requestValidator struct{ fields *validator.Validate }

func (v requestValidator) Validate(value any) error {
	return v.fields.Struct(value)
}

func (h *Handler) Register(e *echo.Echo) {
	e.Validator = requestValidator{validator.New(validator.WithRequiredStructEnabled())}
	e.POST(transcriptionsPath, h.transcribe)
	e.GET("/health/live", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "live"})
	})
	e.GET("/health/ready", h.readiness)
}

// Close runs after the owning server has drained all handlers.
func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}
	if len(h.slots) != 0 {
		return &domain.Failure{Kind: domain.ShuttingDown}
	}

	h.closed = true
	return errors.Join(h.lock.Close(), h.root.Close())
}

func (h *Handler) transcribe(c *echo.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return &domain.Failure{Kind: domain.ShuttingDown}
	}

	select {
	case h.slots <- struct{}{}:
		h.mu.Unlock()
	default:
		h.mu.Unlock()
		return &domain.Failure{Kind: domain.QueueFull}
	}
	defer func() {
		<-h.slots
	}()

	ctx := c.Request().Context()
	input, cleanup, err := h.ReadMultipart(ctx, c.Response(), c.Request())
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		if ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
		return err
	}

	if err = c.Validate(struct {
		Model       string  `validate:"required,max=128"`
		Temperature float64 `validate:"gte=0,lte=1"`
	}{input.Model, input.Fields.Temperature}); err != nil {
		field := "model"
		var invalid validator.ValidationErrors
		if errors.As(err, &invalid) && len(invalid) > 0 && invalid[0].Field() == "Temperature" {
			field = "temperature"
		}
		return NewHTTPFailure("invalid_request", field)
	}

	health, err := h.service.Readiness(ctx)
	if ctx.Err() != nil {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		return err
	}
	if !health.CUDAReady || health.Model != h.opts.Model {
		return &domain.Failure{Kind: domain.NotReady}
	}
	if !health.Multilingual {
		if input.Fields.Language == "auto" {
			input.Fields.Language = "en"
		}
		if input.Fields.Language != "en" {
			return NewHTTPFailure("invalid_request", "language")
		}
	}

	result, err := h.service.Admit(ctx, input.Input)
	err = errors.Join(err, cleanup())
	if ctx.Err() != nil {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		return err
	}

	if input.Format == "text" {
		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(result.Text))
	}
	return c.JSON(http.StatusOK, struct {
		Text string `json:"text"`
	}{result.Text})
}

func (h *Handler) readiness(c *echo.Context) error {
	health, err := h.service.Readiness(c.Request().Context())
	if c.Request().Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}
	if err == nil && health.CUDAReady && health.Model == h.opts.Model {
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	}
	return c.JSON(http.StatusServiceUnavailable, map[string]string{
		"status": "not_ready",
		"reason": readinessReason(health, err),
	})
}

func readinessReason(health transcription.Health, err error) string {
	reason := "not_ready"
	switch health.Status {
	case domain.BackendStarting:
		reason = "starting"
	case domain.BackendRecovering:
		reason = "recovering"
	case domain.BackendStopping:
		reason = "shutting_down"
	case domain.BackendFailed:
		reason = "backend_unavailable"
	}

	var failure *domain.Failure
	if errors.As(err, &failure) {
		switch failure.Kind {
		case domain.GPUUnavailable:
			reason = "gpu_unavailable"
		case domain.BackendUnavailable:
			reason = "backend_unavailable"
		case domain.ProvisioningFailed:
			reason = "provisioning_failed"
		case domain.ShuttingDown:
			reason = "shutting_down"
		}
	}
	return reason
}
