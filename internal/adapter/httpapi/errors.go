package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

const minRetryAfterSeconds = 1

// WriteFailure lets the standard-library private HTTP adapter reuse the same
// bounded presentation. The owning handler calls it at most once, before commit.
func WriteFailure(ctx context.Context, w http.ResponseWriter, err error) error {
	if ctx.Err() != nil {
		return nil
	}

	failure := MapFailure(err)
	w.Header().Set("Content-Type", "application/json")
	if failure.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(failure.RetryAfter))
	}
	w.WriteHeader(failure.Status)
	return json.NewEncoder(w).Encode(failure.Envelope())
}

type ErrorEnvelope struct {
	Error PublicError `json:"error"`
}

type PublicError struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    string  `json:"code"`
}

type HTTPFailure struct {
	Status     int
	Message    string
	Type       string
	Param      *string
	Code       string
	RetryAfter int
}

func (f *HTTPFailure) Error() string {
	return f.Code
}

func (f *HTTPFailure) Envelope() ErrorEnvelope {
	return ErrorEnvelope{PublicError{f.Message, f.Type, f.Param, f.Code}}
}

type presentation struct {
	status            int
	category, message string
}

var registry = map[string]presentation{
	"invalid_request":        {http.StatusBadRequest, "invalid_request_error", "Invalid request."},
	"unsupported_audio":      {http.StatusBadRequest, "invalid_request_error", "Unsupported audio."},
	"audio_duration_limit":   {http.StatusBadRequest, "invalid_request_error", "Audio duration exceeds the limit."},
	"model_not_found":        {http.StatusNotFound, "invalid_request_error", "Model is not served."},
	"route_not_found":        {http.StatusNotFound, "invalid_request_error", "Route not found."},
	"payload_too_large":      {http.StatusRequestEntityTooLarge, "invalid_request_error", "Audio payload exceeds the limit."},
	"rate_limit_exceeded":    {http.StatusTooManyRequests, "rate_limit_error", "Request rate limit exceeded."},
	"rate_limit_unavailable": {http.StatusServiceUnavailable, "service_unavailable_error", "Rate limit storage unavailable."},
	"queue_full":             {http.StatusTooManyRequests, "rate_limit_error", "Waiting queue is full."},
	"operation_timeout":      {http.StatusRequestTimeout, "timeout_error", "Operation deadline exceeded."},
	"not_ready":              {http.StatusServiceUnavailable, "service_unavailable_error", "Service is not ready."},
	"backend_unavailable":    {http.StatusServiceUnavailable, "service_unavailable_error", "Backend unavailable."},
	"gpu_unavailable":        {http.StatusServiceUnavailable, "service_unavailable_error", "GPU unavailable."},
	"provisioning_failed":    {http.StatusServiceUnavailable, "service_unavailable_error", "Model provisioning failed."},
	"shutting_down":          {http.StatusServiceUnavailable, "service_unavailable_error", "Service is shutting down."},
	"internal_error":         {http.StatusInternalServerError, "api_error", "Internal service error."},
}

var kindCodes = map[domain.FailureKind]string{
	domain.InvalidInput:       "invalid_request",
	domain.UnsupportedAudio:   "unsupported_audio",
	domain.AudioTooLong:       "audio_duration_limit",
	domain.UnknownModel:       "model_not_found",
	domain.QueueFull:          "queue_full",
	domain.DeadlineExceeded:   "operation_timeout",
	domain.NotReady:           "not_ready",
	domain.BackendUnavailable: "backend_unavailable",
	domain.GPUUnavailable:     "gpu_unavailable",
	domain.ProvisioningFailed: "provisioning_failed",
	domain.ShuttingDown:       "shutting_down",
	domain.Internal:           "internal_error",
}

func NewHTTPFailure(code, field string) *HTTPFailure {
	p, ok := registry[code]
	if !ok {
		code = "internal_error"
		p = registry[code]
		field = ""
	}

	var param *string
	switch field {
	case "file", "model", "language", "prompt", "temperature", "response_format", "stream":
		param = &field
	}
	return &HTTPFailure{Status: p.status, Message: p.message, Type: p.category, Param: param, Code: code}
}

func MapFailure(err error) *HTTPFailure {
	var transport *HTTPFailure
	if errors.As(err, &transport) && transport != nil {
		field := ""
		if transport.Param != nil {
			field = *transport.Param
		}
		result := NewHTTPFailure(transport.Code, field)
		if result.Code == "rate_limit_exceeded" {
			result.RetryAfter = max(transport.RetryAfter, minRetryAfterSeconds)
		}
		return result
	}

	var service *domain.Failure
	if errors.As(err, &service) && service != nil {
		return NewHTTPFailure(kindCodes[service.Kind], service.Field)
	}

	switch echo.StatusCode(err) {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return NewHTTPFailure("route_not_found", "")
	case http.StatusBadRequest:
		return NewHTTPFailure("invalid_request", "")
	case http.StatusRequestEntityTooLarge:
		return NewHTTPFailure("payload_too_large", "file")
	}
	return NewHTTPFailure("internal_error", "")
}

// ErrorHandler owns presentation and never retries an already-owned response.
func ErrorHandler(c *echo.Context, err error) {
	if responseCommitted(c) || c.Request().Context().Err() != nil {
		return
	}

	failure := MapFailure(err)
	if failure.Code == "rate_limit_exceeded" {
		c.Response().Header().Set(echo.HeaderRetryAfter, strconv.Itoa(max(failure.RetryAfter, minRetryAfterSeconds)))
	}
	_ = c.JSON(failure.Status, failure.Envelope())
}

func responseCommitted(c *echo.Context) bool {
	response, err := echo.UnwrapResponse(c.Response())
	return err != nil || response.Committed
}
