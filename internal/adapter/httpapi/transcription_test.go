package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

type transcriptionStub struct {
	calls  atomic.Int32
	ready  atomic.Int32
	health transcription.Health
	err    error
	admit  func(context.Context, transcription.Input) (transcription.Result, error)
}

func (s *transcriptionStub) Admit(ctx context.Context, input transcription.Input) (transcription.Result, error) {
	s.calls.Add(1)
	if s.admit != nil {
		return s.admit(ctx, input)
	}

	return transcription.Result{Text: "fixture transcript"}, s.err
}

func (s *transcriptionStub) Readiness(context.Context) (transcription.Health, error) {
	s.ready.Add(1)

	return s.health, nil
}

func transcriptionFixture(t *testing.T, stub *transcriptionStub, adjust func(*Options)) (*echo.Echo, Options, *bytes.Buffer) {
	t.Helper()
	opts := Options{
		Model:            "base",
		WorkRoot:         filepath.Join(t.TempDir(), "uploads"),
		AudioBytes:       1024,
		PromptBytes:      32,
		WaitingCapacity:  1,
		OperationTimeout: time.Second,
	}
	if adjust != nil {
		adjust(&opts)
	}

	stub.health = transcription.Health{Status: domain.BackendReady, Model: "base", Multilingual: true, CUDAReady: true}
	logs := &bytes.Buffer{}
	api := settings(t)
	api.RateLimit.Enabled = false
	e := fixture(t, api, nil, logs, "debug")
	h, err := New(opts, stub)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	h.Register(e)

	return e, opts, logs
}

func formRequest(t *testing.T, fields [][2]string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if data != nil {
		file, err := w.CreateFormFile("file", "../../PRIVATE-name.bin")
		if err != nil {
			t.Fatal(err)
		}

		_, _ = file.Write(data)
	}

	for _, field := range fields {
		if err := w.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("X-Request-ID", "owned-request-1")
	r.RemoteAddr = "192.0.2.1:5000"

	return r
}

func TestTranscriptionFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields [][2]string
		code   int
	}{
		{"defaults", [][2]string{{"model", "base"}}, http.StatusOK},
		{
			"names",
			[][2]string{
				{"model", "base"},
				{"language", "russian"},
				{"prompt", "PRIVATE prompt"},
				{"temperature", "0.5"},
				{"stream", "false"},
				{"response_format", "text"},
			},
			http.StatusOK,
		},
		{"missing-model", nil, http.StatusBadRequest},
		{"other-model", [][2]string{{"model", "other"}}, http.StatusNotFound},
		{"path-model", [][2]string{{"model", "../PRIVATE"}}, http.StatusBadRequest},
		{"duplicate", [][2]string{{"model", "base"}, {"model", "base"}}, http.StatusBadRequest},
		{"unknown", [][2]string{{"model", "base"}, {"PRIVATE", "x"}}, http.StatusBadRequest},
		{"language", [][2]string{{"model", "base"}, {"language", "PRIVATE"}}, http.StatusBadRequest},
		{"empty-language", [][2]string{{"model", "base"}, {"language", ""}}, http.StatusBadRequest},
		{"temperature", [][2]string{{"model", "base"}, {"temperature", "NaN"}}, http.StatusBadRequest},
		{"temperature-range", [][2]string{{"model", "base"}, {"temperature", "1.01"}}, http.StatusBadRequest},
		{"utf8", [][2]string{{"model", "base"}, {"prompt", string([]byte{255})}}, http.StatusBadRequest},
		{"prompt-cap", [][2]string{{"model", "base"}, {"prompt", strings.Repeat("x", 33)}}, http.StatusBadRequest},
		{"stream", [][2]string{{"model", "base"}, {"stream", "true"}}, http.StatusBadRequest},
		{"stream-number", [][2]string{{"model", "base"}, {"stream", "0"}}, http.StatusBadRequest},
		{"format", [][2]string{{"model", "base"}, {"response_format", "vtt"}}, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &transcriptionStub{}
			e, opts, logs := transcriptionFixture(t, s, nil)
			s.admit = func(ctx context.Context, input transcription.Input) (transcription.Result, error) {
				data, err := os.ReadFile(input.UploadPath)
				if err != nil ||
					string(data) != "audio" ||
					input.Size != 5 ||
					input.Model != "base" ||
					input.RequestID != "owned-request-1" ||
					logging.RequestID(ctx) != input.RequestID {
					t.Error("owned upload/ID was not preserved")
				}
				if test.name == "defaults" &&
					(input.Fields.Language != "auto" ||
						input.Fields.Prompt != "" ||
						input.Fields.Temperature != 0) {
					t.Error("incorrect per-request defaults")
				}
				if test.name == "names" && (input.Fields.Language != "ru" || input.Fields.Temperature != 0.5) {
					t.Error("native language name was not normalized")
				}

				return transcription.Result{Text: "fixture transcript"}, nil
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, formRequest(t, test.fields, []byte("audio")))
			if w.Code != test.code {
				t.Fatalf("status=%d want=%d body=%s", w.Code, test.code, w.Body.String())
			}
			if w.Header().Get("X-Request-ID") != "owned-request-1" {
				t.Error("response lost ID")
			}
			if test.code != http.StatusOK && (s.calls.Load() != 0 || strings.Contains(w.Body.String(), "PRIVATE")) {
				t.Error("invalid input admitted or private data exposed")
			}
			if test.code == http.StatusOK && s.calls.Load() != 1 {
				t.Error("valid input was not admitted once")
			}
			if test.name == "names" &&
				(w.Body.String() != "fixture transcript" ||
					w.Header().Get("Content-Type") != "text/plain; charset=utf-8") {
				t.Error("text response was not adapted")
			}

			files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "*"))
			for _, file := range files {
				if filepath.Base(file) != ".owner" {
					t.Error("request scratch retained", filepath.Base(file))
				}
			}
			if strings.Contains(logs.String(), "PRIVATE") || strings.Contains(logs.String(), "fixture transcript") {
				t.Error("content logged")
			}
		})
	}
}

