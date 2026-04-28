package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/cotiq/sigvane-cli/internal/config"
	"github.com/cotiq/sigvane-cli/internal/sigvane"
	"github.com/cotiq/sigvane-cli/internal/state"
	"github.com/spf13/cobra"
)

func newInboxPollCommand() *cobra.Command {
	var once bool
	var configPath string
	var statePath string
	cmd := &cobra.Command{
		Use:   "poll [inbox-slug]",
		Short: "Poll inbox feeds and run handlers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var slugFilter string
			if len(args) == 1 {
				slugFilter = args[0]
			}

			return runInboxPoll(cmd.Context(), cmd, inboxPollOptions{
				configPath: configPath,
				statePath:  statePath,
				slugFilter: slugFilter,
				once:       once,
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to the config file")
	cmd.Flags().BoolVar(&once, "once", false, "drain the backlog once and exit")
	cmd.Flags().StringVar(&statePath, "state", "", "path to the state file")
	return cmd
}

type inboxPollOptions struct {
	configPath string
	statePath  string
	slugFilter string
	once       bool
}

func runInboxPoll(ctx context.Context, cmd *cobra.Command, opts inboxPollOptions) error {
	cfg, _, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}

	selectedHandlers, err := selectHandlers(cfg.Handlers, opts.slugFilter)
	if err != nil {
		return err
	}

	resolvedStatePath, err := state.ResolvePath(opts.statePath)
	if err != nil {
		return err
	}

	currentState, err := state.Load(resolvedStatePath)
	if err != nil {
		return err
	}

	warnOrphanedStateEntries(cmd, cfg.Handlers, currentState)

	client, err := sigvane.NewClient(cfg.Server.URL, cfg.Server.APIKey, nil)
	if err != nil {
		return err
	}

	resolvedHandlers, err := resolveSelectedHandlers(ctx, client, selectedHandlers, currentState)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	for {
		hadItemsInIteration := false
		madeProgressInIteration := false

		for _, resolvedHandler := range resolvedHandlers {
			if ctx.Err() != nil {
				return nil
			}

			result, pollErr := pollHandlerOnce(
				ctx,
				cmd,
				client,
				resolvedHandler.handler,
				resolvedHandler.inbox.ID,
				resolvedStatePath,
				currentState,
				cfg.Server.ShutdownGracePeriod,
			)
			if pollErr != nil {
				if errors.Is(pollErr, context.Canceled) {
					return nil
				}
				return pollErr
			}
			if result.hadItems {
				hadItemsInIteration = true
			}
			if result.madeProgress {
				madeProgressInIteration = true
			}
		}

		if madeProgressInIteration {
			continue
		}
		if opts.once && !hadItemsInIteration {
			return nil
		}
		if err := sleepContext(ctx, cfg.Server.PollInterval); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

type pollResult struct {
	hadItems     bool
	madeProgress bool
}

type resolvedHandler struct {
	handler config.HandlerConfig
	inbox   sigvane.Inbox
}

type handlerCommandError struct {
	inbox string
	err   error
}

type handlerShutdownTimeoutError struct {
	inbox       string
	gracePeriod time.Duration
	err         error
}

func (e *handlerCommandError) Error() string {
	return fmt.Sprintf("run handler for inbox %q: %v", e.inbox, e.err)
}

func (e *handlerCommandError) Unwrap() error {
	return e.err
}

func (e *handlerShutdownTimeoutError) Error() string {
	return fmt.Sprintf(
		"shutdown timed out waiting %s for handler %q to exit: %v",
		e.gracePeriod,
		e.inbox,
		e.err,
	)
}

func (e *handlerShutdownTimeoutError) Unwrap() error {
	return e.err
}

func selectHandlers(handlers []config.HandlerConfig, slugFilter string) ([]config.HandlerConfig, error) {
	if slugFilter == "" {
		return handlers, nil
	}

	for _, handler := range handlers {
		if handler.Inbox == slugFilter {
			return []config.HandlerConfig{handler}, nil
		}
	}

	return nil, fmt.Errorf("inbox %q not found in config", slugFilter)
}

func warnOrphanedStateEntries(cmd *cobra.Command, handlers []config.HandlerConfig, currentState state.File) {
	configuredSlugs := make(map[string]struct{}, len(handlers))
	for _, handler := range handlers {
		configuredSlugs[handler.Inbox] = struct{}{}
	}

	for slug := range currentState {
		if _, exists := configuredSlugs[slug]; !exists {
			_, _ = fmt.Fprintf(
				cmd.ErrOrStderr(),
				"warning: ignoring orphaned state entry %q; no matching handler is present in config. remove it with: sigvane state reset %s\n",
				slug,
				slug,
			)
		}
	}
}

func resolveSelectedHandlers(
	ctx context.Context,
	client *sigvane.Client,
	handlers []config.HandlerConfig,
	currentState state.File,
) ([]resolvedHandler, error) {
	inboxes, err := client.ListInboxes(ctx)
	if err != nil {
		return nil, err
	}

	inboxesBySlug := make(map[string]sigvane.Inbox, len(inboxes))
	for _, inbox := range inboxes {
		inboxesBySlug[inbox.Slug] = inbox
	}

	resolvedHandlers := make([]resolvedHandler, 0, len(handlers))
	for _, handler := range handlers {
		inbox, exists := inboxesBySlug[handler.Inbox]
		if !exists {
			return nil, fmt.Errorf("inbox %q not found", handler.Inbox)
		}

		if entry, hasState := currentState[handler.Inbox]; hasState && entry.InboxID != "" && entry.InboxID != inbox.ID {
			return nil, fmt.Errorf(
				"handler %q: inbox_id in state (%s) does not match resolved inbox id (%s); refusing to poll. reset this cursor with: sigvane state reset %s",
				handler.Inbox,
				entry.InboxID,
				inbox.ID,
				handler.Inbox,
			)
		}

		resolvedHandlers = append(resolvedHandlers, resolvedHandler{
			handler: handler,
			inbox:   inbox,
		})
	}

	return resolvedHandlers, nil
}

func runHandler(
	ctx context.Context,
	cmd *cobra.Command,
	handler config.HandlerConfig,
	item sigvane.InboxItem,
	gracePeriod time.Duration,
) error {
	stdin, err := stdinBytes(handler.Stdin, item)
	if err != nil {
		return err
	}

	child := exec.CommandContext(ctx, handler.Command[0], handler.Command[1:]...)
	configureHandlerCommand(child, gracePeriod)
	child.Stdout = cmd.OutOrStdout()
	child.Stderr = cmd.ErrOrStderr()
	if stdin != nil {
		child.Stdin = bytes.NewReader(stdin)
	}

	if err := child.Run(); err != nil {
		if ctx.Err() != nil {
			if isForcedShutdownError(err) {
				return &handlerShutdownTimeoutError{
					inbox:       handler.Inbox,
					gracePeriod: gracePeriod,
					err:         err,
				}
			}
			return ctx.Err()
		}
		return &handlerCommandError{
			inbox: handler.Inbox,
			err:   err,
		}
	}

	return nil
}

func stdinBytes(mode config.StdinMode, item sigvane.InboxItem) ([]byte, error) {
	switch mode {
	case config.StdinModeFullItem:
		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("marshal inbox item %q for handler stdin: %w", item.ID, err)
		}
		return data, nil
	case config.StdinModeBody:
		data, err := base64.StdEncoding.DecodeString(item.Body)
		if err != nil {
			return nil, fmt.Errorf("decode body for inbox item %q: %w", item.ID, err)
		}
		return data, nil
	case config.StdinModeNone:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported stdin mode %q", mode)
	}
}

func pollHandlerOnce(
	ctx context.Context,
	cmd *cobra.Command,
	client *sigvane.Client,
	handler config.HandlerConfig,
	inboxID string,
	statePath string,
	currentState state.File,
	shutdownGracePeriod time.Duration,
) (pollResult, error) {
	cursor := currentState[handler.Inbox].LastItemID
	feed, err := listInboxItemsWithRetry(ctx, cmd, client, handler.Inbox, inboxID, cursor)
	if err != nil {
		return pollResult{}, err
	}

	result := pollResult{
		hadItems: len(feed.Items) > 0,
	}

	for _, item := range feed.Items {
		if err := runHandler(ctx, cmd, handler, item, shutdownGracePeriod); err != nil {
			if errors.Is(err, context.Canceled) {
				return result, err
			}
			var shutdownTimeoutErr *handlerShutdownTimeoutError
			if errors.As(err, &shutdownTimeoutErr) {
				return pollResult{}, err
			}
			var commandErr *handlerCommandError
			if errors.As(err, &commandErr) {
				_, _ = fmt.Fprintf(
					cmd.ErrOrStderr(),
					"warning: handler %q failed for inbox item %q: %v\n",
					handler.Inbox,
					item.ID,
					err,
				)
				return result, nil
			}

			return pollResult{}, err
		}

		currentState[handler.Inbox] = state.Entry{
			InboxID:    inboxID,
			LastItemID: item.ID,
		}
		if err := state.Save(statePath, currentState); err != nil {
			return pollResult{}, err
		}
		result.madeProgress = true

		if ctx.Err() != nil {
			return result, nil
		}
	}

	return result, nil
}

func listInboxItemsWithRetry(
	ctx context.Context,
	cmd *cobra.Command,
	client *sigvane.Client,
	handlerInbox string,
	inboxID string,
	cursor string,
) (sigvane.FeedResponse, error) {
	backoff := time.Second

	for {
		feed, err := client.ListInboxItems(ctx, inboxID, cursor)
		if err == nil {
			return feed, nil
		}

		if !isTransientPollError(err) {
			return sigvane.FeedResponse{}, err
		}

		_, _ = fmt.Fprintf(
			cmd.ErrOrStderr(),
			"warning: transient feed error for inbox %q: %v; retrying in %s\n",
			handlerInbox,
			err,
			backoff,
		)

		if sleepErr := sleepContext(ctx, backoff); sleepErr != nil {
			return sigvane.FeedResponse{}, sleepErr
		}

		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func isTransientPollError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var statusErr *sigvane.HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == 429 || statusErr.StatusCode >= 500
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}

var sleepContext = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
