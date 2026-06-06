package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task-related commands",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("task: choose a subcommand; try %q", "sigvane task --help")
		},
	}
	cmd.AddCommand(newTaskRunCommand())
	return cmd
}
