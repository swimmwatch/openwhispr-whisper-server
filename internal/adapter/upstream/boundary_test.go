package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type trackedReply struct {
	reader io.Reader
	closed bool
	read   int
}

func (r *trackedReply) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n

	return n, err
}

func (r *trackedReply) Close() error {
	r.closed = true

	return nil
}

func validHealth() map[string]any {
	return map[string]any{
		"status":                    "ready",
		"protocol_version":          "1",
		"configuration_fingerprint": strings.Repeat("a", 64),
		"process_generation":        "generation-1",
		"model":                     "base",
		"multilingual":              true,
		"cuda_ready":                true,
	}
}

func TestHealthRejectsInconsistentPrivateState(t *testing.T) {
	for _, test := range []struct {
		name   string
		field  string
		value  any
		status int
		kind   domain.FailureKind
	}{
		{"missing-field", "multilingual", nil, 200, domain.BackendUnavailable},
		{"unknown-field", "private", "PRIVATE", 200, domain.BackendUnavailable},
		{"unknown-state", "status", "PRIVATE", 200, domain.BackendUnavailable},
		{"protocol", "protocol_version", "2", 200, domain.NotReady},
		{"model", "model", "small", 200, domain.NotReady},
		{"ready-without-cuda", "cuda_ready", false, 200, domain.BackendUnavailable},
		{"ready-without-generation", "process_generation", "", 200, domain.BackendUnavailable},
		{"unsafe-generation", "process_generation", "PRIVATE generation", 200, domain.BackendUnavailable},
		{"ready-http-unavailable", "", nil, 503, domain.BackendUnavailable},
		{"starting-http-ok", "status", "starting", 200, domain.BackendUnavailable},
		{"unrecognized-http-status", "", nil, 502, domain.BackendUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := validHealth()
			if test.field != "" {
				wire[test.field] = test.value
			}
			if test.name == "missing-field" {
				delete(wire, test.field)
			}
			if test.name == "starting-http-ok" {
				wire["cuda_ready"] = false
			}

			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}

			body := &trackedReply{reader: bytes.NewReader(data)}
			opts := clientOptions("http://backend.test")
			opts.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.status,
					Body:       body,
				}, nil
			})}
			client, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.Close)
			_, err = client.Health(context.Background())
			failureKind(t, err, test.kind)
			if !body.closed || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("invalid health retained its response or exposed private data")
			}
		})
	}
}

func TestHealthAcceptsLifecycleStates(t *testing.T) {
	for _, state := range []string{"starting", "ready", "busy", "recovering", "stopping", "failed"} {
		t.Run(state, func(t *testing.T) {
			wire := validHealth()
			ready := state == "ready" || state == "busy"
			wire["status"] = state
			wire["cuda_ready"] = ready
			status := http.StatusServiceUnavailable
			if ready {
				status = http.StatusOK
			} else {
				wire["process_generation"] = ""
			}

			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}

			opts := clientOptions("http://backend.test")
			opts.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(bytes.NewReader(data)),
				}, nil
			})}
			client, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.Close)
			health, err := client.Health(context.Background())
			if err != nil || health.Status != domain.BackendState(state) || health.CUDAReady != ready {
				t.Fatalf("health=%+v err=%v", health, err)
			}
		})
	}
}

type failingReader struct {
	err error
}

func (r failingReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestResponseReadFailureClosesBody(t *testing.T) {
	for _, path := range []string{"/health", "/inference"} {
		for _, scenario := range []string{"read-error", "oversize", "invalid-utf8", "trailing-json", "cancel"} {
			t.Run(path+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				readErr := errors.New("PRIVATE socket failure")
				var reader io.Reader = failingReader{readErr}
				limit := int64(healthReplyBytes)
				if path == "/inference" {
					limit = 64
				}

				switch scenario {
				case "oversize":
					reader = strings.NewReader(strings.Repeat("x", int(limit)+100))
				case "invalid-utf8":
					reader = bytes.NewReader([]byte{255})
				case "trailing-json":
					reader = strings.NewReader(`{"text":"x"} {}`)
				case "cancel":
					reader = failingReader{context.Canceled}
				}

				body := &trackedReply{reader: reader}
				calls := 0
				opts := clientOptions("http://backend.test")
				opts.MaxReplyBytes = 64
				opts.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.URL.Path != path {
						data, _ := json.Marshal(validHealth())

						return &http.Response{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewReader(data)),
						}, nil
					}
					if scenario == "cancel" {
						cancel()
					}

					return &http.Response{
						StatusCode: 200,
						Body:       body,
					}, nil
				})}
				client, err := New(opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(client.Close)
				input := normalized()
				if path == "/health" {
					_, err = client.Health(ctx)
				} else {
					_, err = client.Infer(ctx, input)
				}

				want := domain.BackendUnavailable
				if scenario == "cancel" {
					want = domain.DeadlineExceeded
				}
				failureKind(t, err, want)
				if !body.closed || body.read > int(limit)+1 || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("response read was unbounded, leaked content or retained the body")
				}
				if input.Audio.(*borrowedAudio).closed.Load() || calls > 2 {
					t.Fatal("failed response closed borrowed audio or retried")
				}
			})
		}
	}
}

