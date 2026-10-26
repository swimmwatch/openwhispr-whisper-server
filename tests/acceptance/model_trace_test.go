package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A requested observer stop is successful ownership teardown, not a tracer
// failure. Unexpected tracer exit remains the session's nonzero exit status.
const modelObserverSession = `"$@" &
trace_pid=$!
finish() {
 trap '' INT TERM
 kill -TERM "$trace_pid" 2>/dev/null || :
 wait "$trace_pid" || :
 exit 0
}
trap finish INT TERM
wait "$trace_pid"
`

// The observer joins only this backend PID namespace. It does not grant the
// application privileges, alter host ptrace policy or expose native endpoints.
func startLocalModelTrace(t *testing.T, e *localEnvironment, snapshot DockerSnapshot) func() (uint64, error) {
	t.Helper()
	if snapshot.NativeNamespacePID == nil || snapshot.NativeHostPID == nil {
		t.Fatal("native identities are required before model-I/O tracing")
	}
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	fdData, fdErr := e.target.run(e.ctx, "initial model descriptor observation", false, "exec", containers.BackendID, "find", "/proc/"+strconv.Itoa(*snapshot.NativeNamespacePID)+"/fd", "-maxdepth", "1", "-lname", "/prepared/base.bin", "-printf", "%f\\n")
	if fdErr != nil {
		t.Fatal("initial model descriptors could not be observed")
	}
	var initialFDs []int
	for _, field := range strings.Fields(string(fdData)) {
		fd, err := strconv.Atoi(field)
		if err != nil || fd < 0 {
			t.Fatal("initial model descriptor observation is invalid")
		}
		initialFDs = append(initialFDs, fd)
	}
	args := []string{"create", "--attach", "stdout", "--attach", "stderr", "--network", "none", "--pid", "container:" + containers.BackendID, "--read-only", "--cap-drop", "ALL", "--cap-add", "SYS_PTRACE", "--cap-add", "KILL", "--security-opt", "no-new-privileges:true", "--user", "0:0", "--env", "NVIDIA_VISIBLE_DEVICES=void", "--env", "LC_ALL=C", "--entrypoint", "/bin/sh"}
	for _, path := range []string{"/usr/bin/strace", "/lib/x86_64-linux-gnu/libunwind-ptrace.so.0", "/lib/x86_64-linux-gnu/libunwind-x86_64.so.8", "/lib/x86_64-linux-gnu/libunwind.so.8", "/lib/x86_64-linux-gnu/liblzma.so.5"} {
		target := path
		if path == "/usr/bin/strace" {
			target = "/observer/strace"
		}
		args = append(args, "--mount", "type=bind,source="+path+",target="+target+",readonly")
	}
	args = append(args, e.receipt.BackendImage, "-c", modelObserverSession, "p07-observer", "/observer/strace", "-f", "-qq", "-yy", "-e", "trace=openat,read,pread64,recvfrom,mmap,munmap,close", "-e", "raw=read,pread64,recvfrom", "-p", strconv.Itoa(*snapshot.NativeNamespacePID))
	output, err := e.target.run(e.ctx, "create isolated model observer", false, args...)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(output))
	if len(id) != 64 {
		t.Fatal("invalid owned observer identity")
	}
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := e.target.run(e.ctx, "isolated model observation", true, "start", "--attach", id)
		done <- result{data, err}
	}()
	stopped := false
	var observed uint64
	var traceError error
	stop := func() (uint64, error) {
		if stopped {
			return observed, traceError
		}
		stopped = true
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, stopErr := e.target.run(cleanup, "stop isolated model observer", false, "stop", "--signal", "SIGINT", "--timeout", "2", id)
		var trace result
		select {
		case trace = <-done:
		case <-cleanup.Done():
			traceError = errors.New("isolated model observer did not stop within its budget")
		}
		// This exact ID was obtained from our create operation; no foreign target
		// or model-cache volume is selected by observer teardown.
		_, removeErr := e.target.run(cleanup, "remove isolated model observer", false, "rm", "--force", id)
		if stopErr != nil || trace.err != nil || removeErr != nil || traceError != nil {
			traceError = fmt.Errorf("isolated model-I/O observation unavailable (stop=%v, stream=%v, remove=%v); no absence proof recorded", stopErr, trace.err, removeErr)
			return 0, traceError
		}
		var diagnostic []string
		for _, line := range strings.Split(string(trace.data), "\n") {
			if strings.Contains(line, "strace:") && strings.Contains(line, "Operation not permitted") {
				diagnostic = append(diagnostic, line)
			}
		}
		if len(diagnostic) != 0 {
			_ = os.WriteFile(filepath.Join(e.workdir, "trace-denial.log"), []byte(strings.Join(diagnostic, "\n")), 0600)
		}
		observed, traceError = modelPathOperations(trace.data, "/prepared/base.bin", initialFDs...)
		return observed, traceError
	}
	t.Cleanup(func() {
		if _, err := stop(); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		const probe = `while read -r key value rest; do if [ "$key" = TracerPid: ]; then printf '%s\n' "$value"; exit; fi; done < "/proc/$1/status"`
		data, err := e.target.run(e.ctx, "observer attach observation", false, "exec", containers.BackendID, "sh", "-c", probe, "p07-trace", strconv.Itoa(*snapshot.NativeNamespacePID))
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && parseErr == nil && pid > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("model observer could not attach within the bounded backend-only diagnostic scope")
		}
		select {
		case early := <-done:
			done <- early
			t.Fatal("model observer exited before attachment; prerequisite is unavailable")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return stop
}

func modelPathOperations(trace []byte, model string, initialFDs ...int) (uint64, error) {
	if len(trace) == 0 || strings.Contains(string(trace), "error while loading shared libraries") {
		return 0, errors.New("native model trace is unavailable")
	}
	for _, line := range strings.Split(string(trace), "\n") {
		if strings.Contains(line, "strace:") && strings.Contains(line, "Operation not permitted") {
			operation := "unclassified"
			for _, name := range []string{"PTRACE_SEIZE", "PTRACE_ATTACH", "PTRACE_DETACH", "PTRACE_INTERRUPT", "PTRACE_SETOPTIONS", "PTRACE_GETREGS", "attach", "detach"} {
				if strings.Contains(line, name) {
					operation = name
					break
				}
			}
			return 0, fmt.Errorf("native model observer lacks tracing permission (operation=%s)", operation)
		}
	}
	tracked := make(map[int]bool)
	for _, fd := range initialFDs {
		tracked[fd] = true
	}
	fdOperation := regexp.MustCompile(`\b(read|pread64|close)\((0x[0-9a-f]+|[0-9]+)`)
	opened := regexp.MustCompile(`=\s+([0-9]+)`)
	var count uint64
	for _, line := range strings.Split(string(trace), "\n") {
		// Raw read/pread arguments never reveal buffer contents. Track descriptors
		// already open at attachment as well as later model opens, and forget
		// them on close so reused audio/socket descriptors cannot look like I/O.
		if match := fdOperation.FindStringSubmatch(line); match != nil {
			fd, err := strconv.ParseInt(match[2], 0, 32)
			if err == nil {
				if match[1] == "close" {
					delete(tracked, int(fd))
				} else if tracked[int(fd)] {
					count++
				}
			}
		}
		if strings.Contains(line, model) && (strings.Contains(line, "openat(") || strings.Contains(line, "mmap(")) {
			count++
			if strings.Contains(line, "openat(") {
				if match := opened.FindStringSubmatch(line); match != nil {
					if fd, err := strconv.Atoi(match[1]); err == nil {
						tracked[fd] = true
					}
				}
			}
		}
	}
	return count, nil
}

func TestLocalModelObserver(t *testing.T) {
	e := openLocalEnvironment(t, nil)
	snapshot, err := e.target.Observe(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	stop := startLocalModelTrace(t, e, snapshot)
	reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", "base"}, {"language", "en"}})
	if err != nil || reply.Status != http.StatusOK {
		t.Fatal("observer control transcription failed")
	}
	count, err := stop()
	if err != nil || count != 0 {
		t.Fatalf("model observer control failed: operations=%d error=%v", count, err)
	}
}

func TestModelTraceDoesNotConfuseTargetSyscallDenialWithObserverFailure(t *testing.T) {
	data := []byte("[pid 12] mmap(NULL, 4096, PROT_READ, MAP_SHARED, 5, 0) = -1 EPERM (Operation not permitted)\n[pid 12] recvfrom(0x8, 0x1234, 0x400, 0, 0, 0) = 0x200\n")
	count, err := modelPathOperations(data, "/prepared/base.bin")
	if err != nil || count != 0 {
		t.Fatal("an observed non-model syscall failure was mistaken for unavailable tracing")
	}
}

func TestModelTraceTracksExistingDescriptorReadsAndReuse(t *testing.T) {
	data := []byte("read(0x4, 0x11111, 0x400) = 0x400\nclose(4</prepared/base.bin>) = 0\nread(0x4, 0x22222, 0x200) = 0x200\n")
	count, err := modelPathOperations(data, "/prepared/base.bin", 4)
	if err != nil || count != 1 {
		t.Fatal("existing model reads or descriptor reuse were misclassified")
	}
}

func TestModelTraceDetectsHealthyRequestModelReopenWithoutContentOutput(t *testing.T) {
	trace := []byte("[pid 12] read(0x3, 0x111111, 0x400) = 0x400\n[pid 12] openat(AT_FDCWD, \"/prepared/base.bin\", O_RDONLY) = 4\n")
	count, err := modelPathOperations(trace, "/prepared/base.bin")
	if err != nil || count != 1 {
		t.Fatalf("model reload was not observed: count=%d error=%t", count, err != nil)
	}
	if _, err := modelPathOperations(nil, "/prepared/base.bin"); err == nil {
		t.Fatal("missing trace became absence proof")
	}
	if _, err := modelPathOperations([]byte(fmt.Sprintln("strace: Operation not permitted")), "/prepared/base.bin"); err == nil {
		t.Fatal("unavailable trace became absence proof")
	}
}
