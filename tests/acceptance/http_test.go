package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newPublicClient(t *testing.T, endpoint string, limit int64) *PublicClient {
	t.Helper()
	client, err := NewPublicClient(endpoint, 2*time.Second, limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestPublicClientMultipartAndJSON(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/audio/transcriptions" {
			t.Error("request did not use the public transcription route")
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("request unexpectedly used authentication")
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error("request is not multipart")
			return
		}
		var models []string
		files := 0
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error("cannot read multipart request")
				return
			}
			content, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				t.Error("cannot read multipart part")
				return
			}
			switch part.FormName() {
			case "model":
				models = append(models, string(content))
			case "file":
				files++
				if string(content) != "audio fixture" || part.FileName() != "sample.wav" {
					t.Error("audio bytes or filename changed")
				}
			default:
				t.Error("unexpected form field")
			}
		}
		if files != 1 || len(models) != 2 || models[0] != "base" || models[1] != "other" {
			t.Error("workload cannot submit duplicate fields to test server-side rejection")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"text":"recognized speech"}`)
	}))
	defer server.Close()

	client := newPublicClient(t, server.URL, 1024)
	reply, err := client.Transcribe(t.Context(), []byte("audio fixture"), "sample.wav", []FormField{{"model", "base"}, {"model", "other"}})
	if err != nil {
		t.Fatal(err)
	}
	if received.Load() != 1 || reply.Status != http.StatusOK || reply.Text != "recognized speech" || reply.PublicDuration <= 0 {
		t.Fatal("public response or full request timing was not captured")
	}
}

func TestPublicClientResponseParsing(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body, text, code string
		status                              int
		invalid                             bool
	}{
		{name: "json", contentType: "application/json", body: `{"text":"speech"}`, text: "speech", status: http.StatusOK},
		{name: "silence", contentType: "application/json", body: `{"text":""}`, status: http.StatusOK},
		{name: "text", contentType: "text/plain; charset=utf-8", body: "speech\n", text: "speech\n", status: http.StatusOK},
		{name: "error", contentType: "application/json", body: `{"error":{"message":"PRIVATE_RESPONSE","type":"rate_limit_error","param":null,"code":"queue_full"}}`, code: "queue_full", status: http.StatusTooManyRequests},
		{name: "missing text", contentType: "application/json", body: `{}`, status: http.StatusOK, invalid: true},
		{name: "null text", contentType: "application/json", body: `{"text":null}`, status: http.StatusOK, invalid: true},
		{name: "wrong text type", contentType: "application/json", body: `{"text":42}`, status: http.StatusOK, invalid: true},
		{name: "extra success field", contentType: "application/json", body: `{"text":"PRIVATE_RESPONSE","segments":[]}`, status: http.StatusOK, invalid: true},
		{name: "trailing json", contentType: "application/json", body: `{"text":"PRIVATE_RESPONSE"}{}`, status: http.StatusOK, invalid: true},
		{name: "malformed", contentType: "application/json", body: `{"text":"PRIVATE_RESPONSE"`, status: http.StatusOK, invalid: true},
		{name: "unsupported media", contentType: "text/html", body: "PRIVATE_RESPONSE", status: http.StatusOK, invalid: true},
		{name: "missing media", body: `{"text":"PRIVATE_RESPONSE"}`, status: http.StatusOK, invalid: true},
		{name: "non-json error", contentType: "text/plain", body: "PRIVATE_RESPONSE", status: http.StatusServiceUnavailable, invalid: true},
		{name: "missing error", contentType: "application/json", body: `{}`, status: http.StatusBadRequest, invalid: true},
		{name: "missing error code", contentType: "application/json", body: `{"error":{"message":"PRIVATE_RESPONSE","type":"api_error","param":null}}`, status: http.StatusBadRequest, invalid: true},
		{name: "trailing error", contentType: "application/json", body: `{"error":{"code":"internal_error"}}{}`, status: http.StatusInternalServerError, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header()["Content-Type"] = []string{tc.contentType}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			reply, err := newPublicClient(t, server.URL, 1024).Transcribe(t.Context(), nil, "sample.wav", nil)
			if tc.invalid {
				if err == nil || strings.Contains(err.Error(), "PRIVATE_RESPONSE") || reply.Text != "" || reply.ErrorCode != "" {
					t.Fatal("invalid response accepted or raw response retained in diagnostics")
				}
				return
			}
			if err != nil || reply.Status != tc.status || reply.Text != tc.text || reply.ErrorCode != tc.code {
				t.Fatal("response does not match the returned public wire result")
			}
			encoded, err := json.Marshal(reply)
			if err != nil || strings.Contains(string(encoded), "speech") || strings.Contains(string(encoded), "PRIVATE_RESPONSE") {
				t.Fatal("serialized measurements retained transcript or error message")
			}
		})
	}
}

func TestPublicClientReplyBound(t *testing.T) {
	const limit = 32
	for name, size := range map[string]int{"exact": limit, "excess": limit + 1} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/plain")
				_, _ = io.WriteString(w, strings.Repeat("x", size))
			}))
			defer server.Close()
			reply, err := newPublicClient(t, server.URL, limit).Transcribe(t.Context(), nil, "sample.wav", nil)
			if size == limit && (err != nil || len(reply.Text) != limit) {
				t.Fatal("exact-limit response rejected")
			}
			if size > limit && (err == nil || reply.Text != "") {
				t.Fatal("oversized response accepted or retained")
			}
		})
	}
}

func TestPublicClientDoesNotRetryOrRedirect(t *testing.T) {
	var sourceCalls, destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"unexpected"}`)
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := newPublicClient(t, server.URL, 1024).Transcribe(t.Context(), []byte("private audio"), "sample.wav", nil)
	if err == nil || sourceCalls.Load() != 1 || destinationCalls.Load() != 0 {
		t.Fatal("submitted audio was redirected, retried, or redirect accepted as a transcript")
	}
}

