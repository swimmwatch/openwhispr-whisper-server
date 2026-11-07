package acceptance

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// GPUActivity contains only measurements for the selected native host process.
// Other desktop/application process rows are discarded, never retained in logs.
type GPUActivity struct {
	Samples         int
	PeakSMPercent   uint64
	PeakMemoryBytes uint64
}

type gpuActivityReader struct {
	pid      int
	activity GPUActivity
	columns  map[string]int
}

func (r *gpuActivityReader) sample(line string) {
	fields := strings.Fields(line)
	if len(fields) > 1 && fields[0] == "#" {
		if fields[1] == "gpu" {
			r.columns = make(map[string]int)
			for index, name := range fields[1:] {
				r.columns[name] = index
			}
		}
		return
	}
	values := make(map[string]string, 3)
	for _, name := range []string{"pid", "sm", "fb"} {
		index, present := r.columns[name]
		if !present || index >= len(fields) {
			return
		}
		values[name] = fields[index]
	}
	pid, err := strconv.Atoi(values["pid"])
	if err != nil || pid != r.pid {
		return
	}
	sm, smErr := strconv.ParseUint(values["sm"], 10, 64)
	memory, memoryErr := strconv.ParseUint(values["fb"], 10, 64)
	if smErr != nil || memoryErr != nil || sm > 100 || memory > ^uint64(0)/(1<<20) {
		return
	}
	r.activity.Samples++
	if sm > r.activity.PeakSMPercent {
		r.activity.PeakSMPercent = sm
	}
	if bytes := memory * (1 << 20); bytes > r.activity.PeakMemoryBytes {
		r.activity.PeakMemoryBytes = bytes
	}
}

// ObserveGPUActivity samples actual NVIDIA per-process utilization and allocation
// while work runs. Explicit cancellation stops a completed observation; deadline
// expiry or early observer exit remains incomplete. No GPU activity is inferred
// from public latency or device enumeration.
func ObserveGPUActivity(ctx context.Context, nativeHostPID int, ready chan<- struct{}) (GPUActivity, error) {
	if nativeHostPID <= 0 {
		return GPUActivity{}, errors.New("an observed native host PID is required")
	}
	observerContext, stop := context.WithCancel(ctx)
	defer stop()
	command := exec.CommandContext(observerContext, "nvidia-smi", "pmon", "-i", "0", "-s", "um", "-d", "1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 200 * time.Millisecond
	var killedOnCancel atomic.Bool
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if err == nil {
			killedOnCancel.Store(true)
		}
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	stdout, sink := io.Pipe()
	command.Stdout, command.Stderr = sink, io.Discard
	if err := command.Start(); err != nil {
		_ = sink.Close()
		_ = stdout.Close()
		return GPUActivity{}, errors.Join(errors.New("cannot start scoped GPU observer"), ctx.Err())
	}
	if ready != nil {
		close(ready)
	}
	type observation struct {
		activity GPUActivity
		err      error
	}
	done := make(chan observation, 1)
	go func() {
		reader := gpuActivityReader{pid: nativeHostPID}
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			reader.sample(scanner.Text())
		}
		if scanner.Err() != nil {
			stop()
		}
		// A failed scanner must also release the exec copier's sink.Write.
		_ = stdout.Close()
		done <- observation{reader.activity, scanner.Err()}
	}()
	// Wait owns exec's copying goroutine and its WaitDelay. A leader that
	// exits while a descendant holds stdout cannot leave Scan blocked forever.
	waitErr := command.Wait()
	stop()
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	_ = sink.Close()
	result := <-done
	var exited *exec.ExitError
	stopped := false
	if ctx.Err() == context.Canceled && killedOnCancel.Load() && errors.As(waitErr, &exited) {
		status, ok := exited.Sys().(syscall.WaitStatus)
		stopped = ok && status.Signaled() && status.Signal() == syscall.SIGKILL
	}
	if result.err != nil || !stopped {
		return result.activity, errors.Join(errors.New("scoped GPU activity observation failed"), ctx.Err())
	}
	return result.activity, nil
}
