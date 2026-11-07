package acceptance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestResidentConcurrentTimingsRequireRealQueueAndMatchIdentity(t *testing.T) {
	samples, start, end, api, backend := timingFixture(t)
	if _, err := correlateResidentConcurrent(samples, []string{"A", "B"}, start, end, api, backend); err == nil {
		t.Fatal("sequential traffic was mislabeled concurrent queue evidence")
	}
	api = []byte(strings.ReplaceAll(string(api), "00:00:00.31", "00:00:00.15"))
	// Keep B dispatch after A's measured private completion while admission overlaps.
	api = []byte(strings.ReplaceAll(string(api), "00:00:00.155", "00:00:00.315"))
	samples[1].PublicDuration = 250 * time.Millisecond
	backendEvents, err := parseTimingRecords(backend)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range backendEvents {
		if event.Event == "request_completed" && (event.RequestID == "A" || event.RequestID == "B") {
			event.Event = "queue_completed"
			event.Time = event.Time.Add(20 * time.Millisecond)
			data, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			api = append(api, append(data, '\n')...)
		}
	}
	// Caller completion order is deliberately reversed; ID binding must still win.
	run, err := correlateResidentConcurrent([]MeasurementSample{samples[1], samples[0]}, []string{"B", "A"}, start, end, api, backend)
	if err != nil {
		t.Fatal(err)
	}
	if *run.Samples[0].BackendDuration != 40*time.Millisecond || *run.Samples[1].BackendDuration != 50*time.Millisecond || *run.Samples[0].QueueDuration != 165*time.Millisecond {
		t.Fatal("concurrent timings matched completion order instead of request identity")
	}
	if _, err = correlateResidentConcurrent(samples, []string{"A", "A"}, start, end, api, backend); err == nil {
		t.Fatal("ambiguous request identity accepted")
	}
	missing := []byte(strings.Replace(string(backend), `"event":"request_completed","method":"POST","request_id":"B"`, `"event":"unavailable","method":"POST","request_id":"B"`, 1))
	if _, err = correlateResidentConcurrent(samples, []string{"A", "B"}, start, end, api, missing); err == nil {
		t.Fatal("missing private timing became a measured duration")
	}
}
