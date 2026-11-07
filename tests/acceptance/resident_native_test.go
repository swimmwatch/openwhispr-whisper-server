package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type residentNativeObservation struct {
	Succeeded            bool                    `json:"succeeded"`
	HTTPStatus           *int                    `json:"http_status"`
	FailureStage         string                  `json:"failure_stage"`
	SpeechObserved       bool                    `json:"speech_observed"`
	Duration             time.Duration           `json:"native_http_wall"`
	TranscriptCharacters int                     `json:"transcript_characters"`
	TranscriptSHA256     string                  `json:"transcript_sha256"`
	Speech               publicSpeechObservation `json:"recognized_speech"`
}

func residentNativeSample(parent context.Context, target *DockerTarget, container string, nativePort int, preparedName string, budget time.Duration) (residentNativeObservation, error) {
	var result residentNativeObservation
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(nativePort) + "/inference"
	// curl implements the pinned upstream's documented native multipart contract.
	// No private Go encoder/adapter is copied, and no supervisor protocol headers
	// or request model selection are sent directly to the already loaded native.
	args := []string{"exec", container, "curl", "--disable", "--silent", "--show-error", "--noproxy", "*", "--http1.1", "--include", "--header", "Expect:", "--retry", "0", "--max-time", strconv.FormatFloat(max(0.001, time.Until(deadline).Seconds()), 'f', 3, 64),
		"--form-string", "language=en", "--form-string", "prompt=", "--form-string", "temperature=0", "--form-string", "carry_initial_prompt=false", "--form-string", "response_format=json",
		"--form", "file=@/prepared/" + preparedName + ";filename=audio.wav;type=application/octet-stream", endpoint}
	started := time.Now()
	output, err := target.runWithTimeout(ctx, budget, "native comparison", false, args...)
	result.Duration = time.Since(started)
	if err != nil {
		result.FailureStage = "command"
		return result, err
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(output)), nil)
	if err != nil {
		result.FailureStage = "framing"
		return result, errors.New("native comparison returned invalid HTTP framing")
	}
	status := response.StatusCode
	result.HTTPStatus = &status
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, dockerOutputLimit+1))
	if err != nil || len(content) > dockerOutputLimit {
		result.FailureStage = "body"
		return result, errors.New("native comparison response was incomplete or over its bound")
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || response.StatusCode != http.StatusOK || media != "application/json" || !utf8.Valid(content) {
		result.FailureStage = "response"
		return result, errors.New("native comparison did not return successful JSON")
	}
	var decoded struct {
		Text *string `json:"text"`
	}
	if decodePublicJSON(content, &decoded) != nil || decoded.Text == nil {
		result.FailureStage = "json"
		return result, errors.New("native comparison returned invalid transcript JSON")
	}
	result.TranscriptCharacters = utf8.RuneCountInString(*decoded.Text)
	result.TranscriptSHA256 = strings.TrimPrefix(acceptanceSHA256([]byte(*decoded.Text)), "sha256:")
	result.Speech = observePublicSpeech(*decoded.Text)
	result.SpeechObserved = true
	if checkPublicCase(PublicReply{Status: response.StatusCode, Text: *decoded.Text}, nil, http.StatusOK, "") != nil {
		result.FailureStage = "speech"
		return result, errors.New("native comparison failed the public speech oracle")
	}
	result.Succeeded = true
	return result, nil
}

type residentNativeSeries struct {
	RequestedSamples   int                         `json:"requested_samples"`
	Successes          int                         `json:"successes"`
	Errors             int                         `json:"errors"`
	OracleSuccesses    int                         `json:"oracle_successes"`
	Samples            []residentNativeObservation `json:"samples"`
	HTTPWall           *DurationStatistics         `json:"native_http_wall"`
	HTTPWallAudioRatio *RatioStatistics            `json:"native_http_wall_audio_ratio"`
	AudioDuration      time.Duration               `json:"normalized_audio_duration"`
	TeardownAttempted  bool                        `json:"failure_target_down_attempted"`
	TeardownSucceeded  bool                        `json:"failure_target_down_succeeded"`
}

