package acceptance

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResidentConcurrentRejectsOverlapDespiteAnotherQueuedRequest(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	status := http.StatusOK
	duration := int64(100)
	var api, backend []byte
	appendRecord := func(dst *[]byte, at time.Duration, event, id, route string) {
		record := timingRecord{Time: base.Add(at), Event: event, RequestID: id, Method: http.MethodPost, Route: route}
		if event == "request_completed" {
			record.Status, record.DurationMS = &status, &duration
		}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		*dst = append(*dst, append(data, '\n')...)
	}
	for _, item := range []struct {
		at        time.Duration
		event, id string
	}{
		{0, "queue_admitted", "A"}, {100, "queue_dispatched", "A"},
		{110, "queue_admitted", "B"}, {120, "queue_dispatched", "B"},
		{130, "queue_admitted", "C"}, {205, "queue_completed", "A"},
		{210, "request_completed", "A"}, {225, "queue_completed", "B"},
		{230, "request_completed", "B"}, {240, "queue_dispatched", "C"},
		{345, "queue_completed", "C"}, {350, "request_completed", "C"},
	} {
		appendRecord(&api, item.at*time.Millisecond, item.event, item.id, "/v1/audio/transcriptions")
	}
	for i, id := range []string{"A", "B", "C"} {
		appendRecord(&backend, []time.Duration{200, 220, 340}[i]*time.Millisecond, "request_completed", id, "/inference")
	}
	audio := 11 * time.Second
	sample := MeasurementSample{Succeeded: true, PublicDuration: 500 * time.Millisecond, AudioDuration: &audio}
	if _, err := correlateResidentConcurrent([]MeasurementSample{sample, sample, sample}, []string{"A", "B", "C"}, base, base.Add(time.Second), api, backend); err == nil {
		t.Fatal("overlapping dispatches were accepted merely because a third request waited")
	}
}

func checkResidentLogPrivacy(logs []byte, marker string) error {
	if len(strings.TrimSpace(string(logs))) == 0 || strings.TrimSpace(marker) == "" {
		return errors.New("privacy logs or marker unavailable")
	}
	if strings.Contains(string(logs), marker) {
		return errors.New("protected marker appeared in emitted logs")
	}
	return nil
}

func TestResidentLogPrivacyRejectsAnEmittedCanary(t *testing.T) {
	const marker = "P07_RAW_OUTPUT_CANARY"
	if err := checkResidentLogPrivacy([]byte(`{"event":"request_completed","request_id":"safe"}`), marker); err != nil {
		t.Fatal(err)
	}
	if err := checkResidentLogPrivacy([]byte(`{"event":"unexpected_native_output","raw":"`+marker+`"}`), marker); err == nil {
		t.Fatal("emitted raw output escaped the shared privacy guard")
	}
	if err := checkResidentLogPrivacy(nil, marker); err == nil {
		t.Fatal("unavailable logs were accepted as privacy evidence")
	}
	if err := checkResidentLogPrivacy([]byte(`{"event":"request_completed"}`), ""); err == nil {
		t.Fatal("missing privacy marker was accepted")
	}
}

// These are explicit negative-control fixtures, not qualifying mutations of the
// published image. Each owns a fresh unchanged candidate stack and its cleanup.
func runResidentSensitivity(t *testing.T) {
	t.Helper()
	t.Run("per_success_reload", func(t *testing.T) {
		e := openPublishedEnvironment(t, nil)
		before := readyLifecycleSnapshot(t, e)
		nextLifecycleValid(t, e)
		if err := e.target.Restart(e.ctx, "backend"); err != nil {
			t.Fatal(err)
		}
		waitLifecycleHealthy(t, e)
		after := readyLifecycleSnapshot(t, e)
		if err := residentGenerationStable(before, after); err == nil {
			t.Fatal("successful-request restart escaped resident continuity guard")
		}
		nextLifecycleValid(t, e)
		writeResidentEvidence(t, e, "resident-reload-sensitivity.json", map[string]any{"control": "owned backend restarted after successful request", "guard_rejected": true, "qualification": "not_established"})
	})
	t.Run("retained_file", func(t *testing.T) {
		e := openPublishedEnvironment(t, nil)
		state := readyLifecycleSnapshot(t, e)
		if err := releasedAPIResources(state); err != nil {
			t.Fatal(err)
		}
		containers, err := e.target.Inspect(e.ctx, "api")
		if err != nil {
			t.Fatal(err)
		}
		const retain = `set -eu; set -C; printf p07 > /tmp/p07-sensitivity-retained`
		if _, err := e.target.run(e.ctx, "retained-file negative control", false, "exec", containers.ID, "sh", "-ec", retain); err != nil {
			t.Fatal(err)
		}
		state, err = e.target.Observe(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := releasedAPIResources(state); err == nil {
			t.Fatal("retained-file fixture escaped actual API resource observation")
		}
		writeResidentEvidence(t, e, "resident-file-sensitivity.json", map[string]any{"control": "retained file in owned API tmpfs", "guard_rejected": true, "cleanup": "ordinary owned target down; no volume deletion", "qualification": "not_established"})
	})
	t.Run("emitted_log_marker", func(t *testing.T) {
		e := openPublishedEnvironment(t, nil)
		const marker = "P07_RAW_OUTPUT_CANARY"
		// The public request ID is an intentional safe log-injection seam. It
		// seeds the protected canary in actual emitted logs without privileged
		// pipe writes, unsafe native output or a changed executable image.
		client := identifiedLocalClient(t, e, marker)
		defer client.Close()
		reply, err := client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", e.settings.Models.Served}, {"language", "en"}})
		if err != nil || reply.Status != http.StatusOK {
			t.Fatal("emitted marker control request failed")
		}
		logs, err := e.target.Logs(e.ctx, "api")
		if err != nil {
			t.Fatal(err)
		}
		if err := checkResidentLogPrivacy(logs, marker); err == nil {
			t.Fatal("actual emitted marker escaped the shared privacy guard")
		}
		writeResidentEvidence(t, e, "resident-log-sensitivity.json", map[string]any{"control": "public synthetic canary inserted into actual log through permitted request ID", "guard_rejected": true, "not_claimed": "native code mutation or Electron interoperability", "qualification": "not_established"})
	})
}
