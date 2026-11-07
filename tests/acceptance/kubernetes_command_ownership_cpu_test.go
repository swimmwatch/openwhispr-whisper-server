package acceptance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestKubernetesCommandCancellationOwnsDescendants(t *testing.T) {
	// Every command resolves to this CPU-only process, never a cluster client.
	for name, run := range map[string]func(context.Context) ([]byte, error){
		"read": func(ctx context.Context) ([]byte, error) { return kubectlOutput(ctx, "get", "pods") },
		"queue": func(ctx context.Context) ([]byte, error) {
			return (developmentQueueTarget{}).command(ctx, "get", "pods")
		},
		"pairing": func(ctx context.Context) ([]byte, error) {
			return (pairingSelection{}).command(ctx, "selected", "get", "pods")
		},
		"cache": func(ctx context.Context) ([]byte, error) {
			return (kubernetesCacheSelection{}).command(ctx, "get", "pods")
		},
		"graceful": func(ctx context.Context) ([]byte, error) { return gracefulKubectl(ctx, []string{"get", "pods"}, nil) },
		"logs":     func(ctx context.Context) ([]byte, error) { return p08GCLogCommand(ctx, "kubectl", "logs") },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "child.pid")
			script := "#!/bin/sh\nsleep 30 &\nchild=$!\nprintf '%s\\n' \"$child\" > \"$ACCEPTANCE_CHILD_PID\"\nwait \"$child\"\n"
			if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("ACCEPTANCE_CHILD_PID", pidFile)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := run(ctx); done <- err }()
			var pid int
			deadline := time.After(2 * time.Second)
			for pid == 0 {
				select {
				case <-deadline:
					cancel()
					t.Fatal("owned CPU command did not publish child identity")
				case err := <-done:
					t.Fatalf("owned CPU command failed before cancellation: %v", err)
				case <-time.After(5 * time.Millisecond):
					data, err := os.ReadFile(pidFile)
					if err == nil && strings.HasSuffix(string(data), "\n") {
						pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
					}
				}
			}
			defer syscall.Kill(pid, syscall.SIGKILL)
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), dir) {
					t.Error("cancellation cause lost or private selector exposed")
				}
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(pid, syscall.SIGKILL)
				<-done
				t.Fatal("cancelled command waited for a surviving descendant's pipe")
			}
			retired := time.After(time.Second)
			for {
				data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
				if clientCaseProcessGone(err) || err == nil && strings.Contains(string(data), ") Z ") {
					break
				}
				if err != nil {
					var errno syscall.Errno
					_ = errors.As(err, &errno)
					t.Fatalf("owned descendant cleanup could not be established: errno=%d", errno)
				}
				select {
				case <-retired:
					t.Fatal("owned command descendant remained active after cancellation")
				case <-time.After(5 * time.Millisecond):
				}
			}
		})
	}
}

func TestKubernetesCommandPreservesInputAndSafeFailureCause(t *testing.T) {
	input := []byte(`{"kind":"DeleteOptions"}`)
	data, err := runKubernetesCommand(t.Context(), "sh", time.Second, len(input), input, "-c", "cat")
	if err != nil || string(data) != string(input) {
		t.Fatal("bounded command changed stdin or rejected an exact-bound response")
	}
	data, err = runKubernetesCommand(t.Context(), "sh", time.Second, 1024, nil, "-c", "printf PRIVATE_OBSERVATION; printf PRIVATE_CREDENTIAL >&2; exit 7")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 || len(data) != 0 || strings.Contains(err.Error(), "PRIVATE_") {
		t.Fatal("command failure lost its exit identity or exposed private diagnostics")
	}
}

func TestKubernetesQueueAndPairingBoundOutput(t *testing.T) {
	for name, run := range map[string]func(context.Context) ([]byte, error){
		"queue": func(ctx context.Context) ([]byte, error) {
			return (developmentQueueTarget{}).command(ctx, "get", "pods")
		},
		"pairing": func(ctx context.Context) ([]byte, error) {
			return (pairingSelection{}).command(ctx, "selected", "get", "pods")
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\nhead -c 1048577 /dev/zero\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			data, err := run(t.Context())
			if err == nil || len(data) != 0 {
				t.Fatal("unbounded Kubernetes output was accepted as an observation")
			}
		})
	}
}
