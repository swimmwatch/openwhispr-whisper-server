package transcription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestBackendReadiness(t *testing.T) {
	for _, mode := range []string{
		"starting",
		"unclaimed-busy",
		"failed",
		"gpu",
		"protocol",
		"configuration",
		"model",
		"dead",
	} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := readyHealth()
				var err error
				switch mode {
				case "starting":
					h.Status, h.CUDAReady = domain.BackendStarting, false
				case "unclaimed-busy":
					h.Status = domain.BackendBusy
				case "failed":
					h.Status, h.CUDAReady = domain.BackendFailed, false
				case "gpu":
					h.CUDAReady = false
				case "protocol":
					h.ProtocolVersion = "2"
				case "configuration":
					h.ConfigurationFingerprint = "different"
				case "model":
					h.Model = "other"
				case "dead":
					err = &domain.Failure{Kind: domain.BackendUnavailable}
				}
				b := &testBackend{
					health: h,
					err:    err,
				}

				s, _ := newTestService(t, serviceOptions(), b)
				synctest.Wait()
				if h, err := s.Readiness(context.Background()); err == nil || h.CUDAReady {
					t.Fatal("invalid dependency state was ready")
				}

				b.set(readyHealth(), nil)
				awaitReady(t, s)
				if b.calls.Load() != 0 {
					t.Fatal("health triggered inference")
				}
			})
		})
	}
}

func TestHealthDuringBlockedInference(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		defer closeBarrier(release)
		b := &testBackend{health: readyHealth()}
		b.infer = func(context.Context, Normalized) (Result, error) {
			h := readyHealth()
			h.Status = domain.BackendBusy
			b.set(h, nil)
			close(entered)
			<-release
			b.set(readyHealth(), nil)
			return Result{}, nil
		}

		s, _ := newTestService(t, serviceOptions(), b)
		awaitReady(t, s)
		active := submit(s, context.Background(), "active")
		<-entered
		time.Sleep(10 * time.Millisecond)
		for i := 0; i < 20; i++ {
			if _, err := s.Readiness(context.Background()); err != nil {
				t.Fatal("own healthy inference blocked readiness", err)
			}
		}

		if b.calls.Load() != 1 {
			t.Fatal("probes dispatched extra work")
		}
		close(release)
		if err := <-active; err != nil {
			t.Fatal(err)
		}
	})
}

func TestActiveRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, releaseReaders := make(chan struct{}), make(chan struct{})
		defer closeBarrier(releaseReaders)
		b := &testBackend{health: readyHealth()}
		b.infer = func(ctx context.Context, n Normalized) (Result, error) {
			if n.RequestID != "active" {
				return Result{}, nil
			}
			close(entered)
			<-ctx.Done()
			<-releaseReaders
			h := readyHealth()
			h.Status, h.CUDAReady, h.ProcessGeneration = domain.BackendStarting, false, "generation-2"
			b.set(h, nil)
			return Result{}, &domain.Failure{
				Kind:  domain.DeadlineExceeded,
				Cause: ctx.Err(),
			}
		}

		s, n := newTestService(t, serviceOptions(), b)
		awaitReady(t, s)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		active := submit(s, ctx, "active")
		<-entered
		waiting := submit(s, context.Background(), "waiting")
		synctest.Wait()

		cancel()
		synctest.Wait()
		if _, err := s.Readiness(context.Background()); err == nil {
			t.Fatal("cancelling work remained ready")
		}

		n.mu.Lock()
		closed := n.files["active"].closes.Load()
		n.mu.Unlock()
		if closed != 0 || b.calls.Load() != 1 {
			t.Fatal("freed an in-use reader or overlapped cancelled work")
		}

		close(releaseReaders)

		requireFailure(t, <-active, domain.DeadlineExceeded)
		n.released(t, "active")

		time.Sleep(200 * time.Millisecond)

		select {
		case <-s.Done():
			t.Fatal("model reload was incorrectly limited by cleanup time")
		default:
		}

		if b.calls.Load() != 1 {
			t.Fatal("queued work entered during reload")
		}

		h := readyHealth()
		h.ProcessGeneration = "generation-2"
		b.set(h, nil)
		awaitReady(t, s)
		if err := <-waiting; err != nil {
			t.Fatal(err)
		}
		n.released(t, "waiting")
	})
}

