package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Inbox-related commands",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("inbox: choose a subcommand; try %q", "sigvane inbox --help")
		},
	}
	cmd.AddCommand(newInboxPollCommand())
	return cmd
}
