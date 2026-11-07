package acceptance

import (
	"errors"
	"time"
)

var errResidentSupervisorHeapUnknown = errors.New("Go-process live-heap evidence is unavailable, unverified, incomparable or not fresh")
var errResidentSupervisorHeapGrowth = errors.New("Go-process marked live heap exceeded its independently frozen quantization interval")

// Shared only by the two demonstrated Go roles. Process identity is separate
// from its paired native episode: a native crash need not restart either Go PID1.
type residentSupervisorHeapScope struct {
	Role, ContainerID, ImageID string
	StartedAt                  time.Time
	NativeGeneration, Model    string
}

func (s residentSupervisorHeapScope) valid() bool {
	return (s.Role == "api" || s.Role == "backend") && s.ContainerID != "" && s.ImageID != "" && !s.StartedAt.IsZero() && s.NativeGeneration != "" && s.Model != ""
}

type residentSupervisorHeapPolicy struct {
	Scope           residentSupervisorHeapScope
	OrdinaryStart   time.Time
	ProfileVerified bool
}
type residentSupervisorHeapBound struct {
	Policy            residentSupervisorHeapPolicy
	MarkedLiveBytes   residentByteInterval
	FrozenAt          time.Time
	LastCycle         uint64
	LastUptimeSeconds float64
}

func eligibleResidentGCCycles(policy residentSupervisorHeapPolicy, series *residentGCSeries, start, end time.Time) ([]residentGCRecord, error) {
	if !policy.ProfileVerified || !policy.Scope.valid() || start.IsZero() || !end.After(start) || series == nil {
		return nil, errResidentSupervisorHeapUnknown
	}
	var eligible []residentGCRecord
	var previous *residentGCRecord
	for _, record := range series.Records {
		if record.ObservedAt == nil || record.ObservedAt.IsZero() || record.AfterGCLiveBytes.UpperExclusive <= record.AfterGCLiveBytes.LowerInclusive || record.AfterGCLiveBytes.UpperExclusive-record.AfterGCLiveBytes.LowerInclusive != 1<<20 || record.AfterGCLiveBytes.LowerInclusive%(1<<20) != 0 {
			return nil, errResidentSupervisorHeapUnknown
		}
		if previous != nil && (record.Cycle <= previous.Cycle || record.UptimeSeconds < previous.UptimeSeconds || record.ObservedAt.Before(*previous.ObservedAt)) {
			return nil, errResidentSupervisorHeapUnknown
		}
		if !record.ObservedAt.Before(start) && !record.ObservedAt.After(end) {
			if record.Forced {
				return nil, errResidentSupervisorHeapUnknown
			}
			eligible = append(eligible, record)
		}
		copy := record
		previous = &copy
	}
	return eligible, nil
}
func freezeResidentSupervisorHeap(policy residentSupervisorHeapPolicy, series *residentGCSeries, observedThrough time.Time) (*residentSupervisorHeapBound, error) {
	records, err := eligibleResidentGCCycles(policy, series, policy.OrdinaryStart, observedThrough)
	if err != nil || len(records) < 3 {
		return nil, errResidentSupervisorHeapUnknown
	}
	interval := records[0].AfterGCLiveBytes
	for _, record := range records {
		if record.AfterGCLiveBytes != interval {
			return nil, errResidentSupervisorHeapGrowth
		}
	}
	last := records[len(records)-1]
	return &residentSupervisorHeapBound{Policy: policy, MarkedLiveBytes: interval, FrozenAt: observedThrough, LastCycle: last.Cycle, LastUptimeSeconds: last.UptimeSeconds}, nil
}

type residentPostWorkGCProof struct {
	WorkCompleted   time.Time
	ObservedThrough time.Time
	Barrier         *residentGCRecord
	Comparable      []residentGCRecord
}

