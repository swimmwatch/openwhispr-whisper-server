package transcription

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestOwnedBusyCompletionDoesNotRejectNextRequest(t *testing.T) {
	for _, mode := range []string{"sequential", "queued"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				defer closeBarrier(release)
				b := &testBackend{health: readyHealth()}
				b.infer = func(_ context.Context, n Normalized) (Result, error) {
					if n.RequestID == "first" {
						h := readyHealth()
						h.Status = domain.BackendBusy
						b.set(h, nil)
						close(entered)
						<-release
						b.set(readyHealth(), nil)
					}
					return Result{Text: n.RequestID}, nil
				}
				s, n := newTestService(t, serviceOptions(), b)
				awaitReady(t, s)
				first := submit(s, context.Background(), "first")
				<-entered
				time.Sleep(serviceOptions().HealthInterval)
				synctest.Wait()
				if _, err := s.Readiness(context.Background()); err != nil {
					t.Fatal("owned busy inference was not ready", err)
				}
				var second <-chan error
				if mode == "queued" {
					second = submit(s, context.Background(), "second")
					synctest.Wait()
				}
				close(release)
				if err := <-first; err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if _, err := s.Readiness(context.Background()); err != nil {
					t.Fatal("completed owned busy snapshot rejected readiness before next poll", err)
				}
				if mode == "sequential" {
					second = submit(s, context.Background(), "second")
					synctest.Wait()
				}
				select {
				case err := <-second:
					if err != nil {
						t.Fatal("next request rejected after successful completion", err)
					}
				default:
					t.Fatal("next request waited for the next periodic health poll")
				}
				if b.calls.Load() != 2 {
					t.Fatal("next request did not execute exactly once")
				}
				n.released(t, "first", "second")
			})
		})
	}
}

func TestLateOwnedBusyHealthCannotOverwriteCompletedReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		healthEntered, healthRelease := make(chan struct{}), make(chan struct{})
		defer closeBarrier(release)
		defer closeBarrier(healthRelease)
		base := &testBackend{health: readyHealth()}
		var delayed atomic.Bool
		backend := backendFuncs{
			health: func(ctx context.Context) (Health, error) {
				h, err := base.Health(ctx)
				if h.Status == domain.BackendBusy && delayed.CompareAndSwap(false, true) {
					close(healthEntered)
					select {
					case <-healthRelease:
					case <-ctx.Done():
						return Health{}, ctx.Err()
					}
				}
				return h, err
			},
			infer: func(_ context.Context, n Normalized) (Result, error) {
				if n.RequestID == "first" {
					h := readyHealth()
					h.Status = domain.BackendBusy
					base.set(h, nil)
					close(entered)
					<-release
					base.set(readyHealth(), nil)
				}
				base.calls.Add(1)
				return Result{Text: n.RequestID}, nil
			},
		}
		normalizer := &testNormalizer{files: map[string]*trackedAudio{}}
		s := newPortService(t, normalizer, backend)
		awaitReady(t, s)
		first := submit(s, context.Background(), "first")
		<-entered
		time.Sleep(serviceOptions().HealthInterval)
		<-healthEntered
		close(release)
		synctest.Wait()
		close(healthRelease)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := s.Readiness(context.Background()); err != nil {
			t.Fatal("late owned busy health response replaced completion readiness", err)
		}
		second := submit(s, context.Background(), "second")
		synctest.Wait()
		select {
		case err := <-second:
			if err != nil {
				t.Fatal("late busy response rejected the next request", err)
			}
		default:
			t.Fatal("late busy response stalled next dispatch")
		}
		if base.calls.Load() != 2 {
			t.Fatal("late busy response prevented next inference")
		}
		normalizer.released(t, "first", "second")
	})
}

func newBusyPortService(t *testing.T, opts Options, backend InferenceBackend) (*Service, *testNormalizer) {
	t.Helper()
	normalizer := &testNormalizer{files: map[string]*trackedAudio{}}
	s, err := NewService(opts, normalizer, backend, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Shutdown(context.Background()); err != nil && s.Err() == nil {
			t.Error(err)
		}
	})
	return s, normalizer
}

func TestHealthReadWaitHonorsCancellationAndWholeBudget(t *testing.T) {
	for _, mode := range []string{"cancel", "health-budget"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var blocked atomic.Bool
				var active, maximum, inferred atomic.Int32
				entered := make(chan struct{}, 1)
				backend := backendFuncs{
					health: func(ctx context.Context) (Health, error) {
						count := active.Add(1)
						defer active.Add(-1)
						for old := maximum.Load(); count > old; old = maximum.Load() {
							if maximum.CompareAndSwap(old, count) {
								break
							}
						}
						if blocked.Load() {
							select {
							case entered <- struct{}{}:
							default:
							}
							<-ctx.Done()
							return Health{}, ctx.Err()
						}
						return readyHealth(), nil
					},
					infer: func(context.Context, Normalized) (Result, error) { inferred.Add(1); return Result{}, nil },
				}
				opts := serviceOptions()
				s, _ := newBusyPortService(t, opts, backend)
				awaitReady(t, s)
				blocked.Store(true)
				time.Sleep(opts.HealthInterval)
				<-entered
				now := time.Now()
				if _, err := s.Readiness(context.Background()); err != nil {
					t.Fatal("cached readiness blocked behind an outstanding health call", err)
				}
				if time.Now() != now {
					t.Fatal("cached readiness performed blocking health IO")
				}
				time.Sleep(5 * time.Millisecond)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				started := time.Now()
				request := submit(s, ctx, "gate-wait")
				synctest.Wait()
				if mode == "cancel" {
					cancel()
				}
				err := <-request
				if mode == "cancel" {
					requireFailure(t, err, domain.DeadlineExceeded)
					if !errors.Is(err, context.Canceled) {
						t.Fatal("gate cancellation cause lost")
					}
				} else {
					requireFailure(t, err, domain.BackendUnavailable)
					if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != opts.HealthTimeout {
						t.Fatal("waiting for the health gate reset the health timeout", time.Since(started), err)
					}
				}
				if maximum.Load() != 1 || inferred.Load() != 0 {
					t.Fatal("health calls overlapped or waiting health dispatched inference")
				}
				blocked.Store(false)
				awaitReady(t, s)
			})
		})
	}
}

