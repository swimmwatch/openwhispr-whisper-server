package acceptance

import (
	"encoding/json"
	"errors"
	"math"
	"time"
)

var errResidentEnvelope = errors.New("resident resources exceed the measured completed-work envelope or accumulate across blocks")

// All values are actual observations. A point is created only after the observer
// has checked presence of every source counter; zero is never an unknown value.
type residentResourcePoint struct {
	At                                             time.Time
	APIRSSBytes, BackendRSSBytes, GPUBytes, APIFDs uint64
	APIDeletedScratchFDs, APIDeletedScratchBytes   uint64
	// A separately timestamped serial probe; not assumed atomic with aggregate RSS.
	BackendProcessRSS *residentRSSBreakdown
}

// Maximum comes from completed idle/longest-input observations, NOT their
// transient peaks. IdleSpread is the measured idle variability, not an SLO or
// a percentage of model size. This permits the calibrated latest native result
// and allocator high-water state, but does not grant one extra copy per request.
// If actual platform housekeeping exceeds this calibration, the run fails and
// needs evidence/review; this helper never automatically widens its own bounds.
type residentResourceBounds struct {
	APIRSSBytes, BackendRSSBytes, GPUBytes, APIFDs uint64
}
type residentEnvelope struct {
	Maximum, IdleSpread               residentResourceBounds
	ProcessMaximum, ProcessIdleSpread *residentProcessBounds
	SupervisorHeap                    *residentSupervisorHeapBound
	APIHeap                           *residentSupervisorHeapBound
	// APIHeapPending is calibration intent only; it cannot qualify checkSeries.
	APIHeapPending      bool
	BackendPhysicalPeak uint64
}

func residentValues(p residentResourcePoint) [4]uint64 {
	return [4]uint64{p.APIRSSBytes, p.BackendRSSBytes, p.GPUBytes, p.APIFDs}
}
func residentBounds(values [4]uint64) residentResourceBounds {
	return residentResourceBounds{APIRSSBytes: values[0], BackendRSSBytes: values[1], GPUBytes: values[2], APIFDs: values[3]}
}
func (b residentResourceBounds) values() [4]uint64 {
	return [4]uint64{b.APIRSSBytes, b.BackendRSSBytes, b.GPUBytes, b.APIFDs}
}
func calibrateResidentEnvelope(idle, longest []residentResourcePoint) (residentEnvelope, error) {
	if len(idle) < 3 || len(longest) < 3 {
		return residentEnvelope{}, errors.New("at least three completed idle and longest-input observations required")
	}
	low, high := residentValues(idle[0]), residentValues(idle[0])
	for _, p := range idle {
		if p.At.IsZero() || p.APIDeletedScratchFDs != 0 || p.APIDeletedScratchBytes != 0 {
			return residentEnvelope{}, errResidentEnvelope
		}
		values := residentValues(p)
		for i, v := range values {
			low[i] = min(low[i], v)
			high[i] = max(high[i], v)
		}
	}
	var spread [4]uint64
	for i := range spread {
		spread[i] = high[i] - low[i]
	}
	for _, p := range longest {
		if p.At.IsZero() || p.APIDeletedScratchFDs != 0 || p.APIDeletedScratchBytes != 0 {
			return residentEnvelope{}, errResidentEnvelope
		}
		for i, v := range residentValues(p) {
			high[i] = max(high[i], v)
		}
	}
	for i := range high {
		if spread[i] > math.MaxUint64-high[i] {
			return residentEnvelope{}, errResidentEnvelope
		}
	}
	return residentEnvelope{Maximum: residentBounds(high), IdleSpread: residentBounds(spread)}, nil
}
func (e residentEnvelope) checkSeries(points []residentResourcePoint) error {
	if err := e.checkHeapBounds(); err != nil {
		return err
	}
	return e.checkResources(points, e.SupervisorHeap != nil, true)
}

func (e residentEnvelope) checkPassiveSeries(points []residentResourcePoint) error {
	if err := e.checkHeapBounds(); err != nil {
		return err
	}
	return e.checkResourceSeries(points, e.SupervisorHeap != nil)
}

