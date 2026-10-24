package transcription

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

type normalizerFunc func(context.Context, Input) (Normalized, error)

func (f normalizerFunc) Normalize(ctx context.Context, input Input) (Normalized, error) {
	return f(ctx, input)
}

type backendFuncs struct {
	health func(context.Context) (Health, error)
	infer  func(context.Context, Normalized) (Result, error)
}

func (b backendFuncs) Health(ctx context.Context) (Health, error) {
	return b.health(ctx)
}

func (b backendFuncs) Infer(ctx context.Context, audio Normalized) (Result, error) {
	return b.infer(ctx, audio)
}

type closingAudio struct {
	*bytes.Reader
	closes atomic.Int32
	err    error
}

func (a *closingAudio) Close() error {
	a.closes.Add(1)
	return a.err
}

func newPortService(t *testing.T, normalizer Normalizer, backend InferenceBackend) *Service {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	s, err := NewService(serviceOptions(), normalizer, backend, logger)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil && s.Err() == nil {
			t.Error(err)
		}
	})
	return s
}

func TestServiceRejectsInvalidConstruction(t *testing.T) {
	changes := map[string]func(*Options){
		"model": func(opts *Options) {
			opts.Model = ""
		},

		"fingerprint": func(opts *Options) {
			opts.Fingerprint = ""
		},

		"negative_capacity": func(opts *Options) {
			opts.WaitingCapacity = -1
		},

		"overflow_capacity": func(opts *Options) {
			opts.WaitingCapacity = math.MaxInt
		},

		"operation_timeout": func(opts *Options) {
			opts.OperationTimeout = 0
		},

		"cleanup_timeout": func(opts *Options) {
			opts.CleanupTimeout = 0
		},

		"shutdown_timeout": func(opts *Options) {
			opts.ShutdownTimeout = 0
		},

		"health_timeout": func(opts *Options) {
			opts.HealthTimeout = 0
		},

		"health_interval": func(opts *Options) {
			opts.HealthInterval = 0
		},

		"health_interval_exceeds_timeout": func(opts *Options) {
			opts.HealthInterval = opts.HealthTimeout + time.Nanosecond
		},
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			opts := serviceOptions()
			change(&opts)
			service, err := NewService(opts, &testNormalizer{}, &testBackend{}, logger)
			requireFailure(t, err, domain.InvalidInput)
			if service != nil {
				t.Fatal("invalid options started a service")
			}
		})
	}

	for _, dependency := range []string{"normalizer", "backend", "logger"} {
		t.Run(dependency, func(t *testing.T) {
			var normalizer Normalizer = &testNormalizer{}
			var backend InferenceBackend = &testBackend{}
			configuredLogger := logger
			switch dependency {
			case "normalizer":
				normalizer = nil
			case "backend":
				backend = nil
			case "logger":
				configuredLogger = nil
			}
			_, err := NewService(serviceOptions(), normalizer, backend, configuredLogger)
			requireFailure(t, err, domain.InvalidInput)
		})
	}
}

func TestNormalizationFailuresReleaseOwnedAudio(t *testing.T) {
	for _, mode := range []string{
		"unknown_error",
		"semantic_error",
		"panic",
		"missing_audio",
		"deadline",
		"readiness_lost",
		"close_error",
	} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cause := errors.New("private normalization failure")
				audio := &closingAudio{Reader: bytes.NewReader([]byte("normalized"))}
				backend := &testBackend{health: readyHealth()}
				normalizer := normalizerFunc(func(ctx context.Context, _ Input) (Normalized, error) {
					result := Normalized{Audio: audio}
					switch mode {
					case "unknown_error":
						return result, cause
					case "semantic_error":
						return result, &domain.Failure{
							Kind:  domain.UnsupportedAudio,
							Field: "file",
							Cause: cause,
						}
					case "panic":
						panic("private panic")
					case "missing_audio":
						return Normalized{}, nil
					case "deadline":
						<-ctx.Done()
						return result, ctx.Err()
					case "readiness_lost":
						backend.set(Health{}, cause)
						time.Sleep(2 * serviceOptions().HealthInterval)
					case "close_error":
						audio.err = cause
						return result, &domain.Failure{Kind: domain.UnsupportedAudio}
					}
					return result, nil
				})

				s := newPortService(t, normalizer, backend)
				awaitReady(t, s)

				_, err := s.Admit(context.Background(), Input{Model: "base"})

				kind := domain.Internal
				if mode == "semantic_error" || mode == "close_error" {
					kind = domain.UnsupportedAudio
				}
				if mode == "deadline" {
					kind = domain.DeadlineExceeded
				}
				requireFailure(t, err, kind)

				wantCloses := int32(1)
				if mode == "panic" || mode == "missing_audio" {
					wantCloses = 0
				}
				if audio.closes.Load() != wantCloses || backend.calls.Load() != 0 {
					t.Fatalf("failed staging closed audio %d times and dispatched %d operations", audio.closes.Load(), backend.calls.Load())
				}

				if mode == "unknown_error" || mode == "semantic_error" || mode == "close_error" || mode == "readiness_lost" {
					if !errors.Is(err, cause) {
						t.Fatal("normalization or cleanup lost its cause")
					}
				}

				if mode == "close_error" {
					requireFailure(t, s.Err(), domain.Internal)
					if !errors.Is(s.Err(), cause) {
						t.Fatal("terminal cleanup failure lost its cause")
					}
					select {
					case <-s.Done():
					default:
						t.Fatal("failed cleanup did not notify the owner")
					}
				} else if s.Err() != nil {
					t.Fatal("rejected staging incorrectly stopped the service", s.Err())
				}
			})
		})
	}
}

