package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This focused negative deliberately omits GPU allocation from the exact local
// backend image. It is not a replacement Compose recipe or positive GPU evidence.
func TestLocalGPUUnavailable(t *testing.T) {
	runGPUUnavailable(t, openLocalEnvironment(t, nil))
}

func runGPUUnavailable(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	args := []string{"create", "--runtime", "runc", "--network", "none", "--read-only", "--user", "10001:10001", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--env", "NVIDIA_VISIBLE_DEVICES=void", "--mount", "type=bind,source=" + e.config + ",target=/etc/whisper/config.json,readonly", "--mount", "type=bind,source=" + e.prepared + ",target=/prepared,readonly", "--tmpfs", "/tmp:rw,nosuid,noexec,size=64m,uid=10001,gid=10001", "--tmpfs", "/models:rw,nosuid,noexec,size=2m,uid=10001,gid=10001", e.selection.BackendImage, "--config", "/etc/whisper/config.json"}
	data, err := e.target.run(e.ctx, "create no-GPU fault fixture", false, args...)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(data))
	if len(id) != 64 {
		t.Fatal("invalid owned GPU fault container identity")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = e.target.run(ctx, "stop GPU fault fixture", false, "stop", "--timeout", "5", id)
		_, err := e.target.run(ctx, "remove GPU fault fixture", false, "rm", "--force", id)
		if err != nil {
			t.Error(err)
		}
	})
	requests, requestErr := e.target.run(e.ctx, "GPU fault allocation declaration", false, "inspect", "--format", "{{json .HostConfig.DeviceRequests}}", id)
	var deviceRequests []json.RawMessage
	if requestErr != nil || json.Unmarshal(requests, &deviceRequests) != nil || len(deviceRequests) != 0 {
		t.Fatal("negative fixture does not actually omit GPU allocation")
	}
	if _, err := e.target.run(e.ctx, "start GPU fault fixture", false, "start", id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		data, err := e.target.run(e.ctx, "GPU fault process observation", false, "inspect", "--format", "{{json .State}}", id)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Running  bool `json:"Running"`
			ExitCode int  `json:"ExitCode"`
		}
		if json.Unmarshal(data, &state) != nil {
			t.Fatal("GPU fault process state unavailable")
		}
		if !state.Running {
			if state.ExitCode == 0 {
				t.Fatal("missing GPU produced a successful backend lifecycle")
			}
			break
		}
		data, err = e.target.run(e.ctx, "GPU fault private readiness", false, "exec", id, "curl", "--silent", "--noproxy", "*", "--max-time", "1", "http://127.0.0.1:"+strconv.Itoa(e.settings.Backend.Port)+"/health")
		if err == nil {
			var health struct {
				Status    string `json:"status"`
				CUDAReady bool   `json:"cuda_ready"`
			}
			if json.Unmarshal(data, &health) != nil {
				t.Fatal("GPU fault health could not be interpreted")
			}
			if health.Status == "ready" || health.Status == "busy" || health.CUDAReady {
				t.Fatal("missing GPU was advertised as CUDA-ready")
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("missing GPU did not reach bounded startup failure")
		}
		time.Sleep(50 * time.Millisecond)
	}
	logs, err := e.target.run(e.ctx, "GPU fault safe events", true, "logs", id)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(e.workdir, "gpu-fault.log"), logs, 0600)
	foundFailure, nativeStarted := false, false
	for _, line := range strings.Split(string(logs), "\n") {
		var event struct {
			Event     string `json:"event"`
			ErrorCode string `json:"error_code"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Event == "backend_ready" {
			t.Fatal("missing GPU emitted a false ready event")
		}
		if event.Event == "backend_starting" {
			nativeStarted = true
		}
		// An early native process exit maps to backend_unavailable; the CUDA
		// evidence/deadline path maps to gpu_unavailable. REQ-014 requires an
		// unready bounded reason, not one inferred errno from suppressed native
		// output. Provision/configuration failure cannot satisfy this check.
		if event.Event == "backend_failed" && (event.ErrorCode == "gpu_unavailable" || event.ErrorCode == "backend_unavailable") {
			foundFailure = true
		}
	}
	if !foundFailure || !nativeStarted {
		t.Fatal("no GPU fault was recognized; an unrelated fixture failure cannot prove fail-closed CUDA startup")
	}
	t.Log("exact local backend without GPU allocation failed closed; no CPU-ready fallback or device/driver change")
}
