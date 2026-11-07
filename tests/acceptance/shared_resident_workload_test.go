package acceptance

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type sharedResidentWorkloadResult struct {
	PreflightPassed bool
	Summary         MeasurementSummary
}

func measureSharedResidentWorkload(ctx context.Context, client *PublicClient, audio []byte, model string, samples int) (sharedResidentWorkloadResult, error) {
	var result sharedResidentWorkloadResult
	if client == nil || len(audio) == 0 || model == "" || samples < 30 {
		return result, errors.New("invalid resident public workload")
	}
	fields := []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}}
	reply, err := client.Transcribe(ctx, audio, "jfk.wav", fields)
	if checkPublicCase(reply, err, http.StatusOK, "") != nil {
		return result, errors.New("resident public speech preflight failed")
	}
	result.PreflightPassed = true
	result.Summary, err = MeasurePublicSteadyState(ctx, client, TranscriptionWorkload{
		Audio: audio, Filename: "jfk.wav", Fields: fields, AudioDuration: 11 * time.Second, Samples: samples,
	})
	if err != nil {
		return result, err
	}
	if result.Summary.Successes != samples || result.Summary.Errors != 0 || result.Summary.Public == nil || result.Summary.Public.Count != samples || result.Summary.Public.Median <= 0 {
		return result, errors.New("resident public steady-state measurement incomplete")
	}
	return result, nil
}
