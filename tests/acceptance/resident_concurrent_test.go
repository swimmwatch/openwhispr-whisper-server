package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// Correlation is by the caller-owned validated request ID, never goroutine return
// order. The general sequential correlator remains the authority for timings.
func correlateResidentConcurrent(samples []MeasurementSample, ids []string, start, end time.Time, apiLogs, backendLogs []byte) (MeasurementRun, error) {
	if len(samples) == 0 || len(samples) != len(ids) {
		return MeasurementRun{}, errTimingCorrelation
	}
	api, err := parseTimingRecords(apiLogs)
	if err != nil {
		return MeasurementRun{}, err
	}
	backend, err := parseTimingRecords(backendLogs)
	if err != nil {
		return MeasurementRun{}, err
	}
	run := MeasurementRun{}
	seen := map[string]bool{}
	for i, id := range ids {
		if id == "" || seen[id] {
			return MeasurementRun{}, errTimingCorrelation
		}
		seen[id] = true
		filter := func(records []timingRecord) []byte {
			var out []byte
			for _, record := range records {
				if record.RequestID == id || record.Event == "backend_starting" || record.Event == "backend_ready" {
					line, _ := json.Marshal(record)
					out = append(out, line...)
					out = append(out, '\n')
				}
			}
			return out
		}
		correlated, err := CorrelateTimings([]MeasurementSample{samples[i]}, start, end, filter(api), filter(backend))
		if err != nil {
			return run, err
		}
		run.Samples = append(run.Samples, correlated.Samples[0])
	}
	// Require a genuinely waiting request: admission precedes another request's
	// completion while its own dispatch follows it. Tiny scheduling noise alone
	// is not accepted as a concurrent queue workload.
	waiting := false
	for _, admit := range api {
		if admit.Event != "queue_admitted" || !seen[admit.RequestID] {
			continue
		}
		for _, complete := range backend {
			if complete.Event != "request_completed" || complete.Status == nil || *complete.Status != http.StatusOK || complete.RequestID == admit.RequestID || !seen[complete.RequestID] {
				continue
			}
			for _, dispatch := range api {
				if dispatch.Event == "queue_dispatched" && dispatch.RequestID == admit.RequestID && admit.Time.Before(complete.Time) && !dispatch.Time.Before(complete.Time) {
					waiting = true
				}
			}
		}
	}
	if !waiting {
		return run, errors.New("declared concurrent workload did not observe actual queue waiting")
	}
	if err := checkResidentQueueSerialization(api, ids); err != nil {
		return run, err
	}
	return run, nil
}

func runResidentConcurrent(t *testing.T, e *acceptanceEnvironment) MeasurementSummary {
	t.Helper()
	const concurrency, waves = 3, 2
	start := time.Now()
	var samples []MeasurementSample
	var ids []string
	for wave := 0; wave < waves; wave++ {
		ctx, cancel := context.WithCancel(e.ctx)
		gate := make(chan struct{})
		done := make([]chan asyncPublicReply, concurrency)
		consumed := make([]bool, concurrency)
		t.Cleanup(func() {
			cancel()
			for i, replies := range done {
				if !consumed[i] {
					select {
					case <-replies:
					case <-time.After(5 * time.Second):
						t.Error("owned concurrent measurement did not stop after cancellation")
					}
				}
			}
		})
		for index := 0; index < concurrency; index++ {
			id := "resident-queue-" + strconv.Itoa(wave) + "-" + strconv.Itoa(index)
			ids = append(ids, id)
			client := identifiedLocalClient(t, e, id)
			done[index] = make(chan asyncPublicReply, 1)
			go func(out chan<- asyncPublicReply) {
				defer client.Close()
				<-gate
				reply, err := client.Transcribe(ctx, e.audio, "jfk.wav", []FormField{{"model", e.settings.Models.Served}, {"language", "en"}, {"temperature", "0"}})
				out <- asyncPublicReply{reply, err}
			}(done[index])
		}
		close(gate)
		for index, reply := range done {
			select {
			case result := <-reply:
				consumed[index] = true
				duration := 11 * time.Second
				success := result.err == nil && result.reply.Status == http.StatusOK && result.reply.Text != ""
				samples = append(samples, MeasurementSample{Succeeded: success, PublicDuration: result.reply.PublicDuration, AudioDuration: &duration})
				if !success {
					t.Fatal("concurrent measurement failed without retry")
				}
			case <-ctx.Done():
				t.Fatal("concurrent measurement cancelled")
			}
		}
		cancel()
	}
	api, err := e.target.Logs(e.ctx, "api")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := e.target.Logs(e.ctx, "backend")
	if err != nil {
		t.Fatal(err)
	}
	run, err := correlateResidentConcurrent(samples, ids, start, time.Now(), api, backend)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeMeasurements(run)
	if err != nil {
		t.Fatal(err)
	}
	return summary
}
