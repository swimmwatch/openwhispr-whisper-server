package acceptance

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const ownedCommandWaitDelay = 200 * time.Millisecond

// runOwnedCommand owns the synchronous process group through cancellation,
// reap and descendant cleanup. Callers supply CommandContext, arguments,
// environment, streams, output bounds and their own safe error policy.
// Streaming Start/Wait observers retain their separate lifecycle ownership.
func runOwnedCommand(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = ownedCommandWaitDelay
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	err := command.Run()
	// A descendant must not survive even if its immediate parent exits first.
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	return err
}
