package acceptance

import (
	"context"
	"testing"
)

// Run independent performance observations even when resource calibration is
// unavailable. Their success never overrides the separate retention failure.
func runResidentPerformanceComponents(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	before := readyLifecycleSnapshot(t, e)
	ctx, cancel := context.WithCancel(e.ctx)
	type result struct {
		activity GPUActivity
		err      error
	}
	done := make(chan result, 1)
	ready := make(chan struct{})
	go func() {
		activity, err := ObserveGPUActivity(ctx, *before.NativeHostPID, ready)
		done <- result{activity, err}
	}()
	joined := false
	t.Cleanup(func() {
		cancel()
		if !joined {
			<-done
		}
	})
	select {
	case <-ready:
	case early := <-done:
		joined = true
		t.Fatalf("comparison GPU observer unavailable: failed=%t", early.err != nil)
	case <-e.ctx.Done():
		t.Fatal("comparison GPU observer startup cancelled")
	}
	concurrent := runResidentConcurrent(t, e)
	direct, native := runResidentDirectComparisons(t, e)
	after := readyLifecycleSnapshot(t, e)
	if err := residentGenerationStable(before, after); err != nil {
		t.Fatal(err)
	}
	cancel()
	gpu := <-done
	joined = true
	if gpu.err != nil || gpu.activity.Samples == 0 || gpu.activity.PeakSMPercent == 0 || gpu.activity.PeakMemoryBytes == 0 {
		t.Fatal("comparison did not establish actual scoped CUDA activity")
	}
	writeResidentEvidence(t, e, "resident-independent-comparison.json", map[string]any{
		"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity,
		"api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage,
		"concurrent_public": concurrent, "concurrency": 3, "concurrent_waves": 2,
		"direct_backend": direct, "direct_backend_samples": 30,
		"direct_backend_includes_supervisor": true, "gpu_activity": gpu.activity,
		"direct_backend_wall_includes": "production upstream health/inference, two docker exec/curl starts and response consumption; candidate FFmpeg normalization outside timing",
		"direct_native":                native,
		"api_overhead_subtraction":     "not performed: public and direct-backend wall intervals are not comparable transport paths",
		"retention_gate":               "separate; this independent workload does not establish calibrated retention",
		"qualification":                "not_established",
	})
}
