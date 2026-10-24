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

	"github.com/labstack/echo/v5"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/audio"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/httpapi"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/upstream"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

const (
	readHeaderTimeout   = 5 * time.Second
	idleTimeout         = 30 * time.Second
	maxHeaderBytes      = 64 << 10
	healthInterval      = 100 * time.Millisecond
	fatalExitWait       = 100 * time.Millisecond
	uploadCloseInterval = 5 * time.Millisecond
	wavHeaderBytes      = 44
	wavBytesPerSecond   = 32000
	maxRIFFFileBytes    = int64(math.MaxUint32) + 8
)

type runtimeOptions struct {
	decoderCommand []string
	workRoot       string
}

func main() {
	runtime := runtimeOptions{
		decoderCommand: []string{"ffmpeg"},
		workRoot:       "/tmp/whisper-api",
	}

	os.Exit(mainExit(os.Args[1:], os.Environ(), os.Stdout, runtime))
}

func mainExit(args, environ []string, out io.Writer, runtime runtimeOptions) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if run(ctx, args, environ, out, runtime) != nil {
		return 1
	}
	return 0
}

func run(ctx context.Context, args, environ []string, out io.Writer, runtime runtimeOptions) (result error) {
	logger, _ := logging.New(out, "info", "startup")
	logging.Emit(logger, ctx, slog.LevelInfo, logging.Event{Name: logging.Startup})
	defer func() {
		if errors.Is(result, config.ErrInvalid) {
			logging.Emit(
				logger,
				context.Background(),
				slog.LevelError,
				logging.Event{Name: logging.ConfigurationRejected, ErrorCode: "invalid_request"},
			)
		} else if result != nil {
			logger.LogAttrs(
				context.Background(),
				slog.LevelError,
				"api_failed",
				slog.String("event", "api_failed"),
				slog.String("error_code", httpapi.MapFailure(result).Code),
			)
		} else {
			logger.LogAttrs(context.Background(), slog.LevelInfo, "api_stopped", slog.String("event", "api_stopped"))
		}
	}()

	settings, err := config.Load(args, environ)
	if err != nil {
		return err
	}
	logger, err = logging.New(out, settings.Logging.Level, "api")
	if err != nil {
		return config.ErrInvalid
	}
	if ctx.Err() != nil {
		return nil
	}

	fingerprint, err := config.CanonicalFingerprint(settings)
	if err != nil {
		return err
	}

	operation := time.Duration(settings.Timeouts.OperationSeconds) * time.Second
	cleanup := time.Duration(settings.Timeouts.CleanupSeconds) * time.Second
	drain := time.Duration(settings.Timeouts.ShutdownSeconds) * time.Second
	waveBytes := maxWAVBytes(settings.Limits.DecodedSeconds)
	client, err := upstream.New(upstream.Options{
		URL:              settings.API.BackendURL,
		Fingerprint:      fingerprint,
		Model:            settings.Models.Served,
		OperationTimeout: operation,
		MaxWAVBytes:      waveBytes,
		MaxReplyBytes:    waveBytes,
		PromptBytes:      settings.Limits.PromptBytes,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	decoder, err := audio.New(audio.Options{
		Command:          runtime.decoderCommand,
		AudioBytes:       settings.Limits.AudioBytes,
		DecodedSeconds:   settings.Limits.DecodedSeconds,
		OperationTimeout: operation,
		CleanupTimeout:   cleanup,
		Capacity:         settings.Queue.WaitingCapacity + 1,
	})
	if err != nil {
		return err
	}

	healthTimeout := min(time.Second, operation)
	service, err := transcription.NewService(transcription.Options{
		Model:            settings.Models.Served,
		Fingerprint:      fingerprint,
		WaitingCapacity:  settings.Queue.WaitingCapacity,
		OperationTimeout: operation,
		CleanupTimeout:   cleanup,
		ShutdownTimeout:  drain,
		HealthTimeout:    healthTimeout,
		HealthInterval:   min(healthInterval, healthTimeout/2),
	}, decoder, client, logger)
	if err != nil {
		return err
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), shutdownBudget(drain, cleanup))
		defer cancel()
		result = errors.Join(result, service.Shutdown(stop))
	}()

	handler, err := httpapi.New(httpapi.Options{
		Model:            settings.Models.Served,
		WorkRoot:         runtime.workRoot,
		AudioBytes:       settings.Limits.AudioBytes,
		PromptBytes:      settings.Limits.PromptBytes,
		WaitingCapacity:  settings.Queue.WaitingCapacity,
		OperationTimeout: operation,
	}, service)
	if err != nil {
		return err
	}
	defer func() {
		result = errors.Join(result, handler.Close())
	}()

	e := echo.New()
	if err := httpapi.Install(e, logger, settings.API, httpapi.NewMemoryStore(settings.API.RateLimit)); err != nil {
		return err
	}
	handler.Register(e)

	listener, err := net.Listen("tcp", net.JoinHostPort(settings.API.Host, strconv.Itoa(settings.API.Port)))
	if err != nil {
		return &domain.Failure{Kind: domain.Internal, Cause: err}
	}
	return serve(ctx, listener, e, service, handler, drain, cleanup, logger)
}

