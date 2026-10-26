package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func releasedAPIResources(snapshot DockerSnapshot) error {
	if snapshot.API.ScratchFileCount == nil || snapshot.API.ScratchBytes == nil || snapshot.API.FDCount == nil {
		return errors.New("API resource observation is unavailable")
	}
	var control uint64
	if snapshot.API.OwnershipLockFiles != nil {
		control = *snapshot.API.OwnershipLockFiles
	}
	if control > 1 || *snapshot.API.ScratchFileCount != control || *snapshot.API.ScratchBytes != 0 {
		return errors.New("completed API work retained visible scratch files beyond its measured empty startup lock")
	}
	return nil
}

func TestResidentResourceGuardDistinguishesUnknownAndRetainedFiles(t *testing.T) {
	if releasedAPIResources(DockerSnapshot{}) == nil {
		t.Fatal("unknown resource ownership became a clean observation")
	}
	zero, one := uint64(0), uint64(1)
	state := DockerSnapshot{API: DockerResourceSnapshot{ScratchFileCount: &zero, ScratchBytes: &zero, FDCount: &one}}
	if releasedAPIResources(state) != nil {
		t.Fatal("measured clean API ownership was rejected")
	}
	state.API.ScratchFileCount = &one
	if releasedAPIResources(state) == nil {
		t.Fatal("retained request file was not detected")
	}
	state.API.OwnershipLockFiles = &one
	if releasedAPIResources(state) != nil {
		t.Fatal("measured empty startup flock was misclassified as request data")
	}
	two := uint64(2)
	state.API.ScratchFileCount = &two
	if releasedAPIResources(state) == nil {
		t.Fatal("startup flock masked an additional retained request file")
	}
}

