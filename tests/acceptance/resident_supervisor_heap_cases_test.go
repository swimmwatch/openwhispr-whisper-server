package acceptance

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Last successfully verified bounded window only. Raw logs are never cached.
type residentGoHeapWindow struct {
	Scope                  residentSupervisorHeapScope
	Since, ObservedThrough time.Time
	Numeric                residentSupervisorGCObservation
}

func selectResidentSupervisorHeapPolicy(e *acceptanceEnvironment, before DockerSnapshot, ordinaryStart time.Time) (*residentSupervisorHeapPolicy, error) {
	if os.Getenv("P07_SUPERVISOR_GC_TRACE") != "1" && residentSupervisorGCProfileVerified(e) == nil {
		return nil, nil
	}
	return selectResidentGoHeapPolicy(e.ctx, e, "backend", before, ordinaryStart)
}

func selectResidentGoHeapPolicy(ctx context.Context, e *acceptanceEnvironment, role string, before DockerSnapshot, ordinaryStart time.Time) (*residentSupervisorHeapPolicy, error) {
	if before.PrivateHealth == nil || before.PrivateHealth.Status != "ready" || !before.PrivateHealth.CUDAReady || ordinaryStart.IsZero() {
		return nil, errResidentSupervisorHeapUnknown
	}
	state, err := verifiedResidentGoProcess(ctx, e, role)
	if err != nil {
		return nil, err
	}
	policy := &residentSupervisorHeapPolicy{Scope: residentSupervisorHeapScope{Role: role, ContainerID: state.ID, ImageID: state.Image, StartedAt: state.StartedAt, NativeGeneration: before.PrivateHealth.ProcessGeneration, Model: before.PrivateHealth.Model}, OrdinaryStart: ordinaryStart, ProfileVerified: true}
	if !policy.Scope.valid() {
		return nil, errResidentSupervisorHeapUnknown
	}
	return policy, nil
}

