package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/httpapi"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestAPIProcess(t *testing.T) {
	if os.Getenv("WHISPER_TEST_API_PROCESS") != "1" {
		return
	}

	var args []string
	if json.Unmarshal([]byte(os.Getenv("WHISPER_TEST_API_ARGS")), &args) != nil {
		os.Exit(8)
	}
	os.Exit(mainExit(
		args,
		[]string{},
		os.Stdout,
		runtimeOptions{decoderCommand: []string{"ffmpeg"}, workRoot: os.Getenv("WHISPER_TEST_API_ROOT")},
	))
}

func TestAPISIGTERM(t *testing.T) {
	var fingerprint atomic.Value
	fingerprint.Store("")
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":                    "ready",
			"protocol_version":          "1",
			"configuration_fingerprint": fingerprint.Load(),
			"process_generation":        "generation-1",
			"model":                     "base",
			"multilingual":              true,
			"cuda_ready":                true,
		})
	}))
	defer private.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	args := []string{
		"--api-host=127.0.0.1",
		"--api-port=" + strconv.Itoa(port),
		"--backend-url=" + private.URL,
		"--shutdown-seconds=1",
		"--cleanup-seconds=1",
	}
	settings, err := config.Load(args, []string{})
	if err != nil {
		t.Fatal(err)
	}

	value, err := config.CanonicalFingerprint(settings)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Store(value)
	encoded, _ := json.Marshal(args)
	command := exec.Command(os.Args[0], "-test.run=^TestAPIProcess$")
	command.Env = append(
		os.Environ(),
		"WHISPER_TEST_API_PROCESS=1",
		"WHISPER_TEST_API_ARGS="+string(encoded),
		"WHISPER_TEST_API_ROOT="+filepath.Join(t.TempDir(), "uploads"),
		"GORACE=atexit_sleep_ms=0",
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()

	joined := false
	defer func() {
		if !joined {
			_ = command.Process.Kill()
			<-wait
		}
	}()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	ready := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/health/ready")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("API process never became ready")
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-wait:
		joined = true
		if err != nil {
			t.Fatal("SIGTERM did not complete gracefully", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SIGTERM did not stop the API within its budget")
	}
}

type exitNormalizer struct{}

func (exitNormalizer) Normalize(_ context.Context, input transcription.Input) (transcription.Normalized, error) {
	return transcription.Normalized{Audio: io.NopCloser(bytes.NewReader(make([]byte, 46))), Size: 46, RequestID: input.RequestID}, nil
}

type exitBackend struct{ readers <-chan struct{} }

func (exitBackend) Health(context.Context) (transcription.Health, error) {
	return transcription.Health{
		BackendCompatibility: domain.BackendCompatibility{
			ProtocolVersion:          "1",
			ConfigurationFingerprint: strings.Repeat("a", 64),
			ProcessGeneration:        "generation-1",
		},
		Status:       domain.BackendReady,
		Model:        "base",
		CUDAReady:    true,
		Multilingual: true,
	}, nil
}

func (b exitBackend) Infer(ctx context.Context, _ transcription.Normalized) (transcription.Result, error) {
	if b.readers != nil {
		<-ctx.Done()
		<-b.readers
	}

	return transcription.Result{}, &domain.Failure{Kind: domain.BackendUnavailable}
}

func TestAPIFatalReaderExit(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	readers := make(chan struct{})
	s, err := transcription.NewService(transcription.Options{
		Model:            "base",
		Fingerprint:      strings.Repeat("a", 64),
		WaitingCapacity:  1,
		OperationTimeout: 30 * time.Millisecond,
		CleanupTimeout:   30 * time.Millisecond,
		ShutdownTimeout:  time.Second,
		HealthTimeout:    10 * time.Millisecond,
		HealthInterval:   time.Millisecond,
	}, exitNormalizer{}, exitBackend{readers: readers}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	defer func() {
		if readers != nil {
			close(readers)
		}
	}()

	uploads, err := httpapi.New(httpapi.Options{
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
	defer uploads.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	stopped := make(chan error, 1)
	go func() {
		stopped <- serve(context.Background(), listener, http.NotFoundHandler(), s, uploads, time.Second, 30*time.Millisecond, logger)
	}()

	for deadline := time.Now().Add(time.Second); ; {
		if _, err := s.Readiness(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fatal-reader fixture was not ready")
		}
		time.Sleep(time.Millisecond)
	}

	admitted := make(chan error, 1)
	go func() {
		_, err := s.Admit(context.Background(), transcription.Input{Model: "base", RequestID: "stuck-reader"})
		admitted <- err
	}()
	<-s.Done()
	select {
	case err := <-stopped:
		var failure *domain.Failure
		if !errors.As(err, &failure) || failure.Kind != domain.BackendUnavailable {
			t.Fatal("terminal cleanup failure did not fail the command", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("terminal cleanup failure incorrectly entered graceful drain")
	}
	close(readers)
	readers = nil
	<-admitted
}

func TestAPITerminalExit(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	s, err := transcription.NewService(transcription.Options{
		Model:            "base",
		Fingerprint:      strings.Repeat("a", 64),
		WaitingCapacity:  1,
		OperationTimeout: time.Second,
		CleanupTimeout:   30 * time.Millisecond,
		ShutdownTimeout:  30 * time.Millisecond,
		HealthTimeout:    10 * time.Millisecond,
		HealthInterval:   time.Millisecond,
	}, exitNormalizer{}, exitBackend{}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	uploads, err := httpapi.New(httpapi.Options{
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
	defer uploads.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := make(chan error, 1)
	go func() {
		stopped <- serve(ctx, listener, http.NotFoundHandler(), s, uploads, 30*time.Millisecond, 30*time.Millisecond, logger)
	}()

	ready := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if _, err := s.Readiness(context.Background()); err == nil {
			ready = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !ready {
		t.Fatal("terminal-exit fixture was not ready")
	}

	_, err = s.Admit(context.Background(), transcription.Input{Model: "base", RequestID: "faulted-request"})
	var failure *domain.Failure
	if !errors.As(err, &failure) || failure.Kind != domain.BackendUnavailable {
		t.Fatal("uncertain work did not fail", err)
	}

	select {
	case err := <-stopped:
		if !errors.As(err, &failure) || failure.Kind != domain.BackendUnavailable {
			t.Fatal("command masked terminal recovery failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("command did not exit after terminal recovery failure")
	}
}
