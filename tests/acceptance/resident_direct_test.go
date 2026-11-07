package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/audio"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/upstream"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

// The production upstream adapter still owns all private HTTP framing, validation
// and compatibility checks. This test-only transport crosses the existing owned
// backend container boundary, never a published supervisor/native host port.
type residentExecTransport struct {
	target              *DockerTarget
	container, endpoint string
}

func (r residentExecTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		defer request.Body.Close()
	}
	if request.URL.Scheme != "http" || request.URL.Host != r.endpoint || (request.URL.Path != "/health" && request.URL.Path != "/inference") {
		return nil, errors.New("private comparison route unavailable")
	}
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	args := []string{"exec", "-i", r.container, "curl", "--silent", "--show-error", "--noproxy", "*", "--http1.1", "--include", "--header", "Expect:", "--request", request.Method}
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("private comparison requires a deadline")
	}
	remaining := time.Until(deadline).Seconds()
	if remaining <= 0 {
		return nil, context.DeadlineExceeded
	}
	args = append(args, "--max-time", strconv.FormatFloat(remaining, 'f', 3, 64))
	for _, name := range []string{"Content-Type", "X-Request-ID", "X-Whisper-Protocol", "X-Whisper-Configuration", "X-Whisper-Generation", "X-Whisper-Timeout-Ms"} {
		if value := request.Header.Get(name); value != "" {
			args = append(args, "--header", name+": "+value)
		}
	}
	if request.Body != nil {
		args = append(args, "--data-binary", "@-")
	}
	args = append(args, request.URL.String())
	command := exec.CommandContext(ctx, r.target.opts.DockerExecutable, args...)
	command.Dir = r.target.opts.Root
	command.Env = r.target.environment()
	command.Stdin = request.Body
	output := &dockerOutput{cancel: cancel, retain: true}
	diagnostic := &dockerOutput{cancel: cancel}
	command.Stdout = output
	command.Stderr = diagnostic
	err := runOwnedCommand(command)
	if output.overflow || diagnostic.overflow {
		return nil, errors.New("private comparison output exceeded its observation limit")
	}
	if err != nil {
		return nil, errors.Join(errors.New("private comparison transport failed; raw diagnostics suppressed"), request.Context().Err())
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(output.data.Bytes())), request)
	if err != nil {
		return nil, errors.New("private comparison returned an invalid HTTP response")
	}
	return response, nil
}

type residentDirectInput struct {
	wav        []byte
	size       int64
	duration   time.Duration
	fields     domain.InferenceFields
	containers DockerContainers
	before     DockerSnapshot
}

