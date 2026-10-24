package main

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestBackendStartupStopsBeforeNativeOnFailure(t *testing.T) {
	for _, name := range []string{"canceled-provisioning", "missing-prepared-model", "invalid-native-command", "relative-work-root"} {
		t.Run(name, func(t *testing.T) {
			settings, environ, runtime := commandSettings(t)
			root := filepath.Dir(runtime.workRoot)
			ctx := context.Background()
			want := domain.InvalidInput
			event := "backend_failed"
			switch name {
			case "canceled-provisioning":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
				event = "backend_stopped"
			case "missing-prepared-model":
				want = domain.ProvisioningFailed
				if err := os.Remove(settings.Models.Catalog[0].Source.Path); err != nil {
					t.Fatal(err)
				}
			case "invalid-native-command":
				runtime.nativeCommand = nil
			case "relative-work-root":
				runtime.workRoot = "PRIVATE-relative-work"
			}

			var logs bytes.Buffer
			err := run(ctx, []string{"--config", filepath.Join(root, "config.json")}, environ, &logs, runtime)
			if name == "canceled-provisioning" {
				if err != nil {
					t.Fatal("canceled provisioning failed graceful exit", err)
				}
			} else {
				var failure *domain.Failure
				if !errors.As(err, &failure) || failure.Kind != want {
					t.Fatalf("startup failure classification: got %v, want %s", err, want)
				}
			}

			for _, path := range []string{filepath.Join(root, "native-pid"), filepath.Join(root, "work"), settings.Storage.CacheDir} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("incomplete startup initialized native or mutable storage", path, err)
				}
			}
			checkCommandLogs(t, logs.Bytes(), event)
		})
	}
}

func TestBackendWAVAndShutdownBounds(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    int64
	}{
		{1, 32044},
		{134217, 4294944044},
		{134218, 4294967303},
		{math.MaxInt, 4294967303},
	} {
		if got := maxWAVBytes(tc.seconds); got != tc.want {
			t.Errorf("WAV limit for %d seconds: got %d, want %d", tc.seconds, got, tc.want)
		}
	}

	for _, tc := range []struct {
		drain, cleanup, want time.Duration
	}{
		{30 * time.Second, 10 * time.Second, 45 * time.Second},
		{time.Duration(math.MaxInt64) - 6*time.Second, time.Second, time.Duration(math.MaxInt64)},
		{time.Duration(math.MaxInt64) - time.Second, 2 * time.Second, time.Duration(math.MaxInt64)},
		{time.Duration(math.MaxInt64) - time.Second, time.Second, time.Duration(math.MaxInt64)},
	} {
		if got := shutdownBudget(tc.drain, tc.cleanup); got != tc.want {
			t.Errorf("shutdown budget overflow: got %s, want %s", got, tc.want)
		}
	}
}