func TestUnknownModelDoesNotNormalize(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var normalized atomic.Int32
		normalizer := normalizerFunc(func(context.Context, Input) (Normalized, error) {
			normalized.Add(1)
			return Normalized{}, nil
		})
		backend := &testBackend{health: readyHealth()}

		s := newPortService(t, normalizer, backend)
		awaitReady(t, s)

		_, err := s.Admit(context.Background(), Input{Model: "other"})
		requireFailure(t, err, domain.UnknownModel)
		if normalized.Load() != 0 || backend.calls.Load() != 0 {
			t.Fatal("unknown model consumed staging or inference capacity")
		}
	})
}

func TestBackendPanicRecoveryKeepsServiceUsable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var failing atomic.Bool
		failing.Store(true)
		base := &testBackend{health: readyHealth()}

		backend := backendFuncs{
			health: base.Health,
			infer: func(context.Context, Normalized) (Result, error) {
				if failing.Swap(false) {
					health := readyHealth()
					health.ProcessGeneration = "generation-2"
					base.set(health, nil)
					panic("private backend panic")
				}
				return Result{Text: "recovered"}, nil
			},
		}

		normalizer := &testNormalizer{files: map[string]*trackedAudio{}}

		s := newPortService(t, normalizer, backend)
		awaitReady(t, s)

		_, err := s.Admit(context.Background(), Input{
			Model:     "base",
			RequestID: "first",
		})
		requireFailure(t, err, domain.Internal)
		awaitReady(t, s)

		result, err := s.Admit(context.Background(), Input{
			Model:     "base",
			RequestID: "second",
		})
		if err != nil || result.Text != "recovered" || s.Err() != nil {
			t.Fatalf("service did not recover after confirmed generation replacement: %v", err)
		}
		normalizer.released(t, "first", "second")
	})
}

func TestHealthPanicAndDeadlineStayUnready(t *testing.T) {
	for _, mode := range []string{"panic", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				backend := backendFuncs{
					health: func(ctx context.Context) (Health, error) {
						if mode == "panic" {
							panic("private health panic")
						}
						<-ctx.Done()
						return Health{}, ctx.Err()
					},
				}

				s := newPortService(t, &testNormalizer{}, backend)
				time.Sleep(2 * serviceOptions().HealthTimeout)

				kind := domain.Internal
				if mode == "deadline" {
					kind = domain.BackendUnavailable
				}

				health, err := s.Readiness(context.Background())
				requireFailure(t, err, kind)
				if health.CUDAReady {
					t.Fatal("failed health observation reported readiness")
				}
				if s.Err() != nil {
					t.Fatal("failed health observation incorrectly stopped the service", s.Err())
				}
				if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("health failure lost the timeout cause")
				}
			})
		})
	}
}

func TestCancelledReadinessPreservesCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service := &Service{}
	_, err := service.Readiness(ctx)
	requireFailure(t, err, domain.DeadlineExceeded)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled readiness lost its cause")
	}
}

func TestNilFailureRequiresRecovery(t *testing.T) {
	var failure *domain.Failure
	if !requiresRecovery(failure) {
		t.Fatal("unclassified backend failure bypassed recovery")
	}
}