// The first natural completion after work is a barrier: its marking could have
// begun during work. Serialized subsequent cycles start after that collection.
// Keep three subsequent comparable cycles: four total, not three new requests.
func residentPostWorkCycles(policy residentSupervisorHeapPolicy, series *residentGCSeries, workCompleted, observedThrough time.Time) (residentPostWorkGCProof, error) {
	proof := residentPostWorkGCProof{WorkCompleted: workCompleted, ObservedThrough: observedThrough}
	if series == nil {
		series = &residentGCSeries{}
	}
	records, err := eligibleResidentGCCycles(policy, series, workCompleted, observedThrough)
	if err != nil {
		return proof, err
	}
	for _, record := range records {
		if !record.ObservedAt.After(workCompleted) {
			continue
		}
		if proof.Barrier == nil {
			copy := record
			proof.Barrier = &copy
		} else {
			proof.Comparable = append(proof.Comparable, record)
		}
	}
	return proof, nil
}
func freezeResidentSupervisorPostWork(policy residentSupervisorHeapPolicy, series *residentGCSeries, workCompleted, observedThrough time.Time) (*residentSupervisorHeapBound, error) {
	proof, err := residentPostWorkCycles(policy, series, workCompleted, observedThrough)
	if err != nil || len(proof.Comparable) < 3 {
		return nil, errResidentSupervisorHeapUnknown
	}
	bound, err := freezeResidentSupervisorHeap(policy, &residentGCSeries{Records: proof.Comparable}, observedThrough)
	if err != nil {
		return nil, errResidentSupervisorHeapUnknown
	}
	return bound, nil
}

func checkResidentSupervisorMarkedIntervals(upperExclusive uint64, records []residentGCRecord) error {
	if upperExclusive == 0 || len(records) == 0 {
		return errResidentSupervisorHeapUnknown
	}
	for _, record := range records {
		if record.AfterGCLiveBytes.UpperExclusive > upperExclusive {
			return errResidentSupervisorHeapGrowth
		}
	}
	return nil
}

func checkResidentSupervisorHeap(bound *residentSupervisorHeapBound, series *residentGCSeries, measuredStart, lastCompletion, observedThrough time.Time) ([]residentGCRecord, error) {
	if bound == nil || measuredStart.Before(bound.FrozenAt) || lastCompletion.Before(measuredStart) || !observedThrough.After(lastCompletion) {
		return nil, errResidentSupervisorHeapUnknown
	}
	proof, err := residentPostWorkCycles(bound.Policy, series, lastCompletion, observedThrough)
	if err != nil {
		return nil, err
	}
	if proof.Barrier != nil && (proof.Barrier.Cycle <= bound.LastCycle || proof.Barrier.UptimeSeconds < bound.LastUptimeSeconds) {
		return nil, errResidentSupervisorHeapUnknown
	}
	var fresh []residentGCRecord
	for _, record := range proof.Comparable {
		if record.Cycle <= bound.LastCycle || record.UptimeSeconds < bound.LastUptimeSeconds {
			return nil, errResidentSupervisorHeapUnknown
		}
		if err := checkResidentSupervisorMarkedIntervals(bound.MarkedLiveBytes.UpperExclusive, []residentGCRecord{record}); err != nil {
			return nil, err
		}
		fresh = append(fresh, record)
	}
	if len(fresh) < 3 {
		return fresh, errResidentSupervisorHeapUnknown
	}
	return fresh, nil
}

// A proven native recovery changes the paired episode, not the surviving Go
// process or its pre-frozen interval. No interval is learned from failed work.
func rebindResidentNativeEpisode(bound *residentSupervisorHeapBound, next residentSupervisorHeapScope, replacementProven bool) (*residentSupervisorHeapBound, error) {
	if bound == nil || !replacementProven || !next.valid() {
		return nil, errResidentSupervisorHeapUnknown
	}
	old := bound.Policy.Scope
	if old.Role != next.Role || old.ContainerID != next.ContainerID || old.ImageID != next.ImageID || !old.StartedAt.Equal(next.StartedAt) || old.Model != next.Model || old.NativeGeneration == next.NativeGeneration {
		return nil, errResidentSupervisorHeapUnknown
	}
	copy := *bound
	copy.Policy.Scope = next
	return &copy, nil
}

// The Go-only fields are not object-liveness counters: aggregate backend RSS
// includes supervisor housekeeping, and supervisor RSS/anonymous/file values may
// change while natural post-GC marked live heap remains in its calibrated bin.
func residentSupervisorRSSDimension(dimension int) bool {
	return dimension == 1 || dimension >= 4 && dimension <= 6
}