func TestTranscriptionMiddlewareWiring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &transcriptionStub{health: transcription.Health{Status: domain.BackendReady, Model: "base", Multilingual: true, CUDAReady: true}}
		api := settings(t)
		api.RateLimit.Burst = 1
		api.RateLimit.RequestsPerSecond = 1
		e := fixture(t, api, nil, io.Discard, "info")
		h, err := New(Options{
			Model:            "base",
			WorkRoot:         filepath.Join(t.TempDir(), "uploads"),
			AudioBytes:       1024,
			PromptBytes:      32,
			WaitingCapacity:  1,
			OperationTimeout: time.Second,
		}, s)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()
		h.Register(e)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
		if w.Code != http.StatusOK {
			t.Fatal("valid rate-limited route failed", w.Code)
		}

		before := s.ready.Load()
		body := &countedBody{}
		request := formRequest(t, [][2]string{{"model", "base"}}, nil)
		request.Body = body
		request.Header.Set("X-Forwarded-For", "198.51.100.1")
		request.Header.Set("X-Request-ID", "invalid id")
		w = httptest.NewRecorder()
		e.ServeHTTP(w, request)
		if w.Code != http.StatusTooManyRequests ||
			body.reads.Load() != 0 ||
			s.calls.Load() != 1 ||
			s.ready.Load() != before ||
			w.Header().Get("Retry-After") == "" ||
			!logging.ValidRequestID(w.Header().Get("X-Request-ID")) {
			t.Fatal("denied real route performed work or lost correlation")
		}

		for _, path := range []string{"/health/live", "/health/ready"} {
			w = httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK || w.Header().Get("X-Request-ID") == "" {
				t.Fatal("probe was limited or lost correlation")
			}
		}
		time.Sleep(time.Second)
		w = httptest.NewRecorder()
		e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
		if w.Code != http.StatusOK || s.calls.Load() != 2 {
			t.Fatal("real route quota did not refill")
		}
	})
	for _, storeErr := range []error{nil, errors.New("PRIVATE store error")} {
		s := &transcriptionStub{}
		api := settings(t)
		store := &contextStore{fakeStore: fakeStore{allow: false, err: storeErr}}
		e := fixture(t, api, store, io.Discard, "info")
		h, err := New(Options{
			Model:            "base",
			WorkRoot:         filepath.Join(t.TempDir(), "uploads"),
			AudioBytes:       1024,
			PromptBytes:      32,
			WaitingCapacity:  0,
			OperationTimeout: time.Second,
		}, s)
		if err != nil {
			t.Fatal(err)
		}
		h.Register(e)
		body := &countedBody{}
		request := formRequest(t, nil, nil)
		request.Body = body
		w := httptest.NewRecorder()
		e.ServeHTTP(w, request)
		want := http.StatusTooManyRequests
		if storeErr != nil {
			want = http.StatusServiceUnavailable
		}
		if w.Code != want || body.reads.Load() != 0 || s.calls.Load() != 0 || s.ready.Load() != 0 {
			t.Error("limiter store failure bypassed real admission boundary")
		}
		if err := h.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHTTPUploadBoundsAndIsolation(t *testing.T) {
	s := &transcriptionStub{}
	e, opts, _ := transcriptionFixture(t, s, nil)
	for _, size := range []int{int(opts.AudioBytes), int(opts.AudioBytes) + 1} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, make([]byte, size)))
		want := http.StatusOK
		if size > int(opts.AudioBytes) {
			want = http.StatusRequestEntityTooLarge
		}
		if w.Code != want {
			t.Fatalf("payload cap status=%d want=%d", w.Code, want)
		}
	}

	s.health.Multilingual = false
	w := httptest.NewRecorder()
	e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}, {"language", "ru"}}, []byte("audio")))
	if w.Code != http.StatusBadRequest || s.calls.Load() != 1 {
		t.Fatal("English-only model admitted an incompatible language")
	}

	s.admit = func(_ context.Context, input transcription.Input) (transcription.Result, error) {
		if input.Fields.Language != "en" || input.Fields.Prompt != "" || input.Fields.Temperature != 0 {
			t.Error("English/default fields leaked")
		}

		return transcription.Result{}, nil
	}
	w = httptest.NewRecorder()
	e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
	if w.Code != http.StatusOK {
		t.Fatal("English-only auto default rejected")
	}

	s.err = errors.New("PRIVATE unknown cause")
	s.admit = nil
	w = httptest.NewRecorder()
	e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("unknown port failure exposed its cause")
	}
}