func TestGracefulShutdown(t *testing.T) {
	for _, mode := range []string{"complete", "force"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				defer closeBarrier(release)
				b := &testBackend{health: readyHealth()}
				b.infer = func(ctx context.Context, _ Normalized) (Result, error) {
					close(entered)
					select {
					case <-release:
						return Result{}, nil
					case <-ctx.Done():
						h := readyHealth()
						h.ProcessGeneration = "generation-2"
						b.set(h, nil)
						return Result{}, &domain.Failure{
							Kind:  domain.DeadlineExceeded,
							Cause: ctx.Err(),
						}
					}
				}

				s, n := newTestService(t, serviceOptions(), b)
				awaitReady(t, s)
				active := submit(s, context.Background(), "active")
				<-entered
				waiting := submit(s, context.Background(), "waiting")
				synctest.Wait()

				shutdown := make(chan error, 1)
				go func() {
					shutdown <- s.Shutdown(context.Background())
				}()
				synctest.Wait()
				requireFailure(t, <-waiting, domain.ShuttingDown)

				_, err := s.Admit(context.Background(), Input{
					Model:     "base",
					RequestID: "new",
				})
				requireFailure(t, err, domain.ShuttingDown)
				if _, err := s.Readiness(context.Background()); err == nil {
					t.Fatal("shutdown remained ready")
				}

				if mode == "complete" {
					close(release)
				}
				if err := <-active; mode == "force" {
					requireFailure(t, err, domain.ShuttingDown)
				} else if err != nil {
					t.Fatal(err)
				}
				if err := <-shutdown; err != nil {
					t.Fatal(err)
				}

				if b.calls.Load() != 1 {
					t.Fatal("shutdown forwarded waiting or new work")
				}
				n.released(t, "active", "waiting")
			})
		})
	}
}

func TestForcedShutdownRecoveryTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		b := &testBackend{health: readyHealth()}
		b.infer = func(ctx context.Context, _ Normalized) (Result, error) {
			close(entered)
			<-ctx.Done()
			h := readyHealth()
			h.Status, h.CUDAReady = domain.BackendRecovering, false
			b.set(h, nil)
			return Result{}, &domain.Failure{
				Kind:  domain.DeadlineExceeded,
				Cause: ctx.Err(),
			}
		}

		n := &testNormalizer{files: map[string]*trackedAudio{}}
		s, err := NewService(serviceOptions(), n, b, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = s.Shutdown(context.Background())
		}()
		awaitReady(t, s)
		active := submit(s, context.Background(), "active")
		<-entered

		err = s.Shutdown(context.Background())
		requireFailure(t, err, domain.BackendUnavailable)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("shutdown lost the recovery timeout cause")
		}

		requireFailure(t, <-active, domain.ShuttingDown)
		requireFailure(t, s.Err(), domain.BackendUnavailable)

		select {
		case <-s.Done():
		default:
			t.Fatal("failed shutdown did not notify the command")
		}
		n.released(t, "active")
	})
}

func TestShutdownDuringNormalization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &testBackend{health: readyHealth()}

		s, n := newTestService(t, serviceOptions(), b)
		entered := make(chan struct{})
		n.decode = func(ctx context.Context, _ Input) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}

		awaitReady(t, s)
		request := submit(s, context.Background(), "staging")
		<-entered
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		requireFailure(t, <-request, domain.ShuttingDown)
		if b.calls.Load() != 0 {
			t.Fatal("shutdown staging reached inference")
		}
	})
}

func TestServiceFailureClassification(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("PRIVATE cause")
		b := &testBackend{
			health: readyHealth(),
			infer: func(context.Context, Normalized) (Result, error) {
				return Result{}, fmt.Errorf("wrapped: %w", &domain.Failure{
					Kind:  domain.UnsupportedAudio,
					Field: "file",
					Cause: cause,
				})
			},
		}

		s, n := newTestService(t, serviceOptions(), b)
		awaitReady(t, s)

		_, err := s.Admit(context.Background(), Input{
			Model:     "base",
			RequestID: "failure",
		})
		requireFailure(t, err, domain.UnsupportedAudio)
		if !errors.Is(err, cause) {
			t.Fatal("queue lost the wrapped cause")
		}
		n.released(t, "failure")
	})
}

func TestCleanupBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		b := &testBackend{health: readyHealth()}
		b.infer = func(ctx context.Context, _ Normalized) (Result, error) {
			close(entered)
			<-ctx.Done()
			<-release
			h := readyHealth()
			h.ProcessGeneration = "generation-2"
			b.set(h, nil)
			return Result{}, ctx.Err()
		}

		n := &testNormalizer{files: map[string]*trackedAudio{}}
		s, err := NewService(serviceOptions(), n, b, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		awaitReady(t, s)

		ctx, cancel := context.WithCancel(context.Background())
		active := submit(s, ctx, "active")
		<-entered
		cancel()
		<-s.Done()
		requireFailure(t, s.Err(), domain.BackendUnavailable)

		n.mu.Lock()
		closed := n.files["active"].closes.Load()
		n.mu.Unlock()
		if closed != 0 {
			t.Fatal("cleanup timeout freed a reader still in use")
		}
		close(release)

		requireFailure(t, <-active, domain.DeadlineExceeded)
		requireFailure(t, s.Shutdown(context.Background()), domain.BackendUnavailable)
		n.released(t, "active")
	})
}
