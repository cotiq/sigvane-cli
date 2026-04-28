//go:build unix

package commands

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureHandlerCommand(child *exec.Cmd, gracePeriod time.Duration) {
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.Cancel = func() error {
		if child.Process == nil {
			return nil
		}

		if err := syscall.Kill(-child.Process.Pid, syscall.SIGTERM); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}

		// Returning os.ErrProcessDone tells exec.Cmd that a later exit status 0
		// after cancellation should still be treated as success rather than a
		// context-canceled failure.
		return os.ErrProcessDone
	}
	child.WaitDelay = gracePeriod
}

func isForcedShutdownError(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}
