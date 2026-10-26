package acceptance

import (
	"fmt"
	"slices"
	"time"
)

// MeasurementRun keeps startup/model-load observations separate from steady requests.
// Nil optional durations mean unknown, not zero; this type carries no qualification state.
// Callers must not concurrently mutate observations while they are being summarized.
type MeasurementRun struct {
	Startup, ModelLoad *time.Duration
	Samples            []MeasurementSample
}

// MeasurementSample contains actual observations for one steady request, not inferred
// timing. QueueDuration and BackendDuration must be non-overlapping portions of the
// measured PublicDuration. AudioDuration, when known, must be positive.
type MeasurementSample struct {
	Succeeded                                     bool
	PublicDuration                                time.Duration
	AudioDuration, QueueDuration, BackendDuration *time.Duration
	Resources                                     ResourceObservation
}

// ResourceObservation contains measured byte counts. A non-nil zero is a real
// observation, including GPUUsedBytes; it does not establish GPU inference.
type ResourceObservation struct {
	APIRSSBytes, BackendRSSBytes, GPUUsedBytes *uint64
}

// DurationStatistics uses the middle value (mean of the two middle values for an
// even count) and nearest-rank p95. Half-nanosecond medians round down.
type DurationStatistics struct {
	Count       int
	Median, P95 time.Duration
}

// RatioStatistics summarizes per-request backend-execution/audio-duration ratios,
// not the ratio of aggregate durations. Only directly measured pairs contribute.
type RatioStatistics struct {
	Count       int
	Median, P95 float64
}

// ByteStatistics retains how many requests supplied an observation and its peak.
type ByteStatistics struct {
	Count int
	Peak  uint64
}

type ResourceStatistics struct {
	APIRSS, BackendRSS, GPUUsed *ByteStatistics
}

// MeasurementSummary has successful-request timing distributions only. Errors are
// counted separately; resource peaks include failed requests as well. Nil statistics
// indicate no corresponding observations. No minimum sample count or SLO is enforced.
type MeasurementSummary struct {
	Startup, ModelLoad                  *time.Duration
	Successes, Errors                   int
	Public, Queue, Backend, APIOverhead *DurationStatistics
	BackendAudioRatio                   *RatioStatistics
	Resources                           ResourceStatistics
}

// SummarizeMeasurements rejects invalid observations and returns an independently
// owned summary without reordering or retaining caller samples. API overhead is
// public minus measured queue and backend durations, only when both are known.
func SummarizeMeasurements(run MeasurementRun) (MeasurementSummary, error) {
	for name, duration := range map[string]*time.Duration{"startup": run.Startup, "model load": run.ModelLoad} {
		if duration != nil && *duration < 0 {
			return MeasurementSummary{}, fmt.Errorf("negative %s duration", name)
		}
	}
	for i, sample := range run.Samples {
		if err := validateMeasurementSample(sample); err != nil {
			return MeasurementSummary{}, fmt.Errorf("sample %d: %w", i, err)
		}
	}

	result := MeasurementSummary{
		Startup: copyMeasurementDuration(run.Startup), ModelLoad: copyMeasurementDuration(run.ModelLoad),
	}
	var public, queue, backend, overhead []time.Duration
	var ratios []float64
	for _, sample := range run.Samples {
		observeMeasurementBytes(&result.Resources.APIRSS, sample.Resources.APIRSSBytes)
		observeMeasurementBytes(&result.Resources.BackendRSS, sample.Resources.BackendRSSBytes)
		observeMeasurementBytes(&result.Resources.GPUUsed, sample.Resources.GPUUsedBytes)
		if !sample.Succeeded {
			result.Errors++
			continue
		}
		result.Successes++
		public = append(public, sample.PublicDuration)
		if sample.QueueDuration != nil {
			queue = append(queue, *sample.QueueDuration)
		}
		if sample.BackendDuration != nil {
			backend = append(backend, *sample.BackendDuration)
			if sample.AudioDuration != nil {
				// Positive integer nanosecond audio and finite duration bounds make
				// NaN/Inf impossible; no unvalidated floating-point input is accepted.
				ratios = append(ratios, float64(*sample.BackendDuration)/float64(*sample.AudioDuration))
			}
		}
		if sample.QueueDuration != nil && sample.BackendDuration != nil {
			overhead = append(overhead, sample.PublicDuration-*sample.QueueDuration-*sample.BackendDuration)
		}
	}
	result.Public = summarizeMeasurementDurations(public)
	result.Queue = summarizeMeasurementDurations(queue)
	result.Backend = summarizeMeasurementDurations(backend)
	result.APIOverhead = summarizeMeasurementDurations(overhead)
	if len(ratios) != 0 {
		slices.Sort(ratios)
		middle := len(ratios) / 2
		median := ratios[middle]
		if len(ratios)%2 == 0 {
			median = (ratios[middle-1] + median) / 2
		}
		result.BackendAudioRatio = &RatioStatistics{
			Count: len(ratios), Median: median, P95: ratios[len(ratios)-len(ratios)/20-1],
		}
	}
	return result, nil
}

func validateMeasurementSample(sample MeasurementSample) error {
	if sample.PublicDuration < 0 {
		return fmt.Errorf("negative public duration")
	}
	if sample.AudioDuration != nil && *sample.AudioDuration <= 0 {
		return fmt.Errorf("audio duration must be positive when measured")
	}
	for name, duration := range map[string]*time.Duration{"queue": sample.QueueDuration, "backend": sample.BackendDuration} {
		if duration != nil && (*duration < 0 || *duration > sample.PublicDuration) {
			return fmt.Errorf("%s duration is outside public duration", name)
		}
	}
	// Subtract instead of adding components, which could overflow time.Duration.
	if sample.QueueDuration != nil && sample.BackendDuration != nil && *sample.BackendDuration > sample.PublicDuration-*sample.QueueDuration {
		return fmt.Errorf("measured queue and backend exceed public duration")
	}
	return nil
}

func summarizeMeasurementDurations(values []time.Duration) *DurationStatistics {
	if len(values) == 0 {
		return nil
	}
	slices.Sort(values)
	middle := len(values) / 2
	median := values[middle]
	if len(values)%2 == 0 {
		lower := values[middle-1]
		median = lower + (median-lower)/2
	}
	// ceil(0.95*n) == n-floor(n/20), without floating-point rounding or n*95 overflow.
	return &DurationStatistics{Count: len(values), Median: median, P95: values[len(values)-len(values)/20-1]}
}

func observeMeasurementBytes(stats **ByteStatistics, value *uint64) {
	if value == nil {
		return
	}
	if *stats == nil {
		*stats = &ByteStatistics{}
	}
	(*stats).Count++
	(*stats).Peak = max((*stats).Peak, *value)
}

func copyMeasurementDuration(value *time.Duration) *time.Duration {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
