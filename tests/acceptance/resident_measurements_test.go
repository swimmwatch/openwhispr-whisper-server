package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func releasedAPIResources(snapshot DockerSnapshot) error {
	return releasedScratchResources(snapshot.API)
}

func releasedScratchResources(resource DockerResourceSnapshot) error {
	if resource.ScratchFileCount == nil || resource.ScratchBytes == nil || resource.FDCount == nil {
		return errors.New("API resource observation is unavailable")
	}
	var control uint64
	if resource.OwnershipLockFiles != nil {
		control = *resource.OwnershipLockFiles
	}
	if control > 1 || *resource.ScratchFileCount != control || *resource.ScratchBytes != 0 {
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
	runResidentMeasurements(t, openLocalEnvironment(t, nil))
}

func runResidentMeasurements(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	// This is the v3 memory entry, not the legacy RSS-only diagnostic path.
	// Both actual role profiles/proofs are mandatory; intent is checked before
	// touching observers and each effective runtime profile is verified below.
	if os.Getenv("P07_API_GC_TRACE") != "1" || os.Getenv("P07_SUPERVISOR_GC_TRACE") != "1" {
		t.Fatal(errResidentSupervisorHeapUnknown)
	}
	before, err := e.target.Observe(e.ctx)
	if err != nil || before.NativeHostPID == nil || before.PrivateHealth == nil || !before.PrivateHealth.CUDAReady || before.PrivateHealth.Model != "base" {
		t.Fatal("actual native/CUDA/model/resource observations are unavailable")
	}
	if err := releasedAPIResources(before); err != nil {
		t.Fatal(err)
	}
	stopTrace := startModelTrace(t, e, before)
	// Declare and retain actual idle samples before any inference. Idle variance,
	// not a model-size percentage, determines the completed-work tolerance.
	idleResources := residentCompletedWindow(t, e, 5, time.Second)
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
	longest := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
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
	longestResources := residentCompletedWindow(t, e, 3, time.Second)
	coldEnvelope, err := calibrateResidentEnvelope(idleResources, longestResources)
	if err != nil {
		t.Fatal(err)
	}
	// The instrumented memory profile runs the already-required backend/native
	// comparisons before freeze, in this same resident generation. Standard curl
	// exercises fresh native connections unlike the supervisor's keep-alive path;
	// this does not prove worker fairness, allocator saturation or a larger bound.
	if verified := residentSupervisorGCProfileVerified(e); verified != nil && *verified {
		direct, native := runResidentDirectComparisons(t, e)
		writeResidentEvidence(t, e, "resident-premeasurement-comparisons.json", map[string]any{
			"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity,
			"direct_backend": direct, "direct_native": native,
			"phase":                    "required comparisons after isolated longest input and before ordinary calibration",
			"conditioning_request_cap": residentConditioningMaxBlocks * residentConditioningBlockRequests, "conditioning_count_includes_comparison": false,
			"limits":        "separate required workload; no claim that all native workers were exercised; original isolated-longest physical cap unchanged",
			"qualification": "not_established",
		})
	}
	// A single long input does not calibrate the ordinary input allocation shape.
	// Run the declared thirty ordinary requests before freezing the bound. The
	// later 3x20 points are observations only and can never update this envelope.
	warmup := runResidentCalibrationWarmup(t, e, before)
	preconditioningEnvelope, err := calibrateWarmedResidentEnvelope(idleResources, longestResources, *warmup.Completion, warmup.Idle)
	if err != nil {
		t.Fatal(err)
	}
	peaksMu.Lock()
	ordinaryCalibrationPeaks := peaks
	peaks = resourcePeaks{}
	peaksMu.Unlock()
	if ordinaryCalibrationPeaks.Failed || ordinaryCalibrationPeaks.Samples == 0 {
		t.Fatal("ordinary-input premeasurement resource calibration is unavailable")
	}
	writeResidentEvidence(t, e, "resident-warmup-calibration.json", map[string]any{"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity, "api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage, "cold_idle": idleResources, "completed_longest": longestResources, "cold_only_envelope": coldEnvelope, "ordinary_warmup": warmup, "ordinary_calibration_resource_peaks": ordinaryCalibrationPeaks, "isolated_longest_resource_peaks": calibrationPeaks, "api_rss_source": "PID1 /proc/1/smaps_rollup Rss, read separately; all process RSS/Anonymous use rollup; File remains status/RssFile", "preconditioning_envelope": preconditioningEnvelope, "sampling": "ordinary calibration peaks include completed-longest idle, explicitly recorded premeasurement comparisons when the GC profile is verified, thirty ordinary requests and warmed idle; the isolated-longest physical cap is unchanged and all phases precede measured 3x20", "bounded_retention_evidence": "prior exact-candidate diagnostic observed unchanged native RSS at 20/40/60 and subsequent idle, while supervisor RSS grew then plateaued; native pool/per-input allocation shapes are possible contributors, not a proven allocator mechanism", "qualification": "not_established"})
	policy, policyErr := newResidentConditioningPolicy(idleResources, warmup.Idle, preconditioningEnvelope, calibrationPeaks.BackendRSSBytes)
	if policyErr == nil {
		policy.SupervisorHeap, policyErr = selectResidentSupervisorHeapPolicy(e, before, warmup.Started)
	}
	if policyErr == nil {
		policy.APIHeap, policyErr = selectResidentGoHeapPolicy(e.ctx, e, "api", before, warmup.Started)
		if policy.SupervisorHeap == nil || policy.APIHeap == nil {
			policyErr = errResidentSupervisorHeapUnknown
		}
	}
	var conditioning residentConditioning
	var conditioningSummary MeasurementSummary
	conditioningErr := policyErr
	if conditioningErr == nil {
		conditioning, conditioningSummary, conditioningErr = runResidentConditioningCalibration(t, e, before, policy)
	}
	calibrationModelOperations, calibrationTraceErr := stopTrace()
	if calibrationTraceErr != nil {
		conditioningErr = errors.Join(conditioningErr, calibrationTraceErr)
	} else if calibrationModelOperations != 0 {
		conditioningErr = errors.Join(conditioningErr, errors.New("premeasurement calibration performed recurring model-path operations"))
	}
	peaksMu.Lock()
	conditioningPeaks := peaks
	peaks = resourcePeaks{}
	peaksMu.Unlock()
	if conditioningPeaks.Failed || conditioningPeaks.Samples == 0 {
		conditioningErr = errors.Join(conditioningErr, errors.New("conditioning resource sampling unavailable"))
	}
	var frozenEnvelope *residentEnvelope
	if conditioningErr == nil {
		earlier := append(append([]residentResourcePoint(nil), longestResources...), *warmup.Completion)
		var frozen residentEnvelope
		frozen, conditioningErr = freezeConditionedResidentEnvelope(policy, conditioning, earlier)
		if conditioningErr == nil {
			frozenEnvelope = &frozen
		}
	}
	var calibrationModelObservation *uint64
	if calibrationTraceErr == nil {
		calibrationModelObservation = &calibrationModelOperations
	}
	writeResidentEvidence(t, e, "resident-calibration-finalization.json", map[string]any{"conditioning": conditioning, "conditioning_summary": conditioningSummary, "conditioning_resource_peaks": conditioningPeaks, "model_path_operations": calibrationModelObservation, "model_trace_available": calibrationTraceErr == nil, "frozen_envelope": frozenEnvelope, "calibration_established": conditioningErr == nil, "api_rss_source": "PID1 /proc/1/smaps_rollup Rss; no status/VmRSS fallback", "qualification": "not_established"})
	recordResidentSupervisorGCObservation(t, e, "conditioning")
	recordResidentAPIGCObservation(t, e, "conditioning")
	if conditioningErr != nil {
		// Join all already-owned observers before reporting calibration failure.
		stopSampling()
		<-peaksDone
		stopGPU()
		<-gpuDone
		t.Fatal(conditioningErr)
	}
	envelope := *frozenEnvelope
	// The calibration model-I/O guard is now known, not assumed. A fresh scoped
	// trace independently covers the subsequent sixty measured requests/idle.
	stopTrace = startModelTrace(t, e, before)
	var blockResources []residentResourcePoint
	var samples []MeasurementSample
	var blocks []DockerResourceSnapshot
	var backendBlocks []DockerResourceSnapshot
	phaseStart := time.Now()
	measuredContext := e.ctx
	if envelope.SupervisorHeap != nil {
		var cancelMeasured context.CancelFunc
		measuredContext, cancelMeasured = context.WithTimeout(e.ctx, time.Duration(e.settings.Timeouts.OperationSeconds)*time.Second)
		defer cancelMeasured()
	}
	var lastRequestCompleted time.Time
	diagnostic, diagnosticReadErr := measureResidentDiagnosticBlocks(envelope, func(block int) (residentResourcePoint, error) {
		for sample := 0; sample < 20; sample++ {
			reply, err := e.client.Transcribe(measuredContext, e.audio, "jfk.wav", []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}})
			lastRequestCompleted = time.Now()
			success := err == nil && reply.Status == http.StatusOK
			duration := 11 * time.Second
			samples = append(samples, MeasurementSample{Succeeded: success, PublicDuration: reply.PublicDuration, AudioDuration: &duration})
			if !success {
				t.Fatalf("resident block %d sample %d failed; no retry applied", block+1, sample+1)
			}
		}
		resources, state, err := residentActualResources(e.ctx, e)
		if err != nil || state.PrivateHealth == nil || state.NativeHostPID == nil || *state.NativeHostPID != *before.NativeHostPID || state.PrivateHealth.ProcessGeneration != before.PrivateHealth.ProcessGeneration {
			t.Fatal("healthy repeated requests changed native identity or lost observation")
		}
		if err := releasedAPIResources(state); err != nil {
			t.Fatal(err)
		}
		blocks = append(blocks, state.API)
		backendBlocks = append(backendBlocks, state.Backend)
		blockResources = append(blockResources, resources)
		writeResidentEvidence(t, e, "resident-envelope-block-"+strconv.Itoa(block+1)+".json", map[string]any{"idle": idleResources, "completed_longest": longestResources, "envelope": envelope, "blocks": blockResources})
		return resources, nil
	})
	if diagnosticReadErr != nil {
		t.Fatal(diagnosticReadErr)
	}
	// Complete the declared post-repeat idle observation even when block one
	// already failed. These values never become calibration inputs.
	retentionResources := residentCompletedWindow(t, e, 5, time.Second)
	retentionErr := envelope.checkPassiveSeries(retentionResources)
	var supervisorHeapErr error
	if envelope.SupervisorHeap != nil {
		supervisorHeapErr = awaitResidentSupervisorHeapCoverage(t, measuredContext, e, envelope, phaseStart, lastRequestCompleted)
	}
	stopSampling()
	peaks = <-peaksDone
	stopGPU()
	gpu := <-gpuDone
	modelOperations, traceErr := stopTrace()
	var observedModelOperations *uint64
	if traceErr == nil {
		observedModelOperations = &modelOperations
	}
	writeResidentEvidence(t, e, "resident-envelope-diagnostic.json", map[string]any{"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity, "api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage, "idle": idleResources, "completed_longest": longestResources, "cold_only_envelope": coldEnvelope, "ordinary_warmup": warmup, "ordinary_calibration_resource_peaks": ordinaryCalibrationPeaks, "isolated_longest_resource_peaks": calibrationPeaks, "api_rss_source": "PID1 /proc/1/smaps_rollup Rss, read separately; all process RSS/Anonymous use rollup; File remains status/RssFile", "conditioning": conditioning, "conditioning_summary": conditioningSummary, "conditioning_resource_peaks": conditioningPeaks, "calibration_model_path_operations": calibrationModelOperations, "frozen_envelope": envelope, "blocks": diagnostic.Points, "violating_block_prefixes": diagnostic.ViolatingBlocks, "post_repeat_idle": retentionResources, "post_repeat_idle_violated": retentionErr != nil, "calibration_resource_peaks": calibrationPeaks, "repeat_resource_peaks": peaks, "gpu_activity": gpu.activity, "gpu_observation_available": gpu.err == nil, "model_path_operations": observedModelOperations, "model_trace_available": traceErr == nil, "qualification": "not_established", "rss_breakdown_sampling": "separate serial PID1/native precise smaps_rollup Rss/Anonymous and approximate status/RssFile; timestamped independently of precise aggregate RSS; never used to refit frozen bounds"})
	recordResidentSupervisorGCObservation(t, e, "steady")
	recordResidentAPIGCObservation(t, e, "steady")
	// Preserve the first failed verdict after all bounded endpoints and observers
	// have been collected. Continuing diagnostics cannot turn a failure into PASS.
	if diagnostic.Err() != nil {
		t.Fatal(errors.Join(diagnostic.Err(), traceErr))
	}
	if retentionErr != nil {
		t.Fatal(errors.Join(retentionErr, traceErr))
	}
	if supervisorHeapErr != nil {
		t.Fatal(errors.Join(supervisorHeapErr, traceErr))
	}
	if peaks.Failed || peaks.Samples == 0 || gpu.err != nil || gpu.activity.Samples == 0 || gpu.activity.PeakSMPercent == 0 || gpu.activity.PeakMemoryBytes == 0 {
		t.Fatal("actual correlated GPU activity or independently sampled memory evidence is unavailable")
	}
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
	// This isolated phase has exactly one POST producer. Match actual queue and
	// private HTTP events; close the bracket after collecting completion logs.
	var run MeasurementRun
	uncorrelatedTimingReason := ""
	if envelope.SupervisorHeap != nil {
		run = MeasurementRun{Samples: samples}
		uncorrelatedTimingReason = "verified numeric GC diagnostic profile is not the strict JSON-only timing input; queue/backend/API-overhead are unknown here, not passed or inferred from wall time; independent uninstrumented performance receipts are required"
	} else {
		apiLogs, apiErr := e.target.Logs(e.ctx, "api")
		backendLogs, backendErr := e.target.Logs(e.ctx, "backend")
		if apiErr != nil || backendErr != nil {
			t.Fatal("safe stage log observation is unavailable")
		}
		run, err = CorrelateTimings(samples, phaseStart, time.Now(), apiLogs, backendLogs)
		if err != nil {
			t.Fatal(err)
		}
	}
	run.Startup = &e.startup
	summary, err := SummarizeMeasurements(run)
	if err != nil {
		t.Fatal(err)
	}
	report, err := json.MarshalIndent(map[string]any{"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity, "api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage, "model_sha256": e.settings.Models.Catalog[0].Source.SHA256, "summary": summary, "uncorrelated_timing_reason": uncorrelatedTimingReason, "supervisor_heap_bound": envelope.SupervisorHeap, "api_heap_bound": envelope.APIHeap, "ordinary_warmup_summary": warmup.Summary, "conditioning_summary": conditioningSummary, "calibration_model_path_operations": calibrationModelOperations, "warmup_and_conditioning_excluded_from_steady_summary": true, "gpu_activity": gpu.activity, "calibration_resource_peaks": calibrationPeaks, "repeat_resource_peaks": peaks, "idle_api": before.API, "idle_backend": before.Backend, "after_longest_api": calibrated.API, "after_longest_backend": calibrated.Backend, "api_after_blocks": blocks, "backend_after_blocks": backendBlocks, "model_path_operations": modelOperations, "model_load_observation": "native process initialization to CUDA-ready health interval; not pure weight-file I/O", "sampling": "serial resource snapshots separated by 250ms; GPU pmon 1s", "rate_limit_enabled": false, "logging": "debug", "longest_decoded_seconds": 900}, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(e.workdir, "resident-measurements.json"), report, 0600) != nil {
		t.Fatal("cannot retain sanitized local resident observations")
	}
	writeResidentEvidence(t, e, "resident-idle-retention.json", map[string]any{"idle": idleResources, "completed_longest": longestResources, "envelope": envelope, "after_blocks": blockResources, "post_repeat_idle": retentionResources, "declared_idle_samples": 5, "scheduled_sample_gap_seconds": 1})
	if err := envelope.checkPassiveSeries(retentionResources); err != nil {
		t.Fatal(err)
	}
	if envelope.SupervisorHeap != nil {
		writeResidentEvidence(t, e, "resident-profiled-performance-limit.json", map[string]any{"concurrent_public": nil, "direct_backend": nil, "direct_native": nil, "reason": uncorrelatedTimingReason, "premeasurement_comparison_receipt": "resident-premeasurement-comparisons.json", "comparison_limit": "instrumented direct comparisons ran before freeze; separate uninstrumented correlated timing is not established by this run", "public_steady": summary, "qualification": "not_established"})
		// Terminal recovery uses explicit native episodes. Pair recreation retires
		// the old Go lifetimes and requires independent PRE-work calibration.
		runResidentTerminalMeasurements(t, e, envelope, longest)
		t.Log("profiled resident/native/API/Go-heap steady and calibrated terminal checks completed; separate uninstrumented timing components remain required")
		return
	}
	concurrent := runResidentConcurrent(t, e)
	direct, native := runResidentDirectComparisons(t, e)
	writeResidentEvidence(t, e, "resident-performance-comparison.json", map[string]any{"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity, "api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage, "steady_public": summary, "concurrent_public": concurrent, "concurrent_waves": 2, "concurrency": 3, "direct_backend": direct, "direct_backend_samples": 30, "direct_backend_includes_supervisor": true, "direct_backend_wall_includes": "production upstream health and inference, two docker exec/curl starts and full response consumption; candidate FFmpeg normalization occurs once outside timing", "direct_native": native, "comparison": "same pinned public audio/base/en/temperature=0; direct-backend and public wall observations are separate, not subtracted as API overhead", "qualification": "not_established"})
	runResidentTerminalMeasurements(t, e, envelope, longest)
	t.Log("calibrated idle/longest/three-block and terminal resource checks, concurrent timing, private direct-backend comparison, CUDA and model-I/O observations passed")
}
