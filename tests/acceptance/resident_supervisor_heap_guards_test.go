package acceptance

import (
	"context"
	"testing"
	"time"
)

func conditionedHeapEnvelopeFixture(t *testing.T) (residentEnvelope, residentConditioningPolicy) {
	t.Helper()
	policy := conditioningPolicy(t)
	heap := heapPolicyFixture()
	policy.SupervisorHeap = &heap
	series := heapSeriesFixture(heap, 1, time.Second, 1, 1, 1)
	through := heap.OrdinaryStart.Add(10 * time.Second)
	result, err := runResidentConditioning(t.Context(), policy, func(_ context.Context, index int) (residentConditioningEpoch, error) {
		epoch := conditioningEpoch(supervisorHousekeepingPoint(uint64(index + 1)))
		epoch.SupervisorGC = series
		epoch.GCObservedThrough = &through
		return epoch, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result.PostWorkGC = postWorkProofFixture(heap)
	envelope, err := freezeConditionedResidentEnvelope(policy, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	return envelope, policy
}
func TestResidentHeapClassCannotExcuseNativeAPIOrPhysicalFailures(t *testing.T) {
	envelope, policy := conditionedHeapEnvelopeFixture(t)
	for _, mode := range []string{"native-RSS", "native-anonymous", "native-file", "API-RSS", "API-FD", "deleted-FD", "GPU", "physical-cap", "unknown-process"} {
		t.Run(mode, func(t *testing.T) {
			point := supervisorHousekeepingPoint(4)
			switch mode {
			case "native-RSS":
				point.BackendProcessRSS.Native.RSSBytes += 11
			case "native-anonymous":
				point.BackendProcessRSS.Native.AnonymousBytes += 11
			case "native-file":
				point.BackendProcessRSS.Native.FileBytes++
			case "API-RSS":
				point.APIRSSBytes = 103
			case "API-FD":
				point.APIFDs = 9
			case "deleted-FD":
				point.APIDeletedScratchFDs = 1
			case "GPU":
				point.GPUBytes = 501
			case "physical-cap":
				point.BackendRSSBytes = 2001
			case "unknown-process":
				point.BackendProcessRSS = nil
			}
			if err := envelope.checkSeries([]residentResourcePoint{point}); err == nil {
				t.Fatal("flat marked heap excused an independent native/API/physical observation failure")
			}
		})
	}
	plain := policy.Preconditioning
	if err := plain.checkSeries([]residentResourcePoint{supervisorHousekeepingPoint(6)}); err == nil {
		t.Fatal("uninstrumented RSS path silently acquired the class-specific exception")
	}
	invalid := envelope
	copy := *envelope.SupervisorHeap
	copy.Policy.ProfileVerified = false
	invalid.SupervisorHeap = &copy
	if err := invalid.checkSeries([]residentResourcePoint{supervisorHousekeepingPoint(6)}); err == nil {
		t.Fatal("unknown/unverified profile excused RSS growth")
	}
}
func TestResidentHeapClassStillRejectsStrictNativeGrowth(t *testing.T) {
	_, policy := conditionedHeapEnvelopeFixture(t)
	through := policy.SupervisorHeap.OrdinaryStart.Add(10 * time.Second)
	series := heapSeriesFixture(*policy.SupervisorHeap, 1, time.Second, 1, 1, 1)
	result, err := runResidentConditioning(t.Context(), policy, func(_ context.Context, index int) (residentConditioningEpoch, error) {
		epoch := conditioningEpoch(conditioningPoint(uint64(index + 1)))
		epoch.SupervisorGC = series
		epoch.GCObservedThrough = &through
		return epoch, nil
	})
	if err == nil || result.Stable {
		t.Fatal("verified Go heap bypassed native strict-growth conditioning")
	}
}
