package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

func settings(t *testing.T) config.API {
	t.Helper()
	c, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal(err)
	}

	return c.API
}

func fixture(t *testing.T, api config.API, store middleware.RateLimiterStore, out io.Writer, level string) *echo.Echo {
	t.Helper()
	logger, err := logging.New(out, level, "api")
	if err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	if store == nil {
		store = NewMemoryStore(api.RateLimit)
	}
	if err := Install(e, logger, api, store); err != nil {
		t.Fatal(err)
	}

	return e
}

func send(e *echo.Echo, peer, path string, body io.Reader, ids ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, body)
	r.RemoteAddr = peer
	for _, id := range ids {
		r.Header.Add("X-Request-ID", id)
	}

	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)

	return w
}

func TestHTTPFailureMapping(t *testing.T) {
	cases := []struct {
		kind     domain.FailureKind
		code     string
		status   int
		category string
	}{
		{domain.InvalidInput, "invalid_request", http.StatusBadRequest, "invalid_request_error"},
		{domain.UnsupportedAudio, "unsupported_audio", http.StatusBadRequest, "invalid_request_error"},
		{domain.AudioTooLong, "audio_duration_limit", http.StatusBadRequest, "invalid_request_error"},
		{domain.UnknownModel, "model_not_found", http.StatusNotFound, "invalid_request_error"},
		{domain.QueueFull, "queue_full", http.StatusTooManyRequests, "rate_limit_error"},
		{domain.DeadlineExceeded, "operation_timeout", http.StatusRequestTimeout, "timeout_error"},
		{domain.NotReady, "not_ready", http.StatusServiceUnavailable, "service_unavailable_error"},
		{domain.BackendUnavailable, "backend_unavailable", http.StatusServiceUnavailable, "service_unavailable_error"},
		{domain.GPUUnavailable, "gpu_unavailable", http.StatusServiceUnavailable, "service_unavailable_error"},
		{domain.ProvisioningFailed, "provisioning_failed", http.StatusServiceUnavailable, "service_unavailable_error"},
		{domain.ShuttingDown, "shutting_down", http.StatusServiceUnavailable, "service_unavailable_error"},
		{domain.Internal, "internal_error", http.StatusInternalServerError, "api_error"},
	}
	for _, tt := range cases {
		t.Run(tt.code, func(t *testing.T) {
			err := fmt.Errorf("PRIVATE: %w", &domain.Failure{Kind: tt.kind, Field: "model", Cause: errors.New("PRIVATE")})
			mapped := MapFailure(err)
			if mapped.Status != tt.status ||
				mapped.Code != tt.code ||
				mapped.Type != tt.category ||
				mapped.Param == nil ||
				*mapped.Param != "model" {
				t.Fatalf("incorrect mapping: %+v", mapped)
			}

			b, _ := json.Marshal(mapped.Envelope())
			if strings.Contains(string(b), "PRIVATE") {
				t.Fatal("private cause serialized")
			}
			assertErrorSchema(t, b)
		})
	}

	for _, err := range []error{
		errors.New("PRIVATE"),
		&domain.Failure{Kind: "PRIVATE", Field: "PRIVATE"},
		&domain.Failure{Kind: domain.Internal},
	} {
		mapped := MapFailure(err)
		if mapped.Status != http.StatusInternalServerError || mapped.Code != "internal_error" || mapped.Param != nil {
			t.Fatalf("unsafe fallback: %+v", mapped)
		}
	}

	for code, expected := range map[string]struct {
		status   int
		category string
	}{
		"route_not_found":        {http.StatusNotFound, "invalid_request_error"},
		"payload_too_large":      {http.StatusRequestEntityTooLarge, "invalid_request_error"},
		"rate_limit_exceeded":    {http.StatusTooManyRequests, "rate_limit_error"},
		"rate_limit_unavailable": {http.StatusServiceUnavailable, "service_unavailable_error"},
	} {
		mapped := NewHTTPFailure(code, "file")
		if mapped.Status != expected.status || mapped.Code != code || mapped.Type != expected.category {
			t.Fatalf("transport mapping: %+v", mapped)
		}

		b, _ := json.Marshal(mapped.Envelope())
		assertErrorSchema(t, b)
	}

	private := "PRIVATE"
	for _, err := range []error{
		&domain.Failure{Kind: domain.InvalidInput, Field: private},
		&HTTPFailure{Code: "invalid_request", Status: http.StatusOK, Message: private, Type: private, Param: &private},
	} {
		mapped := MapFailure(err)
		b, _ := json.Marshal(mapped.Envelope())
		if mapped.Status != http.StatusBadRequest || mapped.Param != nil || strings.Contains(string(b), private) {
			t.Fatal("untrusted error presentation accepted")
		}
	}
}

