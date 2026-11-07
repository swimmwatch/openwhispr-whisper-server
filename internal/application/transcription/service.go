package transcription

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

const (
	backendProtocolVersion = "1"
	eventQueueAdmitted     = "queue_admitted"
	eventQueueCompleted    = "queue_completed"
	eventQueueDispatched   = "queue_dispatched"
	eventBackendRecovering = "backend_recovering"
	eventAPINotReady       = "api_not_ready"
	eventAPIReady          = "api_ready"
)

type Options struct {
	Model            string
	Fingerprint      string
	WaitingCapacity  int
	OperationTimeout time.Duration
	CleanupTimeout   time.Duration
	ShutdownTimeout  time.Duration
	HealthTimeout    time.Duration
	HealthInterval   time.Duration
}

func (o Options) valid() bool {
	return o.Model != "" &&
		o.Fingerprint != "" &&
		o.WaitingCapacity >= 0 &&
		o.WaitingCapacity < math.MaxInt &&
		o.OperationTimeout > 0 &&
		o.CleanupTimeout > 0 &&
		o.ShutdownTimeout > 0 &&
		o.HealthTimeout > 0 &&
		o.HealthInterval > 0 &&
		o.HealthInterval <= o.HealthTimeout
}

type Service struct {
	opts                                         Options
	normalizer                                   Normalizer
	backend                                      InferenceBackend
	logger                                       *slog.Logger
	mu                                           sync.Mutex
	queue                                        []*ticket
	active                                       *ticket
	health                                       Health
	healthErr                                    error
	observed                                     time.Time
	stopping, recovering                         bool
	announcedReady                               bool
	unsafeGeneration                             string
	terminal                                     error
	wake                                         chan struct{}
	healthCalls                                  chan struct{}
	workerDone, observerDone, done, shutdownDone chan struct{}
	staging                                      context.Context
	stopStaging                                  context.CancelFunc
	observer                                     context.Context
	stopObserver                                 context.CancelFunc
	calls                                        sync.WaitGroup
	stopOnce, doneOnce                           sync.Once
	shutdownErr                                  error
}

type ticket struct {
	ctx               context.Context
	cancel            context.CancelFunc
	stop              func() bool
	audio             Normalized
	done              chan struct{}
	once              sync.Once
	result            Result
	err               error
	generation        string
	cleanupDeadline   time.Time
	submitted, forced bool
}

func NewService(opts Options, normalizer Normalizer, backend InferenceBackend, logger *slog.Logger) (*Service, error) {
	if !opts.valid() || normalizer == nil || backend == nil || logger == nil {
		return nil, &domain.Failure{Kind: domain.InvalidInput}
	}

	stage, stopStage := context.WithCancel(context.Background())
	observer, stopObserver := context.WithCancel(context.Background())
	s := &Service{
		opts:       opts,
		normalizer: normalizer,
		backend:    backend,
		logger:     logger,

		wake:         make(chan struct{}, 1),
		healthCalls:  make(chan struct{}, 1),
		workerDone:   make(chan struct{}),
		observerDone: make(chan struct{}),
		done:         make(chan struct{}),
		shutdownDone: make(chan struct{}),

		staging:      stage,
		stopStaging:  stopStage,
		observer:     observer,
		stopObserver: stopObserver,

		health: Health{
			Status: domain.BackendStarting,
			Model:  opts.Model,
		},
	}

	go s.observe()
	go s.work()
	return s, nil
}

func (s *Service) Admit(parent context.Context, input Input) (result Result, err error) {
	s.mu.Lock()
	_, err = s.readinessLocked()
	if err == nil {
		s.calls.Add(1)
	}
	s.mu.Unlock()
	if err != nil {
		return result, err
	}

	defer s.calls.Done()
	if input.Model != s.opts.Model {
		return result, &domain.Failure{
			Kind:  domain.UnknownModel,
			Field: "model",
		}
	}

	stage, cancelStage := context.WithTimeout(parent, s.opts.OperationTimeout)
	stopStage := context.AfterFunc(s.staging, cancelStage)
	defer cancelStage()
	defer stopStage()

	audio, err := s.normalize(stage, input)
	owned := false
	defer func() {
		if !owned && audio.Audio != nil {
			closeErr := semantic(audio.Audio.Close())
			err = errors.Join(err, closeErr)
			if closeErr != nil {
				s.fail(closeErr)
			}
		}
	}()

	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if stopping {
		return result, &domain.Failure{
			Kind:  domain.ShuttingDown,
			Cause: err,
		}
	}
	if stage.Err() != nil {
		return result, &domain.Failure{
			Kind:  domain.DeadlineExceeded,
			Cause: stage.Err(),
		}
	}
	if err != nil {
		return result, semantic(err)
	}
	if audio.Audio == nil {
		return result, &domain.Failure{Kind: domain.Internal}
	}

	t, err := s.enqueue(parent, audio)
	if err != nil {
		return result, err
	}

	owned = true
	s.log(t.ctx, eventQueueAdmitted, input.RequestID)
	s.notify()
	<-t.done
	return t.result, t.err
}

func (s *Service) enqueue(parent context.Context, audio Normalized) (*ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.readinessLocked()
	if err != nil {
		return nil, err
	}

	count := len(s.queue)
	if s.active != nil {
		count++
	}
	if count >= s.opts.WaitingCapacity+1 {
		return nil, &domain.Failure{Kind: domain.QueueFull}
	}

	ctx, cancel := context.WithTimeout(parent, s.opts.OperationTimeout)
	t := &ticket{
		ctx:    ctx,
		cancel: cancel,
		audio:  audio,
		done:   make(chan struct{}),
	}
	t.stop = context.AfterFunc(ctx, func() {
		s.expireWaiting(t)
	})
	s.queue = append(s.queue, t)
	return t, nil
}

