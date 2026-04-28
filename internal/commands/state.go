package commands

import (
	"fmt"

	"github.com/cotiq/sigvane-cli/internal/state"
	"github.com/spf13/cobra"
)

func newStateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "State-related commands",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("state: choose a subcommand; try %q", "sigvane state --help")
		},
	}
	cmd.AddCommand(newStateResetCommand())
	return cmd
}

func newStateResetCommand() *cobra.Command {
	var statePath string
	cmd := &cobra.Command{
		Use:   "reset <inbox-slug>",
		Short: "Reset the saved cursor for one inbox slug",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			resolvedStatePath, err := state.ResolvePath(statePath)
			if err != nil {
				return err
			}

			currentState, err := state.Load(resolvedStatePath)
			if err != nil {
				return err
			}

			if _, exists := currentState[slug]; exists {
				delete(currentState, slug)
				if err := state.Save(resolvedStatePath, currentState); err != nil {
					return err
				}
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "state reset: %s\n", slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&statePath, "state", "", "path to the state file")
	return cmd
}
