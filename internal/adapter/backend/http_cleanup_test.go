package backend

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateUnreadBodyCleanup(t *testing.T) {
	for _, scenario := range []string{"expired-upload", "stale-generation", "invalid-headers", "unknown-route"} {
		t.Run(scenario, func(t *testing.T) {
			opts, root, _ := supervisorOptions(t)
			s := runningSupervisor(t, opts)
			returned := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(returned)
				s.Handler().ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)

			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = conn.Close()
			})

			health, _ := s.Health(context.Background())
			path, generation, timeout := "/inference", health.ProcessGeneration, "100"
			wantStatus := http.StatusRequestTimeout
			switch scenario {
			case "stale-generation":
				generation, wantStatus = "stale", http.StatusServiceUnavailable
			case "invalid-headers":
				timeout, wantStatus = "invalid", http.StatusBadRequest
			case "unknown-route":
				path, wantStatus = "/load", http.StatusNotFound
			}

			_, err = fmt.Fprintf(
				conn,
				"POST %s HTTP/1.1\r\nHost: test\r\nContent-Type: multipart/form-data; boundary=stalled\r\nContent-Length: 10000\r\nX-Whisper-Protocol: %s\r\nX-Whisper-Configuration: %s\r\nX-Whisper-Generation: %s\r\nX-Whisper-Timeout-Ms: %s\r\n\r\n--stalled\r\nContent-Disposition: form-data; name=\"file\"; filename=\"audio.wav\"\r\nContent-Type: audio/wav\r\n\r\npartial",
				path,
				health.ProtocolVersion,
				health.ConfigurationFingerprint,
				generation,
				timeout,
			)
			if err != nil {
				t.Fatal(err)
			}

			select {
			case <-returned:
			case <-time.After(opts.OperationTimeout + opts.CleanupTimeout + time.Second):
				t.Fatal("private handler exceeded its body cleanup budget")
			}
			if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}

			response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
			aborted := scenario == "expired-upload" && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF))
			if err != nil && !aborted {
				t.Error("private error response was blocked by an unread body", err)
			} else if err == nil {
				_, readErr := io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if readErr != nil || response.StatusCode != wantStatus {
					t.Errorf("private error response: status=%d want=%d read=%v", response.StatusCode, wantStatus, readErr)
				}
			}

			// net/http may briefly half-close a rejected upload before closing TCP.
			stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := server.Config.Shutdown(stop); err != nil {
				t.Error("unread body kept the HTTP connection active after the handler returned", err)
			}

			files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "request-*.wav"))
			calls, _ := filepath.Glob(filepath.Join(root, "call-*"))
			after, _ := s.Health(context.Background())
			if len(files) != 0 || len(calls) != 0 || !after.CUDAReady || after.ProcessGeneration != health.ProcessGeneration {
				t.Error("rejected upload retained scratch, reached native inference or changed the resident child")
			}
		})
	}
}

func TestPrivateSuccessfulKeepAlive(t *testing.T) {
	opts, _, _ := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)

	transport := &http.Transport{MaxConnsPerHost: 1}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: opts.OperationTimeout}

	for i := range 2 {
		request := privateRequest(t, s, server.URL)
		reused := false
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
			reused = info.Reused
		}}))

		response, err := client.Do(request)
		if err != nil {
			t.Fatal("successful private request failed", err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("successful private response: status=%d read=%v", response.StatusCode, readErr)
		}

		if i == 1 && !reused {
			t.Fatal("completed private request prevented connection reuse")
		}

		if i == 0 {
			// The next request must outlive the previous request's read deadline.
			time.Sleep(600 * time.Millisecond)
		}
	}
}
