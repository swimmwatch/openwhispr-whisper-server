package acceptance

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func timingFixture(t *testing.T) ([]MeasurementSample, time.Time, time.Time, []byte, []byte) {
	t.Helper()
	start := time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)
	line := func(offset time.Duration, event, id, method, route string, status int, duration *int64) string {
		record := map[string]any{"time": start.Add(offset).Format(time.RFC3339Nano), "event": event, "request_id": id}
		if method != "" {
			record["method"], record["route"], record["status"] = method, route, status
		}
		if duration != nil {
			record["duration_ms"] = *duration
		}
		content, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		return start.Add(offset+time.Microsecond).Format(time.RFC3339Nano) + " " + string(content) + "\n"
	}
	ms := func(value int64) *int64 { return &value }
	api := line(110*time.Millisecond, "queue_admitted", "A", "", "", 0, nil) + line(120*time.Millisecond, "queue_dispatched", "A", "", "", 0, nil) + line(310*time.Millisecond, "queue_admitted", "B", "", "", 0, nil) + line(315*time.Millisecond, "queue_dispatched", "B", "", "", 0, nil)
	// Physical output order differs across/within log captures. Timestamp order
	// of API completions plus exact private IDs must drive the correlation.
	api += line(400*time.Millisecond, "request_completed", "B", http.MethodPost, "/v1/audio/transcriptions", http.StatusOK, ms(95)) + line(200*time.Millisecond, "request_completed", "A", http.MethodPost, "/v1/audio/transcriptions", http.StatusOK, ms(95))
	api += line(250*time.Millisecond, "request_completed", "health", http.MethodGet, "/health/ready", http.StatusOK, ms(0))
	backend := line(-2*time.Second, "backend_starting", "", "", "", 0, nil) + line(-time.Second, "backend_ready", "", "", "", 0, nil) + line(365*time.Millisecond, "request_completed", "B", http.MethodPost, "/inference", http.StatusOK, ms(40)) + line(170*time.Millisecond, "request_completed", "A", http.MethodPost, "/inference", http.StatusOK, ms(50)) + line(250*time.Millisecond, "request_completed", "health", http.MethodGet, "/health", http.StatusOK, ms(0))
	audio := 11 * time.Second
	samples := []MeasurementSample{{Succeeded: true, PublicDuration: 100 * time.Millisecond, AudioDuration: &audio}, {Succeeded: true, PublicDuration: 100 * time.Millisecond, AudioDuration: &audio}}
	return samples, start, start.Add(time.Second), []byte(api), []byte(backend)
}

func TestCorrelateTimingsUsesRecordedIntervalsAndIDs(t *testing.T) {
	samples, start, end, api, backend := timingFixture(t)
	run, err := CorrelateTimings(samples, start, end, api, backend)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Samples) != 2 || run.Samples[0].QueueDuration == nil || *run.Samples[0].QueueDuration != 10*time.Millisecond || run.Samples[1].QueueDuration == nil || *run.Samples[1].QueueDuration != 5*time.Millisecond {
		t.Fatal("observed queue intervals were not correlated to public completion order")
	}
	if run.Samples[0].BackendDuration == nil || *run.Samples[0].BackendDuration != 50*time.Millisecond || run.Samples[1].BackendDuration == nil || *run.Samples[1].BackendDuration != 40*time.Millisecond {
		t.Fatal("actual private HTTP intervals were not matched by request ID")
	}
	if run.ModelLoad == nil || *run.ModelLoad != time.Second || run.Startup != nil {
		t.Fatal("observed native readiness interval was confused with API startup or pure model IO")
	}
	if samples[0].QueueDuration != nil || samples[0].BackendDuration != nil || run.Samples[0].PublicDuration != samples[0].PublicDuration {
		t.Fatal("caller public observations were modified")
	}
	summary, err := SummarizeMeasurements(run)
	if err != nil || summary.APIOverhead == nil || summary.APIOverhead.Count != 2 {
		t.Fatal("independent observed components cannot produce a valid summary")
	}
	*run.Samples[0].AudioDuration = time.Second
	if *samples[0].AudioDuration != 11*time.Second {
		t.Fatal("correlated samples alias caller-owned duration")
	}
}