func assertErrorSchema(t *testing.T, data []byte) {
	t.Helper()
	var envelope map[string]map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 1 || len(envelope["error"]) != 4 {
		t.Fatal("incorrect error envelope fields")
	}

	for _, key := range []string{"message", "type", "param", "code"} {
		value, ok := envelope["error"][key]
		if !ok {
			t.Fatal("missing error envelope field")
		}
		if key != "param" && (value == "" || value == nil) {
			t.Fatal("empty public error field")
		}
	}
}

func TestRequestIDMiddlewareAndRecoverPrivacy(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		for _, outcome := range []string{"success", "failure", "unknown-error", "panic", "typed-panic", "not-found", "probe"} {
			t.Run(level+"/"+outcome, func(t *testing.T) {
				var out bytes.Buffer
				api := settings(t)
				api.RateLimit.Enabled = false
				e := fixture(t, api, nil, &out, level)
				e.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
					if logging.RequestID(c.Request().Context()) != "accepted.id" || len(c.Request().Header.Values("X-Request-ID")) != 1 {
						t.Fatal("request ID not normalized into owned context")
					}

					switch outcome {
					case "failure":
						return &domain.Failure{Kind: domain.InvalidInput, Cause: errors.New("PRIVATE_ERROR")}
					case "unknown-error":
						return errors.New("PRIVATE_UNKNOWN_ERROR")
					case "panic":
						panic("PRIVATE_PANIC")
					case "typed-panic":
						panic(&domain.Failure{Kind: domain.UnknownModel, Cause: errors.New("PRIVATE_PANIC")})
					}

					return c.String(http.StatusOK, "PRIVATE_TRANSCRIPT")
				})
				e.GET("/health/ready", func(c *echo.Context) error {
					return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "not_ready"})
				})
				path := "/v1/audio/transcriptions?prompt=PRIVATE_QUERY"
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("PRIVATE_AUDIO"))
				r.RemoteAddr = "192.0.2.1:10"
				r.Header.Set("X-Request-ID", "accepted.id")
				r.Header.Set("Authorization", "PRIVATE_HEADER")
				expected := http.StatusOK
				switch outcome {
				case "failure":
					expected = http.StatusBadRequest
				case "unknown-error", "panic", "typed-panic":
					expected = http.StatusInternalServerError
				case "not-found":
					expected = http.StatusNotFound
					r.URL.Path = "/PRIVATE_PATH"
				case "probe":
					expected = http.StatusServiceUnavailable
					r.Method = "GET"
					r.URL.Path = "/health/ready"
				}

				w := httptest.NewRecorder()
				e.ServeHTTP(w, r)
				if w.Code != expected || w.Header().Get("X-Request-ID") != "accepted.id" {
					t.Fatalf("status/header: %d %v", w.Code, w.Header())
				}
				if outcome != "success" && strings.Contains(w.Body.String(), "PRIVATE") {
					t.Fatal("private error response")
				}
				if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), "192.0.2.1") {
					t.Fatal("private request log")
				}

				lines := strings.Split(strings.TrimSpace(out.String()), "\n")
				if (expected >= http.StatusInternalServerError || level != "error") && len(lines) != 1 {
					t.Fatal("completion not logged once")
				}
				if out.Len() > 0 {
					var event map[string]any
					if err := json.Unmarshal(out.Bytes(), &event); err != nil {
						t.Fatal(err)
					}
					if event["status"] != float64(expected) || event["request_id"] != "accepted.id" {
						t.Fatalf("wrong final event: %v", event)
					}
				}
			})
		}
	}

	for _, ids := range [][]string{nil, {"one", "two"}, {"bad\nID"}, {strings.Repeat("x", 129)}, {""}, {"not,one"}, {"é"}} {
		api := settings(t)
		api.RateLimit.Enabled = false
		e := fixture(t, api, nil, io.Discard, "info")
		e.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
			id := c.Response().Header().Get("X-Request-ID")
			if !logging.ValidRequestID(id) ||
				logging.RequestID(c.Request().Context()) != id ||
				c.Request().Header.Get("X-Request-ID") != id {
				t.Fatal("unsafe or inconsistent generated ID")
			}

			return c.NoContent(http.StatusNoContent)
		})
		w := send(e, "192.0.2.1:10", "/v1/audio/transcriptions", nil, ids...)
		if !logging.ValidRequestID(w.Header().Get("X-Request-ID")) {
			t.Fatal("invalid response ID")
		}
	}
}

