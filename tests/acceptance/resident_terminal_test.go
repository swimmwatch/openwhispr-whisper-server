package acceptance

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func writeResidentEvidence(t *testing.T, e *acceptanceEnvironment, name string, facts any) {
	t.Helper()
	data, err := json.MarshalIndent(facts, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(e.workdir, name), data, 0600) != nil {
		t.Fatal("cannot retain private measured resident evidence")
	}
}

type residentTerminalPoint struct {
	Phase            string
	Resources        *residentResourcePoint
	HTTPStatus       *int
	TransportError   *bool
	OperationSeconds int
	WorkCompleted    time.Time
	GoHeapScopes     []residentSupervisorHeapScope
}

func terminalOutcomePoint(phase string, operationSeconds int, result *lifecycleCallResult) residentTerminalPoint {
	point := residentTerminalPoint{Phase: phase, OperationSeconds: operationSeconds}
	if result != nil {
		failed := result.err != nil
		point.TransportError = &failed
		if result.reply.Status != 0 {
			status := result.reply.Status
			point.HTTPStatus = &status
		}
	}
	return point
}

type residentTerminalEvidence struct {
	CalibratedEnvelope *residentEnvelope       `json:"calibrated_envelope"`
	TerminalPoints     []residentTerminalPoint `json:"terminal_points"`
	Qualification      string                  `json:"qualification"`
}

func terminalResourceDecision(envelope *residentEnvelope, points []residentTerminalPoint) error {
	if len(points) == 0 {
		return errors.New("terminal resource observations are unavailable")
	}
	var values []residentResourcePoint
	for _, point := range points {
		p := point.Resources
		if p == nil || p.At.IsZero() || p.GPUBytes == 0 || p.BackendProcessRSS == nil || p.BackendProcessRSS.At.IsZero() {
			return errors.New("terminal resource observations are unavailable")
		}
		if p.APIDeletedScratchFDs != 0 || p.APIDeletedScratchBytes != 0 {
			return errors.New("terminal API work retained deleted-open request storage")
		}
		values = append(values, *p)
	}
	if envelope != nil {
		return envelope.checkPassiveSeries(values)
	}
	// Complete observations do not establish memory stability without an actual
	// independently calibrated envelope. The evidence keeps that fact null.
	return nil
}

func runResidentTerminalMeasurements(t *testing.T, e *acceptanceEnvironment, envelope residentEnvelope, longest []byte) []residentTerminalPoint {
	t.Helper()
	return runResidentTerminalSequence(t, e, &envelope, longest)
}

// This separate observational entry point cannot qualify an unknown envelope.
// All strict functional/resource/process checks still run in the shared sequence.
func runResidentTerminalObservations(t *testing.T, e *acceptanceEnvironment, longest []byte) []residentTerminalPoint {
	t.Helper()
	return runResidentTerminalSequence(t, e, nil, longest)
}

