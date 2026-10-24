package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStrictConfigurationSources(t *testing.T) {
	p := configFile(t, `{"api":{"port":9010},"logging":{"level":"debug"}}`)
	c, err := Load([]string{"--config", p}, []string{"WHISPER_API_PORT=9020"})
	if err != nil || c.API.Port != 9020 || c.Logging.Level != "debug" {
		t.Fatalf("environment/file precedence: %v %+v", err, c)
	}

	c, err = Load([]string{"--config", p, "--api-port=9030"}, []string{"WHISPER_API_PORT=9020"})
	if err != nil || c.API.Port != 9030 || c.API.RateLimit.Burst != 4 {
		t.Fatalf("explicit flag precedence: %v %+v", err, c)
	}

	_, err = Load([]string{"--api-port=9030"}, []string{"WHISPER_API_PORT=PRIVATE_INVALID"})
	if err == nil || strings.Contains(err.Error(), "PRIVATE_INVALID") {
		t.Fatal("overridden invalid source accepted or leaked")
	}
}

func TestStrictConfigurationTypesAndCollections(t *testing.T) {
	invalid := []string{
		`{"api":{"port":8080,"port":9000}}`, `{"api":{"port":"8080"}}`,
		`{"api":{"port":8080.0}}`, `{"api":{"port":true}}`, `{"api":{"port":null}}`,
		`{"api":{"Port":8080}}`, `{"api.port":8080}`, `{"unknown":"PRIVATE"}`,
		`{"mode":"ram-cache"}`, `{"models":{"warmup":true}}`, `{"models":{"served":["base"]}}`,
		`{} {}`, `[]`, `null`, `{"api":{"rateLimit":{"store":"redis"}}}`,
		`{"api":{"rateLimit":{"burst":0}}}`, `{"api":{"rateLimit":{"requestsPerSecond":0}}}`,
		`{"api":{"rateLimit":{"requestsPerSecond":0.1,"burst":4,"idleSeconds":1}}}`,
		`{"api":{"clientIP":{"source":"x-forwarded-for"}}}`,
		`{"api":{"clientIP":{"source":"direct","trustedProxyCIDRs":["10.0.0.0/8"]}}}`,
		`{"api":{"clientIP":{"source":"x-forwarded-for","trustedProxyCIDRs":["0.0.0.0/0"]}}}`,
		`{"api":{"clientIP":{"source":"x-forwarded-for","trustedProxyCIDRs":["BAD"]}}}`,
	}
	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			_, err := Load([]string{"--config", configFile(t, input)}, []string{})
			if err == nil {
				t.Fatal("invalid source accepted")
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("private configuration leaked")
			}
		})
	}

	_, err := Load([]string{"--api-port=8080"}, []string{"WHISPER_UNKNOWN=PRIVATE"})
	if err == nil {
		t.Fatal("unknown environment field accepted")
	}

	_, err = Load(nil, []string{"WHISPER_API_PORT=8000", "WHISPER_API_PORT=8001"})
	if err == nil {
		t.Fatal("duplicate environment field accepted")
	}

	p := configFile(t, `{"api":{"clientIP":{"source":"x-forwarded-for","trustedProxyCIDRs":["10.0.0.0/8"]}}}`)
	c, err := Load([]string{"--config", p, "--trusted-proxy-cidrs-json=[\"192.0.2.0/24\"]"}, []string{})
	if err != nil || len(c.API.ClientIP.TrustedProxyCIDRs) != 1 || c.API.ClientIP.TrustedProxyCIDRs[0] != "192.0.2.0/24" {
		t.Fatalf("collection did not replace: %v %+v", err, c)
	}

	c, err = Load([]string{"--download-set-json=[]"}, []string{})
	if err != nil || len(c.Models.DownloadSet) != 0 {
		t.Fatalf("empty collection did not replace: %v", err)
	}

	p = configFile(
		t,
		`{"models":{"catalog":[{"id":"custom","source":{"kind":"prepared","path":"/models/custom.bin"}}],"served":"custom","downloadSet":[]}}`,
	)

	c, err = Load([]string{"--config", p}, []string{})
	if err != nil || len(c.Models.Catalog) != 1 || c.Models.Catalog[0].ID != "custom" {
		t.Fatalf("catalog replacement: %v %+v", err, c)
	}
}

