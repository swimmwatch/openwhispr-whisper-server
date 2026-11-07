package acceptance

import (
	"testing"
	"time"
)

type residentWarmupCalibration struct {
	DeclaredRequests, Concurrency int
	Started, Completed            time.Time
	Summary                       MeasurementSummary
	Completion                    *residentResourcePoint
	Idle                          []residentResourcePoint
}

// This declared representative phase runs BEFORE freezing any acceptance bound.
// Its samples are never appended to the later sixty measured steady requests.
func runResidentCalibrationWarmup(t *testing.T, e *acceptanceEnvironment, original DockerSnapshot) residentWarmupCalibration {
	t.Helper()
	evidence := residentWarmupCalibration{DeclaredRequests: 30, Concurrency: 1, Started: time.Now()}
	summary, err := MeasurePublicSteadyState(e.ctx, e.client, TranscriptionWorkload{Audio: e.audio, Filename: "jfk.wav", Fields: []FormField{{"model", e.settings.Models.Served}, {"language", "en"}, {"temperature", "0"}}, AudioDuration: 11 * time.Second, Samples: evidence.DeclaredRequests})
	evidence.Completed = time.Now()
	evidence.Summary = summary
	if err != nil || summary.Errors != 0 || summary.Successes != evidence.DeclaredRequests {
		writeResidentEvidence(t, e, "resident-incomplete-warmup.json", evidence)
		t.Fatal("declared ordinary-input calibration failed; no retry or bound widening applied")
	}
	completion, state, err := residentActualResources(e.ctx, e)
	if err != nil || state.PrivateHealth == nil || state.NativeHostPID == nil || *state.NativeHostPID != *original.NativeHostPID || state.PrivateHealth.ProcessGeneration != original.PrivateHealth.ProcessGeneration {
		t.Fatal("ordinary-input warmup changed resident process/model generation or lost actual resource observation")
	}
	evidence.Completion = &completion
	evidence.Idle = residentCompletedWindow(t, e, 5, time.Second)
	return evidence
}
