package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const actualStyleGCTrace = `gc 12 @80.921s 0%: 0.084+1.8+0.021 ms clock, 2.0+0.41/8.9/1.0+0.50 ms cpu, 10->11->6 MB, 12 MB goal, 0 MB stacks, 1 MB globals, 24 P`
const actualStyleScavTrace = `scav 128 KiB work (bg), 4 KiB work (eager), 896 KiB now, 88% util`

func TestResidentGCObservationPreservesActualQuantization(t *testing.T) {
	logs := []byte("2026-10-27T16:00:00.123456789Z " + actualStyleGCTrace + "\n" + actualStyleScavTrace + "\n")
	facts, err := observeResidentSupervisorGC(logs)
	if err != nil {
		t.Fatal(err)
	}
	if facts.GC == nil || len(facts.GC.Records) != 1 || facts.Scavenger == nil || len(facts.Scavenger.Records) != 1 {
		t.Fatal("actual numeric records were lost")
	}
	gc := facts.GC.Records[0]
	if gc.Cycle != 12 || gc.HeapStartWholeMiB != 10 || gc.HeapEndWholeMiB != 11 || gc.AfterGCLiveBytes.LowerInclusive != 6*(1<<20) || gc.AfterGCLiveBytes.UpperExclusive != 7*(1<<20) || gc.HeapGoalWholeMiB != 12 || gc.StackWholeMiB != 0 || gc.GlobalsWholeMiB != 1 || gc.Processors != 24 || gc.Forced || gc.ObservedAt == nil {
		t.Fatal("whole-MiB bytesMarked trace became exact live bytes/RSS or lost actual numeric facts")
	}
	scav := facts.Scavenger.Records[0]
	if scav.BackgroundWholeKiB != 128 || scav.EagerWholeKiB != 4 || scav.ReleasedNowBytes.LowerInclusive != 896*1024 || scav.ReleasedNowBytes.UpperExclusive != 897*1024 || scav.UtilizationPercent != 88 || scav.Forced || scav.ControllerReset || scav.ObservedAt != nil {
		t.Fatal("whole-KiB scavenger observation became exact physical memory or invented a timestamp/marker")
	}
}

func TestResidentGCObservationEmptyRemainsUnknownAndPrivateLinesAreNotRetained(t *testing.T) {
	for _, logs := range []string{"", `{"event":"request_completed","private_fixture":"SECRET"}`, "unrelated private fixture SECRET\n"} {
		facts, err := observeResidentSupervisorGC([]byte(logs))
		if err != nil {
			t.Fatal(err)
		}
		if facts.GC != nil || facts.Scavenger != nil {
			t.Fatal("absent trace records became expected zero observations")
		}
		data, err := json.Marshal(facts)
		if err != nil || strings.Contains(string(data), "SECRET") {
			t.Fatal("unrelated/private log contents escaped sanitized numeric evidence")
		}
	}
}

func TestResidentGCObservationZeroFloorIsNotAnAbsentRecord(t *testing.T) {
	logs := strings.Replace(actualStyleGCTrace, "11->6 MB", "11->0 MB", 1) + "\nscav 0 KiB work (bg), 0 KiB work (eager), 0 KiB now, 99% util\n"
	facts, err := observeResidentSupervisorGC([]byte(logs))
	if err != nil {
		t.Fatal(err)
	}
	if facts.GC == nil || facts.Scavenger == nil || facts.GC.Records[0].AfterGCLiveBytes.LowerInclusive != 0 || facts.GC.Records[0].AfterGCLiveBytes.UpperExclusive != 1<<20 || facts.Scavenger.Records[0].ReleasedNowBytes.UpperExclusive != 1024 {
		t.Fatal("observed quantized zero was confused with no observation or exact zero bytes")
	}
}

