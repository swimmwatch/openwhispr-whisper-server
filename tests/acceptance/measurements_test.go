package acceptance

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func measurementDuration(value time.Duration) *time.Duration { return &value }
func measurementBytes(value uint64) *uint64                  { return &value }

func TestMeasurementSummaryMeasuredStatistics(t *testing.T) {
	run := MeasurementRun{
		Startup:   measurementDuration(100 * time.Second),
		ModelLoad: measurementDuration(80 * time.Second),
	}
	for i := 20; i >= 1; i-- {
		run.Samples = append(run.Samples, MeasurementSample{
			Succeeded: true, PublicDuration: time.Duration(i+3) * time.Second,
			AudioDuration:   measurementDuration(2 * time.Second),
			QueueDuration:   measurementDuration(time.Second),
			BackendDuration: measurementDuration(time.Duration(i) * time.Second),
			Resources:       ResourceObservation{APIRSSBytes: measurementBytes(uint64(i))},
		})
	}
	// Failed requests count as errors, not successful latency/ratio observations.
	// Their resource spikes must not disappear from the measured resource peaks.
	run.Samples = append(run.Samples, MeasurementSample{
		PublicDuration: time.Hour,
		Resources:      ResourceObservation{APIRSSBytes: measurementBytes(99), GPUUsedBytes: measurementBytes(0)},
	})
	got, err := SummarizeMeasurements(run)
	if err != nil {
		t.Fatal(err)
	}
	if got.Successes != 20 || got.Errors != 1 {
		t.Fatalf("counts: %+v", got)
	}
	wantPublic := DurationStatistics{Count: 20, Median: 13500 * time.Millisecond, P95: 22 * time.Second}
	wantBackend := DurationStatistics{Count: 20, Median: 10500 * time.Millisecond, P95: 19 * time.Second}
	wantQueue := DurationStatistics{Count: 20, Median: time.Second, P95: time.Second}
	wantOverhead := DurationStatistics{Count: 20, Median: 2 * time.Second, P95: 2 * time.Second}
	for name, pair := range map[string][2]*DurationStatistics{
		"public": {got.Public, &wantPublic}, "backend": {got.Backend, &wantBackend},
		"queue": {got.Queue, &wantQueue}, "overhead": {got.APIOverhead, &wantOverhead},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s: got %+v, want %+v", name, pair[0], pair[1])
		}
	}
	if got.BackendAudioRatio == nil || *got.BackendAudioRatio != (RatioStatistics{Count: 20, Median: 5.25, P95: 9.5}) {
		t.Fatalf("backend/audio ratio: %+v", got.BackendAudioRatio)
	}
	if got.Resources.APIRSS == nil || *got.Resources.APIRSS != (ByteStatistics{Count: 21, Peak: 99}) {
		t.Fatalf("API RSS observations: %+v", got.Resources.APIRSS)
	}
	if got.Resources.BackendRSS != nil {
		t.Fatal("unobserved backend RSS became known")
	}
	if got.Resources.GPUUsed == nil || *got.Resources.GPUUsed != (ByteStatistics{Count: 1, Peak: 0}) {
		t.Fatal("measured zero GPU bytes must remain distinct from unknown GPU use")
	}
	if got.Startup == nil || *got.Startup != 100*time.Second || got.ModelLoad == nil || *got.ModelLoad != 80*time.Second {
		t.Fatalf("startup/load not separated: %+v", got)
	}
}

