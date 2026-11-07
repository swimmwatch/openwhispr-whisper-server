package acceptance

import "slices"

// The same API process records dispatch before Infer and completion before the
// worker retires that owner. Match every selected successful operation exactly;
// one waiting request alone cannot establish that the other operations serialized.
func checkResidentQueueSerialization(records []timingRecord, ids []string) error {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || wanted[id] {
			return errTimingCorrelation
		}
		wanted[id] = true
	}
	var events []timingRecord
	for _, record := range records {
		if wanted[record.RequestID] && (record.Event == "queue_dispatched" || record.Event == "queue_completed") {
			events = append(events, record)
		}
	}
	slices.SortStableFunc(events, func(a, b timingRecord) int { return a.Time.Compare(b.Time) })
	started, completed := map[string]bool{}, map[string]bool{}
	active := ""
	for _, event := range events {
		if event.Event == "queue_dispatched" {
			if active != "" || started[event.RequestID] {
				return errTimingCorrelation
			}
			active, started[event.RequestID] = event.RequestID, true
		} else {
			if active != event.RequestID || completed[event.RequestID] {
				return errTimingCorrelation
			}
			active, completed[event.RequestID] = "", true
		}
	}
	if active != "" || len(completed) != len(wanted) {
		return errTimingCorrelation
	}
	return nil
}