func (e residentEnvelope) checkHeapBounds() error {
	if e.APIHeapPending || e.APIHeap != nil && e.APIHeap.Policy.Scope.Role != "api" || e.SupervisorHeap != nil && e.SupervisorHeap.Policy.Scope.Role != "backend" {
		return errResidentSupervisorHeapUnknown
	}
	for _, bound := range []*residentSupervisorHeapBound{e.SupervisorHeap, e.APIHeap} {
		if bound != nil && (!bound.Policy.ProfileVerified || !bound.Policy.Scope.valid() || bound.FrozenAt.IsZero() || bound.MarkedLiveBytes.UpperExclusive == 0) {
			return errResidentSupervisorHeapUnknown
		}
	}
	return nil
}

// The calibration collector can check all actual physical/API/native bounds
// before live-heap learning. This does not establish a Go-heap envelope.
func (e residentEnvelope) checkResourceSeries(points []residentResourcePoint, supervisorHeapClass bool) error {
	return e.checkResources(points, supervisorHeapClass, false)
}

func (e residentEnvelope) checkResources(points []residentResourcePoint, supervisorHeapClass, requestBlockEndpoints bool) error {
	if len(points) == 0 {
		return errors.New("resident series is unavailable")
	}
	high, spread := e.Maximum.values(), e.IdleSpread.values()
	for _, p := range points {
		if p.At.IsZero() || p.APIDeletedScratchFDs != 0 || p.APIDeletedScratchBytes != 0 {
			return errResidentEnvelope
		}
		if supervisorHeapClass {
			metrics, err := residentConditioningMetrics(p)
			if err != nil {
				return err
			}
			if e.BackendPhysicalPeak == 0 || metrics[1] > e.BackendPhysicalPeak || metrics[4] > e.BackendPhysicalPeak || metrics[7] > e.BackendPhysicalPeak-metrics[4] {
				return errResidentEnvelope
			}
		}
		for i, v := range residentValues(p) {
			if supervisorHeapClass && i == 1 || (e.APIHeap != nil || e.APIHeapPending) && i == 0 {
				continue
			}
			if spread[i] > math.MaxUint64-high[i] || v > high[i]+spread[i] {
				return errResidentEnvelope
			}
		}
	}
	// Three block endpoints can expose linear accumulation even when the longest
	// fixture left a large high-water mark. One-time bounded retention is allowed;
	// strict growth at every endpoint beyond observed idle noise is not.
	if requestBlockEndpoints && len(points) >= 3 {
		first, last := residentValues(points[0]), residentValues(points[len(points)-1])
		for dimension := range first {
			if supervisorHeapClass && dimension == 1 {
				continue
			}
			increasing := true
			for i := 1; i < len(points); i++ {
				if residentValues(points[i])[dimension] <= residentValues(points[i-1])[dimension] {
					increasing = false
					break
				}
			}
			if increasing && last[dimension]-first[dimension] > spread[dimension] {
				return errResidentEnvelope
			}
		}
	}
	if e.ProcessMaximum != nil || e.ProcessIdleSpread != nil {
		if e.ProcessMaximum == nil || e.ProcessIdleSpread == nil {
			return errResidentConditioningUnknown
		}
		return checkResidentProcessSeries(points, e.ProcessMaximum.Values, e.ProcessIdleSpread.Values, supervisorHeapClass, requestBlockEndpoints)
	}
	return nil
}

func parseResidentDeletedScratch(data []byte) (uint64, uint64, error) {
	var wire struct {
		FDs   *uint64 `json:"deleted_scratch_fds"`
		Bytes *uint64 `json:"deleted_scratch_bytes"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.FDs == nil || wire.Bytes == nil {
		return 0, 0, errors.New("deleted-open scratch observation unavailable")
	}
	if *wire.FDs != 0 || *wire.Bytes != 0 {
		return *wire.FDs, *wire.Bytes, errors.New("completed request retained deleted-open scratch descriptors")
	}
	return *wire.FDs, *wire.Bytes, nil
}
