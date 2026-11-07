package acceptance

import (
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func checkResidentTerminalHeapPhase(t *testing.T, e *acceptanceEnvironment, envelope *residentEnvelope, completed time.Time, phase string) {
	t.Helper()
	if envelope == nil || envelope.SupervisorHeap == nil && envelope.APIHeap == nil {
		return
	}
	if envelope.SupervisorHeap == nil || envelope.APIHeap == nil {
		t.Fatal(errResidentSupervisorHeapUnknown)
	}
	for _, bound := range []*residentSupervisorHeapBound{envelope.SupervisorHeap, envelope.APIHeap} {
		proof, _, err := collectResidentPostWorkHeap(t, e.ctx, e, bound.Policy, *envelope, completed, bound.FrozenAt, bound, bound.Policy.Scope.Role+"-heap-terminal-"+phase+".json")
		if err != nil {
			t.Fatal(err)
		}
		// Only the freshness cursor advances. Neither measured nor failed work can
		// change the independently frozen marked-live interval.
		last := proof.Comparable[len(proof.Comparable)-1]
		bound.LastCycle, bound.LastUptimeSeconds = last.Cycle, last.UptimeSeconds
	}
}

func advanceResidentTerminalNativeEpisode(t *testing.T, e *acceptanceEnvironment, envelope *residentEnvelope, before, after DockerSnapshot, trace lifecycleTrace, phase string) {
	t.Helper()
	if envelope == nil || envelope.SupervisorHeap == nil && envelope.APIHeap == nil {
		return
	}
	if err := lifecycleTraceFailure(trace, true); err != nil {
		t.Fatal(err)
	}
	if before.PrivateHealth == nil || after.PrivateHealth == nil {
		t.Fatal(errResidentSupervisorHeapUnknown)
	}
	old := *envelope
	next := old
	for _, slot := range []**residentSupervisorHeapBound{&next.SupervisorHeap, &next.APIHeap} {
		bound := *slot
		if bound == nil || bound.Policy.Scope.NativeGeneration != before.PrivateHealth.ProcessGeneration {
			t.Fatal(errResidentSupervisorHeapUnknown)
		}
		policy, err := selectResidentGoHeapPolicy(e.ctx, e, bound.Policy.Scope.Role, after, bound.Policy.OrdinaryStart)
		if err != nil {
			t.Fatal(err)
		}
		*slot, err = rebindResidentNativeEpisode(bound, policy.Scope, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	writeResidentEvidence(t, e, "resident-terminal-native-episode-"+phase+".json", map[string]any{"previous": old, "next": next, "native_retirement_trace": trace, "heap_interval_refitted": false})
	*envelope = next
}

// Pair recreation ends both old Go lifetimes. Witness actual host PID/start
// retirement (not fabricated GC records), then independently calibrate each new
// process BEFORE its next measured terminal operation. Existing physical/native
// bounds and the enclosing caller deadline are never reset or expanded.
func recreateResidentTerminalHeapScope(t *testing.T, e *acceptanceEnvironment, envelope *residentEnvelope, settings config.Settings, phase string) {
	t.Helper()
	if envelope == nil || envelope.SupervisorHeap == nil && envelope.APIHeap == nil {
		writeAndRecreate(t, e, settings)
		waitLifecycleHealthy(t, e)
		return
	}
	before := readyLifecycleSnapshot(t, e)
	finish := watchLifecycleReplacement(t, e, before)
	type lifetime struct {
		State     DockerInspection
		HostStart string
	}
	var old []lifetime
	for _, role := range []string{"api", "backend"} {
		state, err := verifiedResidentGoProcess(e.ctx, e, role)
		if err != nil || state.PID <= 0 {
			t.Fatal(errResidentSupervisorHeapUnknown)
		}
		bound := envelope.APIHeap
		if role == "backend" {
			bound = envelope.SupervisorHeap
		}
		if bound == nil || bound.Policy.Scope.Role != role || state.ID != bound.Policy.Scope.ContainerID || state.Image != bound.Policy.Scope.ImageID || !state.StartedAt.Equal(bound.Policy.Scope.StartedAt) || before.PrivateHealth.ProcessGeneration != bound.Policy.Scope.NativeGeneration || before.PrivateHealth.Model != bound.Policy.Scope.Model {
			t.Fatal("unexplained Go/native replacement before declared terminal recreation")
		}
		start, err := lifecyclePIDStart(state.PID)
		if err != nil {
			t.Fatal("old Go process lifetime is unavailable")
		}
		old = append(old, lifetime{state, start})
	}
	writeAndRecreate(t, e, settings)
	waitLifecycleHealthy(t, e)
	trace := finish()
	lifecycleTracePassed(t, trace, true)
	for _, lifetime := range old {
		lifecycleOldProcessGone(t, e, lifetime.State.PID, lifetime.HostStart)
	}
	current := readyLifecycleSnapshot(t, e)
	completed := time.Now()
	resources := *envelope
	resources.SupervisorHeap, resources.APIHeap = nil, nil
	resources.APIHeapPending = true
	for _, role := range []string{"backend", "api"} {
		policy, err := selectResidentGoHeapPolicy(e.ctx, e, role, current, completed)
		if err != nil {
			t.Fatal(err)
		}
		_, bound, err := collectResidentPostWorkHeap(t, e.ctx, e, *policy, resources, completed, time.Time{}, nil, role+"-heap-terminal-prework-"+phase+".json")
		if err != nil {
			t.Fatal(err)
		}
		if role == "backend" {
			resources.SupervisorHeap = bound
		} else {
			resources.APIHeap = bound
			resources.APIHeapPending = false
		}
	}
	writeResidentEvidence(t, e, "resident-terminal-go-lifetime-"+phase+".json", map[string]any{"retired_go_lifetimes": old, "old_native_retirement_trace": trace, "new_prework_envelope": resources, "prework_calibration_completed": time.Now(), "qualification": "not_established"})
	*envelope = resources
}

func TestResidentNativeEpisodeTransitionPreservesBoundAndRejectsGoReplacement(t *testing.T) {
	policy := heapPolicyFixture()
	bound, err := freezeResidentSupervisorPostWork(policy, heapSeriesFixture(policy, 1, time.Second, 1, 1, 1, 1), policy.OrdinaryStart, policy.OrdinaryStart.Add(8*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"api", "backend"} {
		bound.Policy.Scope.Role = role
		original := *bound
		next := bound.Policy.Scope
		next.NativeGeneration = "recovered-generation"
		rebound, err := rebindResidentNativeEpisode(bound, next, true)
		if err != nil || rebound.MarkedLiveBytes != bound.MarkedLiveBytes || rebound.FrozenAt != bound.FrozenAt || rebound.LastCycle != bound.LastCycle || *bound != original {
			t.Fatal("legitimate native recovery refitted/mutated the surviving Go bound", err)
		}
		for _, fault := range []string{"unproven", "container", "start", "image", "model", "role", "unchanged", "missing"} {
			changed := next
			proven := true
			switch fault {
			case "unproven":
				proven = false
			case "container":
				changed.ContainerID = "replacement"
			case "start":
				changed.StartedAt = changed.StartedAt.Add(time.Second)
			case "image":
				changed.ImageID = "replacement"
			case "model":
				changed.Model = "tiny"
			case "role":
				changed.Role = "other"
			case "unchanged":
				changed.NativeGeneration = bound.Policy.Scope.NativeGeneration
			case "missing":
				changed.NativeGeneration = ""
			}
			if _, err := rebindResidentNativeEpisode(bound, changed, proven); err == nil {
				t.Fatalf("%s transition silently reused a stale bound", fault)
			}
		}
	}
}
