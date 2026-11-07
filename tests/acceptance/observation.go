package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type DockerPrivateHealth struct {
	HTTPStatus                                                                  int
	Status, Model, ProcessGeneration, ProtocolVersion, ConfigurationFingerprint string
	CUDAReady                                                                   bool
}

// Nil counters are unknown, not measured zero. API RSS/FD covers PID 1; backend
// covers PID 1 plus the unique resident whisper-server, excluding exec probes.
// Scratch counts visible regular files under /tmp and their logical byte sizes,
// not allocated filesystem blocks or deleted-but-still-open file storage.
type DockerResourceSnapshot struct {
	RSSBytes, FDCount, ScratchFileCount, ScratchBytes *uint64
	// OwnershipLockFiles counts only the fixed empty API startup flock file,
	// independently of raw scratch totals. It is not request-owned content.
	OwnershipLockFiles *uint64
}

// DockerSnapshot is a bounded serial observation, not an atomic system snapshot.
// Process identities remain private; callers must sanitize public reports.
type DockerSnapshot struct {
	At                                time.Time
	PrivateHealth                     *DockerPrivateHealth
	API, Backend                      DockerResourceSnapshot
	NativeHostPID, NativeNamespacePID *int
}

// No payload, argv, file contents or scratch filenames leave this fixed probe.
// A missing tool, denied proc entry or vanished native process fails explicitly.
const dockerResourceProbe = `set -eu
rss_bytes() {
 kb=
 while read -r key value unit; do
  if [ "$key" = VmRSS: ]; then
   [ "$unit" = kB ] || return 1
   kb=$value
   break
  fi
 done < "/proc/$1/status"
 case "$kb" in ''|*[!0-9]*) return 1;; esac
 printf '%s\n' "$((kb * 1024))"
}
fd_count() {
 entries=$(find "/proc/$1/fd" -mindepth 1 -maxdepth 1 -printf '.')
 printf '%s\n' "${#entries}"
}
native=0
if [ "$1" = backend ]; then
 for file in /proc/[0-9]*/comm; do
  IFS= read -r name < "$file" || continue
  if [ "$name" = whisper-server ]; then
   [ "$native" -eq 0 ] || exit 1
   native=${file#/proc/}
   native=${native%/comm}
  fi
 done
 [ "$native" -gt 1 ] || exit 1
fi
rss=$(rss_bytes 1)
fds=$(fd_count 1)
if [ "$native" -gt 1 ]; then
 native_rss=$(rss_bytes "$native")
 native_fds=$(fd_count "$native")
 rss=$((rss + native_rss))
 fds=$((fds + native_fds))
fi
sizes=$(find /tmp -xdev -type f -printf '%s\n')
files=0
bytes=0
for size in $sizes; do
 case "$size" in ''|*[!0-9]*) exit 1;; esac
 files=$((files + 1))
 bytes=$((bytes + size))
done
owner_locks=0
if [ "$1" = api ]; then
 marks=$(find /tmp/whisper-api/.owner.lock -maxdepth 0 -type f -size 0c -printf '.')
 owner_locks=${#marks}
fi
printf '{"rss_bytes":%s,"fd_count":%s,"scratch_file_count":%s,"scratch_bytes":%s,"native_pid":%s,"ownership_lock_files":%s}\n' "$rss" "$fds" "$files" "$bytes" "$native" "$owner_locks"
`