func TestHTTPFailureRouteConformance(t *testing.T) {
	for kind, code := range kindCodes {
		t.Run(string(kind), func(t *testing.T) {
			s := &transcriptionStub{err: fmt.Errorf("wrapped: %w", &domain.Failure{Kind: kind, Field: "prompt", Cause: errors.New("PRIVATE cause")})}
			e, _, logs := transcriptionFixture(t, s, nil)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
			var result ErrorEnvelope
			if json.Unmarshal(w.Body.Bytes(), &result) != nil ||
				w.Code != registry[code].status ||
				result.Error.Code != code ||
				result.Error.Param == nil ||
				*result.Error.Param != "prompt" ||
				strings.Contains(w.Body.String()+logs.String(), "PRIVATE") {
				t.Fatalf("unsafe/wrong mapped port failure: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPublicRouteAbsence(t *testing.T) {
	s := &transcriptionStub{}
	e, _, _ := transcriptionFixture(t, s, nil)
	for _, path := range []string{
		"/load",
		"/inference",
		"/v1/models/base/warmup",
		"/v1/models/base/unload",
		"/v1/models",
		"/v1/audio/translations",
		"/metrics",
		"/admin",
		"/auth",
		"/docs",
	} {
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != http.StatusNotFound || w.Header().Get("X-Request-ID") == "" {
				t.Fatalf("deferred route exposed: %s %s: %d", method, path, w.Code)
			}
		}
	}
	if s.calls.Load() != 0 {
		t.Fatal("absent route admitted work")
	}
}

type countedBody struct{ reads atomic.Int32 }

func (b *countedBody) Read([]byte) (int, error) {
	b.reads.Add(1)

	return 0, io.EOF
}

func (*countedBody) Close() error {
	return nil
}

func TestTranscriptionStagingBounds(t *testing.T) {
	s := &transcriptionStub{}
	e, _, _ := transcriptionFixture(t, s, func(o *Options) {
		o.WaitingCapacity = 0
	})
	entered, release := make(chan struct{}), make(chan struct{})
	s.admit = func(context.Context, transcription.Input) (transcription.Result, error) {
		close(entered)
		<-release

		return transcription.Result{}, nil
	}
	done := make(chan struct{})
	defer func() {
		close(release)
		<-done
	}()
	go func() {
		defer close(done)
		e.ServeHTTP(httptest.NewRecorder(), formRequest(t, [][2]string{{"model", "base"}}, []byte("audio")))
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first staging reservation was not admitted")
	}

	body := &countedBody{}
	r := formRequest(t, [][2]string{{"model", "base"}}, nil)
	r.Body = body
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests || body.reads.Load() != 0 || s.calls.Load() != 1 {
		t.Error("staging overflow read input or admitted work")
	}
}
