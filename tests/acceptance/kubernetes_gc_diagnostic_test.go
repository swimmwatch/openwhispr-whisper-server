package acceptance

// p08GCDiagnosticError never carries command output, identities, or raw errors.
// Keep these values closed: diagnostics can be copied outside private evidence.
type p08GCDiagnosticError struct {
	Stage, Reason string
}

func (e *p08GCDiagnosticError) safeFields() (string, string) {
	if e == nil {
		return "unknown", "unknown"
	}
	switch e.Stage {
	case "window_input", "window_owner_before", "window_profile", "window_anchor_start", "window_health", "window_anchor_end", "window_logs", "window_parse", "window_owner_after":
	default:
		return "unknown", "unknown"
	}
	switch e.Reason {
	case "invalid_input", "identity_unavailable", "profile_unverified", "request_failed", "health_failed", "logs_unavailable", "logs_timeout", "empty_logs", "logs_too_large", "truncated_logs", "invalid_marker_ids", "invalid_role", "invalid_timestamp", "invalid_marker", "ambiguous_marker", "missing_marker", "line_too_large", "invalid_gc", "identity_changed":
	default:
		return "unknown", "unknown"
	}
	return e.Stage, e.Reason
}

func (e *p08GCDiagnosticError) Error() string {
	stage, reason := e.safeFields()
	return "gc diagnostic: " + stage + "/" + reason
}

func (e *p08GCDiagnosticError) Unwrap() error { return errResidentSupervisorHeapUnknown }

func p08GCFailure(stage, reason string) error {
	return &p08GCDiagnosticError{Stage: stage, Reason: reason}
}