func TestNilBackendFailureRecoversBeforeNextRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		base := &testBackend{health: readyHealth()}
		var called atomic.Int32

		backend := backendFuncs{
			health: base.Health,
			infer: func(context.Context, Normalized) (Result, error) {
				if called.Add(1) == 1 {
					health := readyHealth()
					health.ProcessGeneration = "generation-2"
					base.set(health, nil)
					var failure *domain.Failure
					return Result{}, failure
				}
				return Result{Text: "replacement"}, nil
			},
		}

		normalizer := &testNormalizer{files: map[string]*trackedAudio{}}

		s := newPortService(t, normalizer, backend)
		awaitReady(t, s)

		_, err := s.Admit(context.Background(), Input{
			Model:     "base",
			RequestID: "nil-failure",
		})
		requireFailure(t, err, domain.Internal)
		awaitReady(t, s)

		result, err := s.Admit(context.Background(), Input{
			Model:     "base",
			RequestID: "replacement",
		})
		if err != nil || result.Text != "replacement" || called.Load() != 2 {
			t.Fatalf("unclassified failure prevented replacement work: %v", err)
		}
		normalizer.released(t, "nil-failure", "replacement")
	})
}

func TestDispatchRevalidatesBackend(t *testing.T) {
	for _, mode := range []string{"health_error", "missing_generation", "missing_cuda", "busy"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cause := errors.New("private health failure")
				base := &testBackend{health: readyHealth()}
				audio := &closingAudio{Reader: bytes.NewReader([]byte("normalized"))}
				normalizer := normalizerFunc(func(context.Context, Input) (Normalized, error) {
					health := readyHealth()
					var err error
					switch mode {
					case "health_error":
						err = cause
					case "missing_generation":
						health.ProcessGeneration = ""
					case "missing_cuda":
						health.CUDAReady = false
					case "busy":
						health.Status = domain.BackendBusy
					}
					base.set(health, err)
					return Normalized{Audio: audio}, nil
				})

				s := newPortService(t, normalizer, base)
				awaitReady(t, s)

				_, err := s.Admit(context.Background(), Input{Model: "base"})

				kind := domain.NotReady
				if mode == "health_error" {
					kind = domain.Internal
					if !errors.Is(err, cause) {
						t.Fatal("dispatch lost the health error cause")
					}
				}
				requireFailure(t, err, kind)
				if audio.closes.Load() != 1 || base.calls.Load() != 0 {
					t.Fatal("unready dispatch leaked audio or reached inference")
				}
				if s.Err() != nil {
					t.Fatal("rejected dispatch incorrectly stopped the service", s.Err())
				}
			})
		})
	}
}

func TestInferenceCleanupFailureStopsAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("private cleanup failure")
		audio := &closingAudio{
			Reader: bytes.NewReader([]byte("normalized")),
			err:    cause,
		}
		var normalized atomic.Int32
		var inferred atomic.Int32
		normalizer := normalizerFunc(func(context.Context, Input) (Normalized, error) {
			normalized.Add(1)
			return Normalized{Audio: audio}, nil
		})

		backend := backendFuncs{
			health: func(context.Context) (Health, error) {
				return readyHealth(), nil
			},
			infer: func(context.Context, Normalized) (Result, error) {
				inferred.Add(1)
				return Result{Text: "completed"}, nil
			},
		}

		s := newPortService(t, normalizer, backend)
		awaitReady(t, s)

		_, err := s.Admit(context.Background(), Input{Model: "base"})
		requireFailure(t, err, domain.Internal)
		requireFailure(t, s.Err(), domain.Internal)
		if !errors.Is(err, cause) || !errors.Is(s.Err(), cause) {
			t.Fatal("completed inference lost its cleanup error")
		}
		_, err = s.Admit(context.Background(), Input{Model: "base"})
		requireFailure(t, err, domain.ShuttingDown)
		if normalized.Load() != 1 || inferred.Load() != 1 || audio.closes.Load() != 1 {
			t.Fatal("failed cleanup allowed reuse or closed the owned audio again")
		}

		select {
		case <-s.Done():
		default:
			t.Fatal("failed cleanup did not notify the command")
		}
	})
}

func TestFailureMessagesRemainPrivate(t *testing.T) {
	cause := errors.New("private upstream message")
	err := semantic(cause)
	requireFailure(t, err, domain.Internal)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("failure classification lost its cause or exposed it publicly")
	}
}