func TestCompletionHealthTimeoutDoesNotRestartCompletedInference(t *testing.T) {
	for _, mode := range []string{"health-only", "operation-deadline", "caller-cancel"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var blocked atomic.Bool
				var inferred atomic.Int32
				entered := make(chan struct{}, 1)
				backend := backendFuncs{
					health: func(ctx context.Context) (Health, error) {
						if blocked.Load() {
							select {
							case entered <- struct{}{}:
							default:
							}
							<-ctx.Done()
							return Health{}, ctx.Err()
						}
						return readyHealth(), nil
					},
					infer: func(context.Context, Normalized) (Result, error) {
						inferred.Add(1)
						blocked.Store(true)
						return Result{Text: "completed"}, nil
					},
				}
				opts := serviceOptions()
				if mode == "operation-deadline" {
					opts.OperationTimeout = 5 * time.Millisecond
				}
				s, n := newBusyPortService(t, opts, backend)
				awaitReady(t, s)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				type completion struct {
					at     time.Time
					result Result
					err    error
				}
				request := make(chan completion, 1)
				started := time.Now()
				go func() {
					result, err := s.Admit(ctx, Input{Model: "base", RequestID: "completed"})
					request <- completion{at: time.Now(), result: result, err: err}
				}()
				synctest.Wait()
				var done completion
				received, cancelledPending := false, false
				if mode == "caller-cancel" {
					select {
					case done = <-request:
						received = true // Cancellation after a delivered reply cannot revoke it.
					default:
						cancelledPending = true
						cancel()
					}
				}
				if !received {
					done = <-request
				}
				missedDeadline := mode == "operation-deadline" && !done.at.Before(started.Add(opts.OperationTimeout))
				if !missedDeadline && !cancelledPending {
					if done.err != nil || done.result.Text != "completed" {
						t.Fatal("health-only timeout changed successful inference", done.err)
					}
				} else {
					requireFailure(t, done.err, domain.DeadlineExceeded)
					cause := context.DeadlineExceeded
					if cancelledPending {
						cause = context.Canceled
					}
					if !errors.Is(done.err, cause) {
						t.Fatal("post-completion health wait lost operation cancellation")
					}
				}
				synctest.Wait()
				if inferred.Load() != 1 || s.Err() != nil {
					t.Fatal("completed work was retried or failed terminally")
				}
				blocked.Store(false)
				awaitReady(t, s)
				n.released(t, "completed")
			})
		})
	}
}

func TestCompletionDoesNotAuthorizeUnclaimedOrInvalidHealth(t *testing.T) {
	for _, mode := range []string{"unclaimed-busy", "busy-generation-drift", "cpu", "configuration", "dependency-error", "recovering"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				defer closeBarrier(release)
				base := &testBackend{health: readyHealth()}
				base.infer = func(_ context.Context, n Normalized) (Result, error) {
					h := readyHealth()
					h.Status = domain.BackendBusy
					base.set(h, nil)
					close(entered)
					<-release
					h = readyHealth()
					var err error
					switch mode {
					case "unclaimed-busy":
						h.Status = domain.BackendBusy
					case "busy-generation-drift":
						h.Status = domain.BackendBusy
						h.ProcessGeneration = "unclaimed-generation"
					case "cpu":
						h.CUDAReady = false
					case "configuration":
						h.ConfigurationFingerprint = "changed"
					case "dependency-error":
						err = &domain.Failure{Kind: domain.BackendUnavailable}
					case "recovering":
						h.Status = domain.BackendRecovering
						h.CUDAReady = false
					}
					base.set(h, err)
					return Result{Text: n.RequestID}, nil
				}
				s, _ := newTestService(t, serviceOptions(), base)
				awaitReady(t, s)
				first := submit(s, context.Background(), "first")
				<-entered
				time.Sleep(serviceOptions().HealthInterval)
				synctest.Wait()
				close(release)
				if err := <-first; err != nil {
					t.Fatal("completed inference result was changed by dependency observation", err)
				}
				synctest.Wait()
				h, err := s.Readiness(context.Background())
				if err == nil || h.CUDAReady {
					t.Fatal("successful inference authorized an unclaimed or invalid backend state")
				}
				second := submit(s, context.Background(), "second")
				if err := <-second; err == nil {
					t.Fatal("unclaimed or invalid dependency admitted new work")
				}
				if base.calls.Load() != 1 {
					t.Fatal("unready backend received another inference")
				}
			})
		})
	}
}