// All failure epochs use this same immutable candidate/model. Active faults are
// bracketed by existing process/GPU observers; normal successes are never restarted.
func runResidentTerminalSequence(t *testing.T, e *acceptanceEnvironment, envelope *residentEnvelope, longest []byte) []residentTerminalPoint {
	t.Helper()
	var points []residentTerminalPoint
	functionalFailure := func(phase string, result *lifecycleCallResult, message string) {
		point := terminalOutcomePoint(phase, e.settings.Timeouts.OperationSeconds, result)
		failedPoints := append(append([]residentTerminalPoint(nil), points...), point)
		writeResidentEvidence(t, e, "resident-terminal-"+phase+"-functional-failure.json", residentTerminalEvidence{envelope, failedPoints, "not_established"})
		t.Fatal(message)
	}
	record := func(phase string, result *lifecycleCallResult) {
		completed := time.Now()
		waitLifecycleHealthy(t, e)
		p, _, err := residentActualResources(e.ctx, e)
		point := terminalOutcomePoint(phase, e.settings.Timeouts.OperationSeconds, result)
		point.WorkCompleted = completed
		if envelope != nil {
			for _, bound := range []*residentSupervisorHeapBound{envelope.SupervisorHeap, envelope.APIHeap} {
				if bound != nil {
					point.GoHeapScopes = append(point.GoHeapScopes, bound.Policy.Scope)
				}
			}
		}
		if err == nil {
			point.Resources = &p
		}
		points = append(points, point)
		writeResidentEvidence(t, e, "resident-terminal-"+phase+".json", residentTerminalEvidence{envelope, points, "not_established"})
		if err != nil {
			t.Fatal(err)
		}
		if err = terminalResourceDecision(envelope, points); err != nil {
			t.Fatal(err)
		}
		checkResidentTerminalHeapPhase(t, e, envelope, completed, phase)
	}
	for i := 0; i < 3; i++ {
		reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", e.settings.Models.Served}, {"temperature", "invalid"}})
		if checkPublicCase(reply, err, http.StatusBadRequest, "invalid_request") != nil {
			functionalFailure("invalid-"+strconv.Itoa(i), &lifecycleCallResult{reply, err}, "invalid request did not produce its public validation failure")
		}
		record("invalid-"+strconv.Itoa(i), &lifecycleCallResult{reply, err})
		reply, err = e.client.Transcribe(e.ctx, []byte("not audio"), "corrupt.wav", []FormField{{"model", e.settings.Models.Served}})
		if checkPublicCase(reply, err, http.StatusBadRequest, "unsupported_audio") != nil {
			functionalFailure("decode-failed-"+strconv.Itoa(i), &lifecycleCallResult{reply, err}, "decoder failure did not produce its public failure")
		}
		record("decode-failed-"+strconv.Itoa(i), &lifecycleCallResult{reply, err})
	}
	for _, fault := range []string{"cancelled", "native-crashed"} {
		before := readyLifecycleSnapshot(t, e)
		finish := watchLifecycleReplacement(t, e, before)
		call := startLifecycleCall(t, e, before, longest, "longest.flac")
		busy := waitLifecycleBusy(t, e, call, before.PrivateHealth.ProcessGeneration)
		if fault == "cancelled" {
			call.cancel()
		} else {
			ids, err := e.target.Containers(e.ctx)
			if err != nil {
				t.Fatal(err)
			}
			const killNative = `case "$1" in ''|*[!0-9]*) exit 1;; esac; [ "$1" -gt 1 ]; IFS= read -r name < "/proc/$1/comm"; [ "$name" = whisper-server ]; kill -KILL "$1"`
			if _, err = e.target.run(e.ctx, "resident owned native crash", false, "exec", ids.BackendID, "sh", "-eu", "-c", killNative, "p07-owned-native", strconv.Itoa(*busy.NativeNamespacePID)); err != nil {
				t.Fatal(err)
			}
		}
		result := awaitLifecycleCall(t, call, time.Duration(e.settings.Timeouts.CleanupSeconds+5)*time.Second)
		if result.err == nil && result.reply.Status == http.StatusOK {
			functionalFailure(fault, &result, "active fault fabricated successful transcription")
		}
		waitLifecycleHealthy(t, e)
		trace := finish()
		lifecycleTracePassed(t, trace, true)
		after := readyLifecycleSnapshot(t, e)
		if after.PrivateHealth.ProcessGeneration == before.PrivateHealth.ProcessGeneration {
			t.Fatal("active fault reused uncertain resident generation")
		}
		advanceResidentTerminalNativeEpisode(t, e, envelope, before, after, trace, fault)
		record(fault, &result)
		nextLifecycleValid(t, e)
		record(fault+"-control", nil)
	}
	original := e.settings
	reduced := original
	reduced.Timeouts.OperationSeconds = residentDeadlineFixtureSeconds
	recreateResidentTerminalHeapScope(t, e, envelope, reduced, "reduced-timeout")
	before := readyLifecycleSnapshot(t, e)
	finish := watchLifecycleReplacement(t, e, before)
	call := startLifecycleCall(t, e, before, longest, "longest.flac")
	waitLifecycleBusy(t, e, call, before.PrivateHealth.ProcessGeneration)
	result := awaitLifecycleCall(t, call, time.Duration(e.settings.Timeouts.OperationSeconds+e.settings.Timeouts.CleanupSeconds+5)*time.Second)
	timeoutPhase := "active-timeout-reduced-" + strconv.Itoa(e.settings.Timeouts.OperationSeconds) + "s"
	if !lifecycleDeadlineResult(result) {
		functionalFailure(timeoutPhase, &result, "reduced operation fixture did not deliver canonical timeout")
	}
	waitLifecycleHealthy(t, e)
	trace := finish()
	lifecycleTracePassed(t, trace, true)
	after := readyLifecycleSnapshot(t, e)
	advanceResidentTerminalNativeEpisode(t, e, envelope, before, after, trace, timeoutPhase)
	record(timeoutPhase, &result)
	nextLifecycleValid(t, e)
	record("timeout-control", nil)
	recreateResidentTerminalHeapScope(t, e, envelope, original, "restored-default")
	// No expected zero resource counters are written: the restored target is sampled.
	record("restored-default", nil)
	writeResidentEvidence(t, e, "resident-terminal-resources.json", residentTerminalEvidence{envelope, points, "not_established"})
	return points
}
