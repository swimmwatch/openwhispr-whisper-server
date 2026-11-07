package acceptance

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Invoke the actual memory entry in a disposable CPU subprocess with no Docker
// target. Missing observation profiles must fail as unknown BEFORE any observer
// or workload can be reached; a nil-target panic is not an acceptable rejection.
func TestResidentMemoryEntryRequiresBothGoProfiles(t *testing.T) {
	if os.Getenv("P07_PRIVATE_MEMORY_ENTRY_FIXTURE") == "1" {
		runResidentMeasurements(t, &acceptanceEnvironment{})
		t.Fatal("missing Go profiles qualified the resident memory entry")
	}
	for _, profiles := range []struct{ name, api, supervisor string }{
		{"none", "", ""}, {"API-only", "1", ""}, {"supervisor-only", "", "1"},
	} {
		t.Run(profiles.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResidentMemoryEntryRequiresBothGoProfiles$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "P07_API_GC_TRACE=") && !strings.HasPrefix(entry, "P07_SUPERVISOR_GC_TRACE=") && !strings.HasPrefix(entry, "P07_PRIVATE_MEMORY_ENTRY_FIXTURE=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, "P07_PRIVATE_MEMORY_ENTRY_FIXTURE=1", "P07_API_GC_TRACE="+profiles.api, "P07_SUPERVISOR_GC_TRACE="+profiles.supervisor)
			output := &dockerOutput{cancel: cancel, retain: true}
			command.Stdout, command.Stderr = output, output
			command.WaitDelay = 200 * time.Millisecond
			err := command.Run()
			if err == nil || output.overflow || ctx.Err() != nil || !strings.Contains(output.data.String(), errResidentSupervisorHeapUnknown.Error()) || strings.Contains(output.data.String(), "panic:") {
				t.Fatal("missing Go profiles reached the Docker/GPU observer boundary or qualified memory instead of reporting unknown")
			}
		})
	}
}
