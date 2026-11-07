package acceptance

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func runResidentConditioningCalibration(t *testing.T, e *acceptanceEnvironment, original DockerSnapshot, policy residentConditioningPolicy) (residentConditioning, MeasurementSummary, error) {
	t.Helper()
	// WithTimeout also respects an earlier enclosing suite deadline. This is a
	// fixed observation budget from effective configuration, not a latency SLO.
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(e.settings.Timeouts.OperationSeconds)*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var samples []MeasurementSample
	var epochs []residentConditioningEpoch
	var lastRequestCompleted time.Time
	observe := func() (residentResourcePoint, error) {
		p, state, err := residentActualResources(ctx, e)
		if err != nil {
			return p, err
		}
		if state.PrivateHealth == nil || state.NativeHostPID == nil || state.PrivateHealth.Status != "ready" || !state.PrivateHealth.CUDAReady || *state.NativeHostPID != *original.NativeHostPID || state.PrivateHealth.ProcessGeneration != original.PrivateHealth.ProcessGeneration {
			return p, errors.New("conditioning changed healthy resident identity or lost readiness")
		}
		return p, nil
	}
	result, err := runResidentConditioning(ctx, policy, func(ctx context.Context, block int) (residentConditioningEpoch, error) {
		for sample := 0; sample < residentConditioningBlockRequests; sample++ {
			reply, err := e.client.Transcribe(ctx, e.audio, "jfk.wav", []FormField{{"model", e.settings.Models.Served}, {"language", "en"}, {"temperature", "0"}})
			lastRequestCompleted = time.Now()
			duration := 11 * time.Second
			success := err == nil && reply.Status == http.StatusOK
			samples = append(samples, MeasurementSample{Succeeded: success, PublicDuration: reply.PublicDuration, AudioDuration: &duration})
			if !success {
				return residentConditioningEpoch{}, errors.New("conditioning request failed without retry")
			}
		}
		point, err := observe()
		if err != nil {
			return residentConditioningEpoch{}, err
		}
		epoch := residentConditioningEpoch{Completion: point}
		for i := 0; i < 5; i++ {
			p, err := observe()
			if err != nil {
				return epoch, err
			}
			epoch.Idle = append(epoch.Idle, p)
			if i < 4 {
				select {
				case <-ctx.Done():
					return epoch, ctx.Err()
				case <-time.After(time.Second):
				}
			}
		}
		if policy.SupervisorHeap != nil {
			series, through, err := readResidentSupervisorHeap(ctx, e, *policy.SupervisorHeap)
			if err != nil {
				return epoch, err
			}
			epoch.SupervisorGC, epoch.GCObservedThrough = series, &through
		}
		epochs = append(epochs, epoch)
		writeResidentEvidence(t, e, "resident-conditioning-block-"+strconv.Itoa(block+1)+".json", map[string]any{"policy": policy, "epochs": epochs, "declared_maximum_requests": residentConditioningMaxBlocks * residentConditioningBlockRequests, "deadline": deadline, "qualification": "not_established"})
		return epoch, nil
	})
	// Failed native/FD/file/GPU convergence cannot be repaired by heap observation.
	if err == nil && result.Stable && (policy.SupervisorHeap != nil || policy.APIHeap != nil) {
		resources, resourceErr := freezeConditionedResourceEnvelope(policy, result, nil)
		if resourceErr != nil {
			err = resourceErr
		} else {
			if policy.SupervisorHeap != nil {
				result.PostWorkGC, resources.SupervisorHeap, err = collectResidentPostWorkHeap(t, ctx, e, *policy.SupervisorHeap, resources, lastRequestCompleted, time.Time{}, nil, "supervisor-heap-calibration.json")
			}
			if err == nil && policy.APIHeap != nil {
				result.APIPostWorkGC, _, err = collectResidentPostWorkHeap(t, ctx, e, *policy.APIHeap, resources, lastRequestCompleted, time.Time{}, nil, "api-heap-calibration.json")
			}
		}
		if err != nil {
			result.Stable = false
		}
	}
	summary, summaryErr := SummarizeMeasurements(MeasurementRun{Samples: samples})
	writeResidentEvidence(t, e, "resident-conditioning-calibration.json", map[string]any{"policy": policy, "result": result, "summary": summary, "declared_maximum_blocks": residentConditioningMaxBlocks, "declared_requests_per_block": residentConditioningBlockRequests, "declared_maximum_requests": residentConditioningMaxBlocks * residentConditioningBlockRequests, "deadline": deadline, "configured_budget_seconds": e.settings.Timeouts.OperationSeconds, "calibration_observation_succeeded": err == nil && summaryErr == nil, "qualification": "not_established", "limit": "three nearby stable endpoints cannot prove all future keepalive epochs warm; pinned native may close a socket after 100 HTTP requests; subsequent independent measured workload still fails on a late jump"})
	return result, summary, errors.Join(err, summaryErr)
}
