package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

func TestCancellationRecovery(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			opts, root, log := supervisorOptions(t)
			if err := os.WriteFile(filepath.Join(root, "mode"), []byte("stubborn"), 0600); err != nil {
				t.Fatal(err)
			}

			s := runningSupervisor(t, opts)

			first, _ := s.Health(context.Background())
			old := starts(t, root)[0]

			ctx, cancel := context.WithCancel(logging.WithRequestID(context.Background(), "cancel-01"))
			defer cancel()
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(ctx, 80*time.Millisecond)
				defer cancel()
			}

			result := make(chan error, 1)
			request := requestFor(s, "block")
			go func() {
				_, err := s.Infer(ctx, request)
				result <- err
			}()

			await(t, func() bool {
				files, _ := filepath.Glob(filepath.Join(root, "active-*"))
				return len(files) == 1
			})

			if mode == "cancel" {
				cancel()
			}

			start := time.Now()
			err := <-result
			requireKind(t, err, domain.DeadlineExceeded)
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("cause lost", err)
			}
			if time.Since(start) > opts.CleanupTimeout+100*time.Millisecond {
				t.Fatal("cleanup exceeded bound")
			}

			pid, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(old), "start-"))

			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("old native child survived return: %v", err)
			}

			await(t, func() bool {
				h, _ := s.Health(context.Background())
				return h.Status == domain.BackendReady && h.ProcessGeneration != first.ProcessGeneration
			})

			if _, err := s.Infer(context.Background(), requestFor(s, "")); err != nil {
				t.Fatal("recovered inference", err)
			}
			if len(starts(t, root)) != 2 {
				t.Fatal("recovery count not exact")
			}
			if !strings.Contains(log.text(), "backend_recovering") || !strings.Contains(log.text(), "cancel-01") {
				t.Fatal("correlated recovery event missing")
			}
		})
	}
}

func TestSupervisorDeadline(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	opts.OperationTimeout = 150 * time.Millisecond

	s := runningSupervisor(t, opts)

	_, err := s.Infer(context.Background(), requestFor(s, "block"))
	requireKind(t, err, domain.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("supervisor did not enforce own deadline", err)
	}

	oldPID, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(starts(t, root)[0]), "start-"))
	if err := syscall.Kill(oldPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("old work still running", err)
	}
}

func TestNativeCrashRecovery(t *testing.T) {
	opts, root, _ := supervisorOptions(t)

	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())

	_, err := s.Infer(context.Background(), requestFor(s, "crash"))
	requireKind(t, err, domain.BackendUnavailable)

	await(t, func() bool {
		h, _ := s.Health(context.Background())
		return h.Status == domain.BackendReady && h.ProcessGeneration != first.ProcessGeneration
	})
	if _, err := s.Infer(context.Background(), requestFor(s, "")); err != nil {
		t.Fatal(err)
	}
	if len(starts(t, root)) != 2 {
		t.Fatal("crash recovery silently retried submitted work")
	}
}

func TestDependencyLoss(t *testing.T) {
	opts, root, _ := supervisorOptions(t)

	s := runningSupervisor(t, opts)
	if err := os.WriteFile(filepath.Join(root, "bad-gpu"), nil, 0600); err != nil {
		t.Fatal(err)
	}

	await(t, func() bool {
		h, _ := s.Health(context.Background())
		return !h.CUDAReady
	})

	_, err := s.Infer(context.Background(), requestFor(s, ""))
	requireKind(t, err, domain.NotReady)
}

func TestGracefulShutdown(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	opts.ShutdownTimeout = 300 * time.Millisecond

	s := runningSupervisor(t, opts)

	result := make(chan error, 1)
	request := requestFor(s, "block")
	go func() {
		_, err := s.Infer(context.Background(), request)
		result <- err
	}()

	await(t, func() bool {
		files, _ := filepath.Glob(filepath.Join(root, "active-*"))
		return len(files) == 1
	})

	stopped := make(chan error, 1)
	go func() {
		stopped <- s.Shutdown(context.Background())
	}()

	await(t, func() bool {
		h, _ := s.Health(context.Background())
		return h.Status == domain.BackendStopping && !h.CUDAReady
	})

	_, err := s.Infer(context.Background(), requestFor(s, ""))
	requireKind(t, err, domain.ShuttingDown)
	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal("active operation not drained", err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if len(starts(t, root)) != 1 {
		t.Fatal("shutdown restarted backend")
	}

	files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*"))
	if len(files) != 0 {
		t.Fatal("owned work survived shutdown", files)
	}
	if data, err := os.ReadFile(opts.Artifact.Path); err != nil || string(data) != "prepared fixture" {
		t.Fatal("shutdown changed model", err)
	}
}

func TestAbandonedScratch(t *testing.T) {
	opts, _, _ := supervisorOptions(t)
	stale := filepath.Join(opts.WorkRoot, "run-"+strings.Repeat("A", 26))
	unrelated := filepath.Join(opts.WorkRoot, "run-"+strings.Repeat("B", 26))
	for _, dir := range []string{stale, unrelated} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "PRIVATE"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stale, ".owner"), []byte(workMarker), 0600); err != nil {
		t.Fatal(err)
	}

	s := runningSupervisor(t, opts)
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abandoned owned work retained", err)
	}
	if _, err := os.Stat(filepath.Join(unrelated, "PRIVATE")); err != nil {
		t.Fatal("unrelated work removed", err)
	}

	other, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	err = other.Run(context.Background())
	requireKind(t, err, domain.ProvisioningFailed)

	h, _ := s.Health(context.Background())
	if !h.CUDAReady {
		t.Fatal("second writer disturbed live owner")
	}
}

