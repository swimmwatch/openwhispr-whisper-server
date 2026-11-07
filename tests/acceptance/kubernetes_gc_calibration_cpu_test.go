package acceptance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestP08HeapCalibrationKeepsOnlySafeWindowDiagnostics(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	initial := p08HeapOwner{Process: p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "a"}, {ID: "b"}}, apiStart: 1, backendStart: 2, nativePID: 3, nativeStart: 4, generation: "g", fingerprint: "f"}, Containers: [2]p08HeapContainer{{Image: "api-image", Started: base}, {Image: "backend-image", Started: base}}}
	owner := func(context.Context) (p08HeapOwner, error) { return initial, nil }
	for _, item := range []struct {
		name string
		err  error
		want string
	}{
		{"classified", p08GCFailure("window_parse", "logs_too_large"), "gc diagnostic: window_parse/logs_too_large"},
		{"private", errors.New("private command output"), errResidentSupervisorHeapUnknown.Error()},
		{"invalid-fields", &p08GCDiagnosticError{Stage: "private", Reason: "private"}, "gc diagnostic: unknown/unknown"},
	} {
		t.Run(item.name, func(t *testing.T) {
			window := func(context.Context) (p08AnchoredEvidence, error) { return p08AnchoredEvidence{}, item.err }
			work := func(context.Context) (int, error) { t.Fatal("work started before calibration"); return 0, nil }
			evidence, err := calibrateP08Heap(t.Context(), "base", owner, window, work)
			if err == nil || !errors.Is(err, errResidentSupervisorHeapUnknown) || err.Error() != item.want || evidence.Verdict != "unknown" {
				t.Fatalf("unexpected diagnostic or verdict: %v", err)
			}
		})
	}
}

func TestP08HeapCalibrationUsesFrozenNaturalBoundsAcrossWork(t *testing.T) {
	for _, mode := range []string{"valid", "missing-api", "missing-backend", "forced", "growth", "stale", "replacement", "work-failed"} {
		t.Run(mode, func(t *testing.T) {
			base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			initial := p08HeapOwner{Process: p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "a"}, {ID: "b"}}, apiStart: 1, backendStart: 2, nativePID: 3, nativeStart: 4, generation: "g", fingerprint: "f"}, Containers: [2]p08HeapContainer{{Image: "api-image", Started: base}, {Image: "backend-image", Started: base}}}
			ownerCalls := 0
			owner := func(context.Context) (p08HeapOwner, error) {
				ownerCalls++
				if mode == "replacement" && ownerCalls >= 3 {
					copy := initial
					copy.Process.generation = "changed"
					return copy, nil
				}
				return initial, nil
			}
			makeWindow := func(offset int, missing, forced, growth bool) p08AnchoredWindow {
				start := base.Add(time.Duration(offset) * time.Second)
				end := start.Add(6 * time.Second)
				w := p08AnchoredWindow{Start: start, End: end}
				if missing {
					return w
				}
				series := &residentGCSeries{}
				for i := 1; i <= 4; i++ {
					at := start.Add(time.Duration(i) * time.Second)
					lower := uint64(1 << 20)
					if growth && i == 4 {
						lower = 2 << 20
					}
					series.Records = append(series.Records, residentGCRecord{ObservedAt: &at, Cycle: uint64(offset + i), UptimeSeconds: float64(offset + i), AfterGCLiveBytes: residentByteInterval{LowerInclusive: lower, UpperExclusive: lower + 1<<20}, Forced: forced && i == 2})
				}
				w.GC = series
				return w
			}
			calls := 0
			window := func(context.Context) (p08AnchoredEvidence, error) {
				calls++
				offset := 10
				if calls == 2 {
					offset = 30
					if mode == "stale" {
						offset = 10
					}
				}
				return p08AnchoredEvidence{API: makeWindow(offset, mode == "missing-api", mode == "forced", mode == "growth" && calls == 2), Backend: makeWindow(offset, mode == "missing-backend", false, false)}, nil
			}
			workloadCalls := 0
			work := func(context.Context) (int, error) {
				workloadCalls++
				if mode == "work-failed" {
					return 3, errors.New("private")
				}
				return 30, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			got, err := calibrateP08Heap(ctx, "base", owner, window, work)
			if mode == "valid" {
				if err != nil || got.Verdict != "partial_not_qualified" || got.APICalibrationCycles != 3 || got.BackendCalibrationCycles != 3 || got.APIPostCycles != 3 || got.BackendPostCycles != 3 || workloadCalls != 1 {
					t.Fatalf("calibration incomplete: %v", err)
				}
				return
			}
			if err == nil || got.Verdict != "unknown" {
				t.Fatal("unverified or unsafe calibration was accepted")
			}
			if (mode == "missing-api" || mode == "missing-backend" || mode == "forced") && workloadCalls != 0 {
				t.Fatal("work started before frozen calibration")
			}
		})
	}
}
