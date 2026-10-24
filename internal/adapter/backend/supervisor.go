package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/models"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const (
	backendProtocolVersion = "1"
	multipartOverheadBytes = 64 << 10
	nativeDialTimeout      = 2 * time.Second
	nativeIdleTimeout      = 30 * time.Second
	nativeIdleConnections  = 4
	nativeHealthInterval   = 5 * time.Second
	nativeRecoveryDelay    = 100 * time.Millisecond
)

type Options struct {
	NativeCommand, GPUCommand, Environment            []string
	Artifact                                          models.Artifact
	Fingerprint                                       string
	NativePort, GPUDevice                             int
	WorkRoot                                          string
	OperationTimeout, CleanupTimeout, ShutdownTimeout time.Duration
	MaxWAVBytes, MaxReplyBytes                        int64
	PromptBytes                                       int
	Logger                                            *slog.Logger
}

type Request struct {
	Audio         io.Reader
	Size          int64
	Fields        domain.InferenceFields
	Compatibility domain.BackendCompatibility
}

type Supervisor struct {
	opts              Options
	mu                sync.Mutex
	status            domain.BackendState
	started, stopping bool
	current           *nativeChild
	active            *operation
	area              *workArea
	stop, done        chan struct{}
	recover           chan *nativeChild
	client            *http.Client
	transport         *http.Transport
}

func New(opts Options) (*Supervisor, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}

	opts.NativeCommand = slices.Clone(opts.NativeCommand)
	opts.GPUCommand = slices.Clone(opts.GPUCommand)
	if opts.Environment == nil {
		opts.Environment = os.Environ()
	}
	opts.Environment = slices.Clone(opts.Environment)

	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: min(nativeDialTimeout, opts.OperationTimeout)}).DialContext,
		MaxIdleConns:        nativeIdleConnections,
		MaxIdleConnsPerHost: nativeIdleConnections,
		IdleConnTimeout:     nativeIdleTimeout,
	}

	s := &Supervisor{
		opts:      opts,
		status:    domain.BackendStarting,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		recover:   make(chan *nativeChild, 1),
		transport: transport,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	return s, nil
}

func validateOptions(opts Options) error {
	if len(opts.NativeCommand) == 0 || opts.NativeCommand[0] == "" || len(opts.GPUCommand) == 0 ||
		opts.GPUCommand[0] == "" ||
		opts.Logger == nil {
		return failure(domain.InvalidInput, errNative)
	}
	if opts.NativePort < 1 || opts.NativePort > math.MaxUint16 || opts.GPUDevice < 0 || !filepath.IsAbs(opts.WorkRoot) ||
		!filepath.IsAbs(opts.Artifact.Path) {
		return failure(domain.InvalidInput, errNative)
	}
	if !logging.ValidRequestID(opts.Artifact.ID) || !validDigest(opts.Fingerprint) || !validDigest(opts.Artifact.SHA256) ||
		opts.Artifact.Metadata.VocabularySize <= 0 {
		return failure(domain.InvalidInput, errNative)
	}
	if opts.OperationTimeout <= 0 || opts.CleanupTimeout <= 0 || opts.ShutdownTimeout <= 0 ||
		opts.MaxWAVBytes < canonicalWAVMinimumBytes ||
		opts.MaxReplyBytes < 1 ||
		opts.PromptBytes <= 0 {
		return failure(domain.InvalidInput, errNative)
	}
	if opts.MaxWAVBytes > math.MaxInt64-int64(opts.PromptBytes)-multipartOverheadBytes ||
		pathWithin(opts.WorkRoot, opts.Artifact.Path) {
		return failure(domain.InvalidInput, errNative)
	}
	return nil
}

func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil &&
		(rel == "." || rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Run owns the entire native lifecycle and is called exactly once by assembly.
func (s *Supervisor) Run(ctx context.Context) (result error) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return failure(domain.Internal, errNative)
	}
	s.started = true
	s.mu.Unlock()

	defer func() {
		s.transport.CloseIdleConnections()
		s.mu.Lock()
		if !s.stopping {
			s.status = domain.BackendFailed
		}
		s.mu.Unlock()
		close(s.done)
	}()

	area, err := openWork(s.opts.WorkRoot)
	if err != nil {
		return failure(domain.ProvisioningFailed, err)
	}
	s.area = area

	defer func() {
		if err := area.close(); err != nil {
			result = errors.Join(result, failure(domain.ProvisioningFailed, err))
		}
	}()

	for {
		if s.isStopping() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return failure(domain.ShuttingDown, err)
		}

		child, err := s.start(ctx)
		if err != nil {
			if child != nil {
				err = errors.Join(err, s.retire(child))
			}
			if s.isStopping() {
				return errIfRetirementFailed(err)
			}
			return err
		}

		ticker := time.NewTicker(max(time.Millisecond, min(nativeHealthInterval, s.opts.OperationTimeout/4)))
		stop := false
		for !stop {
			select {
			case <-s.stop:
				ticker.Stop()
				return s.drainAndStop(ctx, child, false)
			case <-ctx.Done():
				ticker.Stop()
				return errors.Join(failure(domain.ShuttingDown, ctx.Err()), s.drainAndStop(ctx, child, true))
			case <-child.done:
				s.requestRecovery(child)
				stop = true
			case reported := <-s.recover:
				if reported == child {
					stop = true
				}
			case <-ticker.C:
				if err := s.checkHealth(child.ctx, child); err != nil {
					s.requestRecovery(child)
					stop = true
				}
			}
		}

		ticker.Stop()
		if err := s.retire(child); err != nil {
			return err
		}
		if err := s.waitOperation(child); err != nil {
			return err
		}
		if err := pause(ctx, min(nativeRecoveryDelay, s.opts.OperationTimeout/10), s.stop); err != nil {
			if s.isStopping() {
				return nil
			}
			return failure(domain.ShuttingDown, err)
		}
	}
}

