package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

var residentHeapLiveRoots [][]byte
var residentHeapChurnRoot []byte

type residentLiveHeapFacts struct {
	Before, BaselineThrough, RequestsThrough, ModelThrough uint32
	RequestCopyBytes, ModelCopyBytes                       uint64
}

// This disposable CPU subprocess holds real byte arrays through naturally
// completed GC cycles. It does not call runtime.GC, change GOGC/GOMEMLIMIT, touch
// models/audio or provide any override of the real target's observation gates.
func TestResidentSupervisorHeapActualLiveCopyControls(t *testing.T) {
	if os.Getenv("P07_PRIVATE_LIVE_HEAP_FIXTURE") == "1" {
		settings, err := config.Load(nil, []string{})
		if err != nil {
			os.Exit(2)
		}
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		facts := residentLiveHeapFacts{Before: stats.NumGC, RequestCopyBytes: 44 + 11*16000*2, ModelCopyBytes: uint64(settings.Models.Catalog[0].Source.ExpectedBytes)}
		naturalCycles := func() uint32 {
			runtime.ReadMemStats(&stats)
			target := stats.NumGC + 3
			for attempt := 0; attempt < 32768; attempt++ {
				residentHeapChurnRoot = make([]byte, 64<<10)
				residentHeapChurnRoot[0] = byte(attempt)
				if attempt%8 == 0 {
					runtime.Gosched()
					runtime.ReadMemStats(&stats)
					if stats.NumGC >= target {
						return stats.NumGC
					}
				}
			}
			os.Exit(3)
			return 0
		}
		facts.BaselineThrough = naturalCycles()
		for i := 0; i < 20; i++ {
			copy := make([]byte, int(facts.RequestCopyBytes))
			copy[0] = byte(i)
			residentHeapLiveRoots = append(residentHeapLiveRoots, copy)
		}
		facts.RequestsThrough = naturalCycles()
		model := make([]byte, int(facts.ModelCopyBytes))
		model[0] = 1
		residentHeapLiveRoots = append(residentHeapLiveRoots, model)
		facts.ModelThrough = naturalCycles()
		runtime.KeepAlive(residentHeapLiveRoots)
		_ = json.NewEncoder(os.Stdout).Encode(facts)
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResidentSupervisorHeapActualLiveCopyControls$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GODEBUG=") && !strings.HasPrefix(entry, "P07_PRIVATE_LIVE_HEAP_FIXTURE=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GODEBUG=gctrace=1,scavtrace=1", "P07_PRIVATE_LIVE_HEAP_FIXTURE=1")
	stdout := &dockerOutput{cancel: cancel, retain: true}
	stderr := &dockerOutput{cancel: cancel, retain: true}
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = 200 * time.Millisecond
	if err := command.Run(); err != nil || stdout.overflow || stderr.overflow {
		t.Fatal("bounded natural-GC live-copy subprocess failed; raw diagnostics withheld")
	}
	var facts residentLiveHeapFacts
	if json.Unmarshal(stdout.data.Bytes(), &facts) != nil {
		t.Fatal("actual live-copy fixture counters unavailable")
	}
	observed, err := observeResidentSupervisorGC(stderr.data.Bytes())
	if err != nil || observed.GC == nil {
		t.Fatal("actual naturally emitted GC observations unavailable")
	}
	var baseline, requests, model []residentGCRecord
	for _, record := range observed.GC.Records {
		if record.Forced {
			t.Fatal("live-copy control unexpectedly forced a collection")
		}
		switch {
		case record.Cycle > uint64(facts.Before) && record.Cycle <= uint64(facts.BaselineThrough):
			baseline = append(baseline, record)
		case record.Cycle > uint64(facts.BaselineThrough) && record.Cycle <= uint64(facts.RequestsThrough):
			requests = append(requests, record)
		case record.Cycle > uint64(facts.RequestsThrough) && record.Cycle <= uint64(facts.ModelThrough):
			model = append(model, record)
		}
	}
	if len(baseline) < 3 || len(requests) < 3 || len(model) < 3 {
		t.Fatal("live-copy controls lack comparable fresh natural collections")
	}
	var upper uint64
	for _, record := range baseline {
		upper = max(upper, record.AfterGCLiveBytes.UpperExclusive)
	}
	if !errors.Is(checkResidentSupervisorMarkedIntervals(upper, requests), errResidentSupervisorHeapGrowth) {
		t.Fatal("actual held twenty canonical-request byte copies escaped marked-heap growth detection")
	}
	if !errors.Is(checkResidentSupervisorMarkedIntervals(upper, model), errResidentSupervisorHeapGrowth) {
		t.Fatal("actual held full model-size byte copy escaped marked-heap growth detection")
	}
	runtime.KeepAlive(facts)
}