func serve(
	ctx context.Context,
	listener net.Listener,
	handler http.Handler,
	service *transcription.Service,
	uploads *httpapi.Handler,
	drain, cleanup time.Duration,
	logger *slog.Logger,
) error {
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext: func(net.Listener) context.Context {
			return requests
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}

	httpDone := make(chan error, 1)
	go func() {
		httpDone <- server.Serve(listener)
	}()

	var result error
	received := false
	select {
	case <-ctx.Done():
	case <-service.Done():
		result = service.Err()
		if result != nil {
			// Cleanup has already failed. Exit without draining or freeing in-use work.
			cancelRequests()
			_ = server.Close()
			timer := time.NewTimer(min(cleanup, fatalExitWait))
			defer timer.Stop()
			select {
			case <-httpDone:
			case <-timer.C:
			}
			return result
		}
	case result = <-httpDone:
		received = true
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	}

	logger.LogAttrs(context.Background(), slog.LevelInfo, "api_stopping", slog.String("event", "api_stopping"))
	stop, cancel := context.WithTimeout(context.Background(), shutdownBudget(drain, cleanup))
	defer cancel()

	httpStopped := make(chan error, 1)
	go func() {
		grace, cancel := context.WithTimeout(context.Background(), drain)
		defer cancel()
		err := server.Shutdown(grace)
		if err != nil {
			cancelRequests()
			_ = server.Close()
		}
		httpStopped <- err
	}()

	serviceErr := service.Shutdown(stop)
	select {
	case err := <-httpStopped:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			result = errors.Join(result, err)
		}
	case <-stop.Done():
		result = errors.Join(result, stop.Err())
	}

	cancelRequests()
	_ = server.Close()
	if !received {
		select {
		case err := <-httpDone:
			if !errors.Is(err, http.ErrServerClosed) {
				result = errors.Join(result, err)
			}
		case <-stop.Done():
			result = errors.Join(result, stop.Err())
		}
	}

	for {
		err := uploads.Close()
		if err == nil {
			break
		}
		var failure *domain.Failure
		if !errors.As(err, &failure) || failure.Kind != domain.ShuttingDown {
			result = errors.Join(result, err)
			break
		}
		timer := time.NewTimer(uploadCloseInterval)
		select {
		case <-timer.C:
		case <-stop.Done():
			timer.Stop()
			return errors.Join(serviceErr, &domain.Failure{Kind: domain.Internal, Cause: stop.Err()})
		}
	}

	return errors.Join(result, serviceErr)
}

// Classic RIFF/WAV has a 32-bit size field, even with a larger duration budget.
func maxWAVBytes(decodedSeconds int) int64 {
	if int64(decodedSeconds) > (maxRIFFFileBytes-wavHeaderBytes)/wavBytesPerSecond {
		return maxRIFFFileBytes
	}
	return wavHeaderBytes + int64(decodedSeconds)*wavBytesPerSecond
}

func shutdownBudget(drain, cleanup time.Duration) time.Duration {
	if drain > time.Duration(math.MaxInt64)-cleanup {
		return time.Duration(math.MaxInt64)
	}
	return drain + cleanup
}
