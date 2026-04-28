//go:build !unix

package commands

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

func configureHandlerCommand(child *exec.Cmd, gracePeriod time.Duration) {
	child.Cancel = func() error {
		if child.Process == nil {
			return nil
		}

		if err := child.Process.Kill(); err != nil {
			if errors.Is(err, os.ErrProcessDone) {
				return os.ErrProcessDone
			}
			return err
		}

		// Non-Unix platforms do not have the same SIGTERM-style graceful shutdown
		// path as the Unix process-group implementation above. Returning
		// os.ErrProcessDone still tells exec.Cmd to treat a clean post-cancel exit
		// as success if the process finishes before Kill lands.
		return os.ErrProcessDone
	}
	child.WaitDelay = gracePeriod
}

func isForcedShutdownError(err error) bool {
	return false
}
