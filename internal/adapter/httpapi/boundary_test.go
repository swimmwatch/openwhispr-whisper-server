package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

func handlerOptions(t *testing.T) Options {
	t.Helper()

	return Options{
		Model:            "base",
		WorkRoot:         filepath.Join(t.TempDir(), "uploads"),
		AudioBytes:       1024,
		PromptBytes:      32,
		WaitingCapacity:  1,
		OperationTimeout: time.Second,
	}
}

type readinessService struct {
	*transcriptionStub
	err error
}

func (s readinessService) Readiness(ctx context.Context) (transcription.Health, error) {
	health, _ := s.transcriptionStub.Readiness(ctx)

	return health, s.err
}

func TestReadinessReasonsAndAdmissionDenial(t *testing.T) {
	for _, test := range []struct {
		name   string
		state  domain.BackendState
		kind   domain.FailureKind
		reason string
	}{
		{"starting", domain.BackendStarting, "", "starting"},
		{"recovering", domain.BackendRecovering, "", "recovering"},
		{"stopping", domain.BackendStopping, "", "shutting_down"},
		{"failed", domain.BackendFailed, "", "backend_unavailable"},
		{"gpu", domain.BackendStarting, domain.GPUUnavailable, "gpu_unavailable"},
		{"backend", domain.BackendStarting, domain.BackendUnavailable, "backend_unavailable"},
		{"provisioning", domain.BackendStarting, domain.ProvisioningFailed, "provisioning_failed"},
		{"shutdown", domain.BackendStarting, domain.ShuttingDown, "shutting_down"},
		{"unknown", "", domain.Internal, "not_ready"},
		{"wrong-model", domain.BackendReady, "", "not_ready"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &transcriptionStub{health: transcription.Health{
				Status: test.state,
				Model:  "base",
			}}
			service := readinessService{transcriptionStub: stub}
			if test.kind != "" {
				service.err = &domain.Failure{
					Kind:  test.kind,
					Cause: errors.New("PRIVATE"),
				}
			}
			if test.name == "wrong-model" {
				stub.health.CUDAReady = true
				stub.health.Model = "small"
			}

			api := settings(t)
			api.RateLimit.Enabled = false
			e := fixture(t, api, nil, io.Discard, "info")
			h, err := New(handlerOptions(t), service)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := h.Close(); err != nil {
					t.Error(err)
				}
			})
			h.Register(e)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
			var health map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusServiceUnavailable || len(health) != 2 || health["status"] != "not_ready" || health["reason"] != test.reason {
				t.Fatalf("readiness=%d %s", w.Code, w.Body.String())
			}

			w = httptest.NewRecorder()
			e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
			if w.Code < http.StatusBadRequest || stub.calls.Load() != 0 || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal("unready service admitted work or exposed a cause")
			}
		})
	}
}

func TestHandlerRejectsInvalidOptions(t *testing.T) {
	for _, scenario := range []string{
		"service",
		"model",
		"relative-root",
		"audio",
		"prompt",
		"capacity",
		"overflow-capacity",
		"timeout",
		"overflow-body",
	} {
		t.Run(scenario, func(t *testing.T) {
			opts := handlerOptions(t)
			var service Transcriber = &transcriptionStub{}
			switch scenario {
			case "service":
				service = nil
			case "model":
				opts.Model = "../model"
			case "relative-root":
				opts.WorkRoot = "uploads"
			case "audio":
				opts.AudioBytes = 0
			case "prompt":
				opts.PromptBytes = 0
			case "capacity":
				opts.WaitingCapacity = -1
			case "overflow-capacity":
				opts.WaitingCapacity = math.MaxInt
			case "timeout":
				opts.OperationTimeout = 0
			case "overflow-body":
				opts.AudioBytes = math.MaxInt64
			}

			_, err := New(opts, service)
			var failure *domain.Failure
			if !errors.As(err, &failure) || failure.Kind != domain.InvalidInput {
				t.Fatalf("invalid constructor options accepted: %v", err)
			}
		})
	}
}

