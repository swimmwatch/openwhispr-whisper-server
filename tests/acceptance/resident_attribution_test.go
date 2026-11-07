package acceptance

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type residentAttributionFailure struct {
	Block, Request int
	HTTPStatus     *int
	TransportError bool
	Speech         *publicSpeechObservation
}

// Diagnostic-only route comparison: never freezes/repairs a qualification bound.
// Routes reuse the candidate, input preparation, oracle, process ownership,
// model observer and fixed request/time ceilings of the existing P07 harness.
func TestResidentNativeAttributionDiagnostic(t *testing.T) {
	route := os.Getenv("P07_NATIVE_ATTRIBUTION_ROUTE")
	if route == "" {
		t.Skip("native attribution requires its separate explicit diagnostic route")
	}
	if route != "api" && route != "native" && route != "idle" {
		t.Fatal("native attribution route must be api, native or idle")
	}
	declaredRequests := residentConditioningMaxBlocks * residentConditioningBlockRequests
	if route == "idle" {
		declaredRequests = 0
	}
	if os.Getenv("P07_API_GC_TRACE") != "1" || os.Getenv("P07_SUPERVISOR_GC_TRACE") != "1" {
		t.Fatal("attribution routes require the same declared numeric Go profiles")
	}
	e := openPublishedEnvironment(t, nil)
	before := readyLifecycleSnapshot(t, e)
	stopTrace := startModelTrace(t, e, before)
	var epochs []residentConditioningEpoch
	var mappingWindows []residentMappingWindow
	var nativeSeries []residentNativeSeries
	var successes, failures int
	var normalizedSHA string
	var requestFailure *residentAttributionFailure
	completed := false
	started := time.Now()
	defer func() {
		operations, traceErr := stopTrace()
		var measuredOperations *uint64
		if traceErr == nil {
			measuredOperations = &operations
		}
		writeResidentEvidence(t, e, "native-attribution-"+route+".json", map[string]any{
			"route": route, "scope": e.selection.Scope,
			"runtime_input_identity": e.selection.RuntimeInputIdentity,
			"api_image":              e.selection.APIImage, "backend_image": e.selection.BackendImage,
			"source_audio_sha256": acceptanceSHA256(e.audio), "canonical_audio_sha256": normalizedSHA,
			"started_at": started, "finished_at": time.Now(), "completed": completed,
			"declared_requests":                      declaredRequests,
			"request_failure":                        requestFailure,
			"declared_separate_preparation_requests": map[string]int{"longest_public": 1, "direct_backend": 30, "direct_native": 30, "ordinary_public": 30},
			"successes":                              successes, "errors": failures, "epochs": epochs,
			"native_generation":     before.PrivateHealth.ProcessGeneration,
			"model_path_operations": measuredOperations, "model_trace_available": traceErr == nil,
			"numeric_mapping_windows": mappingWindows,
			"direct_native_series":    nativeSeries,
			"qualification":           "not_established",
			"limits":                  "diagnostic route experiment only, not a replacement or repaired P07 verdict; same cold/longest/direct-comparison/thirty-public-request preparation; native route uses existing curl/new-connection protocol and bypasses API/supervisor inference routing, but background Go supervision remains; connection-reuse differences remain a confounder; idle route sends no foreground inference, waits two seconds per block before the same five resource observations, and retains actual timestamps rather than claiming identical wall duration; no allocator/driver/GC tuning or new privileges",
		})
	}()
	residentCompletedWindow(t, e, 5, time.Second)
	longest := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
	reply, err := e.client.Transcribe(e.ctx, longest, "longest.flac", []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}})
	if err != nil || reply.Status != http.StatusOK || reply.Text == "" {
		t.Fatal("attribution longest-input preparation failed")
	}
	residentCompletedWindow(t, e, 3, time.Second)
	runResidentDirectComparisons(t, e)
	warmup := runResidentCalibrationWarmup(t, e, before)
	policy, err := selectResidentGoHeapPolicy(e.ctx, e, "backend", before, warmup.Started)
	if err != nil {
		t.Fatal(err)
	}
	input := prepareResidentDirectInput(t, e)
	preparedName := prepareResidentNativeMount(t, e, input)
	normalizedSHA = acceptanceSHA256(input.wav)
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(e.settings.Timeouts.OperationSeconds)*time.Second)
	defer cancel()
	observe := func() residentResourcePoint {
		point, state, err := residentActualResources(ctx, e)
		if err != nil || residentGenerationStable(before, state) != nil {
			t.Fatal("attribution lost actual healthy resident identity/resource observations")
		}
		return point
	}
	for block := 0; block < residentConditioningMaxBlocks; block++ {
		if route == "native" {
			series, err := collectResidentNativeSeries(ctx, e.target, input.containers.BackendID, e.settings.Backend.NativePort, preparedName, input.duration, time.Duration(e.settings.Timeouts.OperationSeconds)*time.Second, residentConditioningBlockRequests)
			nativeSeries = append(nativeSeries, series)
			successes += series.Successes
			failures += series.Errors
			if err != nil {
				t.Fatal("attribution direct native series failed without retry")
			}
		} else if route == "idle" {
			// The matched background control sends no inference. It observes the
			// same unchanged supervision after a fixed two-second quiet interval.
			select {
			case <-ctx.Done():
				t.Fatal("attribution background control reached its deadline")
			case <-time.After(2 * time.Second):
			}
		} else {
			for sample := 0; sample < residentConditioningBlockRequests; sample++ {
				reply, err := e.client.Transcribe(ctx, e.audio, "jfk.wav", []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}})
				if checkPublicCase(reply, err, http.StatusOK, "") != nil {
					failures++
					requestFailure = &residentAttributionFailure{Block: block + 1, Request: sample + 1, TransportError: err != nil}
					if reply.Status != 0 {
						status := reply.Status
						requestFailure.HTTPStatus = &status
					}
					if err == nil && reply.Status == http.StatusOK {
						speech := observePublicSpeech(reply.Text)
						requestFailure.Speech = &speech
					}
					t.Fatal("attribution public series failed without retry")
				}
				successes++
			}
		}
		epoch := residentConditioningEpoch{Completion: observe()}
		mappings, err := observeResidentMappings(ctx, e, before)
		if err != nil {
			t.Fatal(err)
		}
		mappingWindows = append(mappingWindows, mappings)
		for i := 0; i < 5; i++ {
			epoch.Idle = append(epoch.Idle, observe())
			if i < 4 {
				select {
				case <-ctx.Done():
					t.Fatal("attribution reached its unchanged observation deadline")
				case <-time.After(time.Second):
				}
			}
		}
		series, through, err := readResidentSupervisorHeap(ctx, e, *policy)
		if err != nil {
			t.Fatal(err)
		}
		epoch.SupervisorGC, epoch.GCObservedThrough = series, &through
		epochs = append(epochs, epoch)
		writeResidentEvidence(t, e, "native-attribution-"+route+"-partial.json", map[string]any{"epochs": epochs, "successes": successes, "errors": failures, "qualification": "not_established"})
	}
	completed = true
	t.Log("bounded route attribution completed; this does not establish P07 memory qualification")
}
