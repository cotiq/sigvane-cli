package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/cotiq/sigvane-cli/internal/config"
	"github.com/cotiq/sigvane-cli/internal/sigvane"
	"github.com/spf13/cobra"
)

const (
	// taskRejectExitCode is the handler exit code that maps to a task reject outcome.
	taskRejectExitCode     = 79
	taskReasonTailByteSize = 8192
)

type taskOutcome string

const (
	taskOutcomeComplete taskOutcome = "complete"
	taskOutcomeFail     taskOutcome = "fail"
	taskOutcomeReject   taskOutcome = "reject"
)

func newTaskRunCommand() *cobra.Command {
	var once bool
	var configPath string
	cmd := &cobra.Command{
		Use:   "run [kind]",
		Short: "Claim tasks and run handlers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var kindFilter string
			if len(args) == 1 {
				kindFilter = args[0]
			}

			return runTaskRun(cmd.Context(), cmd, taskRunOptions{
				configPath: configPath,
				kindFilter: kindFilter,
				once:       once,
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to the config file")
	cmd.Flags().BoolVar(&once, "once", false, "drain available tasks and exit")
	return cmd
}

type taskRunOptions struct {
	configPath string
	kindFilter string
	once       bool
}

type taskHandlerResult struct {
	exitCode   int
	stderrTail string
}

type taskHandlerShutdownTimeoutError struct {
	kind        string
	gracePeriod time.Duration
	err         error
}

func (e *taskHandlerShutdownTimeoutError) Error() string {
	return fmt.Sprintf(
		"shutdown timed out waiting %s for task handler %q to exit: %v",
		e.gracePeriod,
		e.kind,
		e.err,
	)
}

func (e *taskHandlerShutdownTimeoutError) Unwrap() error {
	return e.err
}

func runTaskRun(ctx context.Context, cmd *cobra.Command, opts taskRunOptions) error {
	cfg, _, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	if len(cfg.Tasks) == 0 {
		return errors.New("tasks must contain at least one task for task run")
	}

	selectedTasks, err := selectTaskHandlers(cfg.Tasks, opts.kindFilter)
	if err != nil {
		return err
	}
	tasksByKind := make(map[string]config.TaskConfig, len(selectedTasks))
	kinds := make([]string, 0, len(selectedTasks))
	for _, task := range selectedTasks {
		tasksByKind[task.Kind] = task
		kinds = append(kinds, task.Kind)
	}

	client, err := sigvane.NewClient(cfg.Server.URL, cfg.Server.APIKey, nil)
	if err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		claim, err := claimTaskWithRetry(ctx, cmd, client, kinds)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if !claim.HasTask {
			if opts.once {
				return nil
			}
			if err := sleepContext(ctx, cfg.Server.PollInterval); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
			continue
		}

		taskHandler, exists := tasksByKind[claim.Task.Kind]
		if !exists {
			return fmt.Errorf("claimed task %q has unconfigured kind %q", claim.Task.ID, claim.Task.Kind)
		}

		result, err := runTaskHandler(ctx, cmd, taskHandler, claim.Task, cfg.Server.ShutdownGracePeriod)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			var shutdownTimeoutErr *taskHandlerShutdownTimeoutError
			if errors.As(err, &shutdownTimeoutErr) {
				return err
			}
			return err
		}

		outcome, reason := taskOutcomeForHandlerResult(result)
		if err := reportTaskOutcomeWithRetry(ctx, cmd, client, claim.Task, outcome, reason, cfg.Server.ShutdownGracePeriod); err != nil {
			return err
		}
	}
}

func selectTaskHandlers(tasks []config.TaskConfig, kindFilter string) ([]config.TaskConfig, error) {
	if kindFilter == "" {
		return tasks, nil
	}

	for _, task := range tasks {
		if task.Kind == kindFilter {
			return []config.TaskConfig{task}, nil
		}
	}

	return nil, fmt.Errorf("task kind %q not found in config", kindFilter)
}

func claimTaskWithRetry(
	ctx context.Context,
	cmd *cobra.Command,
	client *sigvane.Client,
	kinds []string,
) (sigvane.ClaimTaskResponse, error) {
	backoff := time.Second

	for {
		claim, err := client.ClaimTask(ctx, kinds)
		if err == nil {
			return claim, nil
		}

		if !isTransientAPIError(err) {
			return sigvane.ClaimTaskResponse{}, err
		}

		_, _ = fmt.Fprintf(
			cmd.ErrOrStderr(),
			"warning: transient task claim error: %v; retrying in %s\n",
			err,
			backoff,
		)

		if sleepErr := sleepContext(ctx, backoff); sleepErr != nil {
			return sigvane.ClaimTaskResponse{}, sleepErr
		}

		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func runTaskHandler(
	ctx context.Context,
	cmd *cobra.Command,
	handler config.TaskConfig,
	task sigvane.Task,
	gracePeriod time.Duration,
) (taskHandlerResult, error) {
	stderrTail := newBoundedTailWriter(taskReasonTailByteSize)
	child := exec.CommandContext(ctx, handler.Command[0], handler.Command[1:]...)
	configureHandlerCommand(child, gracePeriod)
	child.Stdout = cmd.OutOrStdout()
	child.Stderr = io.MultiWriter(cmd.ErrOrStderr(), stderrTail)
	child.Stdin = bytes.NewReader(task.Payload)

	err := child.Run()
	result := taskHandlerResult{
		stderrTail: strings.TrimSpace(stderrTail.String()),
	}
	if err == nil {
		return result, nil
	}

	if ctx.Err() != nil {
		if isForcedShutdownError(err) {
			return taskHandlerResult{}, &taskHandlerShutdownTimeoutError{
				kind:        handler.Kind,
				gracePeriod: gracePeriod,
				err:         err,
			}
		}
		return taskHandlerResult{}, ctx.Err()
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exitCode = exitErr.ExitCode()
		return result, nil
	}

	return taskHandlerResult{}, fmt.Errorf("run task handler for kind %q: %w", handler.Kind, err)
}

func taskOutcomeForHandlerResult(result taskHandlerResult) (taskOutcome, string) {
	switch result.exitCode {
	case 0:
		return taskOutcomeComplete, ""
	case taskRejectExitCode:
		return taskOutcomeReject, taskFailureReason(result)
	default:
		return taskOutcomeFail, taskFailureReason(result)
	}
}

func taskFailureReason(result taskHandlerResult) string {
	if result.stderrTail != "" {
		return result.stderrTail
	}
	return fmt.Sprintf("handler exited with code %d", result.exitCode)
}

func reportTaskOutcomeWithRetry(
	parent context.Context,
	cmd *cobra.Command,
	client *sigvane.Client,
	task sigvane.Task,
	outcome taskOutcome,
	reason string,
	shutdownGracePeriod time.Duration,
) error {
	backoff := time.Second
	ctx := parent
	cancelShutdownContext := func() {}
	usingShutdownContext := false
	defer cancelShutdownContext()

	for {
		err := reportTaskOutcome(ctx, client, task, outcome, reason)
		if err == nil {
			return nil
		}
		if shouldSwitchToShutdownOutcomeContext(parent, err, usingShutdownContext) {
			ctx, cancelShutdownContext = newShutdownOutcomeContext(shutdownGracePeriod)
			usingShutdownContext = true
			continue
		}
		if shouldIgnoreShutdownOutcomeContextError(err, usingShutdownContext) {
			return nil
		}

		var statusErr *sigvane.HTTPStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == 409 {
			_, _ = fmt.Fprintf(
				cmd.ErrOrStderr(),
				"warning: task %q outcome %q was not applied because the lease was stale or already resolved\n",
				task.ID,
				outcome,
			)
			return nil
		}

		if !isTransientAPIError(err) {
			return err
		}

		_, _ = fmt.Fprintf(
			cmd.ErrOrStderr(),
			"warning: transient task outcome error for task %q: %v; retrying in %s\n",
			task.ID,
			err,
			backoff,
		)

		if sleepErr := sleepContext(ctx, backoff); sleepErr != nil {
			if shouldSwitchToShutdownOutcomeContext(parent, sleepErr, usingShutdownContext) {
				ctx, cancelShutdownContext = newShutdownOutcomeContext(shutdownGracePeriod)
				usingShutdownContext = true
				continue
			}
			if shouldIgnoreShutdownOutcomeContextError(sleepErr, usingShutdownContext) {
				return nil
			}
			return sleepErr
		}

		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func shouldSwitchToShutdownOutcomeContext(parent context.Context, err error, usingShutdownContext bool) bool {
	return !usingShutdownContext && parent.Err() != nil && errors.Is(err, context.Canceled)
}

func newShutdownOutcomeContext(gracePeriod time.Duration) (context.Context, context.CancelFunc) {
	if gracePeriod <= 0 {
		gracePeriod = config.DefaultShutdownGracePeriod
	}
	return context.WithTimeout(context.Background(), gracePeriod)
}

func shouldIgnoreShutdownOutcomeContextError(err error, usingShutdownContext bool) bool {
	return usingShutdownContext && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func reportTaskOutcome(ctx context.Context, client *sigvane.Client, task sigvane.Task, outcome taskOutcome, reason string) error {
	switch outcome {
	case taskOutcomeComplete:
		return client.CompleteTask(ctx, task.ID, task.LeaseToken)
	case taskOutcomeFail:
		return client.FailTask(ctx, task.ID, task.LeaseToken, reason)
	case taskOutcomeReject:
		return client.RejectTask(ctx, task.ID, task.LeaseToken, reason)
	default:
		return fmt.Errorf("unsupported task outcome %q", outcome)
	}
}

type boundedTailWriter struct {
	limit int
	buf   []byte
}

func newBoundedTailWriter(limit int) *boundedTailWriter {
	return &boundedTailWriter{
		limit: limit,
	}
}

func (w *boundedTailWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		return len(p), nil
	}

	w.buf = append(w.buf, p...)
	if len(w.buf) > w.limit {
		w.buf = w.buf[len(w.buf)-w.limit:]
	}

	return len(p), nil
}

func (w *boundedTailWriter) String() string {
	return string(w.buf)
}