func stopResidentNativeTarget(target *DockerTarget) error {
	// A failed direct-native call bypassed supervisor admission/recovery. A curl
	// exit or model-ready health response does not establish GPU quiescence.
	// Stop the owned pair before any caller can issue another workload operation.
	budget := max(time.Second, target.opts.CommandTimeout+time.Duration(target.opts.StopTimeoutSeconds)*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	return target.Down(ctx)
}

func collectResidentNativeSeries(ctx context.Context, target *DockerTarget, container string, nativePort int, preparedName string, audioDuration, budget time.Duration, count int) (result residentNativeSeries, err error) {
	result.RequestedSamples, result.AudioDuration = count, audioDuration
	defer func() {
		if err != nil {
			result.TeardownAttempted = true
			stopErr := stopResidentNativeTarget(target)
			result.TeardownSucceeded = stopErr == nil
			err = errors.Join(err, stopErr)
		}
	}()
	if count <= 0 || audioDuration <= 0 {
		return result, errors.New("native comparison requires positive sample and audio duration bounds")
	}
	run := MeasurementRun{}
	for i := 0; i < count; i++ {
		observation, sampleErr := residentNativeSample(ctx, target, container, nativePort, preparedName, budget)
		result.Samples = append(result.Samples, observation)
		run.Samples = append(run.Samples, MeasurementSample{Succeeded: sampleErr == nil, PublicDuration: observation.Duration, AudioDuration: &audioDuration})
		if sampleErr != nil {
			result.Errors++
			return result, sampleErr
		}
		result.Successes++
		result.OracleSuccesses++
	}
	summary, err := SummarizeMeasurements(run)
	if err != nil {
		return result, err
	}
	result.HTTPWall = summary.Public
	// All samples use the same measured audio duration. This is explicitly an
	// HTTP-wrapper wall/audio ratio, not a claimed pure GPU execution ratio.
	result.HTTPWallAudioRatio = &RatioStatistics{Count: count, Median: float64(summary.Public.Median) / float64(audioDuration), P95: float64(summary.Public.P95) / float64(audioDuration)}
	return result, nil
}

// runResidentDirectComparisons is the integration entry point. It prepares one
// input, preserves the original backend series, then runs native work serially.
func runResidentDirectComparisons(t *testing.T, e *acceptanceEnvironment) (MeasurementSummary, residentNativeSeries) {
	t.Helper()
	input := prepareResidentDirectInput(t, e)
	backend := runResidentDirectBackendPrepared(t, e, input)
	native := runResidentDirectNative(t, e, input)
	return backend, native
}

// runResidentDirectNative requires the caller's exclusive target lock and no
// other public/private producer. The shared input must come from the exact same
// prepareResidentDirectInput call used for runResidentDirectBackendPrepared.
func prepareResidentNativeMount(t *testing.T, e *acceptanceEnvironment, input *residentDirectInput) string {
	t.Helper()
	file, err := os.CreateTemp(e.prepared, "p07-direct-normalized-*.wav")
	if err != nil {
		t.Fatal("cannot prepare the test-owned public native input")
	}
	// Public fixture content uses the existing read-only prepared mount; this
	// does not copy a model or change application/image privileges.
	modeErr := file.Chmod(0644)
	written, writeErr := file.Write(input.wav)
	closeErr := file.Close()
	if modeErr != nil || writeErr != nil || closeErr != nil || int64(written) != input.size {
		t.Fatal("cannot prepare the bounded read-only public native input")
	}
	return filepath.Base(file.Name())
}

func runResidentDirectNative(t *testing.T, e *acceptanceEnvironment, input *residentDirectInput) residentNativeSeries {
	t.Helper()
	before, err := e.target.Observe(e.ctx)
	if err != nil || residentGenerationStable(input.before, before) != nil {
		t.Fatal("native comparison requires the unchanged CUDA-ready model process")
	}
	preparedName := prepareResidentNativeMount(t, e, input)
	gpuContext, stopGPU := context.WithCancel(e.ctx)
	type gpuResult struct {
		activity GPUActivity
		err      error
	}
	gpuDone := make(chan gpuResult, 1)
	gpuReady := make(chan struct{})
	go func() {
		activity, err := ObserveGPUActivity(gpuContext, *before.NativeHostPID, gpuReady)
		gpuDone <- gpuResult{activity, err}
	}()
	select {
	case <-gpuReady:
	case early := <-gpuDone:
		stopGPU()
		t.Fatalf("native GPU observer could not start: failed=%t", early.err != nil)
	case <-e.ctx.Done():
		stopGPU()
		<-gpuDone
		t.Fatal("native GPU observer startup cancelled")
	}
	series, seriesErr := collectResidentNativeSeries(e.ctx, e.target, input.containers.BackendID, e.settings.Backend.NativePort, preparedName, input.duration, time.Duration(e.settings.Timeouts.OperationSeconds)*time.Second, 30)
	stopGPU()
	gpu := <-gpuDone
	var after DockerSnapshot
	var observationErr error
	if seriesErr == nil {
		// Successful complete responses establish that the measured native work
		// returned. Only then may ordinary readiness/identity observation resume.
		after, observationErr = e.target.Observe(e.ctx)
		if observationErr == nil {
			observationErr = residentGenerationStable(before, after)
		}
		if gpu.err != nil || gpu.activity.Samples == 0 || gpu.activity.PeakSMPercent == 0 || gpu.activity.PeakMemoryBytes == 0 {
			observationErr = errors.Join(observationErr, errors.New("native comparison correlated CUDA activity was unavailable"))
		}
		if observationErr != nil {
			series.TeardownAttempted = true
			stopErr := stopResidentNativeTarget(e.target)
			series.TeardownSucceeded = stopErr == nil
			observationErr = errors.Join(observationErr, stopErr)
		}
	}
	modelSHA256 := ""
	for _, model := range e.settings.Models.Catalog {
		if model.ID == e.settings.Models.Served {
			modelSHA256 = model.Source.SHA256
		}
	}
	writeResidentEvidence(t, e, "resident-native-comparison.json", map[string]any{
		"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity, "api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage,
		"source_family": "WAV", "source_fixture_sha256": strings.TrimPrefix(acceptanceSHA256(e.audio), "sha256:"), "source_bytes": len(e.audio),
		"uploaded_family": "canonical WAV PCM_s16le mono 16000 Hz", "normalized_sha256": strings.TrimPrefix(acceptanceSHA256(input.wav), "sha256:"), "normalized_bytes": input.size,
		"model": e.settings.Models.Served, "model_sha256": modelSHA256, "language": "en", "temperature": 0, "prompt_bytes": 0, "carry_initial_prompt": false, "response_format": "json", "concurrency": 1,
		"native_process_generation": before.PrivateHealth.ProcessGeneration, "same_resident_process": seriesErr == nil && observationErr == nil,
		"series": series, "gpu_activity": gpu.activity, "gpu_observation_available": gpu.err == nil,
		"timing":     "one docker exec/curl startup plus native loopback HTTP upload/inference/full response consumption; candidate FFmpeg normalization and mount preparation excluded",
		"comparison": "same normalized bytes as the separate supervisor-inclusive backend series; public path uses the pinned source WAV; no API-overhead subtraction or pure GPU execution claim",
		"failed":     seriesErr != nil || observationErr != nil, "qualification": "not_established",
	})
	if seriesErr != nil || observationErr != nil {
		t.Fatal(errors.Join(seriesErr, observationErr))
	}
	return series
}
