package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const probeIdentity = "GPU-11111111-1111-1111-1111-111111111111, NVIDIA GeForce RTX 2060, 7.5\n"

func TestGPUProbeProcess(t *testing.T) {
	mode := os.Getenv("WHISPER_TEST_GPU_PROBE")
	if mode == "" {
		return
	}

	root := os.Getenv("WHISPER_TEST_STATE")
	if err := os.WriteFile(filepath.Join(root, "probe-pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}

	switch mode {
	case "overflow":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("X", maxGPUQueryBytes+1))
	case "blocked":
		time.Sleep(time.Minute)
	case "failure":
		fmt.Fprintln(os.Stderr, "PRIVATE GPU diagnostic")
		os.Exit(1)
	default:
		fmt.Print(probeIdentity)
	}
	os.Exit(0)
}

func probeOptions(t *testing.T, mode string) (Options, string) {
	t.Helper()

	opts, root, _ := supervisorOptions(t)
	opts.GPUCommand = []string{os.Args[0], "-test.run=^TestGPUProbeProcess$", "--"}
	opts.Environment = append(opts.Environment, "WHISPER_TEST_GPU_PROBE="+mode)
	return opts, root
}

func TestGPUQueryBoundsAndCancellation(t *testing.T) {
	for _, mode := range []string{"ready", "overflow", "failure", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			opts, root := probeOptions(t, mode)
			s, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.transport.CloseIdleConnections()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			type result struct {
				output []byte
				err    error
			}
			finished := make(chan result, 1)
			go func() {
				output, err := s.gpuQuery(ctx)
				finished <- result{
					output: output,
					err:    err,
				}
			}()

			if mode == "blocked" {
				await(t, func() bool {
					_, err := os.Stat(filepath.Join(root, "probe-pid"))
					return err == nil
				})
				cancel()
			}

			got := <-finished
			if mode == "ready" {
				if got.err != nil || string(got.output) != probeIdentity {
					t.Fatalf("valid GPU query: %q %v", got.output, got.err)
				}
			} else if !errors.Is(got.err, errGPU) || len(got.output) != 0 {
				t.Fatalf("unsafe GPU result exposed: bytes=%d err=%v", len(got.output), got.err)
			}

			data, err := os.ReadFile(filepath.Join(root, "probe-pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("GPU query returned before its process was reaped", err)
			}
		})
	}
}

func TestNativeHealthValidation(t *testing.T) {
	for _, scenario := range []struct {
		name string
		body string
		code int
	}{
		{
			name: "status",
			body: `{"status":"ok"}`,
			code: 503,
		},
		{
			name: "malformed",
			body: "PRIVATE health body",
			code: 200,
		},
		{
			name: "not-loaded",
			body: `{"status":"loading"}`,
			code: 200,
		},
		{
			name: "oversize",
			body: strings.Repeat("x", maxNativeHealthBytes+1),
			code: 200,
		},
		{
			name: "interrupted",
			body: `{"status":"ok"}`,
			code: 200,
		},
		{
			name: "gpu-replaced",
			body: `{"status":"ok"}`,
			code: 200,
		},
		{
			name: "child-exited",
			body: `{"status":"ok"}`,
			code: 200,
		},
		{
			name: "ready",
			body: `{"status":"ok"}`,
			code: 200,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/health" {
					t.Error("health used an unexpected native route")
				}
				if scenario.name == "interrupted" {
					w.Header().Set("Content-Length", "1000")
				}
				w.WriteHeader(scenario.code)
				_, _ = io.WriteString(w, scenario.body)
			}))
			defer server.Close()

			opts, root := probeOptions(t, "ready")
			opts.CleanupTimeout = 8 * time.Second
			opts.OperationTimeout = 8 * time.Second
			opts.NativePort = server.Listener.Addr().(*net.TCPAddr).Port
			s, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.transport.CloseIdleConnections()

			child := &nativeChild{
				done:     make(chan struct{}),
				evidence: &nativeEvidence{},
			}
			_, _ = child.evidence.Write([]byte(deviceLine + backendLine + allocationLine))
			if scenario.name == "gpu-replaced" {
				child.uuid = "GPU-22222222-2222-2222-2222-222222222222"
			}
			if scenario.name == "child-exited" {
				close(child.done)
			}

			err = s.checkHealth(context.Background(), child)
			switch scenario.name {
			case "ready":
				if err != nil || child.uuid != strings.Split(probeIdentity, ",")[0] {
					t.Fatalf("healthy identity not retained: %q %v", child.uuid, err)
				}
			case "gpu-replaced":
				if !errors.Is(err, errGPU) || child.uuid != "GPU-22222222-2222-2222-2222-222222222222" {
					t.Fatal("GPU identity change accepted or overwrote the original identity", err)
				}
			default:
				if !errors.Is(err, errNative) {
					t.Fatal("invalid native health accepted", err)
				}
			}

			if scenario.name != "ready" && scenario.name != "gpu-replaced" && scenario.name != "child-exited" {
				if _, err := os.Stat(filepath.Join(root, "probe-pid")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("invalid native health triggered a GPU query", err)
				}
			}
		})
	}
}
