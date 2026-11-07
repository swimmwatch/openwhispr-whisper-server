package transcription

import (
	"context"
	"io"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

// Input borrows an immutable upload owned by the blocking HTTP handler.
type Input struct {
	UploadPath string
	Size       int64
	Model      string
	RequestID  string
	Fields     domain.InferenceFields
}

// Normalized owns Audio until Close; Infer only borrows it until returning.
type Normalized struct {
	Audio     io.ReadCloser
	Size      int64
	Duration  time.Duration
	RequestID string
	Fields    domain.InferenceFields
	// ProcessGeneration pins service dispatch to the process it owns for recovery.
	// Direct adapter callers may leave it empty and use a fresh health observation.
	ProcessGeneration string
}

type Result = domain.TranscriptionResult
type Health = domain.BackendHealth

type Normalizer interface {
	Normalize(context.Context, Input) (Normalized, error)
}

type InferenceBackend interface {
	Health(context.Context) (Health, error)
	Infer(context.Context, Normalized) (Result, error)
}
