package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/httpapi"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"golang.org/x/sys/unix"
)

func TestAPICanceledBeforeAssembly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := filepath.Join(t.TempDir(), "uploads")
	var logs bytes.Buffer
	if err := run(ctx, nil, []string{}, &logs, runtimeOptions{workRoot: root}); err != nil {
		t.Fatal("canceled startup failed", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled startup initialized request storage", err)
	}
	if !strings.Contains(logs.String(), `"event":"api_stopped"`) {
		t.Fatal("canceled startup omitted its stop event")
	}
}

func TestAPIStartupFailuresReleaseResources(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer private.Close()

	for _, name := range []string{"decoder-options", "upload-storage", "occupied-listener"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			runtime := runtimeOptions{
				decoderCommand: []string{"ffmpeg"},
				workRoot:       filepath.Join(root, "uploads"),
			}
			args := []string{
				"--api-host=127.0.0.1",
				"--backend-url=" + private.URL,
				"--operation-seconds=1",
				"--cleanup-seconds=1",
				"--shutdown-seconds=1",
			}
			want := domain.InvalidInput
			switch name {
			case "decoder-options":
				runtime.decoderCommand = nil
			case "upload-storage":
				want = domain.ProvisioningFailed
				if err := os.WriteFile(runtime.workRoot, []byte("PRIVATE sentinel"), 0600); err != nil {
					t.Fatal(err)
				}
			case "occupied-listener":
				want = domain.Internal
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				args = append(args, "--api-port="+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
			}

			var logs bytes.Buffer
			err := run(context.Background(), args, []string{}, &logs, runtime)
			var failure *domain.Failure
			if !errors.As(err, &failure) || failure.Kind != want {
				t.Fatalf("startup failure classification: got %v, want %s", err, want)
			}
			if strings.Contains(logs.String(), "PRIVATE") || !strings.Contains(logs.String(), `"event":"api_failed"`) {
				t.Fatal("startup failure was not safely logged")
			}

			switch name {
			case "decoder-options":
				if _, err := os.Stat(runtime.workRoot); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("rejected decoder initialized request storage", err)
				}
			case "upload-storage":
				data, err := os.ReadFile(runtime.workRoot)
				if err != nil || string(data) != "PRIVATE sentinel" {
					t.Fatal("failed startup changed preexisting storage", err)
				}
			case "occupied-listener":
				lock, err := os.OpenFile(filepath.Join(runtime.workRoot, ".owner.lock"), os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal("failed startup retained upload ownership", err)
				}
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAPIWAVAndShutdownBounds(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    int64
	}{
		{1, 32044},
		{134217, 4294944044},
		{134218, 4294967303},
		{math.MaxInt, 4294967303},
	} {
		if got := maxWAVBytes(tc.seconds); got != tc.want {
			t.Errorf("WAV limit for %d seconds: got %d, want %d", tc.seconds, got, tc.want)
		}
	}

	for _, tc := range []struct {
		drain, cleanup, want time.Duration
	}{
		{30 * time.Second, 10 * time.Second, 40 * time.Second},
		{time.Duration(math.MaxInt64) - time.Second, time.Second, time.Duration(math.MaxInt64)},
		{time.Duration(math.MaxInt64), time.Second, time.Duration(math.MaxInt64)},
	} {
		if got := shutdownBudget(tc.drain, tc.cleanup); got != tc.want {
			t.Errorf("shutdown budget overflow: got %s, want %s", got, tc.want)
		}
	}
}

func TestAPIServeListenerFailureAndForcedDrain(t *testing.T) {
	for _, name := range []string{"closed-listener", "forced-http-drain"} {
		t.Run(name, func(t *testing.T) {
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			service, err := transcription.NewService(transcription.Options{
				Model:            "base",
				Fingerprint:      strings.Repeat("a", 64),
				WaitingCapacity:  1,
				OperationTimeout: time.Second,
				CleanupTimeout:   time.Second,
				ShutdownTimeout:  time.Second,
				HealthTimeout:    10 * time.Millisecond,
				HealthInterval:   time.Millisecond,
			}, exitNormalizer{}, exitBackend{}, logger)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Shutdown(context.Background())

			uploads, err := httpapi.New(httpapi.Options{
				Model:            "base",
				WorkRoot:         filepath.Join(t.TempDir(), "uploads"),
				AudioBytes:       1024,
				PromptBytes:      32,
				WaitingCapacity:  1,
				OperationTimeout: time.Second,
			}, service)
			if err != nil {
				t.Fatal(err)
			}
			defer uploads.Close()

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if name == "closed-listener" {
				if err := listener.Close(); err != nil {
					t.Fatal(err)
				}

				err := serve(context.Background(), listener, http.NotFoundHandler(), service, uploads, time.Second, time.Second, logger)
				if !errors.Is(err, net.ErrClosed) {
					t.Fatal("HTTP listener failure was lost", err)
				}

				return
			}

			entered, canceled := make(chan struct{}), make(chan struct{})
			handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
				close(canceled)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			stopped := make(chan error, 1)
			go func() {
				stopped <- serve(ctx, listener, handler, service, uploads, 20*time.Millisecond, time.Second, logger)
			}()

			requestDone := make(chan struct{})
			go func() {
				client := &http.Client{Timeout: time.Second}
				response, err := client.Get("http://" + listener.Addr().String())
				if err == nil {
					_ = response.Body.Close()
				}
				close(requestDone)
			}()

			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("HTTP request did not enter before shutdown")
			}
			cancel()
			select {
			case err := <-stopped:
				if err != nil {
					t.Fatal("bounded HTTP drain was treated as failure", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP shutdown exceeded its drain and cleanup budgets")
			}

			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("expired HTTP drain retained an active request context")
			}

			select {
			case <-requestDone:
			case <-time.After(time.Second):
				t.Fatal("HTTP shutdown retained its client connection")
			}
		})
	}
}