func TestPrivateCancellationCleanup(t *testing.T) {
	opts, _, _ := supervisorOptions(t)

	s := runningSupervisor(t, opts)
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	for i := 0; i < 3; i++ {
		response, err := server.Client().Do(privateRequest(t, s, server.URL))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "request-*.wav"))
		if len(files) != 0 {
			t.Fatal("completed request file retained", files)
		}
	}
}

func TestPrivateStalledUpload(t *testing.T) {
	for _, mode := range []string{"crash", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			opts, _, _ := supervisorOptions(t)
			s, run := runningSupervisorWithResult(t, opts)
			server := httptest.NewServer(s.Handler())
			t.Cleanup(server.Close)

			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = conn.Close()
			})

			first, _ := s.Health(context.Background())
			_, err = fmt.Fprintf(
				conn,
				"POST /inference HTTP/1.1\r\nHost: test\r\nContent-Type: multipart/form-data; boundary=stalled\r\nContent-Length: 10000\r\nX-Whisper-Protocol: %s\r\nX-Whisper-Configuration: %s\r\nX-Whisper-Generation: %s\r\nX-Whisper-Timeout-Ms: %d\r\n\r\n--stalled\r\nContent-Disposition: form-data; name=\"file\"; filename=\"audio.wav\"\r\nContent-Type: audio/wav\r\n\r\npartial",
				first.ProtocolVersion,
				first.ConfigurationFingerprint,
				first.ProcessGeneration,
				opts.OperationTimeout/time.Millisecond,
			)
			if err != nil {
				t.Fatal(err)
			}

			await(t, func() bool {
				files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "request-*.wav"))
				return len(files) == 1
			})

			s.mu.Lock()
			child := s.current
			s.mu.Unlock()

			if mode == "crash" {
				if err := child.cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				await(t, func() bool {
					select {
					case err := <-run:
						t.Fatalf("stalled upload prevented recovery: %v", err)
					default:
					}
					h, _ := s.Health(context.Background())
					return h.CUDAReady && h.ProcessGeneration != first.ProcessGeneration
				})
				if _, err := s.Infer(context.Background(), requestFor(s, "")); err != nil {
					t.Fatal("recovered backend rejected inference", err)
				}
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), opts.ShutdownTimeout+opts.CleanupTimeout+time.Second)
				defer cancel()
				if err := s.Shutdown(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-run:
					if err != nil {
						t.Fatal("stalled upload prevented clean shutdown", err)
					}
				case <-ctx.Done():
					t.Fatal("supervisor did not finish")
				}
			}

			if err := syscall.Kill(-child.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("native process group was not reaped", err)
			}

			files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "request-*.wav"))
			if len(files) != 0 {
				t.Fatal("cancelled upload retained request files")
			}
		})
	}
}

func TestBackendFailureClassification(t *testing.T) {
	for _, mode := range []string{"recovery", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			opts, _, _ := supervisorOptions(t)
			s, run := runningSupervisorWithResult(t, opts)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			s.mu.Lock()
			child := s.current
			child.retireAt = time.Now().Add(-time.Second)
			s.mu.Unlock()

			if mode == "recovery" {
				s.requestRecovery(child)
			} else if err := s.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-run:
				wrapped := fmt.Errorf("supervisor: %w", err)
				var retirement *retirementError
				if !errors.As(wrapped, &retirement) || !errors.Is(wrapped, context.DeadlineExceeded) {
					t.Fatalf("cleanup failure lost its cause: %v", err)
				}
				select {
				case <-child.done:
				case <-ctx.Done():
					t.Fatal("native process was not reaped")
				}
				requireKind(t, wrapped, domain.BackendUnavailable)
			case <-ctx.Done():
				t.Fatal("supervisor did not report cleanup failure")
			}
		})
	}
}
