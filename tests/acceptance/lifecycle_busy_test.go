package acceptance

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func lifecycleNamespacePID(pid int) (int, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == "NSpid:" {
			namespace, err := strconv.Atoi(fields[len(fields)-1])
			if err == nil && namespace > 1 {
				return namespace, nil
			}
		}
	}
	return 0, errors.New("owned native namespace identity unavailable")
}

// Busy is a short-lived state barrier, not a resource-sampling window. Resolve
// its API container and original kernel lifetime BEFORE starting the operation.
// At the barrier read only actual private health and that same kernel process;
// resource counters deliberately remain unknown, never copied from an old sample.
// Ready/terminal snapshots and the independently armed GPU/replacement observer
// continue to own full resource, native reaping and no-overlap checks.
func observeLifecycleBusy(ctx context.Context, e *acceptanceEnvironment, call *lifecycleCall) (DockerSnapshot, error) {
	var snapshot DockerSnapshot
	if call.apiID == "" || call.before.NativeHostPID == nil || call.before.NativeNamespacePID == nil || call.before.PrivateHealth == nil || call.hostStart == "" {
		return snapshot, errors.New("original lifecycle observation identity unavailable")
	}
	health, err := e.target.privateHealth(ctx, call.apiID)
	if err != nil {
		return snapshot, err
	}
	snapshot.At, snapshot.PrivateHealth = time.Now(), health
	if health.Status != "busy" {
		return snapshot, nil
	}
	before := call.before
	if health.ProcessGeneration != before.PrivateHealth.ProcessGeneration || health.Model != before.PrivateHealth.Model || health.ProtocolVersion != before.PrivateHealth.ProtocolVersion || health.ConfigurationFingerprint != before.PrivateHealth.ConfigurationFingerprint {
		return snapshot, errors.New("native/configuration identity changed before lifecycle fault injection")
	}
	pid := *before.NativeHostPID
	start, err := lifecyclePIDStart(pid)
	if err != nil || start != call.hostStart {
		return snapshot, errors.New("original native lifetime changed before lifecycle fault injection")
	}
	namespace, err := lifecycleNamespacePID(pid)
	if err != nil || namespace != *before.NativeNamespacePID {
		return snapshot, errors.New("native namespace identity changed before lifecycle fault injection")
	}
	snapshot.NativeHostPID, snapshot.NativeNamespacePID = &pid, &namespace
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	return snapshot, lifecycleProcessFacts(snapshot, "busy")
}

func lifecycleBusyCPUFixture(t *testing.T) (*acceptanceEnvironment, *lifecycleCall) {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "docker-boundary")
	// Slow unrelated inventory remains valid work, but cannot be required by a
	// one-second active-state barrier. Only private health is immediately ready.
	script := `#!/bin/sh
set -eu
if [ "$1" = exec ]; then
 printf '%s\n%s' "$P07_BUSY_HEALTH" "$P07_BUSY_HTTP_STATUS"
else
 sleep 2
 exit 1
fi
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("P07_BUSY_HEALTH", `{"status":"busy","model":"base","process_generation":"original","protocol_version":"1","configuration_fingerprint":"selected","cuda_ready":true}`)
	t.Setenv("P07_BUSY_HTTP_STATUS", strconv.Itoa(http.StatusOK))
	pid := os.Getpid()
	namespace, err := lifecycleNamespacePID(pid)
	if err != nil {
		t.Fatal(err)
	}
	start, err := lifecyclePIDStart(pid)
	if err != nil {
		t.Fatal(err)
	}
	value := uint64(42)
	resource := DockerResourceSnapshot{RSSBytes: &value, FDCount: &value, ScratchFileCount: &value, ScratchBytes: &value}
	before := DockerSnapshot{PrivateHealth: &DockerPrivateHealth{HTTPStatus: http.StatusOK, Status: "ready", Model: "base", ProcessGeneration: "original", ProtocolVersion: "1", ConfigurationFingerprint: "selected", CUDAReady: true}, NativeHostPID: &pid, NativeNamespacePID: &namespace, API: resource, Backend: resource}
	settings := config.Settings{}
	settings.Timeouts.OperationSeconds = 1
	e := &acceptanceEnvironment{ctx: t.Context(), settings: settings, target: &DockerTarget{owned: true, opts: DockerOptions{DockerExecutable: executable, Root: root, CommandTimeout: 3 * time.Second, BackendPort: 9000}}}
	call := &lifecycleCall{apiID: "owned-api", before: before, hostStart: start, done: make(chan lifecycleCallResult, 1)}
	return e, call
}

func TestLifecycleBusyBarrierUsesActualNarrowHealthWithoutInventingResources(t *testing.T) {
	e, call := lifecycleBusyCPUFixture(t)
	busy := waitLifecycleBusy(t, e, call, "original")
	if lifecycleProcessFacts(busy, "busy") != nil || busy.API.RSSBytes != nil || busy.Backend.RSSBytes != nil || busy.API.ScratchBytes != nil || busy.Backend.ScratchBytes != nil {
		t.Fatal("actual short Busy barrier failed or stale resource counters became fresh observations")
	}
	if lifecycleSnapshotFacts(busy, "busy") == nil {
		t.Fatal("partial Busy state unexpectedly satisfied full ready/terminal resource proof")
	}
}

func TestLifecycleBusyBarrierRejectsChangedIdentityAndNonCUDA(t *testing.T) {
	for _, fault := range []string{"start", "namespace", "generation", "model", "fingerprint", "protocol", "cuda"} {
		t.Run(fault, func(t *testing.T) {
			e, call := lifecycleBusyCPUFixture(t)
			switch fault {
			case "start":
				call.hostStart = "changed"
			case "namespace":
				wrong := *call.before.NativeNamespacePID + 1
				call.before.NativeNamespacePID = &wrong
			case "generation":
				t.Setenv("P07_BUSY_HEALTH", strings.Replace(os.Getenv("P07_BUSY_HEALTH"), "original", "replacement", 1))
			case "model":
				t.Setenv("P07_BUSY_HEALTH", strings.Replace(os.Getenv("P07_BUSY_HEALTH"), "base", "tiny", 1))
			case "fingerprint":
				t.Setenv("P07_BUSY_HEALTH", strings.Replace(os.Getenv("P07_BUSY_HEALTH"), "selected", "changed", 1))
			case "protocol":
				t.Setenv("P07_BUSY_HEALTH", strings.Replace(os.Getenv("P07_BUSY_HEALTH"), `"protocol_version":"1"`, `"protocol_version":"2"`, 1))
			case "cuda":
				t.Setenv("P07_BUSY_HEALTH", strings.Replace(os.Getenv("P07_BUSY_HEALTH"), "true", "false", 1))
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if _, err := observeLifecycleBusy(ctx, e, call); err == nil {
				t.Fatal("changed/unknown native identity or CPU-ready claim satisfied Busy")
			}
		})
	}
}
