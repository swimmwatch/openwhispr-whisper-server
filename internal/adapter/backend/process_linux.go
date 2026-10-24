package backend

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const (
	maxNativeHealthBytes      = 1024
	nativeLoopbackHost        = "127.0.0.1"
	nativeStartupPollInterval = 10 * time.Millisecond
	nativeWaitDelay           = 100 * time.Millisecond
	nativeHealthTimeout       = 2 * time.Second
	nativeStopGrace           = time.Second
)

type nativeChild struct {
	cmd              *exec.Cmd
	evidence         *nativeEvidence
	ctx              context.Context
	cancel           context.CancelFunc
	done, retired    chan struct{}
	waitErr          error
	generation, uuid string
	retireAt         time.Time // Protected by Supervisor.mu.
}

func (s *Supervisor) nativeAddress() string {
	return net.JoinHostPort(nativeLoopbackHost, strconv.Itoa(s.opts.NativePort))
}

func (s *Supervisor) nativeArgs() []string {
	language := "en"
	if s.opts.Artifact.Metadata.Multilingual {
		language = "auto"
	}

	return append(append([]string{}, s.opts.NativeCommand[1:]...),
		"--host", nativeLoopbackHost,
		"--port", strconv.Itoa(s.opts.NativePort),
		"--model", s.opts.Artifact.Path,
		"--device", strconv.Itoa(s.opts.GPUDevice),
		"--language", language,
		"--no-timestamps", "--no-language-probabilities",
		"--tmp-dir", s.area.path,
		"--public", filepath.Join(s.area.path, "public"),
	)
}

func (s *Supervisor) start(parent context.Context) (*nativeChild, error) {
	ctx, cancel := context.WithTimeout(parent, s.opts.OperationTimeout)
	defer cancel()

	listener, err := net.Listen("tcp4", s.nativeAddress())
	if err != nil {
		return nil, failure(domain.BackendUnavailable, err)
	}
	if err := listener.Close(); err != nil {
		return nil, failure(domain.BackendUnavailable, err)
	}

	life, stop := context.WithCancel(parent)
	child := &nativeChild{
		ctx:        life,
		cancel:     stop,
		generation: rand.Text(),
		done:       make(chan struct{}),
		retired:    make(chan struct{}),
		evidence:   &nativeEvidence{device: s.opts.GPUDevice},
	}

	child.cmd = exec.Command(s.opts.NativeCommand[0], s.nativeArgs()...)
	child.cmd.Env = append([]string{}, s.opts.Environment...)
	child.cmd.Dir = s.area.path
	child.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.cmd.Stdout = child.evidence
	child.cmd.Stderr = child.evidence
	child.cmd.WaitDelay = max(time.Millisecond, min(nativeWaitDelay, s.opts.CleanupTimeout/4))
	if err := child.cmd.Start(); err != nil {
		stop()
		return nil, failure(domain.BackendUnavailable, err)
	}

	s.mu.Lock()
	s.current = child
	if !s.stopping {
		s.status = domain.BackendStarting
	}
	s.mu.Unlock()

	s.emit(context.Background(), logging.BackendStarting, "")
	go func() {
		child.waitErr = child.cmd.Wait()
		child.evidence.discard()
		close(child.done)
	}()

	for {
		select {
		case <-s.stop:
			return child, failure(domain.ShuttingDown, context.Canceled)
		case <-ctx.Done():
			kind := domain.BackendUnavailable
			if !child.evidence.snapshot(s.opts.GPUDevice).ready {
				kind = domain.GPUUnavailable
			}
			return child, failure(kind, ctx.Err())
		case <-child.done:
			return child, failure(domain.BackendUnavailable, child.waitErr)
		default:
		}
		if child.evidence.snapshot(s.opts.GPUDevice).ready {
			if err := s.checkHealth(ctx, child); err == nil {
				s.mu.Lock()
				if !s.stopping {
					s.status = domain.BackendReady
				}
				s.mu.Unlock()
				s.emit(context.Background(), logging.BackendReady, "")
				return child, nil
			} else if errors.Is(err, errGPU) {
				return child, failure(domain.GPUUnavailable, err)
			}
		}
		if err := pause(ctx, nativeStartupPollInterval, s.stop); err != nil {
			continue
		}
	}
}

