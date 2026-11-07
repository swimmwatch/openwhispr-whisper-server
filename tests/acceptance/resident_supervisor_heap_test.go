package acceptance

import (
	"context"
	"testing"
	"time"
)

func heapPolicyFixture() residentSupervisorHeapPolicy {
	start := time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)
	return residentSupervisorHeapPolicy{Scope: residentSupervisorHeapScope{Role: "backend", ContainerID: "unit-owned-container", ImageID: "unit-image", StartedAt: start.Add(-time.Minute), NativeGeneration: "unit-generation", Model: "base"}, OrdinaryStart: start, ProfileVerified: true}
}
func heapSeriesFixture(policy residentSupervisorHeapPolicy, firstCycle uint64, offset time.Duration, bins ...uint64) *residentGCSeries {
	result := &residentGCSeries{}
	for i, bin := range bins {
		at := policy.OrdinaryStart.Add(offset + time.Duration(i)*time.Second)
		interval, _ := residentQuantizedBytes(bin, 1<<20)
		result.Records = append(result.Records, residentGCRecord{ObservedAt: &at, Cycle: firstCycle + uint64(i), UptimeSeconds: offset.Seconds() + float64(i) + 100, AfterGCLiveBytes: interval})
	}
	return result
}
func supervisorHousekeepingPoint(step uint64) residentResourcePoint {
	point := conditioningPoint(0)
	point.BackendProcessRSS.Supervisor = residentProcessRSS{RSSBytes: 100 + step*10, AnonymousBytes: 80 + step*9, FileBytes: 20 + step}
	point.BackendRSSBytes = 1000 + step*10
	return point
}
func TestResidentVerifiedHeapClassDoesNotEquateSupervisorRSSWithLiveCopies(t *testing.T) {
	policy := conditioningPolicy(t)
	heap := heapPolicyFixture()
	policy.SupervisorHeap = &heap
	series := heapSeriesFixture(heap, 1, time.Second, 1, 1, 1)
	result, err := runResidentConditioning(t.Context(), policy, func(_ context.Context, block int) (residentConditioningEpoch, error) {
		epoch := conditioningEpoch(supervisorHousekeepingPoint(uint64(block + 1)))
		epoch.SupervisorGC = series
		through := heap.OrdinaryStart.Add(10 * time.Second)
		epoch.GCObservedThrough = &through
		return epoch, nil
	})
	if err != nil || !result.Stable {
		t.Fatal("verified constant post-GC live heap was mislabeled as retained copies solely because supervisor RSS/file pages changed")
	}
	result.PostWorkGC = postWorkProofFixture(heap)
	envelope, err := freezeConditionedResidentEnvelope(policy, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = envelope.checkSeries([]residentResourcePoint{supervisorHousekeepingPoint(4), supervisorHousekeepingPoint(5), supervisorHousekeepingPoint(6)}); err != nil {
		t.Fatal("bounded supervisor housekeeping became a live-object leak after freezing")
	}
}
func TestResidentSupervisorHeapRequiresFreshNaturalComparableCycles(t *testing.T) {
	policy := heapPolicyFixture()
	start := policy.OrdinaryStart
	baseline := heapSeriesFixture(policy, 1, time.Second, 1, 1, 1)
	bound, err := freezeResidentSupervisorHeap(policy, baseline, start.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	fresh := heapSeriesFixture(policy, 4, 8*time.Second, 1, 1, 1, 1)
	if _, err = checkResidentSupervisorHeap(bound, fresh, start.Add(5*time.Second), start.Add(7*time.Second), start.Add(13*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "too-few", "forced-only", "duplicate", "reset", "unknown-time", "unverified", "growth", "no-terminal-cycle", "reused-calibration"} {
		t.Run(mode, func(t *testing.T) {
			p := policy
			s := heapSeriesFixture(policy, 4, 8*time.Second, 1, 1, 1, 1)
			last := start.Add(7 * time.Second)
			switch mode {
			case "missing":
				s = nil
			case "too-few":
				s.Records = s.Records[:2]
			case "forced-only":
				for i := range s.Records {
					s.Records[i].Forced = true
				}
			case "duplicate":
				s.Records[1].Cycle = s.Records[0].Cycle
			case "reset":
				s.Records[2].Cycle = 1
			case "unknown-time":
				s.Records[0].ObservedAt = nil
			case "unverified":
				p.ProfileVerified = false
			case "growth":
				s = heapSeriesFixture(policy, 4, 8*time.Second, 1, 2, 3, 4)
			case "no-terminal-cycle":
				last = start.Add(12 * time.Second)
			case "reused-calibration":
				s = baseline
			}
			copy := *bound
			copy.Policy = p
			if _, err := checkResidentSupervisorHeap(&copy, s, start.Add(5*time.Second), last, start.Add(13*time.Second)); err == nil {
				t.Fatal("missing, stale, incomparable or growing live-heap evidence accepted")
			}
		})
	}
	unverified := policy
	unverified.ProfileVerified = false
	if _, err = freezeResidentSupervisorHeap(unverified, baseline, start.Add(5*time.Second)); err == nil {
		t.Fatal("unverified runtime profile established a heap bound")
	}
	if bound.MarkedLiveBytes.UpperExclusive != 2*(1<<20) {
		t.Fatal("calibration added padding to the quantized upper bound")
	}
}
