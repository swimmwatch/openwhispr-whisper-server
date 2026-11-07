package acceptance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestP08MemoryStartupProbeRejectsOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, env, args string
		ok              bool
	}{
		{"plain", "HOME=/tmp\x00", "/api\x00--config\x00/etc/whisper/config.json\x00--backend-url\x00http://backend\x00", true},
		{"environment override", "WHISPER_OPERATION_SECONDS=999\x00", "/api\x00--config\x00/etc/whisper/config.json\x00", false},
		{"operation flag", "HOME=/tmp\x00", "/api\x00--config\x00/etc/whisper/config.json\x00--operation-seconds=999\x00", false},
		{"rate flag", "HOME=/tmp\x00", "/api\x00--config\x00/etc/whisper/config.json\x00--rate-limit-enabled\x00true\x00", false},
		{"different config", "HOME=/tmp\x00", "/api\x00--config\x00/private/other\x00", false},
		{"missing config", "HOME=/tmp\x00", "/api\x00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			env, args := filepath.Join(dir, "env"), filepath.Join(dir, "args")
			if os.WriteFile(env, []byte(tc.env), 0600) != nil || os.WriteFile(args, []byte(tc.args), 0600) != nil {
				t.Fatal("fixture unavailable")
			}
			out, err := exec.CommandContext(t.Context(), "sh", "-eu", "-c", p08MemoryStartupProbe, "test", env, args).Output()
			if (err == nil) != tc.ok || err == nil && string(out) != "OK" {
				t.Fatal("startup override boundary mismatch")
			}
		})
	}
}

func TestP08MemoryPartialEvidencePreservesUnknownAndMeasuredZero(t *testing.T) {
	unknown := p08MemoryEvidence(p08ConditioningEpoch{}, 10, "fixture", "api", "backend", "before_fence")
	data, err := json.Marshal(unknown)
	if err != nil || !strings.Contains(string(data), `"requests_attempted":null`) || !strings.Contains(string(data), `"completion":null`) || unknown.Qualification != "unknown" {
		t.Fatal("unknown observations became measured values")
	}
	n := 0
	known := p08ConditioningEpoch{RequestsAttempted: &n, residentConditioningEpoch: residentConditioningEpoch{Completion: residentResourcePoint{At: time.Now()}}, NativeDescendantRSSBytes: []uint64{0}}
	observed := p08MemoryEvidence(known, 10, "fixture", "api", "backend", "")
	data, err = json.Marshal(observed)
	if err != nil || !strings.Contains(string(data), `"requests_attempted":0`) || !strings.Contains(string(data), `"native_descendant_rss_bytes":[0]`) || observed.Completion == nil || observed.Qualification != "unknown" {
		t.Fatal("measured zero or partial qualification was lost")
	}
}

func TestP08MemoryCUDASelectionRequiresPrivateCompleteInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	for _, tc := range []struct {
		name, body string
		mode       os.FileMode
		valid      bool
	}{
		{"complete", `{"kubeconfig":"/private/kubeconfig","context":"ctx","namespace":"ns","namespace_uid":"nsuid","release":"rel","driver_namespace":"driver","driver_pod":"gpu","driver_uid":"duid","gpu_uuid":"GPU-test","backend_pod":"backend-0","backend_pod_uid":"buid","backend_port":8081}`, 0600, true},
		{"incomplete", `{"namespace":"ns"}`, 0600, false},
		{"unknown field", `{"kubeconfig":"/private/kubeconfig","unknown":true}`, 0600, false},
		{"nonprivate", `{}`, 0644, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if os.WriteFile(path, []byte(tc.body), 0600) != nil || os.Chmod(path, tc.mode) != nil {
				t.Fatal("fixture unavailable")
			}
			_, err := p08ReadMemoryCUDASelection(path)
			if (err == nil) != tc.valid {
				t.Fatal("selection validation mismatch")
			}
		})
	}
	if _, err := p08ReadMemoryCUDASelection("relative.json"); err == nil {
		t.Fatal("relative selection accepted")
	}
}
