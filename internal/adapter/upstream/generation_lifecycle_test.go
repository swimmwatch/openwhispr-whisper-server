package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

type generationNormalizer struct{}

func (generationNormalizer) Normalize(_ context.Context, input transcription.Input) (transcription.Normalized, error) {
	return transcription.Normalized{
		Audio: io.NopCloser(bytes.NewReader(make([]byte, 46))), Size: 46,
		RequestID: input.RequestID, Fields: input.Fields,
	}, nil
}

func TestGenerationChangeCannotAcknowledgeUnretiredInference(t *testing.T) {
	for _, mode := range []string{"changed", "matching"} {
		t.Run(mode, func(t *testing.T) {
			checkGenerationRetirement(t, mode == "changed")
		})
	}
}

func checkGenerationRetirement(t *testing.T, changed bool) {
	t.Helper()
	const requestID = "generation-request"
	var mu sync.Mutex
	generation, state := "generation-1", "ready"
	requestHealth := 0
	var forwarded atomic.Int32
	entered, cancelled, retire := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			mu.Lock()
			if r.Header.Get("X-Request-ID") == requestID {
				requestHealth++
				if changed && requestHealth == 2 {
					// The process is replaced between the service's observation and
					// the private client's preflight. Neither may silently change
					// the generation whose retirement the service will require.
					generation = "generation-2"
				}
			}
			currentGeneration, currentState := generation, state
			mu.Unlock()
			ready := currentState == "ready"
			if !ready {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": currentState, "protocol_version": "1",
				"configuration_fingerprint": strings.Repeat("a", 64),
				"process_generation":        currentGeneration, "model": "base",
				"multilingual": true, "cuda_ready": ready,
			})
			return
		}
		forwarded.Add(1)
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
			return
		}
		expectedGeneration := "generation-1"
		if changed {
			expectedGeneration = "generation-2"
		}
		if r.Header.Get("X-Whisper-Generation") != expectedGeneration {
			t.Error("unexpected submitted generation")
		}
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		// Transport closure is deliberately not evidence of native retirement.
		<-retire
	}))
	defer server.Close()
	defer func() {
		select {
		case <-retire:
		default:
			close(retire)
		}
	}()
	client, err := New(clientOptions(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	service, err := transcription.NewService(transcription.Options{
		Model: "base", Fingerprint: strings.Repeat("a", 64), WaitingCapacity: 1,
		OperationTimeout: 5 * time.Second, CleanupTimeout: time.Second,
		ShutdownTimeout: 100 * time.Millisecond, HealthTimeout: 200 * time.Millisecond,
		HealthInterval: 10 * time.Millisecond,
	}, generationNormalizer{}, client, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())
	for deadline := time.Now().Add(time.Second); ; {
		if _, err := service.Readiness(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithCancel(logging.WithRequestID(context.Background(), requestID))
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := service.Admit(ctx, transcription.Input{Model: "base", RequestID: requestID})
		result <- err
	}()
	select {
	case err := <-result:
		if !changed {
			t.Fatal("matching generation did not dispatch exactly once", err)
		}
		// Rejecting the generation drift before submission is the simplest
		// safe outcome: no native work needs cancellation or recovery.
		failureKind(t, err, domain.NotReady)
		if forwarded.Load() != 0 {
			t.Fatal("generation rejection nevertheless forwarded inference")
		}
		return
	case <-entered:
		if changed {
			t.Error("changed generation was forwarded instead of rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("request neither rejected drift nor reached inference")
	}
	mu.Lock()
	state = "recovering"
	mu.Unlock()
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("private inference did not observe cancellation")
	}
	var early error
	returned := false
	select {
	case early = <-result:
		returned = true
		t.Error("service acknowledged cleanup while its submitted generation was still unretired")
	case <-time.After(100 * time.Millisecond):
	}
	mu.Lock()
	generation, state = "generation-3", "ready"
	mu.Unlock()
	close(retire)
	if !returned {
		select {
		case early = <-result:
		case <-time.After(time.Second):
			t.Fatal("retired generation did not release the request")
		}
	}
	failureKind(t, early, domain.DeadlineExceeded)
	if !errors.Is(early, context.Canceled) {
		t.Fatal("cancellation cause was lost")
	}
	if forwarded.Load() != 1 {
		t.Fatal("matching generation retried or overlapped inference")
	}
}
