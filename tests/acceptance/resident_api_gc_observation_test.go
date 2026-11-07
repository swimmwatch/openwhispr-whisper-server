package acceptance

import (
	"os"
	"testing"
	"time"
)

// Supplemental diagnostics consume the last scope-verified bounded window once.
// They never reread accumulated request logs or fit a calibration bound.
func recordResidentAPIGCObservation(t *testing.T, e *acceptanceEnvironment, phase string) {
	t.Helper()
	if os.Getenv("P07_API_GC_TRACE") != "1" {
		return
	}
	window, state, err := residentGoDiagnosticWindow(e, "api")
	var observed residentSupervisorGCObservation
	if err == nil {
		observed = window.Numeric
	}
	writeResidentEvidence(t, e, "api-gc-"+phase+".json", map[string]any{
		"observed_at": time.Now().UTC(), "phase": phase,
		"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity,
		"api_container_id": state.ID, "api_started_at": state.StartedAt,
		"api_image_reference": state.Reference, "api_engine_image_id": state.Image,
		"actual_profile_verified": err == nil, "bounded_window": window,
		"gc": observed.GC, "scavenger": observed.Scavenger,
		"qualification": "not_established",
		"limits":        "supplemental numeric window only; missing, stale or consumed windows remain unknown without full-history fallback; independent v3 calibration/measurement artifacts own the heap verdict; whole-MiB marked-live intervals are not exact retained bytes or RSS and cannot prove every sub-resolution copy",
	})
}
