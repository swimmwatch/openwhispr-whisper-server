package transcription

import (
	"context"
	"log/slog"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func (s *Service) compatible(h Health) bool {
	return h.ProtocolVersion == backendProtocolVersion &&
		h.ConfigurationFingerprint == s.opts.Fingerprint &&
		h.Model == s.opts.Model &&
		h.ProcessGeneration != ""
}

func (s *Service) Readiness(ctx context.Context) (Health, error) {
	if ctx.Err() != nil {
		return Health{}, &domain.Failure{
			Kind:  domain.DeadlineExceeded,
			Cause: ctx.Err(),
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readinessLocked()
}

func (s *Service) readinessLocked() (Health, error) {
	h := s.health
	if s.stopping {
		h.CUDAReady = false
		return h, &domain.Failure{
			Kind:  domain.ShuttingDown,
			Cause: s.terminal,
		}
	}
	if s.healthErr != nil {
		h.CUDAReady = false
		return h, semantic(s.healthErr)
	}

	ownedBusy := h.Status == domain.BackendBusy &&
		s.active != nil &&
		s.active.submitted &&
		s.active.generation == h.ProcessGeneration
	if s.recovering ||
		s.observed.IsZero() ||
		time.Since(s.observed) > s.opts.HealthTimeout ||
		!s.compatible(h) ||
		!h.CUDAReady ||
		h.Status != domain.BackendReady && !ownedBusy {
		h.CUDAReady = false
		return h, &domain.Failure{Kind: domain.NotReady}
	}
	return h, nil
}

func (s *Service) readHealth(parent context.Context) (h Health, err error) {
	ctx, cancel := context.WithTimeout(parent, s.opts.HealthTimeout)
	defer cancel()
	// Serialize only dependency observations, never inference or cached public
	// readiness. Commit each result before releasing the gate so an earlier
	// in-flight Busy response cannot overwrite a later completion refresh.
	select {
	case s.healthCalls <- struct{}{}:
	case <-ctx.Done():
		return Health{}, &domain.Failure{Kind: domain.BackendUnavailable, Cause: ctx.Err()}
	}
	defer func() { <-s.healthCalls }()
	defer func() {
		if recover() != nil {
			err = &domain.Failure{Kind: domain.Internal}
		}
		if ctx.Err() != nil {
			h, err = Health{}, &domain.Failure{Kind: domain.BackendUnavailable, Cause: ctx.Err()}
		}
		s.mu.Lock()
		s.health, s.healthErr, s.observed = h, err, time.Now()
		if err == nil && s.compatible(h) && h.CUDAReady && h.Status == domain.BackendReady && s.unsafeGeneration == "" {
			s.recovering = false
		}
		s.mu.Unlock()
	}()

	if ctx.Err() != nil {
		return Health{}, &domain.Failure{Kind: domain.BackendUnavailable, Cause: ctx.Err()}
	}
	return s.backend.Health(ctx)
}

func (s *Service) observe() {
	defer close(s.observerDone)

	ticker := time.NewTicker(s.opts.HealthInterval)
	defer ticker.Stop()

	for {
		_, _ = s.readHealth(s.observer)
		s.logReadiness()
		s.notify()
		select {
		case <-s.observer.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) markRecovery(t *ticket) {
	s.mu.Lock()
	changed := false
	if s.active == t && t.submitted {
		changed = !s.recovering
		s.recovering, s.unsafeGeneration = true, t.generation
		if t.cleanupDeadline.IsZero() {
			t.cleanupDeadline = time.Now().Add(s.opts.CleanupTimeout)
		}
	}
	s.mu.Unlock()

	if changed {
		s.logReadiness()
		s.logger.LogAttrs(t.ctx, slog.LevelInfo, eventBackendRecovering,
			slog.String("event", eventBackendRecovering),
			slog.String("request_id", t.audio.RequestID),
		)
	}
}

func (s *Service) logReadiness() {
	s.mu.Lock()
	_, err := s.readinessLocked()
	ready := err == nil
	changed := ready != s.announcedReady
	s.announcedReady = ready
	s.mu.Unlock()

	if changed {
		event := eventAPINotReady
		if ready {
			event = eventAPIReady
		}
		s.logger.LogAttrs(context.Background(), slog.LevelInfo, event, slog.String("event", event))
	}
}

func (s *Service) recoverBackend(t *ticket) error {
	s.mu.Lock()
	deadline := t.cleanupDeadline
	s.mu.Unlock()

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	for {
		h, err := s.readHealth(ctx)
		if ctx.Err() == nil && err == nil && s.compatible(h) && h.ProcessGeneration != t.generation {
			// P02 creates the next generation only after old work is stopped and reaped.
			s.mu.Lock()
			s.unsafeGeneration = ""
			s.recovering = s.healthErr != nil || !s.compatible(s.health) || !s.health.CUDAReady || s.health.Status != domain.BackendReady
			s.mu.Unlock()
			s.logReadiness()
			s.notify()
			return nil
		}
		timer := time.NewTimer(s.opts.HealthInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &domain.Failure{
				Kind:  domain.BackendUnavailable,
				Cause: ctx.Err(),
			}
		case <-timer.C:
		}
	}
}

// Done closes on terminal failure or completed shutdown so the command can exit.
func (s *Service) Done() <-chan struct{} {
	return s.done
}

func (s *Service) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminal
}

func (s *Service) fail(err error) {
	s.mu.Lock()
	if s.terminal != nil {
		s.mu.Unlock()
		return
	}

	s.terminal, s.stopping = semantic(err), true
	waiting, active := s.queue, s.active
	s.queue = nil
	s.mu.Unlock()

	s.doneOnce.Do(func() {
		close(s.done)
	})

	s.stopStaging()
	s.stopObserver()
	if active != nil {
		active.cancel()
	}

	for _, t := range waiting {
		s.complete(t, Result{}, &domain.Failure{
			Kind:  domain.BackendUnavailable,
			Cause: err,
		})
	}

	s.notify()
	s.logReadiness()
}

func (s *Service) Shutdown(ctx context.Context) error {
	if err := s.Err(); err != nil {
		return err
	}

	s.stopOnce.Do(func() {
		go s.shutdown(ctx)
	})

	select {
	case <-s.shutdownDone:
		return s.shutdownErr
	case <-s.done:
		return s.Err()
	case <-ctx.Done():
		return &domain.Failure{
			Kind:  domain.DeadlineExceeded,
			Cause: ctx.Err(),
		}
	}
}

func (s *Service) shutdown(ctx context.Context) {
	defer close(s.shutdownDone)

	s.mu.Lock()
	s.stopping = true
	waiting, active := s.queue, s.active
	s.queue = nil
	s.mu.Unlock()

	s.stopStaging()
	s.logReadiness()

	for _, t := range waiting {
		s.complete(t, Result{}, &domain.Failure{Kind: domain.ShuttingDown})
	}
	s.notify()

	if active != nil {
		timer := time.NewTimer(s.opts.ShutdownTimeout)
		select {
		case <-active.done:
		case <-timer.C:
		case <-ctx.Done():
		case <-s.done:
		}
		timer.Stop()
		s.mu.Lock()
		active.forced = true
		s.mu.Unlock()
		active.cancel()
	}

	finished := make(chan struct{})
	go func() {
		<-s.workerDone
		s.calls.Wait()
		s.stopObserver()
		<-s.observerDone
		close(finished)
	}()

	timer := time.NewTimer(s.opts.CleanupTimeout)
	defer timer.Stop()
	select {
	case <-finished:
	case <-s.done:
	case <-timer.C:
		s.fail(&domain.Failure{
			Kind:  domain.BackendUnavailable,
			Cause: context.DeadlineExceeded,
		})
	}

	s.shutdownErr = s.Err()

	s.doneOnce.Do(func() {
		close(s.done)
	})
}
