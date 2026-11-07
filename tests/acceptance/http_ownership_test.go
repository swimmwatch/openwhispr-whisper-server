package acceptance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicClientTimesConsumedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, "speech")
	}))
	defer server.Close()
	reply, err := newPublicClient(t, server.URL, 1024).Transcribe(t.Context(), nil, "sample.wav", nil)
	if err != nil || reply.PublicDuration < 20*time.Millisecond {
		t.Fatal("public duration excluded response-body consumption")
	}
}

type replyBody struct {
	io.Reader
	closed bool
	fail   bool
}

func (b *replyBody) Read(data []byte) (int, error) {
	if b.fail {
		return 0, errors.New("PRIVATE_READ_FAILURE")
	}
	return b.Reader.Read(data)
}

func (b *replyBody) Close() error {
	b.closed = true
	return nil
}

type publicTransport func(*http.Request) (*http.Response, error)

func (f publicTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicClientClosesReplyOnEveryOutcome(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		fail bool
	}{
		"success":  {body: `{"text":"speech"}`},
		"invalid":  {body: `{"text":null}`},
		"overflow": {body: strings.Repeat("x", 65)},
		"read":     {fail: true},
	} {
		t.Run(name, func(t *testing.T) {
			body := &replyBody{Reader: strings.NewReader(tc.body), fail: tc.fail}
			client := newPublicClient(t, "http://example.invalid", 64)
			client.client.Transport = publicTransport(func(r *http.Request) (*http.Response, error) {
				_ = r.Body.Close()
				return &http.Response{StatusCode: http.StatusOK, Body: body, Header: http.Header{"Content-Type": {"application/json"}}}, nil
			})
			_, err := client.Transcribe(context.Background(), nil, "sample.wav", nil)
			if !body.closed {
				t.Fatal("HTTP reply body remained owned after the request returned")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_READ_FAILURE") {
				t.Fatal("raw response read failure escaped into diagnostics")
			}
		})
	}
}
