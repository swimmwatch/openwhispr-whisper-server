package main

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/backend"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/httpapi"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/models"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 30 * time.Second
	maxHeaderBytes    = 64 << 10
	httpShutdownLimit = 5 * time.Second
	wavHeaderBytes    = 44
	wavBytesPerSecond = 32000
	maxRIFFFileBytes  = int64(math.MaxUint32) + 8
)

type runtimeOptions struct {
	nativeCommand, gpuCommand []string
	workRoot                  string
}

func main() {
	os.Exit(mainExit(os.Args[1:], os.Environ(), os.Stdout, runtimeOptions{
		nativeCommand: []string{"whisper-server"},
		gpuCommand:    []string{"nvidia-smi"},
		workRoot:      "/tmp/whisper-backend",
	}))
}

func mainExit(args, environ []string, out io.Writer, runtime runtimeOptions) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, args, environ, out, runtime); err != nil {
		return 1
	}
	return 0
}

func run(ctx context.Context, args, environ []string, out io.Writer, runtime runtimeOptions) (result error) {
	logger, _ := logging.New(out, "info", "startup")
	logging.Emit(logger, ctx, slog.LevelInfo, logging.Event{Name: logging.Startup})
	defer func() {
		if result != nil {
			event, code := logging.BackendFailed, httpapi.MapFailure(result).Code
			if errors.Is(result, config.ErrInvalid) {
				event, code = logging.ConfigurationRejected, "invalid_request"
			}
			logging.Emit(logger, context.Background(), slog.LevelError, logging.Event{Name: event, ErrorCode: code})
		} else {
			logging.Emit(logger, context.Background(), slog.LevelInfo, logging.Event{Name: logging.BackendStopped})
		}
	}()

	settings, err := config.Load(args, environ)
	if err != nil {
		return err
	}
	configuredLogger, err := logging.New(out, settings.Logging.Level, "backend")
	if err != nil {
		return config.ErrInvalid
	}
	logger = configuredLogger

	artifact, err := models.Provision(ctx, models.Options{
		Models:   settings.Models,
		CacheDir: settings.Storage.CacheDir,
		Attempts: settings.Downloads.Attempts,
		Timeout:  time.Duration(settings.Timeouts.ProvisioningSeconds) * time.Second,
	}, &http.Client{})
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	fingerprint, err := config.CanonicalFingerprint(settings)
	if err != nil {
		return err
	}

	waveBytes := maxWAVBytes(settings.Limits.DecodedSeconds)
	options := backend.Options{
		NativeCommand:    runtime.nativeCommand,
		GPUCommand:       runtime.gpuCommand,
		Environment:      environ,
		Artifact:         artifact,
		Fingerprint:      fingerprint,
		NativePort:       settings.Backend.NativePort,
		GPUDevice:        settings.Backend.GPUDevice,
		WorkRoot:         runtime.workRoot,
		Logger:           logger,
		OperationTimeout: time.Duration(settings.Timeouts.OperationSeconds) * time.Second,
		CleanupTimeout:   time.Duration(settings.Timeouts.CleanupSeconds) * time.Second,
		ShutdownTimeout:  time.Duration(settings.Timeouts.ShutdownSeconds) * time.Second,
		MaxWAVBytes:      waveBytes,
		MaxReplyBytes:    waveBytes,
		PromptBytes:      settings.Limits.PromptBytes,
	}
	supervisor, err := backend.New(options)
	if err != nil {
		return err
	}
	return serve(ctx, supervisor, settings.Backend.Port, options.ShutdownTimeout, options.CleanupTimeout)
}

func serve(ctx context.Context, supervisor *backend.Supervisor, port int, drain, cleanup time.Duration) error {
	if ctx.Err() != nil {
		return nil
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		return &domain.Failure{Kind: domain.BackendUnavailable, Cause: err}
	}

	server := &http.Server{
		Handler:           supervisor.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          log.New(io.Discard, "", 0),
	}

	// Signal cancellation starts a drain; it must not cancel the native owner first.
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	nativeDone := make(chan error, 1)
	httpDone := make(chan error, 1)
	go func() {
		nativeDone <- supervisor.Run(life)
	}()
	go func() {
		httpDone <- server.Serve(listener)
	}()

	var nativeErr, httpErr error
	nativeFinished, httpFinished := false, false
	select {
	case <-ctx.Done():
	case nativeErr = <-nativeDone:
		nativeFinished = true
	case httpErr = <-httpDone:
		httpFinished = true
	}

	stopCtx, stop := context.WithTimeout(context.Background(), shutdownBudget(drain, cleanup))
	stopErr := supervisor.Shutdown(stopCtx)
	stop()

	// Run owns bounded child retirement; wait for its actual result before exit.
	cancel()
	if !nativeFinished {
		nativeErr = <-nativeDone
	}

	httpCtx, stopHTTP := context.WithTimeout(context.Background(), min(httpShutdownLimit, cleanup))
	httpStopErr := server.Shutdown(httpCtx)
	stopHTTP()
	if httpStopErr != nil {
		httpStopErr = errors.Join(httpStopErr, server.Close())
	}
	if !httpFinished {
		httpErr = <-httpDone
	}
	if errors.Is(httpErr, http.ErrServerClosed) {
		httpErr = nil
	}
	if httpErr != nil || httpStopErr != nil {
		httpErr = &domain.Failure{Kind: domain.BackendUnavailable, Cause: errors.Join(httpErr, httpStopErr)}
	}

	return errors.Join(nativeErr, stopErr, httpErr)
}

func shutdownBudget(drain, cleanup time.Duration) time.Duration {
	var total time.Duration
	for _, part := range []time.Duration{drain, cleanup, httpShutdownLimit} {
		if part > time.Duration(math.MaxInt64)-total {
			return time.Duration(math.MaxInt64)
		}
		total += part
	}
	return total
}

// Classic RIFF/WAV has a 32-bit size field, even with a larger duration budget.
func maxWAVBytes(decodedSeconds int) int64 {
	if int64(decodedSeconds) > (maxRIFFFileBytes-wavHeaderBytes)/wavBytesPerSecond {
		return maxRIFFFileBytes
	}
	return wavHeaderBytes + int64(decodedSeconds)*wavBytesPerSecond
}