func TestMeasurementSummaryUnknownAndPartialObservations(t *testing.T) {
	got, err := SummarizeMeasurements(MeasurementRun{Samples: []MeasurementSample{
		{Succeeded: true, PublicDuration: 10 * time.Second},
		{Succeeded: true, PublicDuration: 8 * time.Second, QueueDuration: measurementDuration(2 * time.Second)},
		{Succeeded: true, PublicDuration: 6 * time.Second, BackendDuration: measurementDuration(3 * time.Second)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Startup != nil || got.ModelLoad != nil || got.APIOverhead != nil || got.BackendAudioRatio != nil {
		t.Fatalf("unknown measurements were inferred: %+v", got)
	}
	if got.Public == nil || *got.Public != (DurationStatistics{Count: 3, Median: 8 * time.Second, P95: 10 * time.Second}) {
		t.Fatalf("public durations: %+v", got.Public)
	}
	if got.Queue == nil || got.Queue.Count != 1 || got.Queue.Median != 2*time.Second || got.Backend == nil || got.Backend.Count != 1 || got.Backend.Median != 3*time.Second {
		t.Fatalf("partial observations were dropped or expanded: %+v", got)
	}
	if got.Resources != (ResourceStatistics{}) {
		t.Fatal("unobserved resources became known")
	}
}

func TestMeasurementSummaryEmptyAndNoSuccess(t *testing.T) {
	for _, samples := range [][]MeasurementSample{nil, {}, {{
		PublicDuration:  5 * time.Second,
		AudioDuration:   measurementDuration(time.Second),
		QueueDuration:   measurementDuration(time.Second),
		BackendDuration: measurementDuration(2 * time.Second),
	}}} {
		got, err := SummarizeMeasurements(MeasurementRun{Samples: samples})
		if err != nil {
			t.Fatal(err)
		}
		if got.Successes != 0 || got.Errors != len(samples) || got.Public != nil || got.Queue != nil || got.Backend != nil || got.APIOverhead != nil || got.BackendAudioRatio != nil {
			t.Fatalf("no-success measurements fabricated statistics: %+v", got)
		}
	}
}

func TestMeasurementSummaryRejectsInvalidMeasurements(t *testing.T) {
	max := time.Duration(math.MaxInt64)
	cases := map[string]MeasurementRun{
		"startup":                 {Startup: measurementDuration(-1)},
		"load":                    {ModelLoad: measurementDuration(-1)},
		"public":                  {Samples: []MeasurementSample{{PublicDuration: -1}}},
		"audio-negative":          {Samples: []MeasurementSample{{AudioDuration: measurementDuration(-1)}}},
		"audio-zero":              {Samples: []MeasurementSample{{AudioDuration: measurementDuration(0)}}},
		"queue-negative":          {Samples: []MeasurementSample{{QueueDuration: measurementDuration(-1)}}},
		"backend-negative":        {Samples: []MeasurementSample{{BackendDuration: measurementDuration(-1)}}},
		"queue-outside-public":    {Samples: []MeasurementSample{{PublicDuration: time.Second, QueueDuration: measurementDuration(2 * time.Second)}}},
		"backend-outside-public":  {Samples: []MeasurementSample{{PublicDuration: time.Second, BackendDuration: measurementDuration(2 * time.Second)}}},
		"combined-outside-public": {Samples: []MeasurementSample{{PublicDuration: 3 * time.Second, QueueDuration: measurementDuration(2 * time.Second), BackendDuration: measurementDuration(2 * time.Second)}}},
		"combined-overflow":       {Samples: []MeasurementSample{{PublicDuration: max, QueueDuration: measurementDuration(max), BackendDuration: measurementDuration(1)}}},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := SummarizeMeasurements(run); err == nil {
				t.Fatal("invalid measurements were accepted")
			}
		})
	}
}

func TestMeasurementSummaryBoundaryArithmetic(t *testing.T) {
	max := time.Duration(math.MaxInt64)
	got, err := SummarizeMeasurements(MeasurementRun{Samples: []MeasurementSample{
		{Succeeded: true, PublicDuration: max, AudioDuration: measurementDuration(1), QueueDuration: measurementDuration(0), BackendDuration: measurementDuration(max)},
		{Succeeded: true, PublicDuration: max - 2, AudioDuration: measurementDuration(1), QueueDuration: measurementDuration(0), BackendDuration: measurementDuration(max - 2)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Public == nil || got.Public.Median != max-1 || got.Public.P95 != max {
		t.Fatalf("duration median overflowed: %+v", got.Public)
	}
	if got.APIOverhead == nil || got.APIOverhead.Median != 0 {
		t.Fatal("measured zero overhead was lost")
	}
	if got.BackendAudioRatio == nil || math.IsNaN(got.BackendAudioRatio.Median) || math.IsInf(got.BackendAudioRatio.Median, 0) || got.BackendAudioRatio.Median <= 0 {
		t.Fatalf("duration-derived ratio is not finite and positive: %+v", got.BackendAudioRatio)
	}
}

func TestMeasurementSummaryPerRequestRatioAndMeasuredZero(t *testing.T) {
	got, err := SummarizeMeasurements(MeasurementRun{Samples: []MeasurementSample{
		{Succeeded: true, PublicDuration: time.Second, AudioDuration: measurementDuration(time.Second), BackendDuration: measurementDuration(time.Second)},
		{Succeeded: true, PublicDuration: 9 * time.Second, AudioDuration: measurementDuration(3 * time.Second), BackendDuration: measurementDuration(9 * time.Second)},
		{Succeeded: true, PublicDuration: time.Second, AudioDuration: measurementDuration(10 * time.Second), BackendDuration: measurementDuration(time.Second)},
		{Succeeded: true, AudioDuration: measurementDuration(time.Second), QueueDuration: measurementDuration(0), BackendDuration: measurementDuration(0)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.BackendAudioRatio == nil || *got.BackendAudioRatio != (RatioStatistics{Count: 4, Median: 0.55, P95: 3}) {
		t.Fatalf("ratios must describe individual measured requests: %+v", got.BackendAudioRatio)
	}
	if got.APIOverhead == nil || *got.APIOverhead != (DurationStatistics{Count: 1}) {
		t.Fatalf("only measured components establish zero overhead: %+v", got.APIOverhead)
	}
}

func TestMeasurementSummaryDoesNotMutateOrAliasSamples(t *testing.T) {
	startup, load, audio, queue, backend, rss := time.Second, 2*time.Second, 3*time.Second, time.Second, time.Second, uint64(12)
	run := MeasurementRun{Startup: &startup, ModelLoad: &load, Samples: []MeasurementSample{
		{Succeeded: true, PublicDuration: 5 * time.Second, AudioDuration: &audio, QueueDuration: &queue, BackendDuration: &backend, Resources: ResourceObservation{APIRSSBytes: &rss}},
		{Succeeded: true, PublicDuration: 3 * time.Second},
	}}
	before := append([]MeasurementSample(nil), run.Samples...)
	got, err := SummarizeMeasurements(run)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(run.Samples, before) || startup != time.Second || load != 2*time.Second || audio != 3*time.Second || queue != time.Second || backend != time.Second || rss != 12 {
		t.Fatal("summarization mutated caller observations")
	}
	if got.Startup == nil || got.ModelLoad == nil || got.Resources.APIRSS == nil {
		t.Fatal("missing measured observations")
	}
	*got.Startup = 0
	*got.ModelLoad = 0
	got.Resources.APIRSS.Peak = 0
	if startup != time.Second || load != 2*time.Second || rss != 12 {
		t.Fatal("summary aliases caller observations")
	}
}
