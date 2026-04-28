package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cotiq/sigvane-cli/internal/config"
	"github.com/spf13/cobra"
)

const defaultConfigTemplate = `version: 1

server:
  url: https://api.sigvane.com
  api_key: ${SIGVANE_API_KEY}
  poll_interval: 5s
  shutdown_grace_period: 30s

handlers:
  - inbox: github-repo
    command: ["/bin/sh", "-c", "cat"]
    stdin: full_item
`

func newConfigInitCommand() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			targetPath, err := resolveConfigInitPath(path)
			if err != nil {
				return err
			}
			if err := writeConfigTemplate(targetPath); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "config template written: %s\n", targetPath)
			if path == "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "next: edit the placeholder values, export SIGVANE_API_KEY, then run \"sigvane config check\"\n")
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "next: edit the placeholder values, export SIGVANE_API_KEY, then run \"sigvane config check --path %s\"\n", targetPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "path to write the config file")
	return cmd
}

func resolveConfigInitPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}

	return config.DefaultPath()
}

func writeConfigTemplate(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config file already exists at %q", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat config path %q: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(defaultConfigTemplate), 0o600); err != nil {
		return fmt.Errorf("write config template %q: %w", path, err)
	}

	return nil
}