func TestClientRejectsInvalidOptionsAndInputs(t *testing.T) {
	for _, scenario := range []string{
		"credentials",
		"path",
		"query",
		"scheme",
		"fingerprint",
		"timeout",
		"wav-bound",
		"reply-bound",
		"prompt-bound",
	} {
		t.Run(scenario, func(t *testing.T) {
			opts := clientOptions("http://backend.test")
			switch scenario {
			case "credentials":
				opts.URL = "http://user:password@backend.test"
			case "path":
				opts.URL = "http://backend.test/inference"
			case "query":
				opts.URL = "http://backend.test?private=1"
			case "scheme":
				opts.URL = "file:///private"
			case "fingerprint":
				opts.Fingerprint = "invalid"
			case "timeout":
				opts.OperationTimeout = 0
			case "wav-bound":
				opts.MaxWAVBytes = math.MaxInt64
			case "reply-bound":
				opts.MaxReplyBytes = math.MaxInt64
			case "prompt-bound":
				opts.PromptBytes = 0
			}

			_, err := New(opts)
			failureKind(t, err, domain.InvalidInput)
		})
	}

	var calls int
	opts := clientOptions("http://backend.test")
	opts.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		return nil, errors.New("unexpected request")
	})}
	client, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	for _, scenario := range []string{"nil-audio", "short", "large", "prompt", "nan", "infinity", "negative", "temperature"} {
		t.Run(scenario, func(t *testing.T) {
			input := normalized()
			switch scenario {
			case "nil-audio":
				input.Audio = nil
			case "short":
				input.Size = minWAVBytes - 1
			case "large":
				input.Size = opts.MaxWAVBytes + 1
			case "prompt":
				input.Fields.Prompt = string([]byte{255})
			case "nan":
				input.Fields.Temperature = math.NaN()
			case "infinity":
				input.Fields.Temperature = math.Inf(1)
			case "negative":
				input.Fields.Temperature = -0.1
			case "temperature":
				input.Fields.Temperature = 1.1
			}

			_, err := client.Infer(context.Background(), input)
			failureKind(t, err, domain.InvalidInput)
		})
	}
	if calls != 0 {
		t.Fatal("invalid input contacted the backend")
	}
}

type gatedReader struct {
	entered chan struct{}
	release chan struct{}
}

func (r gatedReader) Read([]byte) (int, error) {
	close(r.entered)
	<-r.release

	return 0, io.EOF
}

func TestBorrowedBodyCloseJoinsActiveRead(t *testing.T) {
	reader := gatedReader{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	body := &borrowedBody{
		ctx:    context.Background(),
		source: reader,
	}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		if _, err := body.Read(make([]byte, 1)); err != io.EOF {
			t.Error("active read result changed", err)
		}
	}()
	<-reader.entered
	closed := make(chan struct{})
	closeStarted := make(chan struct{})
	go func() {
		defer close(closed)
		close(closeStarted)
		if err := body.Close(); err != nil {
			t.Error(err)
		}
	}()
	<-closeStarted
	select {
	case <-closed:
		t.Error("Close returned while a transport read still owned the audio")
	case <-time.After(20 * time.Millisecond):
	}
	close(reader.release)
	<-readDone
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after the active read returned")
	}
	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("closed body allowed another source read", err)
	}
	if err := body.Close(); err != nil {
		t.Fatal("repeated transport close failed", err)
	}
}

func TestBorrowedBodyCancellationPreventsSourceRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &borrowedBody{
		ctx:    ctx,
		source: failingReader{errors.New("source was read")},
	}
	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled body read its source", err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
}