func (s *Service) normalize(ctx context.Context, input Input) (result Normalized, err error) {
	defer func() {
		if recover() != nil {
			err = &domain.Failure{Kind: domain.Internal}
		}
	}()
	return s.normalizer.Normalize(ctx, input)
}

func semantic(err error) error {
	if err == nil {
		return nil
	}

	var failure *domain.Failure
	if errors.As(err, &failure) && failure != nil {
		return err
	}
	return &domain.Failure{
		Kind:  domain.Internal,
		Cause: err,
	}
}

func (s *Service) complete(t *ticket, result Result, err error) {
	t.once.Do(func() {
		t.stop()
		t.cancel()
		closeErr := semantic(t.audio.Audio.Close())
		t.result, t.err = result, errors.Join(semantic(err), closeErr)
		if closeErr != nil {
			s.fail(closeErr)
		}

		s.log(t.ctx, eventQueueCompleted, t.audio.RequestID)
		close(t.done)
	})
}

func (s *Service) expireWaiting(t *ticket) {
	s.mu.Lock()
	for i, queued := range s.queue {
		if queued == t {
			copy(s.queue[i:], s.queue[i+1:])
			s.queue[len(s.queue)-1] = nil
			s.queue = s.queue[:len(s.queue)-1]
			s.mu.Unlock()
			s.complete(t, Result{}, &domain.Failure{
				Kind:  domain.DeadlineExceeded,
				Cause: t.ctx.Err(),
			})
			s.notify()
			return
		}
	}
	s.mu.Unlock()
}

func (s *Service) work() {
	defer close(s.workerDone)
	for {
		s.mu.Lock()
		if s.stopping && len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		_, readinessErr := s.readinessLocked()
		if len(s.queue) == 0 || readinessErr != nil {
			s.mu.Unlock()
			<-s.wake
			continue
		}
		t := s.queue[0]
		s.queue[0] = nil
		s.queue = s.queue[1:]
		s.active = t
		s.mu.Unlock()

		result, err := s.execute(t)
		s.complete(t, result, err)

		s.mu.Lock()
		s.active = nil
		s.mu.Unlock()
	}
}

func (s *Service) execute(t *ticket) (result Result, err error) {
	if t.ctx.Err() != nil {
		return result, s.cancelled(t)
	}

	h, err := s.readHealth(t.ctx)
	if t.ctx.Err() != nil {
		return result, s.cancelled(t)
	}
	if err != nil {
		return result, semantic(err)
	}
	if !s.compatible(h) || h.Status != domain.BackendReady || !h.CUDAReady {
		return result, &domain.Failure{Kind: domain.NotReady}
	}

	s.mu.Lock()
	t.generation, t.submitted = h.ProcessGeneration, true
	s.mu.Unlock()

	inferDone, watchDone := make(chan struct{}), make(chan struct{})
	stop := context.AfterFunc(t.ctx, func() {
		s.watchCleanup(t, inferDone, watchDone)
	})

	s.log(t.ctx, eventQueueDispatched, t.audio.RequestID)
	result, err = s.infer(t.ctx, t.audio)
	close(inferDone)
	if !stop() {
		<-watchDone
	}

	if t.ctx.Err() != nil {
		err = s.cancelled(t)
	}

	if requiresRecovery(err) {
		s.markRecovery(t)
		if recoveryErr := s.recoverBackend(t); recoveryErr != nil {
			s.fail(recoveryErr)
			err = errors.Join(semantic(err), recoveryErr)
		}
	}

	if err == nil {
		// Infer has returned and its cleanup watcher is stopped: confirm fresh
		// dependency state before retiring the owner of a cached Busy snapshot.
		// A health-only failure affects readiness, not the completed inference.
		_, _ = s.readHealth(t.ctx)
		if t.ctx.Err() != nil {
			// No native work remains to recover, but the end-to-end request budget
			// still applies while waiting for the final dependency observation.
			err = s.cancelled(t)
		}
	}

	return result, semantic(err)
}

func (s *Service) watchCleanup(t *ticket, inferDone <-chan struct{}, watchDone chan<- struct{}) {
	defer close(watchDone)

	s.markRecovery(t)
	s.mu.Lock()
	deadline := t.cleanupDeadline
	s.mu.Unlock()

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-inferDone:
	case <-timer.C:
		s.fail(&domain.Failure{
			Kind:  domain.BackendUnavailable,
			Cause: context.DeadlineExceeded,
		})
	}
}

func requiresRecovery(err error) bool {
	if err == nil {
		return false
	}

	var failure *domain.Failure
	if !errors.As(err, &failure) || failure == nil {
		return true
	}
	switch failure.Kind {
	case domain.DeadlineExceeded, domain.ShuttingDown, domain.BackendUnavailable, domain.Internal:
		return true
	default:
		return false
	}
}

func (s *Service) infer(ctx context.Context, audio Normalized) (result Result, err error) {
	defer func() {
		if recover() != nil {
			err = &domain.Failure{Kind: domain.Internal}
		}
	}()
	return s.backend.Infer(ctx, audio)
}

func (s *Service) cancelled(t *ticket) error {
	s.mu.Lock()
	forced := t.forced
	s.mu.Unlock()

	kind := domain.DeadlineExceeded
	if forced {
		kind = domain.ShuttingDown
	}
	return &domain.Failure{
		Kind:  kind,
		Cause: t.ctx.Err(),
	}
}

func (s *Service) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) log(ctx context.Context, event, id string) {
	s.logger.LogAttrs(ctx, slog.LevelDebug, event, slog.String("event", event), slog.String("request_id", id))
}
