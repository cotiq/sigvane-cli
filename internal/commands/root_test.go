package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewRootCommand(t *testing.T) {
	t.Run("fails on bare root invocation", func(t *testing.T) {
		_, _, err := executeCommand()
		if err == nil {
			t.Fatal("expected root command to return an error")
		}
		if err.Error() != `sigvane: choose a subcommand; try "sigvane --help"` {
			t.Fatalf("root error = %q, want %q", err.Error(), `sigvane: choose a subcommand; try "sigvane --help"`)
		}
	})

	t.Run("fails on bare config invocation", func(t *testing.T) {
		_, _, err := executeCommand("config")
		if err == nil {
			t.Fatal("expected config command to return an error")
		}
		if err.Error() != `config: choose a subcommand; try "sigvane config --help"` {
			t.Fatalf("config error = %q, want %q", err.Error(), `config: choose a subcommand; try "sigvane config --help"`)
		}
	})

	t.Run("fails on bare inbox invocation", func(t *testing.T) {
		_, _, err := executeCommand("inbox")
		if err == nil {
			t.Fatal("expected inbox command to return an error")
		}
		if err.Error() != `inbox: choose a subcommand; try "sigvane inbox --help"` {
			t.Fatalf("inbox error = %q, want %q", err.Error(), `inbox: choose a subcommand; try "sigvane inbox --help"`)
		}
	})

	t.Run("fails on bare state invocation", func(t *testing.T) {
		_, _, err := executeCommand("state")
		if err == nil {
			t.Fatal("expected state command to return an error")
		}
		if err.Error() != `state: choose a subcommand; try "sigvane state --help"` {
			t.Fatalf("state error = %q, want %q", err.Error(), `state: choose a subcommand; try "sigvane state --help"`)
		}
	})

	t.Run("prints version metadata", func(t *testing.T) {
		stdout, stderr, err := executeCommand("version")
		if err != nil {
			t.Fatalf("version command returned error: %v", err)
		}
		if stdout != "sigvane dev (commit unknown, built unknown)\n" {
			t.Fatalf("version stdout = %q, want %q", stdout, "sigvane dev (commit unknown, built unknown)\n")
		}
		if stderr != "" {
			t.Fatalf("version stderr = %q, want empty output", stderr)
		}
	})

	t.Run("prints help without error", func(t *testing.T) {
		stdout, stderr, err := executeCommand("--help")
		if err != nil {
			t.Fatalf("help command returned error: %v", err)
		}
		if stdout == "" {
			t.Fatal("expected help output on stdout")
		}
		if stderr != "" {
			t.Fatalf("help stderr = %q, want empty output", stderr)
		}
	})

	t.Run("validates config without network calls", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")
		writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: plain-token
handlers:
  - inbox: github-repo
    command: ["/usr/bin/true"]
    stdin: none
`)

		stdout, stderr, err := executeCommand("config", "check", "--path", configPath)
		if err != nil {
			t.Fatalf("config check returned error: %v", err)
		}
		if stdout != "config ok: "+configPath+"\n" {
			t.Fatalf("config check stdout = %q, want %q", stdout, "config ok: "+configPath+"\n")
		}
		if stderr != "" {
			t.Fatalf("config check stderr = %q, want empty output", stderr)
		}
	})

	t.Run("surfaces config validation errors", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")
		writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: plain-token
handlers: []
`)

		stdout, stderr, err := executeCommand("config", "check", "--path", configPath)
		if err == nil {
			t.Fatal("expected config check to fail on invalid config")
		}
		if stdout != "" {
			t.Fatalf("config check stdout = %q, want empty output", stdout)
		}
		if stderr == "" {
			t.Fatal("expected config check to print an error to stderr")
		}
	})

	t.Run("writes starter config to explicit path", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")

		stdout, stderr, err := executeCommand("config", "init", "--path", configPath)
		if err != nil {
			t.Fatalf("config init returned error: %v", err)
		}
		if stderr != "" {
			t.Fatalf("config init stderr = %q, want empty output", stderr)
		}
		if stdout == "" {
			t.Fatal("expected config init output on stdout")
		}
		if !bytes.Contains([]byte(stdout), []byte(`sigvane config check --path `+configPath)) {
			t.Fatalf("config init stdout = %q, want next-step hint for explicit path", stdout)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", configPath, err)
		}
		if string(data) != defaultConfigTemplate {
			t.Fatalf("config template = %q, want %q", string(data), defaultConfigTemplate)
		}
	})

	t.Run("writes starter config to default path", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))

		stdout, stderr, err := executeCommand("config", "init")
		if err != nil {
			t.Fatalf("config init returned error: %v", err)
		}
		if stderr != "" {
			t.Fatalf("config init stderr = %q, want empty output", stderr)
		}

		defaultPath := filepath.Join(tempDir, "xdg", "sigvane", "config.yaml")
		if stdout == "" {
			t.Fatal("expected config init output on stdout")
		}
		if !bytes.Contains([]byte(stdout), []byte("sigvane config check")) {
			t.Fatalf("config init stdout = %q, want config check hint", stdout)
		}
		if bytes.Contains([]byte(stdout), []byte("--path")) {
			t.Fatalf("config init stdout = %q, want no explicit path hint for default location", stdout)
		}
		if _, err := os.Stat(defaultPath); err != nil {
			t.Fatalf("Stat(%q): %v", defaultPath, err)
		}
	})

	t.Run("refuses to overwrite existing config", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")
		writeTestFile(t, configPath, "version: 1\n")

		stdout, stderr, err := executeCommand("config", "init", "--path", configPath)
		if err == nil {
			t.Fatal("expected config init to fail when config already exists")
		}
		if stdout != "" {
			t.Fatalf("config init stdout = %q, want empty output", stdout)
		}
		if stderr == "" {
			t.Fatal("expected config init to print an error to stderr")
		}
	})

	t.Run("keeps example config in sync with init template", func(t *testing.T) {
		examplePath := filepath.Join("..", "..", "examples", "config.example.yaml")

		data, err := os.ReadFile(examplePath)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", examplePath, err)
		}
		if string(data) != defaultConfigTemplate {
			t.Fatalf("example config = %q, want %q", string(data), defaultConfigTemplate)
		}
	})
}

func executeCommand(args ...string) (string, string, error) {
	return executeCommandWithContext(context.Background(), args...)
}

func executeCommandWithContext(ctx context.Context, args ...string) (string, string, error) {
	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}
