package transcription

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

type trackedAudio struct {
	*bytes.Reader
	closes atomic.Int32
	inUse  atomic.Bool
	unsafe atomic.Bool
}

func (a *trackedAudio) Close() error {
	if a.inUse.Load() {
		a.unsafe.Store(true)
	}
	a.closes.Add(1)
	return nil
}

type testNormalizer struct {
	mu     sync.Mutex
	files  map[string]*trackedAudio
	decode func(context.Context, Input) error
}

func (n *testNormalizer) Normalize(ctx context.Context, input Input) (Normalized, error) {
	if n.decode != nil {
		if err := n.decode(ctx, input); err != nil {
			return Normalized{}, err
		}
	}

	a := &trackedAudio{Reader: bytes.NewReader(make([]byte, 46))}
	n.mu.Lock()
	n.files[input.RequestID] = a
	n.mu.Unlock()

	return Normalized{
		Audio:     a,
		Size:      46,
		Duration:  time.Second / 16000,
		RequestID: input.RequestID,
		Fields:    input.Fields,
	}, nil
}

func (n *testNormalizer) released(t *testing.T, ids ...string) {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, id := range ids {
		a := n.files[id]
		if a == nil || a.closes.Load() != 1 || a.unsafe.Load() {
			t.Fatalf("resource %s was not closed exactly once after use", id)
		}
	}
}

type testBackend struct {
	mu     sync.Mutex
	health Health
	err    error
	infer  func(context.Context, Normalized) (Result, error)
	calls  atomic.Int32
}

func (b *testBackend) Health(context.Context) (Health, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.health, b.err
}

func (b *testBackend) set(health Health, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.health, b.err = health, err
}

func (b *testBackend) Infer(ctx context.Context, audio Normalized) (Result, error) {
	b.calls.Add(1)
	a := audio.Audio.(*trackedAudio)
	a.inUse.Store(true)
	defer a.inUse.Store(false)
	return b.infer(ctx, audio)
}

func readyHealth() Health {
	return Health{
		BackendCompatibility: domain.BackendCompatibility{
			ProtocolVersion:          "1",
			ConfigurationFingerprint: strings.Repeat("a", 64),
			ProcessGeneration:        "generation-1",
		},
		Status:       domain.BackendReady,
		Model:        "base",
		Multilingual: true,
		CUDAReady:    true,
	}
}

func serviceOptions() Options {
	return Options{
		Model:            "base",
		Fingerprint:      strings.Repeat("a", 64),
		WaitingCapacity:  16,
		OperationTimeout: time.Second,
		CleanupTimeout:   100 * time.Millisecond,
		ShutdownTimeout:  100 * time.Millisecond,
		HealthTimeout:    20 * time.Millisecond,
		HealthInterval:   5 * time.Millisecond,
	}
}

func newTestService(t *testing.T, opts Options, b *testBackend) (*Service, *testNormalizer) {
	t.Helper()
	n := &testNormalizer{files: map[string]*trackedAudio{}}
	s, err := NewService(opts, n, b, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})

	return s, n
}

