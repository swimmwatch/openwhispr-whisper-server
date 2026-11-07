package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func clientOptions(url string) Options {
	return Options{
		URL:              url,
		Fingerprint:      strings.Repeat("a", 64),
		Model:            "base",
		OperationTimeout: time.Second,
		MaxWAVBytes:      1 << 20,
		MaxReplyBytes:    1 << 20,
		PromptBytes:      8192,
	}
}

func healthReply(w http.ResponseWriter, fingerprint string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                    "ready",
		"protocol_version":          "1",
		"configuration_fingerprint": fingerprint,
		"process_generation":        "generation-1",
		"model":                     "base",
		"multilingual":              true,
		"cuda_ready":                true,
	})
}

type borrowedAudio struct {
	*bytes.Reader
	closed atomic.Bool
}

func (b *borrowedAudio) Close() error {
	b.closed.Store(true)

	return nil
}

func normalized() transcription.Normalized {
	return transcription.Normalized{Audio: &borrowedAudio{Reader: bytes.NewReader(make([]byte, 46))}, Size: 46, RequestID: "request-1"}
}

func failureKind(t *testing.T, err error, want domain.FailureKind) {
	t.Helper()
	var f *domain.Failure
	if !errors.As(err, &f) || f.Kind != want {
		t.Fatalf("failure=%v want=%s", err, want)
	}
}

func TestUpstreamContractIsolation(t *testing.T) {
	var calls atomic.Int32
	var expectedPrompt atomic.Value
	expectedPrompt.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			healthReply(w, strings.Repeat("a", 64))

			return
		}
		calls.Add(1)
		prompt := expectedPrompt.Load().(string)
		if r.URL.Path != "/inference" ||
			r.Method != http.MethodPost ||
			r.Header.Get("X-Whisper-Protocol") != "1" ||
			r.Header.Get("X-Whisper-Configuration") != strings.Repeat("a", 64) ||
			r.Header.Get("X-Whisper-Generation") != "generation-1" ||
			r.Header.Get("X-Request-ID") != "request-1" {
			t.Error("private compatibility/correlation contract not wired")
		}

		ms, err := strconv.Atoi(r.Header.Get("X-Whisper-Timeout-Ms"))
		if err != nil || ms <= 0 || ms > 1000 {
			t.Error("private deadline is not positive/capped")
		}
		if err := r.ParseMultipartForm(64 << 10); err != nil {
			t.Error(err)

			return
		}
		defer r.MultipartForm.RemoveAll()
		if len(r.MultipartForm.Value) != 4 ||
			r.FormValue("prompt") != prompt ||
			r.FormValue("language") != "auto" ||
			r.FormValue("temperature") != "0" ||
			r.FormValue("response_format") != "json" {
			t.Error("fields/defaults leaked between requests")
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)

			return
		}

		data, _ := io.ReadAll(file)
		_ = file.Close()
		if header.Filename != "audio.wav" || len(data) != 46 {
			t.Error("borrowed normalized audio changed")
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"text": prompt + " current result"})
	}))
	defer server.Close()

	c, err := New(clientOptions(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	for _, prompt := range []string{"PRIVATE prompt", ""} {
		expectedPrompt.Store(prompt)
		input := normalized()
		input.Fields.Prompt = prompt
		result, err := c.Infer(context.Background(), input)
		if err != nil || result.Text != prompt+" current result" {
			t.Fatal("inference result was not request-local", result, err)
		}
		if input.Audio.(*borrowedAudio).closed.Load() {
			t.Fatal("upstream closed caller-owned audio")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("inference was repeated or omitted")
	}
}

func TestSmallTemperatureFitsPrivateFieldLimit(t *testing.T) {
	var expected atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			healthReply(w, strings.Repeat("a", 64))
			return
		}
		form, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		found := false
		for {
			part, err := form.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if part.FormName() != "temperature" {
				continue
			}
			found = true
			// The supervisor permits at most 64 bytes for numeric fields.
			value, err := io.ReadAll(io.LimitReader(part, 65))
			if err != nil || len(value) > 64 {
				t.Error("valid temperature exceeded the private field bound")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			got, err := strconv.ParseFloat(string(value), 64)
			if err != nil || math.Float64bits(got) != expected.Load() {
				t.Error("temperature changed during private transport", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		if !found {
			t.Error("temperature was not sent")
		}
		_, _ = io.WriteString(w, `{"text":"current"}`)
	}))
	defer server.Close()
	client, err := New(clientOptions(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, temperature := range []float64{1e-100, math.SmallestNonzeroFloat64, 0, 1, 0.1} {
		expected.Store(math.Float64bits(temperature))
		input := normalized()
		input.Fields.Temperature = temperature
		result, err := client.Infer(context.Background(), input)
		if err != nil || result.Text != "current" {
			t.Errorf("valid temperature %g failed private inference: %v", temperature, err)
		}
	}
}

func TestPortFailureConversion(t *testing.T) {
	for _, test := range []struct {
		name, code, body string
		status           int
		kind             domain.FailureKind
	}{
		{"not-ready", "not_ready", "", http.StatusServiceUnavailable, domain.NotReady},
		{"gpu", "gpu_unavailable", "", http.StatusServiceUnavailable, domain.GPUUnavailable},
		{"timeout", "operation_timeout", "", http.StatusRequestTimeout, domain.DeadlineExceeded},
		{"internal", "internal_error", "", http.StatusInternalServerError, domain.Internal},
		{"malformed", "", "PRIVATE malformed native reply", http.StatusOK, domain.BackendUnavailable},
		{"missing-text", "", "{}", http.StatusOK, domain.BackendUnavailable},
		{"truncated", "", "{\"text\":", http.StatusOK, domain.BackendUnavailable},
		{"extra-result", "", "{\"text\":\"x\",\"PRIVATE\":true}", http.StatusOK, domain.BackendUnavailable},
		{"unknown", "PRIVATE", "", http.StatusServiceUnavailable, domain.BackendUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					healthReply(w, strings.Repeat("a", 64))

					return
				}
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(test.status)
				if test.code != "" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": map[string]any{
							"message": "PRIVATE cause",
							"code":    test.code,
							"type":    "PRIVATE type",
							"param":   "PRIVATE",
						},
					})
				} else {
					_, _ = io.WriteString(w, test.body)
				}
			}))
			defer server.Close()

			c, _ := New(clientOptions(server.URL))
			_, err := c.Infer(context.Background(), normalized())
			failureKind(t, err, test.kind)
			if strings.Contains(err.Error(), "PRIVATE") || calls.Load() != 1 {
				t.Fatal("port leaked private output or retried inference")
			}
		})
	}
}

