package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Config-related commands",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("config: choose a subcommand; try %q", "sigvane config --help")
		},
	}
	cmd.AddCommand(newConfigCheckCommand())
	cmd.AddCommand(newConfigInitCommand())
	return cmd
}
