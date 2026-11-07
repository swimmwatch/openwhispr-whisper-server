package transcription

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestCompletedRequestReleasesZeroWaitingCapacity(t *testing.T) {
	opts := serviceOptions()
	opts.WaitingCapacity = 0
	opts.HealthTimeout = time.Second
	backend := &testBackend{health: readyHealth(), infer: func(context.Context, Normalized) (Result, error) {
		return Result{Text: "completed"}, nil
	}}
	normalizer := &testNormalizer{files: map[string]*trackedAudio{}}
	service, err := NewService(opts, normalizer, backend, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	for deadline := time.Now().Add(time.Second); ; {
		if _, err := service.Readiness(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
	for range 10000 {
		result, err := service.Admit(context.Background(), Input{Model: "base", RequestID: "sequential"})
		if err != nil || result.Text != "completed" {
			t.Fatal("a completed request retained zero-waiting admission capacity", err)
		}
	}
}
