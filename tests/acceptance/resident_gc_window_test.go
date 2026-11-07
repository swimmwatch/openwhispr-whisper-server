package acceptance

import (
	"testing"
	"time"
)

func TestResidentGCInclusiveWindowsDeduplicateWithoutLosingRecords(t *testing.T) {
	policy := heapPolicyFixture()
	first := heapSeriesFixture(policy, 1, time.Second, 1, 1)
	merged, err := appendResidentGCWindow(nil, first)
	if err != nil {
		t.Fatal(err)
	}
	second := heapSeriesFixture(policy, 2, 2*time.Second, 1, 1, 1)
	merged, err = appendResidentGCWindow(merged, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Records) != 4 || merged.Records[2].Cycle != 3 || !residentGCCursor(policy.OrdinaryStart, merged).Equal(*merged.Records[3].ObservedAt) {
		t.Fatal("inclusive cursor lost or double-counted an actual GC event")
	}
	proof, err := residentPostWorkCycles(policy, merged, policy.OrdinaryStart, policy.OrdinaryStart.Add(5*time.Second))
	if err != nil || proof.Barrier == nil || len(proof.Comparable) != 3 {
		t.Fatal("one barrier and three distinct comparable cycles were not preserved across windows")
	}
	conflict := heapSeriesFixture(policy, 4, 4*time.Second, 2)
	if _, err = appendResidentGCWindow(merged, conflict); err == nil {
		t.Fatal("conflicting repeated cycle became trusted numeric evidence")
	}
	reset := heapSeriesFixture(policy, 1, 6*time.Second, 1)
	if _, err = appendResidentGCWindow(merged, reset); err == nil {
		t.Fatal("cycle reset became a new comparable event")
	}
	repeated, err := appendResidentGCWindow(first, first)
	if err != nil || len(repeated.Records) != 2 {
		t.Fatal("repeated inclusive window was counted as new GC coverage")
	}
}