// A normal startup interrupted by Shutdown is not itself a service failure.
func errIfRetirementFailed(err error) error {
	var retirement *retirementError
	if errors.As(err, &retirement) {
		return failure(domain.BackendUnavailable, retirement)
	}
	return nil
}

func (s *Supervisor) isStopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

func (s *Supervisor) Health(ctx context.Context) (domain.BackendHealth, error) {
	if err := ctx.Err(); err != nil {
		return domain.BackendHealth{}, failure(domain.NotReady, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	h := domain.BackendHealth{
		BackendCompatibility: domain.BackendCompatibility{
			ProtocolVersion:          backendProtocolVersion,
			ConfigurationFingerprint: s.opts.Fingerprint,
		},
		Status:       s.status,
		Model:        s.opts.Artifact.ID,
		Multilingual: s.opts.Artifact.Metadata.Multilingual,
	}

	if s.current != nil {
		h.ProcessGeneration = s.current.generation
	}

	h.CUDAReady = (s.status == domain.BackendReady || s.status == domain.BackendBusy) && !s.stopping
	return h, nil
}

func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopping {
		s.stopping = true
		s.status = domain.BackendStopping
		close(s.stop)
	}
	started := s.started
	s.mu.Unlock()

	if !started {
		return nil
	}

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return failure(domain.DeadlineExceeded, ctx.Err())
	}
}

func (s *Supervisor) requestRecovery(child *nativeChild) {
	s.mu.Lock()
	if s.current != child {
		s.mu.Unlock()
		return
	}

	eventCtx := context.Background()
	if s.active != nil {
		eventCtx = s.active.ctx
	}
	if child.retireAt.IsZero() {
		child.retireAt = time.Now().Add(s.opts.CleanupTimeout)
	}
	if !s.stopping {
		s.status = domain.BackendRecovering
	}

	child.cancel()

	select {
	case s.recover <- child:
	default:
	}
	s.mu.Unlock()

	s.emit(eventCtx, logging.BackendRecovering, "")
}

func (s *Supervisor) drainAndStop(ctx context.Context, child *nativeChild, immediate bool) error {
	s.mu.Lock()
	s.stopping = true
	s.status = domain.BackendStopping
	op := s.active
	s.mu.Unlock()

	s.emit(context.Background(), logging.BackendStopping, "")

	if op != nil && !immediate {
		timer := time.NewTimer(s.opts.ShutdownTimeout)
		select {
		case <-op.finished:
		case <-op.ctx.Done():
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
	}
	if op != nil {
		op.cancel()
	}

	err := s.retire(child)
	return errors.Join(err, s.waitOperation(child))
}

func (s *Supervisor) waitOperation(child *nativeChild) error {
	s.mu.Lock()
	op := s.active
	s.mu.Unlock()

	if op == nil || op.child != child {
		return nil
	}

	s.mu.Lock()
	remaining := time.Until(child.retireAt)
	s.mu.Unlock()

	if remaining <= 0 {
		return failure(domain.BackendUnavailable, context.DeadlineExceeded)
	}

	timer := time.NewTimer(remaining)
	defer timer.Stop()

	select {
	case <-op.finished:
		return nil
	case <-timer.C:
		return failure(domain.BackendUnavailable, context.DeadlineExceeded)
	}
}

func (s *Supervisor) emit(ctx context.Context, event logging.EventName, code string) {
	logging.Emit(s.opts.Logger, ctx, slog.LevelInfo, logging.Event{Name: event, ErrorCode: code})
}

func failure(kind domain.FailureKind, cause error) error {
	return &domain.Failure{Kind: kind, Cause: cause}
}

func pause(ctx context.Context, duration time.Duration, stop <-chan struct{}) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-stop:
		return context.Canceled
	case <-timer.C:
		return nil
	}
}
