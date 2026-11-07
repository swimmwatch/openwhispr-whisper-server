package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/audio"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/httpapi"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/upstream"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
)

// This exercises real FFmpeg and both Go HTTP boundaries, but no native/CUDA code.
func TestShortWAVThroughPublicAPI(t *testing.T) {
	opts, root := sampleCountOptions(t)
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())
	private := httptest.NewServer(s.Handler())
	defer private.Close()
	client, err := upstream.New(upstream.Options{
		URL: private.URL, Model: "base", Fingerprint: opts.Fingerprint,
		OperationTimeout: opts.OperationTimeout, MaxWAVBytes: opts.MaxWAVBytes,
		MaxReplyBytes: opts.MaxReplyBytes, PromptBytes: opts.PromptBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	decoder, err := audio.New(audio.Options{
		Command: []string{"ffmpeg"}, AudioBytes: opts.MaxWAVBytes, DecodedSeconds: 1,
		OperationTimeout: opts.OperationTimeout, CleanupTimeout: opts.CleanupTimeout, Capacity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := transcription.NewService(transcription.Options{
		Model: "base", Fingerprint: opts.Fingerprint, WaitingCapacity: 0,
		OperationTimeout: opts.OperationTimeout, CleanupTimeout: opts.CleanupTimeout,
		ShutdownTimeout: opts.ShutdownTimeout, HealthTimeout: 100 * time.Millisecond,
		HealthInterval: 5 * time.Millisecond,
	}, decoder, client, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := service.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	publicWork := filepath.Join(t.TempDir(), "uploads")
	handler, err := httpapi.New(httpapi.Options{
		Model: "base", WorkRoot: publicWork, AudioBytes: opts.MaxWAVBytes,
		PromptBytes: opts.PromptBytes, WaitingCapacity: 0, OperationTimeout: opts.OperationTimeout,
	}, service)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := handler.Close(); err != nil {
			t.Error(err)
		}
	}()
	e := echo.New()
	e.HTTPErrorHandler = httpapi.ErrorHandler
	handler.Register(e)
	public := httptest.NewServer(e)
	defer public.Close()
	await(t, func() bool { _, err := service.Readiness(context.Background()); return err == nil })

	for _, frames := range []int{1, 199, 200, 201, 16000} {
		t.Run(strconv.Itoa(frames), func(t *testing.T) {
			wave := wavWithFrames(frames)
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			file, err := form.CreateFormFile("file", "audio.wav")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = file.Write(wave)
			_ = form.WriteField("model", "base")
			format := "json"
			if frames%2 == 0 {
				format = "text"
			}
			_ = form.WriteField("response_format", format)
			_ = form.Close()
			response, err := public.Client().Post(public.URL+"/v1/audio/transcriptions", form.FormDataContentType(), &body)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("public inference: status=%d body=%s error=%v", response.StatusCode, data, readErr)
			}
			text := string(data)
			if format == "json" {
				var result struct{ Text string }
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				text = result.Text
			}
			if text != "fixture transcript" {
				t.Fatalf("public transcript=%q", text)
			}
			requireNativeWAV(t, root, wave)
			entries, err := os.ReadDir(publicWork)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					t.Fatal("public request retained upload or normalized audio", entry.Name())
				}
			}
			files, err := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "request-*.wav"))
			if err != nil || len(files) != 0 {
				t.Fatalf("private staging retained: files=%v error=%v", files, err)
			}
			health, _ := s.Health(context.Background())
			if health.ProcessGeneration != first.ProcessGeneration {
				t.Fatal("public inference restarted healthy native")
			}
		})
	}
}