func TestUploadRootOwnershipAndClose(t *testing.T) {
	for _, scenario := range []string{"file", "symlink", "lock-symlink", "lock-directory", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			opts := handlerOptions(t)
			if scenario == "file" {
				if err := os.WriteFile(opts.WorkRoot, nil, 0600); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "symlink" {
				if err := os.Symlink(t.TempDir(), opts.WorkRoot); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(opts.WorkRoot, 0700); err != nil {
					t.Fatal(err)
				}

				lock := filepath.Join(opts.WorkRoot, ".owner.lock")
				if scenario == "lock-symlink" {
					target := filepath.Join(t.TempDir(), "private")
					if err := os.WriteFile(target, nil, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, lock); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "lock-directory" {
					if err := os.Mkdir(lock, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "locked" {
					first, err := New(opts, &transcriptionStub{})
					if err != nil {
						t.Fatal(err)
					}
					defer first.Close()
				}
			}

			_, err := New(opts, &transcriptionStub{})
			var failure *domain.Failure
			if !errors.As(err, &failure) || failure.Kind != domain.ProvisioningFailed {
				t.Fatalf("unsafe/shared upload root accepted: %v", err)
			}
		})
	}

	opts := handlerOptions(t)
	service := &transcriptionStub{}
	h, err := New(opts, service)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal("handler close was not idempotent", err)
	}

	api := settings(t)
	api.RateLimit.Enabled = false
	e := fixture(t, api, nil, io.Discard, "info")
	h.Register(e)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
	if w.Code != http.StatusServiceUnavailable ||
		!strings.Contains(w.Body.String(), "shutting_down") ||
		service.ready.Load() != 0 ||
		service.calls.Load() != 0 {
		t.Fatal("closed handler performed request work")
	}

	next, err := New(opts, service)
	if err != nil {
		t.Fatal("closed handler retained the upload-root lock", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedMultipartDeniedBeforeReadiness(t *testing.T) {
	for _, scenario := range []string{
		"media-type",
		"boundary",
		"declared-body-cap",
		"streamed-body-cap",
		"transfer-encoding",
		"duplicate-disposition",
		"header-cap",
		"bad-temperature",
		"missing-file",
		"empty-file",
	} {
		t.Run(scenario, func(t *testing.T) {
			stub := &transcriptionStub{}
			e, opts, _ := transcriptionFixture(t, stub, nil)
			request := formRequest(t, [][2]string{{"model", "base"}}, []byte("audio"))
			switch scenario {
			case "media-type":
				request.Header.Set("Content-Type", "application/json")
			case "boundary":
				request.Header.Set("Content-Type", "multipart/form-data; boundary="+strings.Repeat("a", 71))
			case "declared-body-cap":
				request.ContentLength = opts.AudioBytes + int64(opts.PromptBytes) + multipartFramingBytes + 1
			case "streamed-body-cap":
				request.Body = io.NopCloser(io.MultiReader(request.Body, strings.NewReader(strings.Repeat("x", multipartFramingBytes+1024))))
				request.ContentLength = -1
			case "transfer-encoding", "duplicate-disposition", "header-cap":
				data, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				header := "Content-Transfer-Encoding: base64\r\n"
				if scenario == "duplicate-disposition" {
					header = "Content-Disposition: form-data; name=\"file\"\r\n"
				}
				if scenario == "header-cap" {
					header = "X-Extra: " + strings.Repeat("x", multipartHeaderBytes) + "\r\n"
				}
				data = bytes.Replace(data, []byte("\r\n\r\n"), []byte("\r\n"+header+"\r\n"), 1)
				request.Body = io.NopCloser(bytes.NewReader(data))
				request.ContentLength = int64(len(data))
			case "bad-temperature":
				request = formRequest(t, [][2]string{{"model", "base"}, {"temperature", "private"}}, []byte("audio"))
			case "missing-file":
				request = formRequest(t, [][2]string{{"model", "base"}}, nil)
			case "empty-file":
				request = formRequest(t, [][2]string{{"model", "base"}}, []byte{})
			}

			w := httptest.NewRecorder()
			e.ServeHTTP(w, request)
			want := http.StatusBadRequest
			if strings.Contains(scenario, "body-cap") {
				want = http.StatusRequestEntityTooLarge
			}
			if w.Code != want || stub.ready.Load() != 0 || stub.calls.Load() != 0 {
				t.Fatalf("malformed input reached readiness/admission: status=%d body=%s", w.Code, w.Body.String())
			}

			files, err := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*"))
			if err != nil || len(files) != 0 {
				t.Fatal("malformed multipart retained scratch", err)
			}
		})
	}
}

func TestMiddlewareRejectsInvalidConfigurationAndPeer(t *testing.T) {
	logger, err := logging.New(io.Discard, "info", "api")
	if err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []string{"logger", "store", "rate", "ip"} {
		t.Run(scenario, func(t *testing.T) {
			api := settings(t)
			selectedLogger := logger
			var store middleware.RateLimiterStore = NewMemoryStore(api.RateLimit)
			switch scenario {
			case "logger":
				selectedLogger = nil
			case "store":
				store = nil
			case "rate":
				api.RateLimit.Burst = 0
			case "ip":
				api.ClientIP = config.ClientIP{
					Source:            "x-forwarded-for",
					TrustedProxyCIDRs: []string{"invalid"},
				}
			}
			if err := Install(echo.New(), selectedLogger, api, store); err == nil {
				t.Fatal("invalid middleware configuration accepted")
			}
		})
	}

	for _, peer := range []string{"invalid", "[fe80::1%eth0]:123"} {
		store := &fakeStore{allow: true}
		e := fixture(t, settings(t), store, io.Discard, "info")
		e.POST(transcriptionsPath, func(c *echo.Context) error {
			t.Error("invalid client identity reached the handler")

			return c.NoContent(http.StatusOK)
		})
		w := send(e, peer, transcriptionsPath, nil)
		if w.Code != http.StatusBadRequest || len(store.keys) != 0 {
			t.Fatal("invalid client identity reached the rate-limit store")
		}
	}
}

func TestHandlerCloseRequiresDrainedRequests(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	stub := &transcriptionStub{
		health: transcription.Health{
			Model:        "base",
			Multilingual: true,
			CUDAReady:    true,
		},
		admit: func(context.Context, transcription.Input) (transcription.Result, error) {
			close(entered)
			<-release

			return transcription.Result{}, nil
		},
	}
	api := settings(t)
	api.RateLimit.Enabled = false
	e := fixture(t, api, nil, io.Discard, "info")
	h, err := New(handlerOptions(t), stub)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Register(e)
	request := formRequest(t, [][2]string{{"model", "base"}}, []byte("audio"))
	go func() {
		defer close(finished)
		e.ServeHTTP(httptest.NewRecorder(), request)
	}()
	defer func() {
		close(release)
		<-finished
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not reach admission")
	}

	var failure *domain.Failure
	if err := h.Close(); !errors.As(err, &failure) || failure.Kind != domain.ShuttingDown {
		t.Fatal("handler released upload ownership before request drain", err)
	}
}

func TestUploadRecoveryPreservesUnownedSessions(t *testing.T) {
	opts := handlerOptions(t)
	if err := os.Mkdir(opts.WorkRoot, 0700); err != nil {
		t.Fatal(err)
	}

	for index, marker := range []string{"wrong", uploadMarker + "extra", "symlink", "directory"} {
		dir := filepath.Join(opts.WorkRoot, "run-"+strings.Repeat(string(rune('A'+index)), 26))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}

		path := filepath.Join(dir, ".owner")
		switch marker {
		case "symlink":
			target := filepath.Join(t.TempDir(), "marker")
			if err := os.WriteFile(target, []byte(uploadMarker), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		case "directory":
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		default:
			if err := os.WriteFile(path, []byte(marker), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}

	h, err := New(opts, &transcriptionStub{})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*"))
	if err != nil || len(files) != 4 {
		t.Fatal("startup recovery removed an unowned or forged session", err)
	}
}