func TestConcurrentCorrelation(t *testing.T) {
	api := settings(t)
	api.RateLimit.Enabled = false
	var out bytes.Buffer
	e := fixture(t, api, nil, &out, "info")
	e.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
		return c.String(http.StatusOK, logging.RequestID(c.Request().Context()))
	})
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			id := fmt.Sprintf("request-%d", i)
			w := send(e, "192.0.2.1:10", "/v1/audio/transcriptions", nil, id)
			if w.Body.String() != id || w.Header().Get("X-Request-ID") != id {
				t.Error("cross-request correlation")
			}
		}(i)
	}
	wg.Wait()
	if len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 40 {
		t.Fatal("missing concurrent completion logs")
	}
}

type fakeStore struct {
	allow bool
	err   error
	keys  []string
}

func (s *fakeStore) Allow(id string) (bool, error) {
	s.keys = append(s.keys, id)

	return s.allow, s.err
}

type contextStore struct {
	fakeStore
	called bool
}

func (s *contextStore) AllowContext(c *echo.Context, id string) (bool, error) {
	s.called = true
	c.Response().Header().Set("Retry-After", "3")

	return s.fakeStore.Allow(id)
}

type observedBody struct{ reads int }

func (b *observedBody) Read(p []byte) (int, error) {
	b.reads++

	return 0, io.EOF
}

func TestRateStoreSubstitutionAndFailure(t *testing.T) {
	for _, tt := range []struct {
		allow  bool
		err    error
		status int
		code   string
	}{
		{true, nil, http.StatusOK, ""},
		{false, nil, http.StatusTooManyRequests, "rate_limit_exceeded"},
		{false, errors.New("PRIVATE"), http.StatusServiceUnavailable, "rate_limit_unavailable"},
		{true, errors.New("PRIVATE"), http.StatusServiceUnavailable, "rate_limit_unavailable"},
	} {
		s := &contextStore{fakeStore: fakeStore{allow: tt.allow, err: tt.err}}
		api := settings(t)
		e := fixture(t, api, s, io.Discard, "info")
		calls := 0
		e.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
			calls++
			_, _ = io.ReadAll(c.Request().Body)

			return c.NoContent(http.StatusOK)
		})
		body := &observedBody{}
		w := send(e, "[::ffff:192.0.2.1]:10", "/v1/audio/transcriptions", body)
		if w.Code != tt.status || !s.called || len(s.keys) != 1 || s.keys[0] != "192.0.2.1" {
			t.Fatalf("store substitution: %d %v", w.Code, s)
		}
		if tt.status != http.StatusOK {
			if calls != 0 || body.reads != 0 {
				t.Fatal("denied admission performed work")
			}
			if !strings.Contains(w.Body.String(), tt.code) || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal("unsafe denial")
			}
		}
		if tt.status == http.StatusTooManyRequests && w.Header().Get("Retry-After") != "3" {
			t.Fatal("context retry header lost")
		}
		if tt.status == http.StatusServiceUnavailable && w.Header().Get("Retry-After") != "" {
			t.Fatal("storage error advertises quota delay")
		}
	}
}