func TestPublicClientCancellation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	client := newPublicClient(t, server.URL, 1024)
	go func() {
		_, err := client.Transcribe(ctx, nil, "sample.wav", nil)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("public request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("caller cancellation was lost")
		}
	case <-time.After(time.Second):
		t.Fatal("client outlived caller cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("cancelled HTTP request remained open")
	}
}

func TestPublicClientBodyTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := NewPublicClient(server.URL, 30*time.Millisecond, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	if _, err := client.Transcribe(ctx, nil, "sample.wav", nil); err == nil || time.Since(started) >= 500*time.Millisecond {
		t.Fatal("stalled response body outlived the configured timeout")
	}
}

func TestPublicClientSettingsAndTransportPrivacy(t *testing.T) {
	for _, endpoint := range []string{"", "file:///private/path", "http://user:PRIVATE_CREDENTIAL@example.com", "http://example.com/?token=PRIVATE_CREDENTIAL", "http://example.com/#PRIVATE_CREDENTIAL"} {
		_, err := NewPublicClient(endpoint, time.Second, 1024)
		if err == nil || strings.Contains(err.Error(), "PRIVATE_CREDENTIAL") || strings.Contains(err.Error(), "/private/path") {
			t.Fatal("invalid endpoint accepted or exposed")
		}
	}
	for _, settings := range []struct {
		timeout time.Duration
		limit   int64
	}{{0, 1024}, {-time.Second, 1024}, {time.Second, 0}, {time.Second, -1}} {
		if _, err := NewPublicClient("http://localhost", settings.timeout, settings.limit); err == nil {
			t.Fatal("unbounded client configuration accepted")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := server.URL
	server.Close()
	_, err := newPublicClient(t, endpoint, 1024).Transcribe(t.Context(), []byte("PRIVATE_AUDIO"), "PRIVATE_FILENAME", []FormField{{"prompt", "PRIVATE_PROMPT"}})
	if err == nil || strings.Contains(err.Error(), endpoint) || strings.Contains(err.Error(), "PRIVATE_") {
		t.Fatal("transport failure exposed endpoint or request content")
	}
}
