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
		Short:         "Sigvane CLI: poll inbox feeds and run handlers",
		Long:          "Sigvane is a small Go worker that polls Sigvane inboxes via the inbox feed API and runs a local command per inbox item.",
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New(`sigvane: choose a subcommand; try "sigvane --help"`)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newInboxCommand())
	root.AddCommand(newConfigCommand())
	root.AddCommand(newStateCommand())
	root.AddCommand(newVersionCommand())
	return root
}
