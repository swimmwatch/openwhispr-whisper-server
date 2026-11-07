package acceptance

import (
	"testing"
	"time"
)

func postWorkProofFixture(policy residentSupervisorHeapPolicy) *residentPostWorkGCProof {
	series := heapSeriesFixture(policy, 1, time.Second, 1, 1, 1, 1)
	proof, _ := residentPostWorkCycles(policy, series, policy.OrdinaryStart.Add(time.Millisecond), policy.OrdinaryStart.Add(10*time.Second))
	return &proof
}

func TestResidentPostWorkHeapMissingVariableAndRetainedCopies(t *testing.T) {
	policy := heapPolicyFixture()
	start := policy.OrdinaryStart
	for _, mode := range []string{"missing", "three-total", "forced-only", "variable", "reset", "stale", "unknown-time"} {
		t.Run(mode, func(t *testing.T) {
			series := heapSeriesFixture(policy, 1, time.Second, 1, 1, 1, 1)
			boundary := start.Add(time.Millisecond)
			switch mode {
			case "missing":
				series = nil
			case "three-total":
				series.Records = series.Records[:3]
			case "forced-only":
				for i := range series.Records {
					series.Records[i].Forced = true
				}
			case "variable":
				series.Records[2].AfterGCLiveBytes, _ = residentQuantizedBytes(2, 1<<20)
			case "reset":
				series.Records[3].Cycle = 1
			case "stale":
				boundary = start.Add(6 * time.Second)
			case "unknown-time":
				series.Records[0].ObservedAt = nil
			}
			if _, err := freezeResidentSupervisorPostWork(policy, series, boundary, start.Add(7*time.Second)); err == nil {
				t.Fatal("unknown/variable post-work evidence established a calibrated bin")
			}
		})
	}
	baseline := heapSeriesFixture(policy, 1, time.Second, 1, 1, 1, 1)
	bound, err := freezeResidentSupervisorPostWork(policy, baseline, start.Add(time.Millisecond), start.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	for _, heldMiB := range []uint64{8, 143} {
		records := heapSeriesFixture(policy, 5, 8*time.Second, heldMiB, heldMiB, heldMiB, heldMiB)
		if _, err = checkResidentSupervisorHeap(bound, records, start.Add(7*time.Second), start.Add(7500*time.Millisecond), start.Add(13*time.Second)); err == nil {
			t.Fatal("held request/model-sized objects survived post-work GC without detection")
		}
	}
}

func TestResidentPostWorkHeapDoesNotLearnInFlightGarbage(t *testing.T) {
	policy := heapPolicyFixture()
	start := policy.OrdinaryStart
	// The active cycle and the first post-work completion can still describe
	// request work/floating garbage. The following three natural cycles cannot.
	series := heapSeriesFixture(policy, 1, time.Second, 2, 2, 1, 1, 1)
	boundary := start.Add(1500 * time.Millisecond)
	bound, err := freezeResidentSupervisorPostWork(policy, series, boundary, start.Add(7*time.Second))
	if err != nil || bound == nil || bound.MarkedLiveBytes.UpperExclusive != 2*(1<<20) {
		t.Fatal("active/floating garbage was mistaken for retained post-work live objects or widened the reference bin")
	}
	fresh := heapSeriesFixture(policy, 6, 8*time.Second, 3, 3, 1, 1, 1)
	if _, err = checkResidentSupervisorHeap(bound, fresh, start.Add(7*time.Second), start.Add(8500*time.Millisecond), start.Add(14*time.Second)); err != nil {
		t.Fatal("measured active garbage contaminated the independent post-work liveness check")
	}
}