func verifiedResidentGoProcess(ctx context.Context, e *acceptanceEnvironment, role string) (DockerInspection, error) {
	expected := e.selection.APIID
	if role == "backend" {
		expected = e.selection.BackendID
	} else if role != "api" {
		return DockerInspection{}, errResidentSupervisorHeapUnknown
	}
	state, err := e.target.Inspect(ctx, role)
	if err != nil || !state.Running || state.ID == "" || state.StartedAt.IsZero() || expected == "" || state.Image != expected {
		return DockerInspection{}, errResidentSupervisorHeapUnknown
	}
	profile, err := e.target.run(ctx, "effective Go numeric GC observation policy", false, "exec", state.ID, "sh", "-ec", `printf '%s\n%s\n%s\n' "${GODEBUG-}" "${GOGC-}" "${GOMEMLIMIT-}"`)
	if err != nil || string(profile) != "gctrace=1,scavtrace=1\n\n\n" {
		return DockerInspection{}, errResidentSupervisorHeapUnknown
	}
	return state, nil
}
func readResidentSupervisorHeap(ctx context.Context, e *acceptanceEnvironment, policy residentSupervisorHeapPolicy) (*residentGCSeries, time.Time, error) {
	return readResidentSupervisorHeapSince(ctx, e, policy, policy.OrdinaryStart)
}
func readResidentSupervisorHeapSince(ctx context.Context, e *acceptanceEnvironment, policy residentSupervisorHeapPolicy, since time.Time) (*residentGCSeries, time.Time, error) {
	if policy.Scope.Role == "api" {
		e.apiGCWindow = nil
	} else {
		e.supervisorGCWindow = nil
	}
	if !policy.ProfileVerified || !policy.Scope.valid() || since.IsZero() {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	state, err := verifiedResidentGoProcess(ctx, e, policy.Scope.Role)
	if err != nil || state.ID != policy.Scope.ContainerID || state.Image != policy.Scope.ImageID || !state.StartedAt.Equal(policy.Scope.StartedAt) {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	paired, err := e.target.Observe(ctx)
	if err != nil || paired.PrivateHealth == nil || paired.PrivateHealth.Status != "ready" || !paired.PrivateHealth.CUDAReady || paired.PrivateHealth.ProcessGeneration != policy.Scope.NativeGeneration || paired.PrivateHealth.Model != policy.Scope.Model {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	// An explicit cutoff, captured before the read, lets even an empty GC window
	// advance safely without re-reading an ever-growing request-log history.
	through := time.Now()
	if !through.After(since) {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	logs, err := e.target.run(ctx, "bounded post-work Go numeric GC observation", true, "logs", "--timestamps", "--since", since.UTC().Format(time.RFC3339Nano), "--until", through.UTC().Format(time.RFC3339Nano), state.ID)
	if err != nil {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	// Bracket the logs with actual process identity; a replacement during the
	// observation cannot contribute cycles to the previous process's bound.
	after, err := e.target.Inspect(ctx, policy.Scope.Role)
	if err != nil || !after.Running || after.ID != state.ID || after.Image != state.Image || !after.StartedAt.Equal(state.StartedAt) {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	pairedAfter, err := e.target.Observe(ctx)
	if err != nil || pairedAfter.PrivateHealth == nil || pairedAfter.PrivateHealth.Status != "ready" || !pairedAfter.PrivateHealth.CUDAReady || pairedAfter.PrivateHealth.ProcessGeneration != policy.Scope.NativeGeneration || pairedAfter.PrivateHealth.Model != policy.Scope.Model {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	observed, err := observeResidentSupervisorGC(logs)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, errResidentSupervisorHeapUnknown
	}
	if observed.GC != nil {
		for _, record := range observed.GC.Records {
			if record.ObservedAt == nil || record.ObservedAt.Before(since) || record.ObservedAt.After(through) {
				return nil, time.Time{}, errResidentSupervisorHeapUnknown
			}
		}
	}
	window := &residentGoHeapWindow{Scope: policy.Scope, Since: since, ObservedThrough: through, Numeric: observed}
	if policy.Scope.Role == "api" {
		e.apiGCWindow = window
	} else {
		e.supervisorGCWindow = window
	}
	return observed.GC, through, nil
}

func residentGoDiagnosticWindow(e *acceptanceEnvironment, role string) (*residentGoHeapWindow, DockerInspection, error) {
	window := e.apiGCWindow
	if role == "backend" {
		window = e.supervisorGCWindow
		e.supervisorGCWindow = nil
	} else {
		e.apiGCWindow = nil
	}
	if window == nil || window.Scope.Role != role || window.Since.IsZero() || !window.ObservedThrough.After(window.Since) {
		return nil, DockerInspection{}, errResidentSupervisorHeapUnknown
	}
	state, err := verifiedResidentGoProcess(e.ctx, e, role)
	if err != nil || state.ID != window.Scope.ContainerID || state.Image != window.Scope.ImageID || !state.StartedAt.Equal(window.Scope.StartedAt) {
		return nil, DockerInspection{}, errResidentSupervisorHeapUnknown
	}
	paired, err := e.target.Observe(e.ctx)
	if err != nil || paired.PrivateHealth == nil || paired.PrivateHealth.ProcessGeneration != window.Scope.NativeGeneration || paired.PrivateHealth.Model != window.Scope.Model {
		return nil, DockerInspection{}, errResidentSupervisorHeapUnknown
	}
	return window, state, nil
}

// Calibration and measured work consume this same collector. API liveness GETs
// are separately counted; they never send POST or create/extend a budget.
func collectResidentPostWorkHeap(t *testing.T, ctx context.Context, e *acceptanceEnvironment, policy residentSupervisorHeapPolicy, resources residentEnvelope, workCompleted, measuredStart time.Time, bound *residentSupervisorHeapBound, artifact string) (proof *residentPostWorkGCProof, frozen *residentSupervisorHeapBound, observationErr error) {
	t.Helper()
	var samples []residentResourcePoint
	var observed *residentGCSeries
	cursor := workCompleted
	phaseStarted := time.Now()
	deadline, _ := ctx.Deadline()
	var health residentHealthObservation
	defer func() {
		writeResidentEvidence(t, e, artifact, map[string]any{"scope": policy.Scope, "reference_bound": bound, "last_request_completed": workCompleted, "measured_start": measuredStart, "actual_postwork_gc_records": observed, "postwork_proof": proof, "postwork_resources_including_health_cost": samples, "public_health_workload": health, "phase_started_at": phaseStarted, "phase_finished_at": time.Now(), "caller_deadline": deadline, "coverage_established": observationErr == nil, "qualification": "not_established", "limits": "one natural completion barrier, then three comparable natural post-work cycles; exact whole-MiB bin, no padding, no proof of every sub-resolution copy; original resource caps and caller deadline unchanged"})
	}()
	if !policy.ProfileVerified || workCompleted.IsZero() {
		return nil, nil, errResidentSupervisorHeapUnknown
	}
	for {
		if err := ctx.Err(); err != nil {
			return proof, nil, errors.Join(errResidentSupervisorHeapUnknown, err)
		}
		p, _, err := residentActualResources(ctx, e)
		if err != nil {
			return proof, nil, err
		}
		samples = append(samples, p)
		if err = resources.checkResourceSeries(samples, resources.BackendPhysicalPeak != 0); err != nil {
			return proof, nil, err
		}
		window, through, err := readResidentSupervisorHeapSince(ctx, e, policy, cursor)
		if err != nil {
			return proof, nil, err
		}
		observed, err = appendResidentGCWindow(observed, window)
		if err != nil {
			return proof, nil, err
		}
		cursor = residentGCCursor(through, observed)
		if err := ctx.Err(); err != nil {
			return proof, nil, errors.Join(errResidentSupervisorHeapUnknown, err)
		}
		selected, err := residentPostWorkCycles(policy, observed, workCompleted, through)
		proof = &selected
		if err != nil {
			return proof, nil, err
		}
		if bound == nil {
			if len(selected.Comparable) >= 3 {
				frozen, err = freezeResidentSupervisorPostWork(policy, observed, workCompleted, through)
				return proof, frozen, err
			}
		} else {
			_, err = checkResidentSupervisorHeap(bound, observed, measuredStart, workCompleted, through)
			if err == nil {
				return proof, bound, nil
			}
			if !errors.Is(err, errResidentSupervisorHeapUnknown) {
				return proof, nil, err
			}
			// A new actor/cycle reset or unverifiable record cannot be retried into a
			// successful scope. Insufficient natural idle cycles can only await time.
			if selected.Barrier != nil && (selected.Barrier.Cycle <= bound.LastCycle || selected.Barrier.UptimeSeconds < bound.LastUptimeSeconds) {
				return proof, nil, errResidentSupervisorHeapUnknown
			}
		}
		if policy.Scope.Role == "api" && health.Calls < residentHealthObservationMaxCalls {
			if err := runResidentHealthObservation(ctx, e.client, &health, min(residentHealthObservationBatch, residentHealthObservationMaxCalls-health.Calls)); err != nil {
				return proof, nil, err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return proof, nil, errors.Join(errResidentSupervisorHeapUnknown, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}
func awaitResidentSupervisorHeapCoverage(t *testing.T, ctx context.Context, e *acceptanceEnvironment, envelope residentEnvelope, measuredStart, lastCompletion time.Time) error {
	t.Helper()
	if envelope.SupervisorHeap == nil {
		return errResidentSupervisorHeapUnknown
	}
	_, _, err := collectResidentPostWorkHeap(t, ctx, e, envelope.SupervisorHeap.Policy, envelope, lastCompletion, measuredStart, envelope.SupervisorHeap, "supervisor-heap-steady.json")
	if err != nil {
		return err
	}
	if envelope.APIHeap == nil {
		return errResidentSupervisorHeapUnknown
	}
	_, _, err = collectResidentPostWorkHeap(t, ctx, e, envelope.APIHeap.Policy, envelope, lastCompletion, measuredStart, envelope.APIHeap, "api-heap-steady.json")
	return err
}
