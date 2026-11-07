package acceptance

import (
	"context"
	"errors"
	"time"
)

// 4096 health requests can generate more than 1 MiB of access logs. Keep a
// finite cap while retaining complete streams for the existing marker checks.
const p08GCLogLimit = 8 << 20
const p08GCLogTimeout = 30 * time.Second

// The output and stderr never escape on failure. The caller owns the deadline;
// the shorter local timeout does not extend it.
func p08GCLogCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	data, err := runKubernetesCommand(ctx, executable, p08GCLogTimeout, p08GCLogLimit-1, nil, args...)
	if errors.Is(err, errKubernetesOutputLimit) {
		return nil, p08GCFailure("window_logs", "logs_too_large")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, errors.Join(p08GCFailure("window_logs", "logs_timeout"), err)
	}
	if err != nil {
		return nil, errors.Join(p08GCFailure("window_logs", "logs_unavailable"), err)
	}
	return data, nil
}

func p08GCLogs(ctx context.Context, target p08ResourceTarget, pod, container string) ([]byte, error) {
	if !target.valid() {
		return nil, p08GCFailure("window_logs", "logs_unavailable")
	}
	args := []string{"--kubeconfig", target.Kubeconfig, "--context", target.Context, "--namespace", target.Namespace, "--request-timeout=25s", "logs", pod, "-c", container, "--timestamps=true"}
	if target.run == nil {
		return p08GCLogCommand(ctx, "kubectl", args...)
	}
	ctx, cancel := context.WithTimeout(ctx, p08GCLogTimeout)
	defer cancel()
	data, err := target.run(ctx, args...)
	if ctx.Err() == context.DeadlineExceeded {
		return nil, p08GCFailure("window_logs", "logs_timeout")
	}
	if err != nil || ctx.Err() != nil {
		return nil, p08GCFailure("window_logs", "logs_unavailable")
	}
	if len(data) >= p08GCLogLimit {
		return nil, p08GCFailure("window_logs", "logs_too_large")
	}
	return data, nil
}