func TestResidentGCObservationMarkersAreActuallyObserved(t *testing.T) {
	facts, err := observeResidentSupervisorGC([]byte(actualStyleGCTrace + " (forced)\n" + actualStyleScavTrace + " (forced)\n" + actualStyleScavTrace + " [controller reset]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !facts.GC.Records[0].Forced || !facts.Scavenger.Records[0].Forced || facts.Scavenger.Records[0].ControllerReset || facts.Scavenger.Records[1].Forced || !facts.Scavenger.Records[1].ControllerReset {
		t.Fatal("forced/reset flags were inferred instead of taken from the observed marker")
	}
}

func TestResidentGCObservationRejectsMalformedOrOverflowRecords(t *testing.T) {
	for _, bad := range []string{
		"gc 12 @80.921s 0%: SECRET",
		strings.TrimSuffix(actualStyleGCTrace, " P"),
		strings.Replace(actualStyleGCTrace, "11->6 MB", "11->18446744073709551615 MB", 1),
		strings.Replace(actualStyleGCTrace, "0.084+", "1e9999+", 1),
		actualStyleGCTrace + " private SECRET",
		actualStyleScavTrace + " private SECRET",
		"invalid-timestamp " + actualStyleGCTrace,
		strings.Repeat("x", dockerOutputLimit+1),
	} {
		facts, err := observeResidentSupervisorGC([]byte(bad))
		if err == nil || facts.GC != nil || facts.Scavenger != nil {
			t.Fatal("malformed, private or truncated trace became trusted numeric evidence")
		}
		if strings.Contains(err.Error(), "SECRET") {
			t.Fatal("raw diagnostics entered the returned error")
		}
	}
}

// This hook is supplemental only: unavailable logs/parsing are recorded as
// unknown, never used to change the independently computed memory verdict.
func recordResidentSupervisorGCObservation(t *testing.T, e *acceptanceEnvironment, phase string) {
	t.Helper()
	type report struct {
		Phase                               string                           `json:"phase"`
		Scope                               string                           `json:"scope"`
		RuntimeInputIdentity                string                           `json:"runtime_input_identity"`
		Profile                             string                           `json:"diagnostic_profile"`
		ProfileRequested                    bool                             `json:"profile_requested"`
		ActualProfileVerified               *bool                            `json:"actual_profile_verified"`
		LogReadSucceeded                    bool                             `json:"log_read_succeeded"`
		ParseSucceeded                      *bool                            `json:"parse_succeeded"`
		Observations                        *residentSupervisorGCObservation `json:"observations"`
		Window                              *residentGoHeapWindow            `json:"bounded_window"`
		GCRecordCount, ScavengerRecordCount *int
		Interpretation                      string   `json:"interpretation"`
		Qualification                       string   `json:"qualification"`
		Sources                             []string `json:"sources"`
	}
	facts := report{Phase: phase, Scope: e.selection.Scope, RuntimeInputIdentity: e.selection.RuntimeInputIdentity, Profile: "diagnostic-supervisor-gc-only", ActualProfileVerified: residentSupervisorGCProfileVerified(e), ProfileRequested: os.Getenv("P07_SUPERVISOR_GC_TRACE") == "1", Qualification: "not_established", Interpretation: "Go 1.27.3 after-GC C is bytesMarked printed as whole MiB: [C*MiB,(C+1)*MiB), not exact live bytes or process RSS; scavenger now is heapReleased printed as whole KiB; absent records and unverified runtime profile remain unknown; finite/jittered observations cannot prove absence of all retained copies; no memory acceptance bound or verdict is changed", Sources: []string{"https://go.googlesource.com/go/+/refs/tags/go1.27.3/src/runtime/extern.go#114", "https://go.googlesource.com/go/+/refs/tags/go1.27.3/src/runtime/mgc.go#1394", "https://go.googlesource.com/go/+/refs/tags/go1.27.3/src/runtime/mgc.go#1624", "https://go.googlesource.com/go/+/refs/tags/go1.27.3/src/runtime/mgcscavenge.go#699"}}
	window, _, err := residentGoDiagnosticWindow(e, "backend")
	facts.LogReadSucceeded = err == nil
	if err == nil {
		valid := true
		facts.ParseSucceeded, facts.ActualProfileVerified = &valid, &valid
		facts.Window, facts.Observations = window, &window.Numeric
		if window.Numeric.GC != nil {
			count := len(window.Numeric.GC.Records)
			facts.GCRecordCount = &count
		}
		if window.Numeric.Scavenger != nil {
			count := len(window.Numeric.Scavenger.Records)
			facts.ScavengerRecordCount = &count
		}
	}
	data, err := json.MarshalIndent(facts, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(e.workdir, "supervisor-gc-"+phase+".json"), data, 0600) != nil {
		t.Log("supplemental supervisor GC diagnostic could not be retained; no GC facts or memory verdict established")
	}
}
