package backend

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
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

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/upstream"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

type pairedNormalizer struct{ closes atomic.Int32 }

type pairedAudio struct {
	io.Reader
	closes *atomic.Int32
}

func (a *pairedAudio) Close() error {
	a.closes.Add(1)
	return nil
}

func (n *pairedNormalizer) Normalize(_ context.Context, input transcription.Input) (transcription.Normalized, error) {
	data := wavFixture()
	return transcription.Normalized{
		Audio:     &pairedAudio{Reader: bytes.NewReader(data), closes: &n.closes},
		Size:      int64(len(data)),
		Duration:  100 * time.Millisecond,
		RequestID: input.RequestID,
		Fields:    input.Fields,
	}, nil
}

func pairedService(url, fingerprint string) (*transcription.Service, *upstream.Client, *pairedNormalizer, error) {
	client, err := upstream.New(upstream.Options{
		URL:              url,
		Fingerprint:      fingerprint,
		Model:            "base",
		OperationTimeout: 2 * time.Second,
		MaxWAVBytes:      32044,
		MaxReplyBytes:    1 << 20,
		PromptBytes:      8192,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	normalizer := &pairedNormalizer{}
	s, err := transcription.NewService(
		transcription.Options{
			Model:            "base",
			Fingerprint:      fingerprint,
			WaitingCapacity:  2,
			OperationTimeout: 2 * time.Second,
			CleanupTimeout:   800 * time.Millisecond,
			ShutdownTimeout:  100 * time.Millisecond,
			HealthTimeout:    100 * time.Millisecond,
			HealthInterval:   5 * time.Millisecond,
		},
		normalizer,
		client,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	return s, client, normalizer, err
}

func TestActiveRecoveryThroughPrivateHTTP(t *testing.T) {
	opts, root, logs := supervisorOptions(t)
	opts.CleanupTimeout = 800 * time.Millisecond
	if err := os.WriteFile(filepath.Join(root, "mode"), []byte("stubborn"), 0600); err != nil {
		t.Fatal(err)
	}

	backend := runningSupervisor(t, opts)
	private := httptest.NewServer(backend.Handler())
	defer private.Close()

	s, client, normalizer, err := pairedService(private.URL, opts.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	defer func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()

	await(t, func() bool {
		_, err := s.Readiness(context.Background())
		return err == nil
	})

	old := starts(t, root)[0]

	ctx, cancel := context.WithCancel(logging.WithRequestID(context.Background(), "paired-cancel-1"))
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := s.Admit(
			ctx,
			transcription.Input{
				Model:     "base",
				RequestID: "paired-cancel-1",
				Fields:    domain.InferenceFields{Language: "auto", Prompt: "block"},
			},
		)
		result <- err
	}()

	await(t, func() bool {
		files, _ := filepath.Glob(filepath.Join(root, "active-*"))
		return len(files) == 1
	})

	cancel()

	await(t, func() bool {
		_, err := s.Readiness(context.Background())
		return err != nil
	})

	requireKind(t, <-result, domain.DeadlineExceeded)

	pid, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(old), "start-"))
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("API released work before the old native child was reaped", err)
	}

	await(t, func() bool {
		_, err := s.Readiness(context.Background())
		return err == nil
	})

	if _, err := s.Admit(
		context.Background(),
		transcription.Input{Model: "base", RequestID: "next-request", Fields: domain.InferenceFields{Language: "auto"}},
	); err != nil {
		t.Fatal(err)
	}
	if len(starts(t, root)) != 2 || normalizer.closes.Load() != 2 {
		t.Fatal("request was retried, reloaded normally or leaked")
	}
	if !strings.Contains(logs.text(), "paired-cancel-1") {
		t.Fatal("recovery lost the original request ID")
	}
}

func TestForcedShutdownThroughPrivateHTTP(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	opts.CleanupTimeout = 800 * time.Millisecond
	if err := os.WriteFile(filepath.Join(root, "mode"), []byte("stubborn"), 0600); err != nil {
		t.Fatal(err)
	}

	backend := runningSupervisor(t, opts)
	private := httptest.NewServer(backend.Handler())
	defer private.Close()

	s, client, normalizer, err := pairedService(private.URL, opts.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	defer func() {
		_ = s.Shutdown(context.Background())
	}()

	await(t, func() bool {
		_, err := s.Readiness(context.Background())
		return err == nil
	})

	old := starts(t, root)[0]

	result := make(chan error, 1)
	go func() {
		_, err := s.Admit(
			context.Background(),
			transcription.Input{
				Model:     "base",
				RequestID: "forced-shutdown",
				Fields:    domain.InferenceFields{Language: "auto", Prompt: "block"},
			},
		)
		result <- err
	}()

	await(t, func() bool {
		files, _ := filepath.Glob(filepath.Join(root, "active-*"))
		return len(files) == 1
	})
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	requireKind(t, <-result, domain.ShuttingDown)

	pid, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(old), "start-"))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("shutdown completed before old native child was reaped", err)
	}
	if normalizer.closes.Load() != 1 {
		t.Fatal("shutdown did not release normalized audio exactly once")
	}
}

func TestAPIServiceChild(t *testing.T) {
	if os.Getenv("WHISPER_TEST_API_SERVICE") != "1" {
		return
	}

	s, client, _, err := pairedService(os.Getenv("WHISPER_TEST_PRIVATE_URL"), strings.Repeat("b", 64))
	if err != nil {
		os.Exit(8)
	}
	defer client.Close()

	await(t, func() bool {
		_, err := s.Readiness(context.Background())
		return err == nil
	})
	_, _ = s.Admit(
		context.Background(),
		transcription.Input{
			Model:     "base",
			RequestID: "lost-api-request",
			Fields:    domain.InferenceFields{Language: "auto", Prompt: "block"},
		},
	)
	os.Exit(9)
}

func TestAPILoss(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	opts.CleanupTimeout = 800 * time.Millisecond
	if err := os.WriteFile(filepath.Join(root, "mode"), []byte("stubborn"), 0600); err != nil {
		t.Fatal(err)
	}

	backend := runningSupervisor(t, opts)
	private := httptest.NewServer(backend.Handler())
	defer private.Close()

	old := starts(t, root)[0]

	command := exec.Command(os.Args[0], "-test.run=^TestAPIServiceChild$")
	command.Env = append(
		os.Environ(),
		"WHISPER_TEST_API_SERVICE=1",
		"WHISPER_TEST_PRIVATE_URL="+private.URL,
		"GORACE=atexit_sleep_ms=0",
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()

	await(t, func() bool {
		files, _ := filepath.Glob(filepath.Join(root, "active-*"))
		return len(files) == 1
	})

	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("API helper was not forcibly terminated")
	}

	pid, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(old), "start-"))

	await(t, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	})

	await(t, func() bool {
		h, _ := backend.Health(context.Background())
		return h.Status == domain.BackendReady
	})

	if _, err := backend.Infer(context.Background(), requestFor(backend, "")); err != nil {
		t.Fatal("API loss left the pair unusable", err)
	}
	if len(starts(t, root)) != 2 {
		t.Fatal("API loss did not isolate and replace the old native process")
	}
}
