package commands

import (
	"fmt"

	"github.com/cotiq/sigvane-cli/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version, commit, and build date",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "sigvane %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
		},
	}
}
