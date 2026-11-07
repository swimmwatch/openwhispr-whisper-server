package acceptance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func conditioningPoint(delta uint64) residentResourcePoint {
	at := time.Now()
	return residentResourcePoint{At: at, APIRSSBytes: 100, BackendRSSBytes: 1000 + delta, GPUBytes: 500, APIFDs: 8, BackendProcessRSS: &residentRSSBreakdown{At: at, Supervisor: residentProcessRSS{100, 80, 20}, Native: residentProcessRSS{900 + delta, 700 + delta, 200}}}
}
func conditioningPolicy(t *testing.T) residentConditioningPolicy {
	t.Helper()
	idle := []residentResourcePoint{conditioningPoint(0), conditioningPoint(10), conditioningPoint(0)}
	p, err := newResidentConditioningPolicy(idle, idle, residentEnvelope{Maximum: residentResourceBounds{100, 1010, 500, 8}, IdleSpread: residentResourceBounds{2, 10, 0, 0}}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func conditioningEpoch(p residentResourcePoint) residentConditioningEpoch {
	return residentConditioningEpoch{Completion: p, Idle: []residentResourcePoint{p, p, p}}
}

func TestResidentConditioningRejectsSubnoiseExactLinearLeak(t *testing.T) {
	policy := conditioningPolicy(t)
	calls := 0
	result, err := runResidentConditioning(t.Context(), policy, func(context.Context, int) (residentConditioningEpoch, error) {
		calls++
		return conditioningEpoch(conditioningPoint(uint64(calls))), nil
	})
	if !errors.Is(err, errResidentConditioningBudget) || result.Stable || calls != residentConditioningMaxBlocks {
		t.Fatal("strictly linear per-request retention was absorbed into an apparent stable calibration")
	}
}
func TestResidentConditioningBoundsAndIndependentFutureWork(t *testing.T) {
	policy := conditioningPolicy(t)
	result, err := runResidentConditioning(t.Context(), policy, func(context.Context, int) (residentConditioningEpoch, error) {
		return conditioningEpoch(conditioningPoint(100)), nil
	})
	if err != nil || !result.Stable || len(result.Epochs) != 3 {
		t.Fatal("bounded independently observed plateau rejected")
	}
	envelope, err := freezeConditionedResidentEnvelope(policy, result, []residentResourcePoint{conditioningPoint(0)})
	if err != nil {
		t.Fatal(err)
	}
	if err = envelope.checkSeries([]residentResourcePoint{conditioningPoint(100), conditioningPoint(100), conditioningPoint(100)}); err != nil {
		t.Fatal("stable measured tail rejected")
	}
	if err = envelope.checkSeries([]residentResourcePoint{conditioningPoint(111)}); err == nil {
		t.Fatal("late independent-work jump refitted the frozen bound")
	}
	missing := conditioningPoint(100)
	missing.BackendProcessRSS = nil
	if err = envelope.checkSeries([]residentResourcePoint{missing}); err == nil {
		t.Fatal("unknown process memory treated as zero")
	}
	// Even if aggregate RSS stays bounded, a native increase cannot be hidden by
	// an offsetting supervisor decrease.
	hidden := conditioningPoint(100)
	hidden.BackendProcessRSS.Native.RSSBytes += 11
	if err = envelope.checkSeries([]residentResourcePoint{hidden}); err == nil {
		t.Fatal("aggregate memory concealed a native allocation increase")
	}
}
func TestResidentConditioningExhaustionCapsAndUnknownFacts(t *testing.T) {
	for _, mode := range []string{"growing", "step-growth", "model-copy", "native-model-copy", "API-cap", "GPU-cap", "FD-cap", "unknown-process", "missing-idle"} {
		t.Run(mode, func(t *testing.T) {
			policy := conditioningPolicy(t)
			calls := 0
			result, err := runResidentConditioning(t.Context(), policy, func(_ context.Context, block int) (residentConditioningEpoch, error) {
				calls++
				p := conditioningPoint(uint64((block + 1) * 20))
				switch mode {
				case "step-growth":
					p = conditioningPoint(uint64((block + 1) / 2 * 20))
				case "native-model-copy":
					p.BackendProcessRSS.Native.RSSBytes = 2001
				case "model-copy":
					p.BackendRSSBytes = 2001
				case "API-cap":
					p.APIRSSBytes = 103
				case "GPU-cap":
					p.GPUBytes = 501
				case "FD-cap":
					p.APIFDs = 9
				case "unknown-process":
					p.BackendProcessRSS = nil
				}
				epoch := conditioningEpoch(p)
				if mode == "missing-idle" {
					epoch.Idle = nil
				}
				return epoch, nil
			})
			if err == nil || result.Stable {
				t.Fatal("unbounded, unavailable or over-cap calibration accepted")
			}
			if (mode == "growing" || mode == "step-growth") && (!errors.Is(err, errResidentConditioningBudget) || calls != residentConditioningMaxBlocks) {
				t.Fatal("growing allocation did not exhaust its fixed request budget")
			}
			if mode != "growing" && mode != "step-growth" && calls != 1 {
				t.Fatal("unsafe calibration continued producing requests")
			}
		})
	}
}
func TestResidentConditioningCannotInflateNoiseFromItsOwnIdleSamples(t *testing.T) {
	policy := conditioningPolicy(t)
	result, err := runResidentConditioning(t.Context(), policy, func(_ context.Context, block int) (residentConditioningEpoch, error) {
		epoch := conditioningEpoch(conditioningPoint(uint64(block % 2 * 20)))
		epoch.Idle = []residentResourcePoint{conditioningPoint(0), conditioningPoint(100), conditioningPoint(0)}
		return epoch, nil
	})
	if !errors.Is(err, errResidentConditioningBudget) || result.Stable {
		t.Fatal("conditioning observations enlarged their own independent noise allowance")
	}
}

func TestResidentConditioningDeadlineCannotEstablishCalibration(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	result, err := runResidentConditioning(ctx, conditioningPolicy(t), func(context.Context, int) (residentConditioningEpoch, error) {
		calls++
		return conditioningEpoch(conditioningPoint(0)), nil
	})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errResidentConditioningBudget) || calls != 0 || result.Stable {
		t.Fatal("expired calibration budget admitted work")
	}
	ctx, deadlineCancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Nanosecond))
	defer deadlineCancel()
	result, err = runResidentConditioning(ctx, conditioningPolicy(t), func(context.Context, int) (residentConditioningEpoch, error) {
		calls++
		return conditioningEpoch(conditioningPoint(0)), nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 0 || result.Stable {
		t.Fatal("elapsed configured deadline admitted conditioning requests")
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	result, err = runResidentConditioning(ctx, conditioningPolicy(t), func(context.Context, int) (residentConditioningEpoch, error) {
		cancel()
		return conditioningEpoch(conditioningPoint(0)), nil
	})
	if !errors.Is(err, context.Canceled) || result.Stable {
		t.Fatal("deadline loss during measurement became stable calibration")
	}
}