func TestInvalidSourcesCannotBeHiddenByOverrides(t *testing.T) {
	for _, input := range []string{
		`{"api":{"backendUrl":"PRIVATE_NOT_URL"}}`,
		`{"api":{"host":"PRIVATE_NOT_HOST/"}}`,
		`{"storage":{"cacheDir":"PRIVATE_RELATIVE"}}`,
		`{"models":{"downloadSet":["base","base"]}}`,
		`{"api":{"clientIP":{"source":"direct","trustedProxyCIDRs":["10.0.0.0/8"]}}}`,
	} {
		_, err := Load([]string{
			"--config",
			configFile(t, input),
			"--backend-url=http://backend:8081",
			"--api-host=0.0.0.0",
			"--cache-dir=/models",
			"--download-set-json=[]",
			"--client-ip-source=direct",
			"--trusted-proxy-cidrs-json=[]",
		}, []string{})
		if err == nil {
			t.Fatalf("invalid lower layer accepted: %s", input)
		}
	}

	for _, value := range []string{"NaN", "Inf", "+Inf", "-Inf"} {
		_, err := Load([]string{"--rate-limit-requests-per-second=1"}, []string{"WHISPER_RATE_LIMIT_REQUESTS_PER_SECOND=" + value})
		if err == nil {
			t.Fatal("nonfinite overridden source accepted")
		}
	}

	if _, err := Load([]string{"--rate-limit-idle-seconds=9223372036854775807"}, []string{}); err == nil {
		t.Fatal("overflowing duration accepted")
	}
}

func TestBackendPortsValidatedPerSource(t *testing.T) {
	for _, tt := range []struct {
		name    string
		file    string
		environ []string
		flags   []string
		invalid bool
	}{
		{
			"file-before-flag",
			`{"backend":{"port":8082,"nativePort":8082}}`,
			nil,
			[]string{"--backend-port=8081"},
			true,
		},
		{
			"file-before-environment",
			`{"backend":{"port":8082,"nativePort":8082}}`,
			[]string{"WHISPER_BACKEND_PORT=8081"},
			nil,
			true,
		},
		{
			"environment-before-flag",
			`{}`,
			[]string{"WHISPER_BACKEND_PORT=8082", "WHISPER_NATIVE_PORT=8082"},
			[]string{"--backend-port=8081"},
			true,
		},
		{"valid-split-overrides", `{"backend":{"port":8082}}`, nil, []string{"--native-port=8083"}, false},
		{"invalid-merged-ports", `{"backend":{"port":8082}}`, nil, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--config", configFile(t, tt.file)}, tt.flags...)
			environ := append([]string{}, tt.environ...)
			_, err := Load(args, environ)
			if (err != nil) != tt.invalid {
				t.Fatalf("source validation: got error %v, want invalid=%v", err, tt.invalid)
			}
		})
	}
}

func TestStrictSourceDecoding(t *testing.T) {
	for _, input := range []string{
		`{"api":{"host":1234}}`,
		`{"api":{"rateLimit":{"enabled":null}}}`,
		`{"api":{"rateLimit":{"enabled":1}}}`,
		`{"api":{"clientIP":{"trustedProxyCIDRs":[null]}}}`,
		`{"api":{"unknown":{}}}`,
		`{"models":{"catalog":[{"id":"custom","source":{"kind":"prepared","path":"/models/custom.bin","Path":"/private"}}]}}`,
		`{"models":{"catalog":[{"id":"custom","source":{"kind":"prepared","Path":"/models/custom.bin"}}],"served":"custom","downloadSet":[]}}`,
		`{"timeouts":{"cleanupSeconds":9223372036854775807}}`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := Load([]string{
				"--config",
				configFile(t, input),
				"--api-host=localhost",
				"--rate-limit-enabled=true",
				"--trusted-proxy-cidrs-json=[]",
				"--cleanup-seconds=10",
			}, []string{})
			if err == nil {
				t.Fatal("invalid lower source accepted")
			}
		})
	}
}

func TestExplicitFlagsAndEnvironment(t *testing.T) {
	c, err := Load(nil, []string{"WHISPER_RATE_LIMIT_ENABLED=true", "WHISPER_API_PORT=9000"})
	if err != nil || !c.API.RateLimit.Enabled || c.API.Port != 9000 {
		t.Fatalf("absent flags changed environment values: %v", err)
	}

	for _, args := range [][]string{
		{"--api-port=9000", "--api-port=9001"},
		{"--config=", "--config="},
		{"--rate-limit-enabled=false", "--rate-limit-enabled=true"},
		{"--download-set-json=[]", "--download-set-json=[]"},
		{"--unknown=private"},
	} {
		if _, err := Load(args, []string{}); err == nil {
			t.Fatalf("invalid flags accepted: %v", args)
		}
	}

	for _, environ := range [][]string{
		{"WHISPER_API_PORT"}, {"WHISPER_UNKNOWN=private"},
		{"WHISPER_LOG_LEVEL=debug", "WHISPER_LOG_LEVEL=info"},
		{"WHISPER_RATE_LIMIT_ENABLED=1"},
	} {
		if _, err := Load(nil, environ); err == nil {
			t.Fatal("invalid environment accepted")
		}
	}

	c, err = Load([]string{"--rate-limit-enabled=false", "--download-set-json=[]"}, []string{"WHISPER_RATE_LIMIT_ENABLED=true", "WHISPER_API_PORT=9000"})
	if err != nil || c.API.RateLimit.Enabled || c.API.Port != 9000 || len(c.Models.DownloadSet) != 0 {
		t.Fatalf("explicit false/empty or absent flag precedence: %v", err)
	}
}
