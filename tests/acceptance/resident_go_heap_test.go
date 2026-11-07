package acceptance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestResidentAPIHeapReplacesOnlyAbsoluteRSSAndNeedsProof(t *testing.T) {
	policy := conditioningPolicy(t)
	heap := heapPolicyFixture()
	heap.Scope.Role, heap.Scope.ContainerID, heap.Scope.ImageID = "api", "actual-api-container", "actual-api-image"
	policy.APIHeap = &heap
	result, err := runResidentConditioning(t.Context(), policy, func(_ context.Context, block int) (residentConditioningEpoch, error) {
		point := conditioningPoint(0)
		point.APIRSSBytes = 1000 + uint64(block)
		return conditioningEpoch(point), nil
	})
	if err != nil || !result.Stable {
		t.Fatal("API RSS housekeeping still rejected before independent heap calibration", err)
	}
	if _, err = freezeConditionedResidentEnvelope(policy, result, nil); !errors.Is(err, errResidentSupervisorHeapUnknown) {
		t.Fatal("missing API proof qualified an RSS bypass")
	}
	pending, pendingErr := freezeConditionedResourceEnvelope(policy, result, nil)
	if pendingErr != nil {
		t.Fatal(pendingErr)
	}
	if err := pending.checkSeries([]residentResourcePoint{conditioningPoint(0)}); !errors.Is(err, errResidentSupervisorHeapUnknown) {
		t.Fatal("pending API calibration became a qualified resource envelope")
	}
	result.APIPostWorkGC = postWorkProofFixture(heap)
	envelope, err := freezeConditionedResidentEnvelope(policy, result, nil)
	if err != nil || envelope.APIHeap == nil {
		t.Fatal("API proof did not freeze an independent bound", err)
	}
	point := conditioningPoint(0)
	point.APIRSSBytes = 5000
	if err = envelope.checkSeries([]residentResourcePoint{point}); err != nil {
		t.Fatal("frozen API heap evidence still subject to absolute RSS rejection", err)
	}
	endpoints := []residentResourcePoint{point, point, point}
	endpoints[1].APIRSSBytes++
	endpoints[2].APIRSSBytes += 3
	if err = envelope.checkSeries(endpoints); err == nil {
		t.Fatal("API request-block accumulation was erased")
	}
	for _, fault := range []string{"fd", "scratch", "gpu", "native"} {
		p := point
		copy := *p.BackendProcessRSS
		p.BackendProcessRSS = &copy
		switch fault {
		case "fd":
			p.APIFDs++
		case "scratch":
			p.APIDeletedScratchBytes++
		case "gpu":
			p.GPUBytes++
		case "native":
			p.BackendProcessRSS.Native.RSSBytes += 11
		}
		if err := envelope.checkSeries([]residentResourcePoint{p}); err == nil {
			t.Fatalf("API heap bypassed independent %s obligation", fault)
		}
	}
	fresh := heapSeriesFixture(heap, 5, 12*time.Second, 1, 1, 1, 1)
	if _, err = checkResidentSupervisorHeap(envelope.APIHeap, fresh, heap.OrdinaryStart.Add(11*time.Second), heap.OrdinaryStart.Add(11500*time.Millisecond), heap.OrdinaryStart.Add(17*time.Second)); err != nil {
		t.Fatal(err)
	}
	fresh.Records[2].AfterGCLiveBytes, _ = residentQuantizedBytes(2, 1<<20)
	if _, err = checkResidentSupervisorHeap(envelope.APIHeap, fresh, heap.OrdinaryStart.Add(11*time.Second), heap.OrdinaryStart.Add(11500*time.Millisecond), heap.OrdinaryStart.Add(17*time.Second)); !errors.Is(err, errResidentSupervisorHeapGrowth) {
		t.Fatal("API retained heap growth escaped independent bound")
	}
}

func TestResidentGoRolesFreezeIndependentIntervals(t *testing.T) {
	policy := conditioningPolicy(t)
	supervisor, api := heapPolicyFixture(), heapPolicyFixture()
	api.Scope.Role, api.Scope.ContainerID, api.Scope.ImageID = "api", "actual-api", "api-image"
	policy.SupervisorHeap, policy.APIHeap = &supervisor, &api
	result, err := runResidentConditioning(t.Context(), policy, func(context.Context, int) (residentConditioningEpoch, error) {
		return conditioningEpoch(conditioningPoint(0)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result.PostWorkGC = postWorkProofFixture(supervisor)
	apiSeries := heapSeriesFixture(api, 1, time.Second, 2, 2, 2, 2)
	proof, err := residentPostWorkCycles(api, apiSeries, api.OrdinaryStart, api.OrdinaryStart.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	result.APIPostWorkGC = &proof
	envelope, err := freezeConditionedResidentEnvelope(policy, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.APIHeap.MarkedLiveBytes == envelope.SupervisorHeap.MarkedLiveBytes || envelope.APIHeap.Policy.Scope.Role != "api" || envelope.SupervisorHeap.Policy.Scope.Role != "backend" {
		t.Fatal("API heap inherited the supervisor interval or identity")
	}
	if envelope.Maximum != policy.Preconditioning.Maximum || envelope.IdleSpread != policy.Preconditioning.IdleSpread {
		t.Fatal("heap calibration refitted independent physical/API RSS observations")
	}
	crossed := envelope
	crossed.APIHeap = envelope.SupervisorHeap
	if err := crossed.checkSeries([]residentResourcePoint{conditioningPoint(0)}); !errors.Is(err, errResidentSupervisorHeapUnknown) {
		t.Fatal("supervisor proof qualified an API RSS bypass")
	}
}

func TestResidentPassiveSamplesDoNotBecomeRequestBlockAccumulation(t *testing.T) {
	policy := conditioningPolicy(t)
	envelope := policy.Preconditioning
	envelope.ProcessMaximum = &residentProcessBounds{Values: [6]uint64{100, 80, 20, 1010, 810, 200}}
	envelope.ProcessIdleSpread = &residentProcessBounds{}
	points := []residentResourcePoint{conditioningPoint(0), conditioningPoint(1), conditioningPoint(2)}
	if err := envelope.checkResourceSeries(points, false); err != nil {
		t.Fatal("passive samples below every absolute cap were misclassified as request-block accumulation", err)
	}
	if err := envelope.checkSeries(points); err == nil {
		t.Fatal("request-block endpoint accumulation escaped the retained trend guard")
	}
	points[2].GPUBytes = 501
	if err := envelope.checkResourceSeries(points, false); err == nil {
		t.Fatal("passive sampling lost a genuine absolute GPU cap")
	}
}
