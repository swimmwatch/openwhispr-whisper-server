package acceptance

import (
	"context"
	"errors"
	"time"
)

const residentConditioningMaxBlocks = 6
const residentConditioningBlockRequests = 20

var errResidentConditioningBudget = errors.New("resident conditioning did not stabilize within its declared request/time budget")
var errResidentConditioningCap = errors.New("resident conditioning exceeded its independently frozen hard cap")
var errResidentConditioningUnknown = errors.New("resident conditioning requires actual complete process RSS observations")

type residentConditioningEpoch struct {
	Completion        residentResourcePoint
	Idle              []residentResourcePoint
	SupervisorGC      *residentGCSeries
	GCObservedThrough *time.Time
}
type residentConditioning struct {
	Epochs             []residentConditioningEpoch
	Stable             bool
	FrozenNoise        [10]uint64
	ResourceTailStable bool
	PostWorkGC         *residentPostWorkGCProof
	APIPostWorkGC      *residentPostWorkGCProof
}
type residentConditioningPolicy struct {
	Noise           [10]uint64
	Preconditioning residentEnvelope
	BackendPeak     uint64
	SupervisorHeap  *residentSupervisorHeapPolicy
	APIHeap         *residentSupervisorHeapPolicy
}

func residentConditioningMetrics(p residentResourcePoint) ([10]uint64, error) {
	var result [10]uint64
	if p.At.IsZero() || p.BackendProcessRSS == nil || p.BackendProcessRSS.At.IsZero() {
		return result, errResidentConditioningUnknown
	}
	if p.APIDeletedScratchFDs != 0 || p.APIDeletedScratchBytes != 0 {
		return result, errResidentEnvelope
	}
	base := residentValues(p)
	copy(result[:4], base[:])
	s, n := p.BackendProcessRSS.Supervisor, p.BackendProcessRSS.Native
	copy(result[4:], []uint64{s.RSSBytes, s.AnonymousBytes, s.FileBytes, n.RSSBytes, n.AnonymousBytes, n.FileBytes})
	return result, nil
}
func newResidentConditioningPolicy(coldIdle, warmedIdle []residentResourcePoint, preconditioning residentEnvelope, backendPeak uint64) (residentConditioningPolicy, error) {
	policy := residentConditioningPolicy{Preconditioning: preconditioning, BackendPeak: backendPeak}
	if backendPeak == 0 {
		return policy, errResidentConditioningUnknown
	}
	for _, window := range [][]residentResourcePoint{coldIdle, warmedIdle} {
		if len(window) < 3 {
			return policy, errResidentConditioningUnknown
		}
		low, err := residentConditioningMetrics(window[0])
		if err != nil {
			return policy, err
		}
		high := low
		for _, point := range window {
			values, err := residentConditioningMetrics(point)
			if err != nil {
				return policy, err
			}
			for i, v := range values {
				low[i] = min(low[i], v)
				high[i] = max(high[i], v)
			}
		}
		for i := range low {
			policy.Noise[i] = max(policy.Noise[i], high[i]-low[i])
		}
	}
	return policy, nil
}
func (p residentConditioningPolicy) checkCap(point residentResourcePoint) error {
	values, err := residentConditioningMetrics(point)
	if err != nil {
		return err
	}
	high, spread := p.Preconditioning.Maximum.values(), p.Preconditioning.IdleSpread.values()
	for _, i := range []int{0, 2, 3} {
		if i == 0 && p.APIHeap != nil {
			continue
		}
		if spread[i] > ^uint64(0)-high[i] || values[i] > high[i]+spread[i] {
			return errResidentConditioningCap
		}
	}
	if values[1] > p.BackendPeak || values[4] > p.BackendPeak || values[7] > p.BackendPeak-values[4] {
		return errResidentConditioningCap
	}
	return nil
}
func (p residentConditioningPolicy) stableTail(epochs []residentConditioningEpoch) bool {
	if len(epochs) < 3 {
		return false
	}
	tail := epochs[len(epochs)-3:]
	first, err := residentConditioningMetrics(tail[0].Completion)
	if err != nil {
		return false
	}
	low, high := first, first
	second, _ := residentConditioningMetrics(tail[1].Completion)
	third, _ := residentConditioningMetrics(tail[2].Completion)
	for _, epoch := range tail {
		values, err := residentConditioningMetrics(epoch.Completion)
		if err != nil {
			return false
		}
		for i, v := range values {
			low[i] = min(low[i], v)
			high[i] = max(high[i], v)
		}
	}
	for i := range high {
		if i == 0 && p.APIHeap != nil {
			continue
		}
		if p.SupervisorHeap != nil && residentSupervisorRSSDimension(i) {
			continue
		}
		if high[i]-low[i] > p.Noise[i] {
			return false
		}
		if first[i] < second[i] && second[i] < third[i] {
			return false
		}
	}
	return true
}
func runResidentConditioning(ctx context.Context, policy residentConditioningPolicy, measure func(context.Context, int) (residentConditioningEpoch, error)) (residentConditioning, error) {
	result := residentConditioning{FrozenNoise: policy.Noise}
	for _, heap := range []*residentSupervisorHeapPolicy{policy.SupervisorHeap, policy.APIHeap} {
		if heap != nil && (!heap.ProfileVerified || !heap.Scope.valid()) {
			return result, errResidentSupervisorHeapUnknown
		}
	}
	for block := 0; block < residentConditioningMaxBlocks; block++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(errResidentConditioningBudget, err)
		}
		epoch, err := measure(ctx, block)
		if err != nil {
			return result, err
		}
		result.Epochs = append(result.Epochs, epoch)
		if err := ctx.Err(); err != nil {
			return result, errors.Join(errResidentConditioningBudget, err)
		}
		if len(epoch.Idle) < 3 {
			return result, errResidentConditioningUnknown
		}
		for _, point := range append([]residentResourcePoint{epoch.Completion}, epoch.Idle...) {
			if err := policy.checkCap(point); err != nil {
				return result, err
			}
		}
		if policy.stableTail(result.Epochs) {
			result.Stable = true
			result.ResourceTailStable = true
			return result, nil
		}
	}
	return result, errResidentConditioningBudget
}

