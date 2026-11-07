package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestUnreadRequestBodyCannotBlockPublicResponse(t *testing.T) {
	for _, scenario := range []struct {
		name, method, path string
		status             int
	}{
		{"rate-denied", http.MethodPost, transcriptionsPath, http.StatusTooManyRequests},
		{"unknown-route", http.MethodPost, "/not-public", http.StatusNotFound},
		{"liveness", http.MethodGet, "/health/live", http.StatusOK},
		{"readiness", http.MethodGet, "/health/ready", http.StatusOK},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			stub := &transcriptionStub{health: transcription.Health{Model: "base", CUDAReady: true}}
			e := fixture(t, settings(t), &fakeStore{allow: false}, io.Discard, "info")
			opts := handlerOptions(t)
			opts.OperationTimeout = 100 * time.Millisecond
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
			server := httptest.NewServer(e)
			defer server.Close()
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			// Deliberately send headers only. The handler must not wait for an
			// unauthenticated caller to finish an irrelevant or rejected body.
			if _, err := fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: test\r\nContent-Length: 128\r\n\r\n", scenario.method, scenario.path); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal("public response blocked on unread request body", err)
			}
			defer response.Body.Close()
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatal("public response was not completed", err)
			}
			if response.StatusCode != scenario.status || response.Header.Get("X-Request-ID") == "" {
				t.Fatalf("status=%d; missing response or request correlation", response.StatusCode)
			}
			if stub.calls.Load() != 0 {
				t.Fatal("rejected or probe request reached admission")
			}
			var extra [1]byte
			if _, err := conn.Read(extra[:]); err != io.EOF {
				t.Fatal("unread body retained its connection", err)
			}
		})
	}
}

func TestConsumedUploadKeepsConnectionAndSeparateResponseBudget(t *testing.T) {
	const responseBytes = 4 << 20
	const budget = time.Second
	stub := &transcriptionStub{health: transcription.Health{Status: domain.BackendReady, Model: "base", CUDAReady: true}}
	stub.admit = func(ctx context.Context, _ transcription.Input) (transcription.Result, error) {
		if stub.calls.Load() == 1 {
			// Upload/staging and admitted execution have distinct budgets. A
			// response-write timeout must not start before inference completes.
			timer := time.NewTimer(budget + 100*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return transcription.Result{}, ctx.Err()
			}
		}
		return transcription.Result{Text: strings.Repeat("x", responseBytes)}, nil
	}
	api := settings(t)
	api.RateLimit.Enabled = false
	e := fixture(t, api, nil, io.Discard, "info")
	opts := handlerOptions(t)
	opts.OperationTimeout = budget
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
	server := httptest.NewServer(e)
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	for range 2 {
		request := formRequest(t, [][2]string{{"model", "base"}, {"response_format", "text"}}, []byte("audio"))
		if err := request.Write(conn); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(reader, request)
		if err != nil {
			t.Fatal("normal request lost its keepalive or execution budget", err)
		}
		n, readErr := io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Close || n != responseBytes || readErr != nil {
			t.Fatalf("large response failed: status=%d closed=%v bytes=%d err=%v", response.StatusCode, response.Close, n, readErr)
		}
	}
	if stub.calls.Load() != 2 {
		t.Fatal("keepalive requests did not execute exactly once")
	}
}

func TestSlowResponseReaderReleasesStagingSlot(t *testing.T) {
	stub := &transcriptionStub{health: transcription.Health{Status: domain.BackendReady, Model: "base", CUDAReady: true}}
	stub.admit = func(context.Context, transcription.Input) (transcription.Result, error) {
		if stub.calls.Load() == 1 {
			return transcription.Result{Text: strings.Repeat("x", 16<<20)}, nil
		}
		return transcription.Result{Text: "next"}, nil
	}
	api := settings(t)
	api.RateLimit.Enabled = false
	e := fixture(t, api, nil, io.Discard, "info")
	opts := handlerOptions(t)
	opts.WaitingCapacity = 0
	opts.OperationTimeout = 100 * time.Millisecond
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
	finished := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { finished <- struct{}{} }()
		e.ServeHTTP(w, r)
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.(*net.TCPConn).SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	request := formRequest(t, [][2]string{{"model", "base"}}, []byte("audio"))
	if err := request.Write(conn); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("a client not reading its response retained a staging slot")
	}
	form := formRequest(t, [][2]string{{"model", "base"}}, []byte("audio"))
	request, err = http.NewRequest(http.MethodPost, server.URL+transcriptionsPath, form.Body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = form.Header.Clone()
	client := server.Client()
	client.Timeout = time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || stub.calls.Load() != 2 {
		t.Fatal("slow response retained admission capacity", response.StatusCode)
	}
	<-finished
}
