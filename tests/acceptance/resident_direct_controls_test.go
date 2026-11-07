package acceptance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type residentTrackedBody struct {
	io.Reader
	closed bool
}

func (b *residentTrackedBody) Close() error { b.closed = true; return nil }

func TestResidentExecTransportForwardsBodyAndOwnsRelease(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "exec-fixture")
	script := `#!/bin/sh
body=$(dd bs=4096 2>/dev/null)
[ "$body" = '{"control":"value"}' ] || exit 4
case "$*" in *'Content-Type: application/json'*'X-Request-ID: owned-control'*) ;; *) exit 5;; esac
printf 'HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n{}'
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	target := &DockerTarget{opts: DockerOptions{DockerExecutable: executable, Root: t.TempDir()}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	body := &residentTrackedBody{Reader: strings.NewReader(`{"control":"value"}`)}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8081/inference", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "owned-control")
	transport := residentExecTransport{target, "owned-fixture", "127.0.0.1:8081"}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if !body.closed {
		t.Fatal("private comparison did not release its borrowed request body")
	}
	// A rejected route also owns and closes the supplied request body.
	body = &residentTrackedBody{Reader: strings.NewReader("private control")}
	request, err = http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8081/unavailable", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = transport.RoundTrip(request); err == nil || !body.closed {
		t.Fatal("rejected private request retained its body or was executed")
	}
}

func TestResidentExecTransportRetiresChildAfterLeaderExit(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child-pid")
	t.Setenv("P07_PRIVATE_CHILD_PID", childPath)
	executable := filepath.Join(dir, "exec-fixture")
	script := "#!/bin/sh\nsleep 30 &\nprintf '%s\\n' \"$!\" > \"$P07_PRIVATE_CHILD_PID\"\nprintf 'HTTP/1.1 " + strconv.Itoa(http.StatusOK) + " " + http.StatusText(http.StatusOK) + "\\r\\nContent-Length: 2\\r\\n\\r\\n{}'\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	target := &DockerTarget{opts: DockerOptions{DockerExecutable: executable, Root: dir}}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8081/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, requestErr := (residentExecTransport{target, "owned-fixture", "127.0.0.1:8081"}).RoundTrip(request)
	if response != nil {
		response.Body.Close()
	}
	data, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatal("owned private transport child was not observed")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatal("owned private transport child identity unavailable")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	if requestErr == nil || ctx.Err() != nil {
		t.Fatal("inherited private transport pipe was accepted or exhausted the caller budget")
	}
	deadline := time.Now().Add(time.Second)
	for {
		data, err = os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if clientCaseProcessGone(err) || err == nil && strings.Contains(string(data), ") Z ") {
			return
		}
		if err != nil {
			var errno syscall.Errno
			_ = errors.As(err, &errno)
			t.Fatalf("owned private transport child state unavailable: errno=%d", errno)
		}
		if time.Now().After(deadline) {
			t.Fatal("private transport descendant survived its exited leader")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestResidentExecTransportPreservesTruncatedResponseFailure(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "exec-fixture")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'HTTP/1.1 200 OK\\r\\nContent-Length: 20\\r\\n\\r\\n{}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	target := &DockerTarget{opts: DockerOptions{DockerExecutable: executable, Root: t.TempDir()}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8081/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (residentExecTransport{target, "owned-fixture", "127.0.0.1:8081"}).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err = io.ReadAll(response.Body); err == nil {
		t.Fatal("truncated private response was converted into a complete response")
	}
}