func (s *Supervisor) checkHealth(parent context.Context, child *nativeChild) error {
	ctx, cancel := context.WithTimeout(
		parent,
		max(time.Millisecond, min(nativeHealthTimeout, s.opts.CleanupTimeout/4, s.opts.OperationTimeout/4)),
	)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.nativeAddress()+"/health", nil)
	if err != nil {
		return errNative
	}

	response, err := s.client.Do(request)
	if err != nil {
		return errNative
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errNative
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxNativeHealthBytes+1))
	if err != nil || len(body) > maxNativeHealthBytes {
		return errNative
	}

	var health struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &health) != nil || health.Status != "ok" {
		return errNative
	}

	output, err := s.gpuQuery(ctx)
	if err != nil {
		return errGPU
	}

	identity, err := matchGPU(output, child.evidence.snapshot(s.opts.GPUDevice))
	if err != nil {
		return errGPU
	}
	if child.uuid != "" && child.uuid != identity {
		return errGPU
	}

	child.uuid = identity

	select {
	case <-child.done:
		return errNative
	default:
	}
	return nil
}

// GPU query output is bounded, never logged and never attached to public errors.
func (s *Supervisor) gpuQuery(ctx context.Context) ([]byte, error) {
	args := append(
		append([]string{}, s.opts.GPUCommand[1:]...),
		"--query-gpu=uuid,name,compute_cap",
		"--format=csv,noheader,nounits",
	)
	cmd := exec.CommandContext(ctx, s.opts.GPUCommand[0], args...)
	cmd.Env = append([]string{}, s.opts.Environment...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}

	cmd.WaitDelay = max(time.Millisecond, min(nativeWaitDelay, s.opts.CleanupTimeout/4))
	output := &limitedOutput{limit: maxGPUQueryBytes}
	cmd.Stdout = output
	cmd.Stderr = io.Discard

	err := cmd.Run()
	if err != nil || output.overflow {
		return nil, errGPU
	}
	return output.buffer.Bytes(), nil
}

type limitedOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (o *limitedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	n := len(p)
	if len(p) > o.limit-o.buffer.Len() {
		o.overflow = true
		p = p[:o.limit-o.buffer.Len()]
	}

	_, _ = o.buffer.Write(p)
	return n, nil
}

type retirementError struct{ cause error }

func (e *retirementError) Error() string {
	return "native cleanup failed"
}

func (e *retirementError) Unwrap() error {
	return e.cause
}

// Only Run calls retire. Infer waits for retired, never waits/reaps independently.
func (s *Supervisor) retire(child *nativeChild) (result error) {
	child.cancel()

	s.mu.Lock()
	if child.retireAt.IsZero() {
		child.retireAt = time.Now().Add(s.opts.CleanupTimeout)
	}
	deadline := child.retireAt
	s.mu.Unlock()

	defer func() {
		if result != nil {
			result = failure(domain.BackendUnavailable, result)
		} else {
			close(child.retired)
		}
	}()

	pid := child.cmd.Process.Pid
	signalGroup := func(signal syscall.Signal) error {
		err := syscall.Kill(-pid, signal)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}

	if err := signalGroup(syscall.SIGTERM); err != nil {
		return &retirementError{err}
	}

	grace := time.NewTimer(max(time.Millisecond, min(nativeStopGrace, time.Until(deadline)/2)))

	select {
	case <-child.done:
	case <-grace.C:
	}
	grace.Stop()

	if err := signalGroup(syscall.SIGKILL); err != nil {
		return &retirementError{err}
	}

	remaining := time.Until(deadline)
	if remaining <= 0 {
		return &retirementError{context.DeadlineExceeded}
	}

	timer := time.NewTimer(remaining)
	defer timer.Stop()

	select {
	case <-child.done:
	case <-timer.C:
		return &retirementError{context.DeadlineExceeded}
	}

	// A vanished leader is insufficient if another member still owns GPU work.
	for {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		} else if err != nil {
			return &retirementError{err}
		}
		if time.Now().After(deadline) {
			return &retirementError{context.DeadlineExceeded}
		}
		time.Sleep(time.Millisecond)
	}
}
