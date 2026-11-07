package acceptance

import (
	"encoding/json"
	"reflect"
	"time"
)

// Inclusive Docker --since windows may repeat their final numeric record. Keep
// actual records once; never advance to the observer's wall clock, which could
// skip a record emitted between Docker's snapshot and that clock read.
func appendResidentGCWindow(accumulated *residentGCSeries, window *residentGCSeries) (*residentGCSeries, error) {
	if window == nil {
		return accumulated, nil
	}
	result := &residentGCSeries{}
	if accumulated != nil {
		result.Records = append(result.Records, accumulated.Records...)
	}
	for _, record := range window.Records {
		if record.ObservedAt == nil {
			return nil, errResidentSupervisorHeapUnknown
		}
		if len(result.Records) > 0 {
			last := result.Records[len(result.Records)-1]
			duplicate := false
			for _, prior := range result.Records {
				if prior.Cycle == record.Cycle {
					if !reflect.DeepEqual(record, prior) {
						return nil, errResidentSupervisorHeapUnknown
					}
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			if record.Cycle < last.Cycle || record.UptimeSeconds < last.UptimeSeconds || record.ObservedAt.Before(*last.ObservedAt) {
				return nil, errResidentSupervisorHeapUnknown
			}
		}
		result.Records = append(result.Records, record)
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > dockerOutputLimit {
		return nil, errResidentSupervisorHeapUnknown
	}
	return result, nil
}
func residentGCCursor(fallback time.Time, series *residentGCSeries) time.Time {
	if series == nil || len(series.Records) == 0 {
		return fallback
	}
	// fallback may be an explicit Docker --until cutoff captured BEFORE reading.
	// Unlike an observer's later wall clock, that completed window is safe to
	// advance across even if it contained only request logs and no GC records.
	last := *series.Records[len(series.Records)-1].ObservedAt
	if last.After(fallback) {
		return last
	}
	return fallback
}
