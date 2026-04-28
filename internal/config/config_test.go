package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolvePathUsesDocumentedPrecedence(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "override.yaml")
	envPath := filepath.Join(tempDir, "env.yaml")
	cwdPath := filepath.Join(tempDir, "sigvane.yaml")
	xdgPath := filepath.Join(tempDir, "xdg", "sigvane", "config.yaml")

	writeTestFile(t, overridePath, "version: 1\n")
	writeTestFile(t, envPath, "version: 1\n")
	writeTestFile(t, cwdPath, "version: 1\n")
	writeTestFile(t, xdgPath, "version: 1\n")

	t.Setenv("SIGVANE_CONFIG", envPath)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	t.Chdir(tempDir)

	path, err := ResolvePath(overridePath)
	if err != nil {
		t.Fatalf("ResolvePath with override returned error: %v", err)
	}
	if path != overridePath {
		t.Fatalf("override path = %q, want %q", path, overridePath)
	}

	path, err = ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath with env override returned error: %v", err)
	}
	if path != envPath {
		t.Fatalf("env path = %q, want %q", path, envPath)
	}

	t.Setenv("SIGVANE_CONFIG", "")
	path, err = ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath with cwd config returned error: %v", err)
	}
	if path != filepath.Join(".", "sigvane.yaml") {
		t.Fatalf("cwd path = %q, want %q", path, filepath.Join(".", "sigvane.yaml"))
	}

	if err := os.Remove(cwdPath); err != nil {
		t.Fatalf("Remove(%q): %v", cwdPath, err)
	}

	path, err = ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath with xdg config returned error: %v", err)
	}
	if path != xdgPath {
		t.Fatalf("xdg path = %q, want %q", path, xdgPath)
	}
}

func TestResolvePathReportsTriedLocations(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SIGVANE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	t.Chdir(tempDir)

	_, err := ResolvePath("")
	if err == nil {
		t.Fatal("expected ResolvePath to fail when no config file exists")
	}
	if !strings.Contains(err.Error(), "sigvane.yaml") {
		t.Fatalf("error %q does not mention cwd config path", err.Error())
	}
	if !strings.Contains(err.Error(), filepath.Join(tempDir, "xdg", "sigvane", "config.yaml")) {
		t.Fatalf("error %q does not mention xdg config path", err.Error())
	}
}

func TestLoadInterpolatesAPIKeyAndDefaultsPollInterval(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")
	t.Setenv("SIGVANE_API_KEY", "test-api-key")

	writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
`)

	cfg, resolvedPath, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if resolvedPath != configPath {
		t.Fatalf("resolved path = %q, want %q", resolvedPath, configPath)
	}
	if cfg.Server.APIKey != "test-api-key" {
		t.Fatalf("API key = %q, want %q", cfg.Server.APIKey, "test-api-key")
	}
	if cfg.Server.PollInterval != DefaultPollInterval {
		t.Fatalf("poll interval = %s, want %s", cfg.Server.PollInterval, DefaultPollInterval)
	}
	if cfg.Server.ShutdownGracePeriod != DefaultShutdownGracePeriod {
		t.Fatalf("shutdown grace period = %s, want %s", cfg.Server.ShutdownGracePeriod, DefaultShutdownGracePeriod)
	}
	if cfg.Handlers[0].Stdin != StdinModeFullItem {
		t.Fatalf("stdin mode = %q, want %q", cfg.Handlers[0].Stdin, StdinModeFullItem)
	}
}

func TestLoadRejectsUnsupportedEnvironmentReferenceSyntax(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")

	writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: ${my_api_key}
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
`)

	_, _, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load to reject unsupported environment variable syntax")
	}
	if !strings.Contains(err.Error(), "server.api_key uses unsupported environment variable syntax") {
		t.Fatalf("error = %q, want unsupported environment variable syntax message", err.Error())
	}
}

func TestLoadTrimsAPIKeyWhitespace(t *testing.T) {
	t.Run("trims literal api key", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")

		writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: "  plain-token  "
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
`)

		cfg, _, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		if cfg.Server.APIKey != "plain-token" {
			t.Fatalf("API key = %q, want %q", cfg.Server.APIKey, "plain-token")
		}
	})

	t.Run("trims environment api key", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")
		t.Setenv("SIGVANE_API_KEY", "  test-api-key  ")

		writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
`)

		cfg, _, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		if cfg.Server.APIKey != "test-api-key" {
			t.Fatalf("API key = %q, want %q", cfg.Server.APIKey, "test-api-key")
		}
	})

	t.Run("rejects whitespace-only environment api key", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")
		t.Setenv("SIGVANE_API_KEY", "   ")

		writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
`)

		_, _, err := Load(configPath)
		if err == nil {
			t.Fatal("expected Load to reject whitespace-only environment API key")
		}
		if !strings.Contains(err.Error(), `environment variable "SIGVANE_API_KEY" is empty`) {
			t.Fatalf("error = %q, want empty environment variable message", err.Error())
		}
	})

	t.Run("rejects unset environment api key", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "sigvane.yaml")

		writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: ${SIGVANE_API_KEY}
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
`)

		_, _, err := Load(configPath)
		if err == nil {
			t.Fatal("expected Load to reject unset environment API key")
		}
		if !strings.Contains(err.Error(), `references unset environment variable "SIGVANE_API_KEY"`) {
			t.Fatalf("error = %q, want unset environment variable message", err.Error())
		}
	})
}

func TestLoadAcceptsMultipleHandlers(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")

	writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: plain-token
  poll_interval: 10s
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
  - inbox: billing-webhooks
    command: ["./bin/process-billing"]
    stdin: body
`)

	cfg, _, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.Handlers) != 2 {
		t.Fatalf("handler count = %d, want 2", len(cfg.Handlers))
	}
}

func TestLoadPreservesExplicitPollInterval(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")

	writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: plain-token
  poll_interval: 12s
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: body
`)

	cfg, _, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Server.PollInterval != 12*time.Second {
		t.Fatalf("poll interval = %s, want %s", cfg.Server.PollInterval, 12*time.Second)
	}
}

func TestLoadPreservesExplicitShutdownGracePeriod(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")

	writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: plain-token
  shutdown_grace_period: 12s
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: body
`)

	cfg, _, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Server.ShutdownGracePeriod != 12*time.Second {
		t.Fatalf("shutdown grace period = %s, want %s", cfg.Server.ShutdownGracePeriod, 12*time.Second)
	}
}

func TestLoadRejectsNegativeShutdownGracePeriod(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")

	writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: plain-token
  shutdown_grace_period: -1s
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: body
`)

	_, _, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load to reject negative shutdown grace period")
	}
	if !strings.Contains(err.Error(), "server.shutdown_grace_period must be positive") {
		t.Fatalf("error = %q, want shutdown grace period validation message", err.Error())
	}
}

func TestLoadRejectsDuplicateHandlerSlugs(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sigvane.yaml")

	writeTestFile(t, configPath, `
version: 1
server:
  url: https://api.sigvane.com
  api_key: plain-token
handlers:
  - inbox: github-repo
    command: ["./bin/process-github"]
    stdin: full_item
  - inbox: github-repo
    command: ["./bin/process-billing"]
    stdin: body
`)

	_, _, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load to reject duplicate handler slugs")
	}
	if !strings.Contains(err.Error(), `duplicates slug "github-repo"`) {
		t.Fatalf("error = %q, want duplicate slug validation message", err.Error())
	}
}

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(contents, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
