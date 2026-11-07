package acceptance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

var errKubernetesOutputLimit = errors.New("Kubernetes observation exceeds output bound")

// Preserve cancellation and process error identity without printing command
// arguments, authentication diagnostics, or private Kubernetes observations.
type kubernetesCommandError struct{ cause error }

func (e *kubernetesCommandError) Error() string { return "bounded Kubernetes command failed" }
func (e *kubernetesCommandError) Unwrap() error { return e.cause }

type kubernetesCommandOutput struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	overflow bool
}

func (b *kubernetesCommandOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		b.overflow = true
		b.cancel()
		return 0, errKubernetesOutputLimit
	}
	return b.buffer.Write(p)
}

// Callers still own explicit target arguments and action authorization. The
// consumed variants differ only in timeout, stdout bound and bounded stdin.
func runKubernetesCommand(parent context.Context, executable string, timeout time.Duration, limit int, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	output := &kubernetesCommandOutput{limit: limit, cancel: cancel}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout, cmd.Stderr = output, io.Discard
	err := runOwnedCommand(cmd)
	if output.overflow {
		return nil, errKubernetesOutputLimit
	}
	if err != nil || ctx.Err() != nil {
		return nil, &kubernetesCommandError{errors.Join(err, ctx.Err())}
	}
	return output.buffer.Bytes(), nil
}
