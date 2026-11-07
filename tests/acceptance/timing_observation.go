package acceptance

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

var errTimingCorrelation = errors.New("timing observations are incomplete, ambiguous or inconsistent; private records withheld")

type timingRecord struct {
	Time       time.Time `json:"time"`
	Event      string    `json:"event"`
	RequestID  string    `json:"request_id"`
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	Status     *int      `json:"status"`
	DurationMS *int64    `json:"duration_ms"`
}

// CorrelateTimings requires an isolated concurrency-one workload and same-clock
// Docker logs. Successful samples correspond to chronological public completions;
// without request IDs in MeasurementSample this cannot authenticate arbitrary
// foreign POST traffic merely from matching cardinality. The caller owns isolation.
//
// Queue uses actual admission/dispatch timestamps. BackendDuration is the observed
// private HTTP duration at millisecond precision, not pure CUDA/kernel time.
// ModelLoad, when unambiguous, is the native starting-to-ready observation interval
// including initialization and health probes, never a claim of pure weight-file IO.
// Startup remains unknown for the caller to supply independently. On any error,
// uncorrelated components remain nil while independent valid samples are returned.
func CorrelateTimings(samples []MeasurementSample, start, end time.Time, apiLogs, backendLogs []byte) (MeasurementRun, error) {
	run := MeasurementRun{Samples: make([]MeasurementSample, len(samples))}
	var successful []int
	for i, sample := range samples {
		run.Samples[i] = sample
		run.Samples[i].AudioDuration = copyMeasurementDuration(sample.AudioDuration)
		run.Samples[i].QueueDuration, run.Samples[i].BackendDuration = nil, nil
		run.Samples[i].Resources = ResourceObservation{timingCopyBytes(sample.Resources.APIRSSBytes), timingCopyBytes(sample.Resources.BackendRSSBytes), timingCopyBytes(sample.Resources.GPUUsedBytes)}
		if sample.Succeeded {
			successful = append(successful, i)
		}
	}
	if start.IsZero() || !end.After(start) {
		return run, errTimingCorrelation
	}
	for _, sample := range run.Samples {
		if validateMeasurementSample(sample) != nil {
			return run, errTimingCorrelation
		}
	}
	api, err := parseTimingRecords(apiLogs)
	if err != nil {
		return run, errTimingCorrelation
	}
	backend, err := parseTimingRecords(backendLogs)
	if err != nil {
		return run, errTimingCorrelation
	}
	failed := false
	var starts, readies []time.Time
	for _, record := range backend {
		if record.Event == "backend_starting" {
			starts = append(starts, record.Time)
		}
		if record.Event == "backend_ready" {
			readies = append(readies, record.Time)
		}
	}
	if len(starts) == 1 && len(readies) == 1 {
		if readies[0].Before(starts[0]) {
			failed = true
		} else if !readies[0].After(start) {
			duration := readies[0].Sub(starts[0])
			run.ModelLoad = &duration
		}
	}
	inside := func(at time.Time) bool { return !at.Before(start) && !at.After(end) }
	var completions []timingRecord
	admitted, dispatched := map[string][]timingRecord{}, map[string][]timingRecord{}
	for _, record := range api {
		if !inside(record.Time) {
			continue
		}
		switch record.Event {
		case "queue_admitted":
			admitted[record.RequestID] = append(admitted[record.RequestID], record)
		case "queue_dispatched":
			dispatched[record.RequestID] = append(dispatched[record.RequestID], record)
		case "request_completed":
			if record.Method == http.MethodPost && record.Route == "/v1/audio/transcriptions" && record.Status != nil && *record.Status == http.StatusOK {
				completions = append(completions, record)
			}
		}
	}
	if len(completions) != len(successful) {
		return run, errTimingCorrelation
	}
	slices.SortFunc(completions, func(a, b timingRecord) int { return a.Time.Compare(b.Time) })
	ids := map[string]bool{}
	for i, record := range completions {
		if !logging.ValidRequestID(record.RequestID) || ids[record.RequestID] || i > 0 && record.Time.Equal(completions[i-1].Time) {
			return run, errTimingCorrelation
		}
		ids[record.RequestID] = true
	}
	private := map[string][]timingRecord{}
	for _, record := range backend {
		if inside(record.Time) && record.Event == "request_completed" && record.Method == http.MethodPost && record.Route == "/inference" && record.Status != nil && *record.Status == http.StatusOK {
			if !ids[record.RequestID] {
				failed = true
			}
			private[record.RequestID] = append(private[record.RequestID], record)
		}
	}
	for order, completion := range completions {
		index := successful[order]
		admit, dispatch, inference := admitted[completion.RequestID], dispatched[completion.RequestID], private[completion.RequestID]
		if len(admit) != 1 || len(dispatch) != 1 || len(inference) != 1 {
			failed = true
			continue
		}
		if dispatch[0].Time.Before(admit[0].Time) || completion.Time.Before(dispatch[0].Time) || inference[0].Time.Before(dispatch[0].Time) || completion.Time.Before(inference[0].Time) {
			failed = true
			continue
		}
		queue := dispatch[0].Time.Sub(admit[0].Time)
		native, ok := timingMilliseconds(inference[0].DurationMS)
		_, apiOK := timingMilliseconds(completion.DurationMS)
		if !ok || !apiOK {
			failed = true
			continue
		}
		candidate := run.Samples[index]
		candidate.QueueDuration, candidate.BackendDuration = &queue, &native
		if validateMeasurementSample(candidate) != nil {
			failed = true
			continue
		}
		run.Samples[index] = candidate
	}
	if failed {
		return run, errTimingCorrelation
	}
	return run, nil
}

func parseTimingRecords(content []byte) ([]timingRecord, error) {
	if len(content) > 1<<20 {
		return nil, errTimingCorrelation
	}
	var records []timingRecord
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if line[0] != '{' {
			prefix, body, ok := strings.Cut(string(line), " ")
			if !ok {
				return nil, errTimingCorrelation
			}
			if _, err := time.Parse(time.RFC3339Nano, prefix); err != nil {
				return nil, errTimingCorrelation
			}
			line = []byte(body)
		}
		var record timingRecord
		if json.Unmarshal(line, &record) != nil || record.Time.IsZero() {
			return nil, errTimingCorrelation
		}
		records = append(records, record)
	}
	return records, nil
}

func timingMilliseconds(value *int64) (time.Duration, bool) {
	if value == nil || *value < 0 || *value > math.MaxInt64/int64(time.Millisecond) {
		return 0, false
	}
	return time.Duration(*value) * time.Millisecond, true
}
func timingCopyBytes(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
