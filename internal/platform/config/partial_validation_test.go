package config

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestPartialCatalogValidationCannotBeOverridden(t *testing.T) {
	valid := `[{"id":"base","source":{"kind":"prepared","path":"/models/base.bin"}}]`
	for name, catalog := range map[string]string{
		"empty":          `[]`,
		"duplicate":      `[{"id":"base","source":{"kind":"prepared","path":"/models/base.bin"}},{"id":"base","source":{"kind":"prepared","path":"/models/base.bin"}}]`,
		"missing-id":     `[{"source":{"kind":"prepared","path":"/models/base.bin"}}]`,
		"source-kind":    `[{"id":"base","source":{"kind":"future","path":"/models/base.bin"}}]`,
		"source-digest":  `[{"id":"base","source":{"kind":"prepared","path":"/models/base.bin","sha256":"invalid"}}]`,
		"expected-bytes": `[{"id":"base","source":{"kind":"prepared","path":"/models/base.bin","expected_bytes":-1}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			path := configFile(t, `{"models":{"catalog":`+catalog+`}}`)
			_, err := Load([]string{"--config", path, "--catalog-json=" + valid, "--download-set-json=[]"}, []string{})
			if !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid nested source was hidden by a valid catalog override", err)
			}
		})
	}
}

func TestNumericValidationPreservesRepresentableBoundaries(t *testing.T) {
	maximum := strconv.FormatInt(maxSeconds, 10)
	overflow := strconv.FormatInt(maxSeconds+1, 10)
	for _, field := range []struct{ flag, env string }{
		{"operation-seconds", "WHISPER_OPERATION_SECONDS"},
		{"cleanup-seconds", "WHISPER_CLEANUP_SECONDS"},
		{"shutdown-seconds", "WHISPER_SHUTDOWN_SECONDS"},
		{"provisioning-seconds", "WHISPER_PROVISIONING_SECONDS"},
		{"decoded-seconds", "WHISPER_DECODED_SECONDS"},
		{"rate-limit-idle-seconds", "WHISPER_RATE_LIMIT_IDLE_SECONDS"},
	} {
		t.Run(field.flag, func(t *testing.T) {
			if _, err := Load([]string{"--" + field.flag + "=" + maximum}, []string{}); err != nil {
				t.Fatal("maximum representable seconds rejected", err)
			}
			if _, err := Load([]string{"--" + field.flag + "=60"}, []string{field.env + "=" + overflow}); !errors.Is(err, ErrInvalid) {
				t.Fatal("overflowing lower-priority seconds were hidden", err)
			}
		})
	}

	maximumRate := strconv.FormatFloat(math.MaxFloat64, 'g', -1, 64)
	settings, err := Load(nil, []string{"WHISPER_RATE_LIMIT_REQUESTS_PER_SECOND=" + maximumRate})
	if err != nil {
		t.Fatal("maximum finite request rate rejected", err)
	}
	if settings.API.RateLimit.RequestsPerSecond != math.MaxFloat64 || settings.Validate() != nil {
		t.Fatal("maximum finite request rate was changed or rejected by typed validation")
	}

	if _, err := Load([]string{"--rate-limit-idle-seconds=3"}, []string{
		"WHISPER_RATE_LIMIT_BURST=9", "WHISPER_RATE_LIMIT_REQUESTS_PER_SECOND=3",
	}); err != nil {
		t.Fatal("exact refill window assembled from partial sources was rejected", err)
	}
	path := configFile(t, `{"api":{"rateLimit":{"burst":9,"requestsPerSecond":3,"idleSeconds":2}}}`)
	if _, err := Load([]string{"--config", path, "--rate-limit-idle-seconds=3"}, []string{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid lower-priority refill window was hidden", err)
	}
}
