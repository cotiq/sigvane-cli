package commands

import (
	"fmt"

	"github.com/cotiq/sigvane-cli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCheckCommand() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate the resolved config without making network calls",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, resolvedPath, err := config.Load(path)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "config ok: %s\n", resolvedPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "path to the config file")
	return cmd
}
