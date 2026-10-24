package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

type cancellingReadiness struct {
	*transcriptionStub
	cancel context.CancelFunc
	err    error
}

func (s cancellingReadiness) Readiness(context.Context) (transcription.Health, error) {
	s.ready.Add(1)
	s.cancel()

	return s.health, s.err
}

func TestTranscriptionReadinessCancellation(t *testing.T) {
	for _, scenario := range []string{"error", "not-ready", "ready"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			stub := &transcriptionStub{}
			service := cancellingReadiness{transcriptionStub: stub, cancel: cancel}
			if scenario == "error" {
				service.err = context.Canceled
			}
			if scenario == "ready" {
				stub.health = transcription.Health{Status: domain.BackendReady, Model: "base", Multilingual: true, CUDAReady: true}
			}

			api := settings(t)
			api.RateLimit.Enabled = false
			e := fixture(t, api, nil, &bytes.Buffer{}, "debug")
			opts := Options{
				Model:            "base",
				WorkRoot:         filepath.Join(t.TempDir(), "uploads"),
				AudioBytes:       1024,
				PromptBytes:      32,
				WaitingCapacity:  1,
				OperationTimeout: time.Second,
			}
			h, err := New(opts, service)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := h.Close(); err != nil {
					t.Error(err)
				}
			})
			h.Register(e)
			server := httptest.NewUnstartedServer(e)
			server.Config.BaseContext = func(net.Listener) context.Context {
				return ctx
			}
			server.Start()
			defer server.Close()

			client := server.Client()
			client.Timeout = 2 * time.Second
			input := formRequest(t, [][2]string{{"model", "base"}}, []byte("audio"))
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/audio/transcriptions", input.Body)
			if err != nil {
				t.Fatal(err)
			}

			request.Header = input.Header.Clone()
			response, err := client.Do(request)
			if response != nil {
				_ = response.Body.Close()
				t.Fatalf("cancelled readiness produced an HTTP response: %d", response.StatusCode)
			}
			if !errors.Is(err, io.EOF) {
				t.Fatalf("expected an aborted response, got %v", err)
			}
			if stub.ready.Load() != 1 || stub.calls.Load() != 0 {
				t.Fatal("cancellation skipped readiness or reached admission")
			}

			files, err := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*"))
			if err != nil || len(files) != 0 {
				t.Fatal("cancelled readiness retained upload scratch", err)
			}
		})
	}
}
