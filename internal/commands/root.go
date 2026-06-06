// Package commands defines the Cobra command tree for the Sigvane CLI.
package commands

import (
	"errors"

	"github.com/spf13/cobra"
)

// NewRootCommand returns the root `sigvane` command with all subcommands wired in.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "sigvane",
		Short:         "Sigvane CLI: poll inbox feeds or run claimed tasks",
		Long:          "Sigvane is a small Go worker that polls Sigvane inboxes or claims Sigvane tasks and runs local commands.",
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New(`sigvane: choose a subcommand; try "sigvane --help"`)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newInboxCommand())
	root.AddCommand(newTaskCommand())
	root.AddCommand(newConfigCommand())
	root.AddCommand(newStateCommand())
	root.AddCommand(newVersionCommand())
	return root
}
