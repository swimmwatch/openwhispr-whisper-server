package config

import (
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsValidateRejectsInvalidProgrammaticValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Settings)
	}{
		{"schema", func(c *Settings) {
			c.SchemaVersion = 2
		}},
		{"rate-idle-budget", func(c *Settings) {
			c.API.RateLimit.IdleSeconds = 1
		}},
		{"rate-not-finite", func(c *Settings) {
			c.API.RateLimit.RequestsPerSecond = math.Inf(1)
		}},
		{"rate-nan", func(c *Settings) {
			c.API.RateLimit.RequestsPerSecond = math.NaN()
		}},
		{"rate-idle-overflow", func(c *Settings) {
			c.API.RateLimit.IdleSeconds = math.MaxInt
		}},
		{"untrusted-forwarding", func(c *Settings) {
			c.API.ClientIP.Source = "x-forwarded-for"
		}},
		{"equal-backend-ports", func(c *Settings) {
			c.Backend.NativePort = c.Backend.Port
		}},
		{"relative-cache", func(c *Settings) {
			c.Storage.CacheDir = "PRIVATE-relative"
		}},
		{"invalid-bind-host", func(c *Settings) {
			c.API.Host = "PRIVATE/host"
		}},
		{"backend-credentials", func(c *Settings) {
			c.API.BackendURL = "https://PRIVATE:secret@backend:8081"
		}},
		{"invalid-model-source", func(c *Settings) {
			c.Models.Catalog[0].Source.Kind = "PRIVATE-kind"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := Load(nil, []string{})
			if err != nil {
				t.Fatal(err)
			}

			tc.change(&settings)
			if err := settings.Validate(); !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("invalid settings accepted or exposed: %v", err)
			}
		})
	}
}

func TestConfigurationRejectsMalformedDocuments(t *testing.T) {
	for _, document := range []string{
		`{"api":`,
		`{"api":{"port":8080`,
		`{"models":{"downloadSet":["base"`,
		`{"models":{"served":"base",}}`,
		`{"api":{"port":8080}} trailing`,
		`{"models":{"downloadSet":` + strings.Repeat("[", 101) + `"base"` + strings.Repeat("]", 101) + `}}`,
		`{"models":{"downloadSet":["base",null]}}`,
		`{"models":{"served":"PRIVATE/invalid"}}`,
		`{"models":{"downloadSet":["PRIVATE/invalid"]}}`,
	} {
		t.Run(document, func(t *testing.T) {
			_, err := Load([]string{"--config", configFile(t, document), "--served-model=base", "--download-set-json=[]"}, []string{})
			if !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("malformed lower source accepted or exposed: %v", err)
			}
		})
	}

	if _, err := Load([]string{"--config", filepath.Join(t.TempDir(), "PRIVATE-missing.json")}, []string{}); !errors.Is(err, ErrInvalid) ||
		strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("missing configuration file accepted or exposed: %v", err)
	}
}

func TestPrivateBackendURLAndModelSourceValidation(t *testing.T) {
	for _, value := range []string{"http://backend:1", "https://backend:65535/", "http://[::1]:8081"} {
		if _, err := Load([]string{"--backend-url=" + value}, []string{}); err != nil {
			t.Errorf("valid private backend rejected: %q: %v", value, err)
		}
	}

	for _, value := range []string{
		"http://backend:0", "http://backend:65536", "http://backend:PRIVATE",
		"http://backend/path", "http://backend/?secret=PRIVATE", "http://backend/#PRIVATE",
		"ftp://backend", "http://user:PRIVATE@backend", "http://[fe80::1%25eth0]:8081",
	} {
		if _, err := Load([]string{"--backend-url=" + value}, []string{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid private backend accepted: %q: %v", value, err)
		}
	}

	for _, tc := range []struct {
		name   string
		source Source
	}{
		{"download-without-checksum", Source{
			Kind: "download",
			URL:  "https://example.org/model.bin",
		}},
		{"download-downgrade", Source{
			Kind:   "download",
			URL:    "http://example.org/model.bin",
			SHA256: strings.Repeat("a", 64),
		}},
		{"download-credentials", Source{
			Kind:   "download",
			URL:    "https://user:PRIVATE@example.org/model.bin",
			SHA256: strings.Repeat("a", 64),
		}},
		{"download-with-path", Source{
			Kind:   "download",
			URL:    "https://example.org/model.bin",
			Path:   "/models/model.bin",
			SHA256: strings.Repeat("a", 64),
		}},
		{"prepared-relative", Source{
			Kind: "prepared",
			Path: "PRIVATE-relative.bin",
		}},
		{"prepared-with-url", Source{
			Kind: "prepared",
			Path: "/models/model.bin",
			URL:  "https://example.org/model.bin",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models := Models{
				Catalog: []Model{{
					ID:     "base",
					Source: tc.source,
				}},
				Served: "base",
			}
			if err := models.Validate(); !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("invalid model source accepted or exposed: %v", err)
			}
		})
	}
}
