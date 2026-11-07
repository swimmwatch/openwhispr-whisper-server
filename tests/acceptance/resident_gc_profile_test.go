package acceptance

import (
	"testing"
)

// The opt-in environment is intent only. Bind the actual scoped runtime flag to
// the existing immutable-container observations before collecting statistics.
func recordSupervisorGCProfile(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	state, err := verifiedResidentGoProcess(e.ctx, e, "backend")
	if err != nil {
		t.Fatal("actual supervisor GC profile or image is unavailable or changes GC policy")
	}
	const expected = "gctrace=1,scavtrace=1"
	snapshot := readyLifecycleSnapshot(t, e)
	writeResidentEvidence(t, e, "runtime-observation-profile.json", map[string]any{
		"profile": "diagnostic-supervisor-gc-only", "effective_godebug": expected,
		"backend_container_id": state.ID, "backend_engine_image_id": state.Image,
		"backend_image_reference": state.Reference, "native_generation": snapshot.PrivateHealth.ProcessGeneration,
		"scope": e.selection.Scope, "api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage,
		"tuning":        "none: no GOGC, GOMEMLIMIT or forced collection",
		"limits":        "numeric logging perturbs timing; Go supervisor only, not native C++ or API heap",
		"qualification": "not_established",
	})
	verified := true
	e.supervisorGCProfileVerified = &verified
}

func residentSupervisorGCProfileVerified(e *acceptanceEnvironment) *bool {
	if e.supervisorGCProfileVerified == nil {
		return nil
	}
	verified := *e.supervisorGCProfileVerified
	return &verified
}
