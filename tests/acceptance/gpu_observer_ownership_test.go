package acceptance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGPUObserverMalformedOutputStopsOwnedProcess(t *testing.T) {
	// A test-owned executable supplies malformed output, never the NVIDIA tool.
	dir := t.TempDir()
	executable := filepath.Join(dir, "nvidia-smi")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nhead -c 65537 /dev/zero\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ObserveGPUActivity(ctx, 123, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("malformed observer output passed")
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("scanner failure did not stop and reap its owned observer")
	}
}

func TestGPUObserverEarlyExitRemainsIncomplete(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "nvidia-smi")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '# gpu pid sm fb\n0 123 25 64\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	activity, err := ObserveGPUActivity(t.Context(), 123, nil)
	if err == nil || activity.Samples != 1 || activity.PeakSMPercent != 25 {
		t.Fatal("early observer exit passed or discarded its partial observations")
	}
}

func TestGPUObserverEarlyExitClosesDescendantPipe(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "nvidia-smi")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nsleep 30 &\nprintf '# gpu pid sm fb\\n0 123 25 64\\n'\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ObserveGPUActivity(ctx, 123, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("early observer exit with inherited stdout was accepted")
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("early observer exit waited for caller cancellation to close a descendant pipe")
	}
}

func TestGPUObserverFailureBeforeCancellationRemainsIncomplete(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "nvidia-smi")
	pidFile := filepath.Join(dir, "observer.pid")
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" > \"$ACCEPTANCE_OBSERVER_PID\"\nsleep 30 &\nexit 7\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ACCEPTANCE_OBSERVER_PID", pidFile)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ObserveGPUActivity(ctx, 123, nil)
		done <- err
	}()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("failed observer was accepted before caller cancellation")
			}
			return
		case <-deadline:
			cancel()
			<-done
			t.Fatal("CPU observer did not exit within its fixture budget")
		case <-time.After(time.Millisecond):
			data, err := os.ReadFile(pidFile)
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || parseErr != nil || pid <= 0 {
				continue
			}
			if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); !errors.Is(err, os.ErrNotExist) {
				continue
			}
			// The leader was reaped with exit 7; cancellation now releases only
			// its descendant's inherited pipe, not a healthy running observer.
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("observer exit failure was masked by later caller cancellation")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("failed observer did not close its inherited pipe")
			}
			return
		}
	}
}

func TestGPUObserverExplicitStopCompletesObservation(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "nvidia-smi")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := ObserveGPUActivity(ctx, 123, ready)
		done <- err
	}()
	select {
	case <-ready:
		cancel()
	case err := <-done:
		t.Fatalf("owned CPU observer failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("owned CPU observer did not start")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("explicit observation stop was not accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("owned CPU observer did not stop")
	}
}

func TestGPUObserverDeadlineRemainsIncomplete(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "nvidia-smi")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := ObserveGPUActivity(ctx, 123, nil)
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), dir) {
		t.Fatal("observer deadline was reported as a completed observation or exposed private target")
	}
}