func (t *DockerTarget) privateHealth(ctx context.Context, apiID string) (*DockerPrivateHealth, error) {
	data, err := t.run(ctx, "private health observation", false, "exec", apiID, "curl", "--silent", "--show-error", "--noproxy", "*", "--connect-timeout", "1", "--max-time", "2", "--write-out", "\n%{http_code}", "http://backend:"+strconv.Itoa(t.opts.BackendPort)+"/health")
	if err != nil {
		return nil, err
	}
	split := strings.LastIndexByte(string(data), '\n')
	if split < 0 {
		return nil, errors.New("private health HTTP observation unavailable")
	}
	status, err := strconv.Atoi(strings.TrimSpace(string(data[split+1:])))
	if err != nil || status != http.StatusOK && status != http.StatusServiceUnavailable {
		return nil, errors.New("private health HTTP observation failed")
	}
	var wire struct {
		Status                   string `json:"status"`
		Model                    string `json:"model"`
		ProcessGeneration        string `json:"process_generation"`
		ProtocolVersion          string `json:"protocol_version"`
		ConfigurationFingerprint string `json:"configuration_fingerprint"`
		CUDAReady                *bool  `json:"cuda_ready"`
	}
	if json.Unmarshal(data[:split], &wire) != nil || wire.CUDAReady == nil || wire.Model == "" || wire.ProtocolVersion == "" || wire.ConfigurationFingerprint == "" {
		return nil, errors.New("private health fields unavailable")
	}
	switch wire.Status {
	case "starting", "ready", "busy", "recovering", "stopping", "failed":
	default:
		return nil, errors.New("private health state unavailable")
	}
	if (wire.Status == "ready" || wire.Status == "busy") && wire.ProcessGeneration == "" {
		return nil, errors.New("resident process generation unavailable")
	}
	return &DockerPrivateHealth{HTTPStatus: status, Status: wire.Status, Model: wire.Model, ProcessGeneration: wire.ProcessGeneration, ProtocolVersion: wire.ProtocolVersion, ConfigurationFingerprint: wire.ConfigurationFingerprint, CUDAReady: *wire.CUDAReady}, nil
}

func (t *DockerTarget) resourceSnapshot(ctx context.Context, id, role string) (DockerResourceSnapshot, *int, error) {
	data, err := t.run(ctx, role+" resource observation", false, "exec", id, "sh", "-eu", "-c", dockerResourceProbe, "p07-observe", role)
	if err != nil {
		return DockerResourceSnapshot{}, nil, err
	}
	var wire struct {
		RSSBytes           *uint64 `json:"rss_bytes"`
		FDCount            *uint64 `json:"fd_count"`
		ScratchFileCount   *uint64 `json:"scratch_file_count"`
		ScratchBytes       *uint64 `json:"scratch_bytes"`
		NativePID          *int    `json:"native_pid"`
		OwnershipLockFiles *uint64 `json:"ownership_lock_files"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.RSSBytes == nil || wire.FDCount == nil || wire.ScratchFileCount == nil || wire.ScratchBytes == nil || role == "backend" && (wire.NativePID == nil || *wire.NativePID <= 1) {
		return DockerResourceSnapshot{}, nil, errors.New(role + " resource fields unavailable")
	}
	resource := DockerResourceSnapshot{RSSBytes: wire.RSSBytes, FDCount: wire.FDCount, ScratchFileCount: wire.ScratchFileCount, ScratchBytes: wire.ScratchBytes, OwnershipLockFiles: wire.OwnershipLockFiles}
	if role == "api" {
		return resource, nil, nil
	}
	return resource, wire.NativePID, nil
}

// Observe reads only this owned target. It preserves independent partial facts
// alongside an error when another probe is unavailable, never substituting zero.
func (t *DockerTarget) Observe(ctx context.Context) (DockerSnapshot, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	snapshot := DockerSnapshot{At: time.Now()}
	apiID, err := t.container(ctx, "api")
	if err != nil {
		return snapshot, err
	}
	backendID, err := t.container(ctx, "backend")
	if err != nil {
		return snapshot, err
	}
	var failures []error
	snapshot.PrivateHealth, err = t.privateHealth(ctx, apiID)
	if err != nil {
		failures = append(failures, err)
	}
	snapshot.API, _, err = t.resourceSnapshot(ctx, apiID, "api")
	if err != nil {
		failures = append(failures, err)
	}
	snapshot.Backend, snapshot.NativeNamespacePID, err = t.resourceSnapshot(ctx, backendID, "backend")
	if err != nil {
		failures = append(failures, err)
	}
	processes, err := t.processes(ctx, "backend")
	if err != nil {
		failures = append(failures, err)
	} else {
		for _, process := range processes {
			if process.Command == "whisper-server" {
				if snapshot.NativeHostPID != nil {
					snapshot.NativeHostPID = nil
					failures = append(failures, errors.New("multiple resident native host processes observed"))
					break
				}
				pid := process.PID
				snapshot.NativeHostPID = &pid
			}
		}
		if snapshot.NativeHostPID == nil {
			failures = append(failures, errors.New("resident native host process unavailable"))
		}
	}
	return snapshot, errors.Join(failures...)
}