// Conditioning never refits preconditioning RSS/GPU/FD observations or noise.
// API absolute RSS can be replaced only by independently frozen heap proof;
// only a stable backend tail supplies the new completed-state native RAM maximum.
func freezeConditionedResourceEnvelope(policy residentConditioningPolicy, result residentConditioning, earlier []residentResourcePoint) (residentEnvelope, error) {
	if !result.Stable || len(result.Epochs) < 3 || !policy.stableTail(result.Epochs) {
		return residentEnvelope{}, errResidentConditioningBudget
	}
	envelope := policy.Preconditioning
	envelope.APIHeapPending = policy.APIHeap != nil
	var processHigh [6]uint64
	points := append([]residentResourcePoint(nil), earlier...)
	for _, epoch := range result.Epochs[len(result.Epochs)-3:] {
		points = append(points, epoch.Completion)
		points = append(points, epoch.Idle...)
	}
	for _, point := range points {
		values, err := residentConditioningMetrics(point)
		if err != nil {
			return residentEnvelope{}, err
		}
		envelope.Maximum.BackendRSSBytes = max(envelope.Maximum.BackendRSSBytes, values[1])
		for i := range processHigh {
			processHigh[i] = max(processHigh[i], values[i+4])
		}
	}
	envelope.IdleSpread.BackendRSSBytes = policy.Noise[1]
	if envelope.IdleSpread.BackendRSSBytes > policy.BackendPeak || envelope.Maximum.BackendRSSBytes > policy.BackendPeak-envelope.IdleSpread.BackendRSSBytes {
		return residentEnvelope{}, errResidentConditioningCap
	}
	envelope.ProcessMaximum = &residentProcessBounds{Values: processHigh}
	var processSpread [6]uint64
	copy(processSpread[:], policy.Noise[4:])
	envelope.ProcessIdleSpread = &residentProcessBounds{Values: processSpread}
	if policy.SupervisorHeap != nil {
		envelope.BackendPhysicalPeak = policy.BackendPeak
	}
	return envelope, nil
}

func freezeConditionedResidentEnvelope(policy residentConditioningPolicy, result residentConditioning, earlier []residentResourcePoint) (residentEnvelope, error) {
	envelope, err := freezeConditionedResourceEnvelope(policy, result, earlier)
	if err != nil {
		return residentEnvelope{}, err
	}
	if policy.SupervisorHeap != nil {
		proof := result.PostWorkGC
		if proof == nil || proof.Barrier == nil || len(proof.Comparable) < 3 {
			return residentEnvelope{}, errResidentSupervisorHeapUnknown
		}
		records := append([]residentGCRecord{*proof.Barrier}, proof.Comparable...)
		bound, err := freezeResidentSupervisorPostWork(*policy.SupervisorHeap, &residentGCSeries{Records: records}, proof.WorkCompleted, proof.ObservedThrough)
		if err != nil {
			return residentEnvelope{}, err
		}
		envelope.SupervisorHeap = bound
	}
	if policy.APIHeap != nil {
		proof := result.APIPostWorkGC
		if proof == nil || proof.Barrier == nil || len(proof.Comparable) < 3 {
			return residentEnvelope{}, errResidentSupervisorHeapUnknown
		}
		records := append([]residentGCRecord{*proof.Barrier}, proof.Comparable...)
		bound, err := freezeResidentSupervisorPostWork(*policy.APIHeap, &residentGCSeries{Records: records}, proof.WorkCompleted, proof.ObservedThrough)
		if err != nil {
			return residentEnvelope{}, err
		}
		envelope.APIHeap, envelope.APIHeapPending = bound, false
	}
	return envelope, nil
}

// These arrays use the same explicit order as residentConditioningMetrics:
// supervisor RSS/anonymous/file, then native RSS/anonymous/file.
type residentProcessBounds struct{ Values [6]uint64 }

func checkResidentProcessSeries(points []residentResourcePoint, high, spread [6]uint64, supervisorHeapClass, requestBlockEndpoints bool) error {
	var previous [6]uint64
	var first [6]uint64
	increasing := [6]bool{true, true, true, true, true, true}
	for index, point := range points {
		values, err := residentConditioningMetrics(point)
		if err != nil {
			return err
		}
		var current [6]uint64
		copy(current[:], values[4:])
		for i, v := range current {
			if supervisorHeapClass && i < 3 {
				continue
			}
			if spread[i] > ^uint64(0)-high[i] || v > high[i]+spread[i] {
				return errResidentEnvelope
			}
			if index == 0 {
				first = current
			} else if v <= previous[i] {
				increasing[i] = false
			}
		}
		previous = current
	}
	if requestBlockEndpoints && len(points) >= 3 {
		for i := range previous {
			if supervisorHeapClass && i < 3 {
				continue
			}
			if increasing[i] && previous[i]-first[i] > spread[i] {
				return errResidentEnvelope
			}
		}
	}
	return nil
}