func TestUpstreamHealthAndBounds(t *testing.T) {
	for _, mode := range []string{"mismatch", "oversize", "socket-loss", "deadline", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					fingerprint := strings.Repeat("a", 64)
					if mode == "mismatch" {
						fingerprint = strings.Repeat("b", 64)
					}
					healthReply(w, fingerprint)

					return
				}
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				switch mode {
				case "oversize":
					_, _ = io.WriteString(w, `{"text":"`+strings.Repeat("x", 4096)+`"}`)
				case "socket-loss":
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
				case "deadline":
					<-r.Context().Done()
				case "redirect":
					http.Redirect(w, r, "/inference", http.StatusTemporaryRedirect)
				}
			}))
			defer server.Close()

			opts := clientOptions(server.URL)
			if mode == "oversize" {
				opts.MaxReplyBytes = 1024
			}

			opts.OperationTimeout = 200 * time.Millisecond
			c, _ := New(opts)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			_, err := c.Infer(ctx, normalized())
			want := domain.BackendUnavailable
			if mode == "mismatch" {
				want = domain.NotReady
			}
			if mode == "deadline" {
				want = domain.DeadlineExceeded
			}
			failureKind(t, err, want)
			if calls.Load() > 1 || mode == "mismatch" && calls.Load() != 0 {
				t.Fatal("mismatch dispatched or failure retried")
			}
		})
	}
}