func awaitReady(t *testing.T, s *Service) {
	t.Helper()
	for i := 0; i < 10; i++ {
		synctest.Wait()
		if _, err := s.Readiness(context.Background()); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("service did not observe a ready backend")
}

type correlationKey struct{}

func submit(s *Service, ctx context.Context, id string) <-chan error {
	ctx = context.WithValue(ctx, correlationKey{}, id)
	done := make(chan error, 1)
	go func() {
		_, err := s.Admit(ctx, Input{
			Model:     "base",
			RequestID: id,
			Fields:    domain.InferenceFields{Language: "auto"},
		})
		done <- err
	}()
	return done
}

func requireFailure(t *testing.T, err error, kind domain.FailureKind) {
	t.Helper()
	var f *domain.Failure
	if !errors.As(err, &f) || f.Kind != kind {
		t.Fatalf("failure=%v want=%s", err, kind)
	}
}

func closeBarrier(barrier chan struct{}) {
	select {
	case <-barrier:
	default:
		close(barrier)
	}
}

func TestFIFOAdmission(t *testing.T) {
	for _, capacity := range []int{0, 2, 16} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered := make(chan string, capacity+1)
				release := make(chan struct{})
				var active, maximum atomic.Int32
				b := &testBackend{health: readyHealth()}
				b.infer = func(ctx context.Context, n Normalized) (Result, error) {
					if ctx.Value(correlationKey{}) != n.RequestID {
						t.Error("queue lost its owned request context")
					}
					current := active.Add(1)
					defer active.Add(-1)
					if current > maximum.Load() {
						maximum.Store(current)
					}
					entered <- n.RequestID
					select {
					case <-release:
						return Result{Text: n.RequestID}, nil
					case <-ctx.Done():
						return Result{}, ctx.Err()
					}
				}

				opts := serviceOptions()
				opts.WaitingCapacity = capacity
				s, n := newTestService(t, opts, b)
				awaitReady(t, s)

				ids := []string{"active"}
				results := []<-chan error{submit(s, context.Background(), "active")}
				if <-entered != "active" {
					t.Fatal("first request was not dispatched")
				}
				for i := 0; i < capacity; i++ {
					id := fmt.Sprintf("waiting-%d", i)
					ids = append(ids, id)
					results = append(results, submit(s, context.Background(), id))
					synctest.Wait()
				}

				_, err := s.Admit(context.Background(), Input{
					Model:     "base",
					RequestID: "overflow",
				})
				requireFailure(t, err, domain.QueueFull)
				n.released(t, "overflow")

				for i, result := range results {
					release <- struct{}{}
					if err := <-result; err != nil {
						t.Fatal(err)
					}
					if i+1 < len(ids) && <-entered != ids[i+1] {
						t.Fatal("backend entry order violated FIFO")
					}
				}

				if maximum.Load() != 1 || b.calls.Load() != int32(capacity+1) {
					t.Fatal("GPU work overlapped or rejected work dispatched")
				}
				n.released(t, ids...)
			})
		})
	}
}

func TestWaitingExpiry(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				defer closeBarrier(release)
				b := &testBackend{
					health: readyHealth(),
					infer: func(ctx context.Context, n Normalized) (Result, error) {
						if n.RequestID != "active" {
							return Result{}, nil
						}
						close(entered)
						<-release
						return Result{}, nil
					},
				}

				opts := serviceOptions()
				opts.WaitingCapacity = 1
				s, n := newTestService(t, opts, b)
				awaitReady(t, s)
				active := submit(s, context.Background(), "active")
				<-entered

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				waiting := submit(s, ctx, "waiting")
				synctest.Wait()
				if mode == "cancel" {
					cancel()
				} else {
					time.Sleep(31 * time.Millisecond)
				}

				requireFailure(t, <-waiting, domain.DeadlineExceeded)
				n.released(t, "waiting")

				replacement := submit(s, context.Background(), "replacement")
				synctest.Wait()
				select {
				case err := <-replacement:
					t.Fatalf("expired request retained queue capacity: %v", err)
				default:
				}

				close(release)
				if err := <-active; err != nil {
					t.Fatal(err)
				}
				if err := <-replacement; err != nil {
					t.Fatal(err)
				}

				if b.calls.Load() != 2 {
					t.Fatal("expired waiting request dispatched")
				}
				n.released(t, "replacement")
			})
		})
	}
}

func TestOperationDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan Normalized, 2)
		release := make(chan struct{})
		b := &testBackend{health: readyHealth()}
		b.infer = func(ctx context.Context, n Normalized) (Result, error) {
			entered <- n
			if n.RequestID == "active" {
				<-release
				return Result{}, nil
			}
			<-ctx.Done()
			h := readyHealth()
			h.ProcessGeneration = "generation-2"
			b.set(h, nil)
			return Result{}, &domain.Failure{
				Kind:  domain.DeadlineExceeded,
				Cause: ctx.Err(),
			}
		}

		opts := serviceOptions()
		opts.OperationTimeout = 100 * time.Millisecond
		s, n := newTestService(t, opts, b)
		awaitReady(t, s)
		active := submit(s, context.Background(), "active")
		<-entered

		start := time.Now()
		waiting := submit(s, context.Background(), "waiting")
		synctest.Wait()

		time.Sleep(60 * time.Millisecond)

		close(release)
		if err := <-active; err != nil {
			t.Fatal(err)
		}
		<-entered

		requireFailure(t, <-waiting, domain.DeadlineExceeded)
		if elapsed := time.Since(start); elapsed != opts.OperationTimeout {
			t.Fatalf("queue wait reset the operation deadline: %s", elapsed)
		}
		n.released(t, "active", "waiting")
	})
}
