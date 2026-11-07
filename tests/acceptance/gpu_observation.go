package acceptance

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
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
// while work runs. Start/stop the caller-owned context around the workload; no
// GPU activity is inferred from public latency or device enumeration.
func ObserveGPUActivity(ctx context.Context, nativeHostPID int, ready chan<- struct{}) (GPUActivity, error) {
	if nativeHostPID <= 0 {
		return GPUActivity{}, errors.New("an observed native host PID is required")
	}
	command := exec.CommandContext(ctx, "nvidia-smi", "pmon", "-i", "0", "-s", "um", "-d", "1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return GPUActivity{}, errors.New("cannot open scoped GPU observer")
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return GPUActivity{}, errors.New("cannot start scoped GPU observer")
	}
	if ready != nil {
		close(ready)
	}
	reader := gpuActivityReader{pid: nativeHostPID}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		reader.sample(scanner.Text())
	}
	waitErr := command.Wait()
	if scanner.Err() != nil || waitErr != nil && ctx.Err() == nil {
		return reader.activity, errors.New("scoped GPU activity observation failed")
	}
	return reader.activity, nil
}
