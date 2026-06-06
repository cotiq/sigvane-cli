package commands

import "time"

const (
	initialRetryBackoff = time.Second
	maxRetryBackoff     = 30 * time.Second
)

type retryAction int

const (
	retryStop retryAction = iota
	retryNow
	retryAfterBackoff
)

type retryWithBackoffOptions struct {
	Operation func() error
	Classify  func(error) (retryAction, error)
	Warn      func(error, time.Duration)
	Sleep     func(time.Duration) error
}

func retryWithBackoff(opts retryWithBackoffOptions) error {
	backoff := initialRetryBackoff

	for {
		err := opts.Operation()
		if err == nil {
			return nil
		}

		action, returnErr := opts.Classify(err)
		switch action {
		case retryStop:
			return returnErr
		case retryNow:
			continue
		case retryAfterBackoff:
			if opts.Warn != nil {
				opts.Warn(err, backoff)
			}
		}

		if sleepErr := opts.Sleep(backoff); sleepErr != nil {
			action, returnErr := opts.Classify(sleepErr)
			if action == retryNow {
				continue
			}
			if action == retryStop {
				return returnErr
			}
			return sleepErr
		}

		backoff *= 2
		if backoff > maxRetryBackoff {
			backoff = maxRetryBackoff
		}
	}
}

func classifyTransientAPIError(err error) (retryAction, error) {
	if !isTransientAPIError(err) {
		return retryStop, err
	}

	return retryAfterBackoff, nil
}
