package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestEventAllowlistDropsUnknownEventsAndRequestFields(t *testing.T) {
	var out bytes.Buffer
	logger, err := New(&out, "debug", "backend")
	if err != nil {
		t.Fatal(err)
	}

	Emit(logger, context.Background(), slog.LevelError, Event{
		Name:      "PRIVATE-event",
		ErrorCode: "internal_error",
	})
	if out.Len() != 0 {
		t.Fatal("unknown operational event was emitted")
	}

	Emit(logger, context.Background(), slog.LevelError, Event{
		Name:       BackendFailed,
		Method:     "POST",
		Route:      "/inference",
		Status:     503,
		DurationMS: 42,
		ErrorCode:  "backend_unavailable",
	})

	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["event"] != "backend_failed" ||
		event["error_code"] != "backend_unavailable" ||
		event["component"] != "backend" {
		t.Fatalf("safe failure event was lost: %v", event)
	}

	for _, field := range []string{"method", "route", "status", "duration_ms", "request_id"} {
		if _, exists := event[field]; exists {
			t.Errorf("non-request event included %s", field)
		}
	}
}