func TestRateLimitAdmissionRefillProbeBypassAndStoreIsolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := settings(t)
		api.RateLimit.RequestsPerSecond = 1
		api.RateLimit.Burst = 1
		api.RateLimit.IdleSeconds = 1
		store := NewMemoryStore(api.RateLimit)
		e := fixture(t, api, store, io.Discard, "info")
		e.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
			return c.NoContent(http.StatusOK)
		})
		e.GET("/health/live", func(c *echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{"status": "live"})
		})
		if send(e, "192.0.2.1:10", "/v1/audio/transcriptions", nil).Code != http.StatusOK {
			t.Fatal("initial admission")
		}

		body := &observedBody{}
		w := send(e, "192.0.2.1:10", "/v1/audio/transcriptions", body)
		retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
		if w.Code != http.StatusTooManyRequests || err != nil || retry < 1 || body.reads != 0 {
			t.Fatal("quota denial contract")
		}
		if send(e, "192.0.2.2:10", "/v1/audio/transcriptions", nil).Code != http.StatusOK {
			t.Fatal("independent IP exhausted")
		}

		r := httptest.NewRequest("GET", "/health/live", nil)
		r.RemoteAddr = "192.0.2.1:10"
		probe := httptest.NewRecorder()
		e.ServeHTTP(probe, r)
		if probe.Code != http.StatusOK || strings.TrimSpace(probe.Body.String()) != "{\"status\":\"live\"}" {
			t.Fatal("probe did not bypass")
		}
		time.Sleep(time.Second)
		if send(e, "192.0.2.1:10", "/v1/audio/transcriptions", nil).Code != http.StatusOK {
			t.Fatal("quota did not refill")
		}

		for i := 0; i < 2; i++ {
			fresh := NewMemoryStore(api.RateLimit)
			allowed, err := fresh.Allow("reset")
			if err != nil || !allowed {
				t.Fatal("independent/restarted store retained quota")
			}
		}
	})
}

func TestTrustedClientIP(t *testing.T) {
	for _, tt := range []struct {
		source, peer, xff, expected string
		trusted                     []string
	}{
		{"direct", "192.0.2.1:10", "198.51.100.1", "192.0.2.1", nil},
		{"x-forwarded-for", "10.0.0.1:10", "198.51.100.1, 10.0.0.2", "198.51.100.1", []string{"10.0.0.0/24"}},
		{"x-forwarded-for", "10.0.0.1:10", "198.51.100.1, 192.168.1.1", "192.168.1.1", []string{"10.0.0.0/24"}},
		{"x-forwarded-for", "192.0.2.1:10", "198.51.100.1", "192.0.2.1", []string{"10.0.0.0/24"}},
		{"x-forwarded-for", "10.0.0.1:10", "198.51.100.1, invalid", "10.0.0.1", []string{"10.0.0.0/24"}},
		{
			"x-forwarded-for",
			"[2001:db8::1]:10",
			"[::ffff:198.51.100.1], 2001:db8::2",
			"198.51.100.1",
			[]string{"2001:db8::/64"},
		},
		{"x-forwarded-for", "127.0.0.1:10", "198.51.100.1", "127.0.0.1", []string{"10.0.0.0/24"}},
	} {
		api := settings(t)
		api.ClientIP.Source = tt.source
		api.ClientIP.TrustedProxyCIDRs = tt.trusted
		s := &fakeStore{allow: true}
		e := fixture(t, api, s, io.Discard, "info")
		e.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
			return c.NoContent(http.StatusOK)
		})
		r := httptest.NewRequest("POST", "/v1/audio/transcriptions", nil)
		r.RemoteAddr = tt.peer
		r.Header.Set("X-Forwarded-For", tt.xff)
		r.Header.Set("X-Real-IP", "203.0.113.1")
		r.Header.Set("Forwarded", "for=203.0.113.2")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if len(s.keys) != 1 || s.keys[0] != tt.expected {
			t.Fatalf("unsafe IP selection for %+v: %v", tt, s.keys)
		}
	}
}

func TestHTTPFailureResponseOwnership(t *testing.T) {
	api := settings(t)
	api.RateLimit.Enabled = false
	e := fixture(t, api, nil, io.Discard, "info")
	e.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
		if err := c.String(http.StatusCreated, "owned"); err != nil {
			return err
		}

		return errors.New("PRIVATE")
	})
	w := send(e, "192.0.2.1:10", "/v1/audio/transcriptions", nil, "same-id")
	if w.Code != http.StatusCreated || w.Body.String() != "owned" || w.Header().Get("X-Request-ID") != "same-id" {
		t.Fatal("committed response overwritten")
	}

	e2 := fixture(t, api, nil, io.Discard, "info")
	e2.POST("/v1/audio/transcriptions", func(c *echo.Context) error {
		return errors.New("PRIVATE")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/v1/audio/transcriptions", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	e2.ServeHTTP(w, r)
	if w.Body.Len() != 0 {
		t.Fatal("response attempted after disconnect")
	}
}
