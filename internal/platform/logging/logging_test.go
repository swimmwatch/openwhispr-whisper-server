package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestStructuredLoggingPrivacyAtEverySeverity(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			var out bytes.Buffer
			logger, err := New(&out, level, "api")
			if err != nil {
				t.Fatal(err)
			}

			ctx := WithRequestID(context.Background(), "safe-id")
			Emit(logger, ctx, slog.LevelError, Event{
				Name:      RequestCompleted,
				Method:    "PRIVATE",
				Route:     "/PRIVATE?q=PRIVATE",
				Status:    500,
				ErrorCode: "PRIVATE",
			})
			if strings.Contains(out.String(), "PRIVATE") {
				t.Fatal("unsafe event fields leaked")
			}

			var entry map[string]any
			if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["request_id"] != "safe-id" || entry["status"] != float64(500) || entry["component"] != "api" {
				t.Fatalf("invalid event: %v", entry)
			}
			if RequestID(WithRequestID(ctx, "PRIVATE\n")) != "" {
				t.Fatal("unsafe context request ID accepted")
			}
		})
	}

	if _, err := New(&bytes.Buffer{}, "PRIVATE", "api"); err == nil {
		t.Fatal("unknown severity accepted")
	}
}

func TestStrictLogLevelsAndComponents(t *testing.T) {
	for name, expected := range map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	} {
		logger, err := New(&bytes.Buffer{}, name, "api")
		if err != nil || !logger.Enabled(context.Background(), expected) || logger.Enabled(context.Background(), expected-1) {
			t.Fatalf("incorrect configured level: %s", name)
		}
	}

	for _, name := range []string{"DEBUG", "Info", "warning", "info+4", "debug-4", "error+0", "", "PRIVATE\n"} {
		_, err := New(&bytes.Buffer{}, name, "api")
		if err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("unsupported level accepted or leaked")
		}
	}

	if _, err := New(&bytes.Buffer{}, "info", "PRIVATE"); err == nil {
		t.Fatal("unknown component accepted")
	}
}
