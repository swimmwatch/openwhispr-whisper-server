package acceptance

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// TranscriptionWorkload uses caller-owned, bounded public fixture bytes. Source,
// license, checksum, model and configuration identity belong to the target report;
// this HTTP workload alone does not establish those identities or CUDA activity.
type TranscriptionWorkload struct {
	Audio         []byte
	Filename      string
	Fields        []FormField
	AudioDuration time.Duration
	Samples       int
}

// MeasurePublicSteadyState executes the declared sample count at concurrency one,
// counting every attempted request, not retrying failed inference. Successful
// public durations include response consumption. Backend/queue/startup/resource
// observations remain unknown and require independent target instrumentation.
// A cancelled run returns its partial measurements AND the cancellation error.
func MeasurePublicSteadyState(ctx context.Context, client *PublicClient, workload TranscriptionWorkload) (MeasurementSummary, error) {
	if client == nil || workload.Samples < 30 || workload.AudioDuration <= 0 || len(workload.Audio) == 0 || workload.Filename == "" {
		return MeasurementSummary{}, errors.New("steady measurements require a client, audio fixture, duration and at least 30 samples")
	}
	run := MeasurementRun{}
	for range workload.Samples {
		if err := ctx.Err(); err != nil {
			summary, _ := SummarizeMeasurements(run)
			return summary, err
		}
		reply, err := client.Transcribe(ctx, workload.Audio, workload.Filename, workload.Fields)
		run.Samples = append(run.Samples, MeasurementSample{
			Succeeded:      err == nil && reply.Status == http.StatusOK,
			PublicDuration: reply.PublicDuration,
			AudioDuration:  &workload.AudioDuration,
		})
	}
	summary, err := SummarizeMeasurements(run)
	if err != nil {
		return MeasurementSummary{}, err
	}
	return summary, ctx.Err()
}
