package config

import (
	"strings"
	"testing"
)

func TestBindHostValidation(t *testing.T) {
	for _, host := range []string{
		"localhost", "backend", "0.0.0.0", "::", "2001:db8::1", "127.0.0.1",
		"3backend", "api-backend.namespace.svc.cluster.local.",
		strings.Repeat("a", 63) + ".example", strings.Repeat("a.", 126) + "a",
	} {
		if !validHost(host) {
			t.Errorf("valid bind host rejected: %q", host)
		}
	}

	for _, host := range []string{
		"", ".", "backend..local", "backend.local..", "-backend", "backend-",
		"api_backend", "backend/path", "https://backend", "[::1]", "fe80::1%eth0", "бэкенд",
		strings.Repeat("a", 64) + ".example", strings.Repeat("a.", 126) + "a.",
	} {
		if validHost(host) {
			t.Errorf("invalid bind host accepted: %q", host)
		}
	}
}

func TestModelChecksumValidation(t *testing.T) {
	model := Model{ID: "base", Source: Source{Kind: "prepared", Path: "/models/base.bin"}}
	for _, checksum := range []string{"", strings.Repeat("0123456789abcdef", 4)} {
		model.Source.SHA256 = checksum
		if err := validateModel(model); err != nil {
			t.Fatal("valid optional checksum rejected")
		}
	}

	for _, checksum := range []string{
		strings.Repeat("A", 64),
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("g", 64),
	} {
		model.Source.SHA256 = checksum
		if err := validateModel(model); err == nil {
			t.Fatal("invalid model checksum accepted")
		}
	}
}

func TestUniqueModelCollectionsBeforeOverrides(t *testing.T) {
	validCatalog := `[{"id":"base","source":{"kind":"prepared","path":"/models/base.bin"}}]`
	for _, input := range []string{
		`{"models":{"catalog":[{"id":"base","source":{"kind":"prepared","path":"/models/a.bin"}},{"id":"base","source":{"kind":"prepared","path":"/models/b.bin"}}]}}`,
		`{"models":{"downloadSet":["base","base"]}}`,
	} {
		_, err := Load([]string{
			"--config",
			configFile(t, input),
			"--catalog-json=" + validCatalog,
			"--download-set-json=[]",
		}, []string{})
		if err == nil {
			t.Fatal("duplicate model collection hidden by override")
		}
	}
}