func TestCorrelateTimingsMissingOrContradictoryFactsStayUnknown(t *testing.T) {
	for _, mode := range []string{"missing-backend", "missing-admit", "duplicate-backend", "negative-queue", "negative-duration", "overflow-duration", "missing-duration", "backend-after-api", "component-exceeds-public"} {
		t.Run(mode, func(t *testing.T) {
			samples, start, end, api, backend := timingFixture(t)
			lines := strings.Split(strings.TrimSpace(string(backend)), "\n")
			// The A private completion is the fourth record.
			switch mode {
			case "missing-backend":
				lines = append(lines[:3], lines[4:]...)
			case "duplicate-backend":
				lines = append(lines, lines[3])
			case "negative-duration":
				lines[3] = strings.Replace(lines[3], `"duration_ms":50`, `"duration_ms":-1`, 1)
			case "overflow-duration":
				lines[3] = strings.Replace(lines[3], `"duration_ms":50`, `"duration_ms":`+jsonNumber(math.MaxInt64), 1)
			case "missing-duration":
				lines[3] = strings.Replace(lines[3], `"duration_ms":50,`, "", 1)
			case "backend-after-api":
				lines[3] = strings.ReplaceAll(lines[3], "00:00:00.17", "00:00:00.27")
			case "component-exceeds-public":
				samples[0].PublicDuration = 55 * time.Millisecond
			case "missing-admit":
				api = []byte(strings.Replace(string(api), `"event":"queue_admitted","request_id":"A"`, `"event":"unrelated","request_id":"A"`, 1))
			case "negative-queue":
				api = []byte(strings.ReplaceAll(string(api), "00:00:00.12", "00:00:00.10"))
			}
			backend = []byte(strings.Join(lines, "\n") + "\n")
			run, err := CorrelateTimings(samples, start, end, api, backend)
			if err == nil {
				t.Fatal("missing or contradictory timing facts accepted")
			}
			if run.Samples[0].QueueDuration != nil || run.Samples[0].BackendDuration != nil {
				t.Fatal("invalid record manufactured known request components")
			}
			if run.Samples[1].QueueDuration == nil || run.Samples[1].BackendDuration == nil {
				t.Fatal("valid independent sample was discarded instead of returning partial observations")
			}
		})
	}
}

func TestCorrelateTimingsRejectsUnboundCompletionSets(t *testing.T) {
	for _, mode := range []string{"missing-api", "missing-api-and-private", "duplicate-api", "foreign-backend", "ambiguous-completion-order"} {
		t.Run(mode, func(t *testing.T) {
			samples, start, end, api, backend := timingFixture(t)
			switch mode {
			case "missing-api":
				api = []byte(strings.Replace(string(api), `"request_id":"A","route":"/v1/audio/transcriptions"`, `"request_id":"A","route":"/health/ready"`, 1))
			case "missing-api-and-private":
				api = []byte(strings.Replace(string(api), `"request_id":"A","route":"/v1/audio/transcriptions"`, `"request_id":"A","route":"/health/ready"`, 1))
				backend = []byte(strings.Replace(string(backend), `"request_id":"A","route":"/inference"`, `"request_id":"A","route":"/health"`, 1))
			case "duplicate-api":
				api = []byte(strings.ReplaceAll(string(api), `"request_id":"B"`, `"request_id":"A"`))
			case "foreign-backend":
				backend = []byte(strings.ReplaceAll(string(backend), `"request_id":"B"`, `"request_id":"FOREIGN_PRIVATE_ID"`))
			case "ambiguous-completion-order":
				api = []byte(strings.ReplaceAll(string(api), "00:00:00.4", "00:00:00.2"))
			}
			run, err := CorrelateTimings(samples, start, end, api, backend)
			if err == nil {
				t.Fatal("unbound or ambiguous workload completion set accepted")
			}
			if strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "request_id") {
				t.Fatal("private correlation data leaked in error")
			}
			if len(run.Samples) != len(samples) {
				t.Fatal("failed correlation discarded original public observations")
			}
		})
	}
}

func TestCorrelateTimingsUnknownsBoundsAndSafeErrors(t *testing.T) {
	samples, start, end, api, backend := timingFixture(t)
	old := 77 * time.Millisecond
	samples[0].QueueDuration = &old
	samples[0].BackendDuration = &old
	run, err := CorrelateTimings(samples, start, end, nil, nil)
	if err == nil || run.Samples[0].QueueDuration != nil || run.Samples[0].BackendDuration != nil || *samples[0].QueueDuration != old {
		t.Fatal("missing observations reused stale components or changed caller")
	}
	for _, content := range [][]byte{[]byte("PRIVATE_RAW_LOG_NOT_JSON"), []byte(`{"time":"PRIVATE_INVALID_TIME"}`), []byte(strings.Repeat("x", (1<<20)+1))} {
		_, err := CorrelateTimings(samples, start, end, content, backend)
		if err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("unusable external log bytes did not fail safely")
		}
	}
	if _, err := CorrelateTimings(samples, end, start, api, backend); err == nil {
		t.Fatal("negative observation window accepted")
	}
	_, err = CorrelateTimings(samples, start, end, api, []byte(strings.ReplaceAll(string(backend), "23:59:59", "23:59:57")))
	if err == nil {
		t.Fatal("negative observed native startup interval accepted")
	}
}

func jsonNumber(value int64) string { content, _ := json.Marshal(value); return string(content) }
