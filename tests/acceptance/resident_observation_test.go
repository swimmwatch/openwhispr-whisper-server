package acceptance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// Only counts and logical sizes leave this fixed probe. API decoder children
// are checked independently below: a linked-file count alone misses open storage.
// $1 is a scratch root and remaining arguments are explicit process IDs.
const residentDeletedScratchProbe = `set -eu
root=$1
shift
fds=0
bytes=0
for pid in "$@"; do
 [ -d "/proc/$pid/fd" ] || exit 1
 for fd in /proc/"$pid"/fd/*; do
  [ -L "$fd" ] || continue
  target=$(readlink "$fd") || exit 1
  case "$target" in "$root"/*' (deleted)')
   [ -f "$fd" ] || continue
   size=$(stat -L -c '%s' "$fd") || exit 1
   case "$size" in ''|*[!0-9]*) exit 1;; esac
   fds=$((fds+1))
   bytes=$((bytes+size))
  ;; esac
 done
done
printf '{"deleted_scratch_fds":%s,"deleted_scratch_bytes":%s}\n' "$fds" "$bytes"
`

// Use only after an operation has terminated. The existing general resource
// observer intentionally counts visible files only; this closes that evidence
// gap without changing its contract or adding a production diagnostic endpoint.
func residentActualResources(ctx context.Context, e *acceptanceEnvironment) (residentResourcePoint, DockerSnapshot, error) {
	state, err := e.target.Observe(ctx)
	if err != nil || state.NativeHostPID == nil || state.NativeNamespacePID == nil || state.API.RSSBytes == nil || state.Backend.RSSBytes == nil || state.API.FDCount == nil {
		return residentResourcePoint{}, state, errors.New("actual resident resource observation unavailable")
	}
	p := residentResourcePoint{At: state.At, APIRSSBytes: *state.API.RSSBytes, BackendRSSBytes: *state.Backend.RSSBytes, APIFDs: *state.API.FDCount}
	if err := releasedAPIResources(state); err != nil {
		return p, state, err
	}
	allocations, err := lifecycleGPUQuery(ctx, map[int]struct{}{*state.NativeHostPID: {}})
	if err != nil {
		return p, state, err
	}
	gpu, present := allocations[*state.NativeHostPID]
	if !present || gpu == 0 {
		return p, state, errors.New("owned native GPU allocation unavailable")
	}
	p.GPUBytes = gpu
	processes, err := e.target.processes(ctx, "api")
	if err != nil {
		return p, state, err
	}
	for _, process := range processes {
		if process.Command == "ffmpeg" {
			return p, state, errors.New("completed API work retained a live decoder process")
		}
	}
	containers, err := e.target.Containers(ctx)
	if err != nil {
		return p, state, err
	}
	data, err := e.target.run(ctx, "deleted-open API scratch observation", false, "exec", containers.APIID, "sh", "-eu", "-c", residentDeletedScratchProbe, "p07-deleted-scratch", "/tmp", "1")
	if err != nil {
		return p, state, err
	}
	p.APIDeletedScratchFDs, p.APIDeletedScratchBytes, err = parseResidentDeletedScratch(data)
	if err != nil {
		return p, state, err
	}
	// Keep the original aggregate measurement/bounds unchanged. This independent
	// breakdown diagnoses supervisor GC versus native allocation; it is not used
	// to refit or enlarge an acceptance bound using measured-block values.
	const nativeIdentity = `case "$2" in ''|*[!0-9]*) exit 1;; esac; [ "$2" -gt 1 ]; IFS= read -r name < "/proc/$2/comm"; [ "$name" = whisper-server ]; `
	breakdownAt := time.Now()
	data, err = e.target.run(ctx, "owned backend process RSS breakdown", false, "exec", containers.BackendID, "sh", "-eu", "-c", nativeIdentity+residentProcessRSSProbe, "p07-rss-probe", "1", strconv.Itoa(*state.NativeNamespacePID))
	if err != nil {
		return p, state, err
	}
	p.BackendProcessRSS, err = parseResidentRSSBreakdown(data, breakdownAt)
	return p, state, err
}

func residentCompletedWindow(t *testing.T, e *acceptanceEnvironment, count int, cadence time.Duration) []residentResourcePoint {
	t.Helper()
	var points []residentResourcePoint
	var first DockerSnapshot
	for i := 0; i < count; i++ {
		point, state, err := residentActualResources(e.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if state.PrivateHealth == nil || state.PrivateHealth.Status != "ready" || !state.PrivateHealth.CUDAReady {
			t.Fatal("idle retention lost CUDA readiness")
		}
		if i == 0 {
			first = state
		} else if *state.NativeHostPID != *first.NativeHostPID || state.PrivateHealth.ProcessGeneration != first.PrivateHealth.ProcessGeneration {
			t.Fatal("idle retention replaced a healthy resident native process")
		}
		points = append(points, point)
		if i+1 < count {
			select {
			case <-e.ctx.Done():
				t.Fatal("resident sampling cancelled")
			case <-time.After(cadence):
			}
		}
	}
	return points
}

func TestResidentDeletedOpenProbeObservesActualUnlinkedStorage(t *testing.T) {
	directory := t.TempDir()
	file, err := os.CreateTemp(directory, "request-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.WriteString("disposable resource control"); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
	observe := func() ([]byte, error) {
		command := exec.CommandContext(t.Context(), "sh", "-eu", "-c", residentDeletedScratchProbe, "p07-deleted-scratch", directory, strconv.Itoa(os.Getpid()))
		return command.Output()
	}
	data, err := observe()
	if err != nil {
		t.Fatal(err)
	}
	fds, bytes, err := parseResidentDeletedScratch(data)
	if err == nil || fds != 1 || bytes != uint64(len("disposable resource control")) {
		t.Fatal("real deleted-but-open request data escaped the observation")
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err = observe()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = parseResidentDeletedScratch(data); err != nil {
		t.Fatal("closed request storage was not released")
	}
}
