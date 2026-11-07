package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
)

const maxHTTPStatus = 599

type requestIDKey struct{}

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidRequestID(id string) bool {
	return validID.MatchString(id)
}

func WithRequestID(ctx context.Context, id string) context.Context {
	if !ValidRequestID(id) {
		id = ""
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// New constructs an injected JSON logger. Commands pass os.Stdout at assembly.
func New(out io.Writer, level, component string) (*slog.Logger, error) {
	var severity slog.Level
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, level) ||
		severity.UnmarshalText([]byte(level)) != nil {
		return nil, errors.New("invalid log level")
	}
	if !slices.Contains([]string{"api", "backend", "startup"}, component) {
		return nil, errors.New("invalid log component")
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: severity})).With("component", component), nil
}

type EventName string

const (
	Startup               EventName = "startup"
	ConfigurationRejected EventName = "configuration_rejected"
	RequestCompleted      EventName = "request_completed"
	BackendStarting       EventName = "backend_starting"
	BackendReady          EventName = "backend_ready"
	BackendRecovering     EventName = "backend_recovering"
	BackendStopping       EventName = "backend_stopping"
	BackendFailed         EventName = "backend_failed"
	BackendStopped        EventName = "backend_stopped"
)

type Event struct {
	Name       EventName
	Method     string
	Route      string
	Status     int
	DurationMS int64
	ErrorCode  string
}

// Emit exposes only bounded operational fields, never arbitrary attributes/errors.
func Emit(logger *slog.Logger, ctx context.Context, level slog.Level, event Event) {
	switch event.Name {
	case Startup,
		ConfigurationRejected,
		RequestCompleted,
		BackendStarting,
		BackendReady,
		BackendRecovering,
		BackendStopping,
		BackendFailed,
		BackendStopped:
	default:
		return
	}

	attrs := []slog.Attr{slog.String("event", string(event.Name))}
	if id := RequestID(ctx); ValidRequestID(id) {
		attrs = append(attrs, slog.String("request_id", id))
	}
	if event.Name == RequestCompleted {
		attrs = append(attrs, requestAttributes(event)...)
	}

	switch event.ErrorCode {
	case "invalid_request",
		"unsupported_audio",
		"audio_duration_limit",
		"model_not_found",
		"route_not_found",
		"payload_too_large",
		"rate_limit_exceeded",
		"rate_limit_unavailable",
		"queue_full",
		"operation_timeout",
		"not_ready",
		"backend_unavailable",
		"gpu_unavailable",
		"provisioning_failed",
		"shutting_down",
		"internal_error":
		attrs = append(attrs, slog.String("error_code", event.ErrorCode))
	}

	logger.LogAttrs(ctx, level, string(event.Name), attrs...)
}

func requestAttributes(event Event) []slog.Attr {
	method := event.Method
	switch method {
	case "GET", "POST", "HEAD", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		method = "OTHER"
	}

	route := event.Route
	switch route {
	case "/v1/audio/transcriptions", "/health/live", "/health/ready", "/inference", "/health":
	default:
		route = "unknown"
	}

	status := event.Status
	if status < http.StatusContinue || status > maxHTTPStatus {
		status = 0
	}

	return []slog.Attr{
		slog.String("method", method),
		slog.String("route", route),
		slog.Int("status", status),
		slog.Int64("duration_ms", max(event.DurationMS, 0)),
	}
}