// prepareResidentDirectInput normalizes once for both the supervisor-inclusive
// and true native comparisons. Neither timed series owns another decoder run.
func prepareResidentDirectInput(t *testing.T, e *acceptanceEnvironment) *residentDirectInput {
	t.Helper()
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, err := e.target.Observe(e.ctx)
	if err != nil || state.PrivateHealth == nil {
		t.Fatal("direct backend compatibility observation unavailable")
	}
	fields := domain.InferenceFields{Language: "en", Temperature: 0}
	input := filepath.Join(e.workdir, "direct-public-input.wav")
	if err = os.WriteFile(input, e.audio, 0600); err != nil {
		t.Fatal(err)
	}
	decoder, err := audio.New(audio.Options{Command: []string{e.target.opts.DockerExecutable, "exec", "-i", containers.APIID, "ffmpeg"}, AudioBytes: e.settings.Limits.AudioBytes, DecodedSeconds: e.settings.Limits.DecodedSeconds, OperationTimeout: time.Duration(e.settings.Timeouts.OperationSeconds) * time.Second, CleanupTimeout: time.Duration(e.settings.Timeouts.CleanupSeconds) * time.Second, Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := decoder.Normalize(e.ctx, transcription.Input{UploadPath: input, Size: int64(len(e.audio)), Model: e.settings.Models.Served, Fields: fields})
	if err != nil {
		t.Fatal("candidate decoder could not prepare the direct comparison input")
	}
	wav, err := io.ReadAll(io.LimitReader(normalized.Audio, normalized.Size+1))
	closeErr := normalized.Audio.Close()
	if err != nil || closeErr != nil || int64(len(wav)) != normalized.Size {
		t.Fatal("bounded direct comparison input unavailable")
	}
	return &residentDirectInput{wav: wav, size: normalized.Size, duration: normalized.Duration, fields: fields, containers: containers, before: state}
}

func runResidentDirectBackend(t *testing.T, e *acceptanceEnvironment) MeasurementSummary {
	t.Helper()
	return runResidentDirectBackendPrepared(t, e, prepareResidentDirectInput(t, e))
}

// runResidentDirectBackendPrepared preserves the original thirty supervisor-
// inclusive measurements. The native comparison is a separately named series.
func runResidentDirectBackendPrepared(t *testing.T, e *acceptanceEnvironment, input *residentDirectInput) MeasurementSummary {
	t.Helper()
	endpoint := "127.0.0.1:" + strconv.Itoa(e.settings.Backend.Port)
	client, err := upstream.New(upstream.Options{URL: "http://" + endpoint, Fingerprint: input.before.PrivateHealth.ConfigurationFingerprint, Model: e.settings.Models.Served, OperationTimeout: time.Duration(e.settings.Timeouts.OperationSeconds) * time.Second, MaxWAVBytes: input.size, MaxReplyBytes: 1 << 20, PromptBytes: e.settings.Limits.PromptBytes, Client: &http.Client{Transport: residentExecTransport{e.target, input.containers.BackendID, endpoint}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	run := MeasurementRun{}
	for i := 0; i < 30; i++ {
		start := time.Now()
		result, err := client.Infer(e.ctx, transcription.Normalized{Audio: io.NopCloser(bytes.NewReader(input.wav)), Size: input.size, Duration: input.duration, RequestID: "resident-direct-" + strconv.Itoa(i), Fields: input.fields})
		elapsed := time.Since(start)
		run.Samples = append(run.Samples, MeasurementSample{Succeeded: err == nil && strings.TrimSpace(result.Text) != "", PublicDuration: elapsed, AudioDuration: &input.duration})
		if err != nil || strings.TrimSpace(result.Text) == "" {
			t.Fatal("direct backend comparison failed without retry; raw contents withheld")
		}
	}
	summary, err := SummarizeMeasurements(run)
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func TestResidentExecTransportFailureAndBounds(t *testing.T) {
	for _, test := range []struct {
		name, script string
		cancel       bool
		wantSuccess  bool
	}{
		{"response", "printf 'HTTP/1.1 " + strconv.Itoa(http.StatusOK) + " " + http.StatusText(http.StatusOK) + "\\r\\nContent-Length: 2\\r\\n\\r\\n{}'", false, true},
		{"malformed", "printf 'private raw contents'", false, false},
		{"failed", "printf 'private raw diagnostic' >&2;exit 7", false, false},
		{"overflow", "dd if=/dev/zero bs=1048577 count=1 2>/dev/null", false, false},
		{"diagnostic-overflow", "dd if=/dev/zero bs=1048577 count=1 >&2 2>/dev/null", false, false},
		{"cancel", "exec sleep 30", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "exec-fixture")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\n"+test.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			target := &DockerTarget{opts: DockerOptions{DockerExecutable: executable, Root: t.TempDir()}}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if test.cancel {
				ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8081/health", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := (residentExecTransport{target, "owned-fixture", "127.0.0.1:8081"}).RoundTrip(request)
			if test.wantSuccess {
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil || string(body) != "{}" {
					t.Fatal("actual HTTP response not returned")
				}
			} else if err == nil {
				t.Fatal("failed private observation became success")
			} else if strings.Contains(err.Error(), "private raw") {
				t.Fatal("raw private contents escaped transport diagnostics")
			}
		})
	}
}