func TestLocalResidentMeasurements(t *testing.T) {
	e := openLocalEnvironment(t, nil)
	before, err := e.target.Observe(e.ctx)
	if err != nil || before.NativeHostPID == nil || before.PrivateHealth == nil || !before.PrivateHealth.CUDAReady || before.PrivateHealth.Model != "base" {
		t.Fatal("actual native/CUDA/model/resource observations are unavailable")
	}
	if err := releasedAPIResources(before); err != nil {
		t.Fatal(err)
	}
	stopTrace := startLocalModelTrace(t, e, before)
	gpuContext, stopGPU := context.WithCancel(e.ctx)
	t.Cleanup(stopGPU)
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
		t.Fatalf("GPU activity observer could not start: failed=%t", early.err != nil)
	case <-e.ctx.Done():
		t.Fatal("GPU observer startup cancelled")
	}
	type resourcePeaks struct {
		APIRSSBytes, BackendRSSBytes uint64
		Samples                      int
		Failed                       bool
	}
	samplingContext, stopSampling := context.WithCancel(e.ctx)
	t.Cleanup(stopSampling)
	peaksDone := make(chan resourcePeaks, 1)
	var peaks resourcePeaks
	var peaksMu sync.Mutex
	go func() {
		defer func() {
			peaksMu.Lock()
			final := peaks
			peaksMu.Unlock()
			peaksDone <- final
		}()
		for {
			observation, err := e.target.Observe(samplingContext)
			if samplingContext.Err() != nil {
				break
			}
			peaksMu.Lock()
			if err != nil || observation.API.RSSBytes == nil || observation.Backend.RSSBytes == nil {
				peaks.Failed = true
				peaksMu.Unlock()
				break
			}
			peaks.Samples++
			if *observation.API.RSSBytes > peaks.APIRSSBytes {
				peaks.APIRSSBytes = *observation.API.RSSBytes
			}
			if *observation.Backend.RSSBytes > peaks.BackendRSSBytes {
				peaks.BackendRSSBytes = *observation.Backend.RSSBytes
			}
			peaksMu.Unlock()
			select {
			case <-samplingContext.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()
	longest := readLocalAudio(t, filepath.Join(e.receipt.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
	reply, err := e.client.Transcribe(e.ctx, longest, "longest.flac", []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}})
	if err != nil || reply.Status != http.StatusOK || reply.Text == "" {
		t.Fatal("actual exact-900-second decoded fixture did not transcribe")
	}
	calibrated, err := e.target.Observe(e.ctx)
	if err != nil || releasedAPIResources(calibrated) != nil {
		t.Fatal("longest-input calibration did not release API work resources")
	}
	peaksMu.Lock()
	calibrationPeaks := peaks
	peaks = resourcePeaks{}
	peaksMu.Unlock()
	if calibrationPeaks.Failed || calibrationPeaks.Samples == 0 {
		t.Fatal("isolated longest-input resource calibration is unavailable")
	}
	var samples []MeasurementSample
	var blocks []DockerResourceSnapshot
	var backendBlocks []DockerResourceSnapshot
	phaseStart := time.Now()
	for block := 0; block < 3; block++ {
		for sample := 0; sample < 20; sample++ {
			reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}})
			success := err == nil && reply.Status == http.StatusOK
			duration := 11 * time.Second
			samples = append(samples, MeasurementSample{Succeeded: success, PublicDuration: reply.PublicDuration, AudioDuration: &duration})
			if !success {
				t.Fatalf("resident block %d sample %d failed; no retry applied", block+1, sample+1)
			}
		}
		state, err := e.target.Observe(e.ctx)
		if err != nil || state.PrivateHealth == nil || state.NativeHostPID == nil || *state.NativeHostPID != *before.NativeHostPID || state.PrivateHealth.ProcessGeneration != before.PrivateHealth.ProcessGeneration {
			t.Fatal("healthy repeated requests changed native identity or lost observation")
		}
		if err := releasedAPIResources(state); err != nil {
			t.Fatal(err)
		}
		blocks = append(blocks, state.API)
		backendBlocks = append(backendBlocks, state.Backend)
		// Compare with the completed longest-input epoch, allowing only the
		// standard transport's bounded idle-connection pool, not per-request FDs.
		if state.API.FDCount == nil || calibrated.API.FDCount == nil || *state.API.FDCount > *calibrated.API.FDCount+uint64(http.DefaultMaxIdleConnsPerHost) {
			t.Fatal("resident blocks accumulated API descriptors beyond calibrated transport ownership")
		}
	}
	stopSampling()
	peaks = <-peaksDone
	stopGPU()
	gpu := <-gpuDone
	if peaks.Failed || peaks.Samples == 0 || gpu.err != nil || gpu.activity.Samples == 0 || gpu.activity.PeakSMPercent == 0 || gpu.activity.PeakMemoryBytes == 0 {
		t.Fatal("actual correlated GPU activity or independently sampled memory evidence is unavailable")
	}
	modelOperations, traceErr := stopTrace()
	if traceErr != nil {
		t.Fatal(traceErr)
	}
	if modelOperations != 0 {
		t.Fatalf("healthy requests performed %d recurring model-path operations", modelOperations)
	}
	// Deliberately retained test-owned file proves the real resource assertion.
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.target.run(e.ctx, "resource assertion fixture", false, "exec", containers.APIID, "sh", "-ec", "printf fixture > /tmp/whisper-api/p07-retained-fixture")
	if err != nil {
		t.Fatal(err)
	}
	retained, err := e.target.Observe(e.ctx)
	if err != nil || releasedAPIResources(retained) == nil {
		t.Fatal("real retained-file mutation escaped the resource assertion")
	}
	_, err = e.target.run(e.ctx, "restore resource assertion fixture", false, "exec", containers.APIID, "sh", "-ec", `[ "$(realpath /tmp/whisper-api/p07-retained-fixture)" = /tmp/whisper-api/p07-retained-fixture ]; rm -- /tmp/whisper-api/p07-retained-fixture`)
	if err != nil {
		t.Fatal(err)
	}
	apiLogs, apiErr := e.target.Logs(e.ctx, "api")
	backendLogs, backendErr := e.target.Logs(e.ctx, "backend")
	if apiErr != nil || backendErr != nil {
		t.Fatal("safe stage log observation is unavailable")
	}
	// This isolated phase has exactly one POST producer. Match actual queue and
	// private HTTP events; close the bracket after collecting completion logs.
	run, err := CorrelateTimings(samples, phaseStart, time.Now(), apiLogs, backendLogs)
	if err != nil {
		t.Fatal(err)
	}
	run.Startup = &e.startup
	summary, err := SummarizeMeasurements(run)
	if err != nil {
		t.Fatal(err)
	}
	report, err := json.MarshalIndent(map[string]any{"scope": "local_validation_not_release", "runtime_input_identity": e.receipt.RuntimeInputIdentity, "api_image": e.receipt.APIImage, "backend_image": e.receipt.BackendImage, "model_sha256": e.settings.Models.Catalog[0].Source.SHA256, "summary": summary, "gpu_activity": gpu.activity, "calibration_resource_peaks": calibrationPeaks, "repeat_resource_peaks": peaks, "idle_api": before.API, "idle_backend": before.Backend, "after_longest_api": calibrated.API, "after_longest_backend": calibrated.Backend, "api_after_blocks": blocks, "backend_after_blocks": backendBlocks, "model_path_operations": modelOperations, "model_load_observation": "native process initialization to CUDA-ready health interval; not pure weight-file I/O", "sampling": "serial resource snapshots separated by 250ms; GPU pmon 1s", "rate_limit_enabled": false, "logging": "debug", "longest_decoded_seconds": 900}, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(e.workdir, "resident-measurements.json"), report, 0600) != nil {
		t.Fatal("cannot retain sanitized local resident observations")
	}
	t.Log("local longest-input/three-block workload, correlated CUDA activity, model-I/O observation and released API resources passed")
}
