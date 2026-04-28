// Package config loads and validates the Sigvane CLI config file.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPollInterval is the idle sleep interval when the config omits poll_interval.
const DefaultPollInterval = 5 * time.Second

// DefaultShutdownGracePeriod is the time allowed for a handler to stop after shutdown begins.
const DefaultShutdownGracePeriod = 30 * time.Second

// StdinMode controls what the CLI writes to the handler's stdin.
type StdinMode string

const (
	// StdinModeFullItem writes the full inbox item JSON to stdin.
	StdinModeFullItem StdinMode = "full_item"
	// StdinModeBody writes the decoded inbox item body bytes to stdin.
	StdinModeBody StdinMode = "body"
	// StdinModeNone leaves stdin empty.
	StdinModeNone StdinMode = "none"
)

// Config is the root v1 CLI config document.
type Config struct {
	Version  int             `yaml:"version"`
	Server   ServerConfig    `yaml:"server"`
	Handlers []HandlerConfig `yaml:"handlers"`
}

// ServerConfig holds API connection settings for the Sigvane server.
type ServerConfig struct {
	URL                 string        `yaml:"url"`
	APIKey              string        `yaml:"api_key"`
	PollInterval        time.Duration `yaml:"poll_interval"`
	ShutdownGracePeriod time.Duration `yaml:"shutdown_grace_period"`
}

// HandlerConfig maps one inbox slug to one local command.
type HandlerConfig struct {
	Inbox   string    `yaml:"inbox"`
	Command []string  `yaml:"command"`
	Stdin   StdinMode `yaml:"stdin"`
}

// Load resolves, parses, expands, and validates the config file.
func Load(overridePath string) (Config, string, error) {
	path, err := ResolvePath(overridePath)
	if err != nil {
		return Config{}, "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, "", fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, "", fmt.Errorf("parse config %q: %w", path, err)
	}

	cfg.Server.APIKey, err = resolveAPIKey(cfg.Server.APIKey)
	if err != nil {
		return Config{}, "", fmt.Errorf("invalid config %q: %w", path, err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return Config{}, "", fmt.Errorf("invalid config %q: %w", path, err)
	}

	return cfg, path, nil
}

// ResolvePath returns the config file path using the documented discovery order.
func ResolvePath(overridePath string) (string, error) {
	tried := make([]string, 0, 4)

	if overridePath != "" {
		return overridePath, nil
	}

	if envPath := os.Getenv("SIGVANE_CONFIG"); envPath != "" {
		return envPath, nil
	}

	cwdPath := filepath.Join(".", "sigvane.yaml")
	tried = append(tried, cwdPath)
	if fileExists(cwdPath) {
		return cwdPath, nil
	}

	xdgPath, err := defaultConfigPath()
	if err != nil {
		return "", err
	}
	tried = append(tried, xdgPath)
	if fileExists(xdgPath) {
		return xdgPath, nil
	}

	return "", fmt.Errorf("config file not found; tried %s", strings.Join(tried, ", "))
}

// DefaultPath returns the default XDG-style config file location.
func DefaultPath() (string, error) {
	return defaultConfigPath()
}

func (c *Config) applyDefaults() {
	if c.Server.PollInterval == 0 {
		c.Server.PollInterval = DefaultPollInterval
	}
	if c.Server.ShutdownGracePeriod == 0 {
		c.Server.ShutdownGracePeriod = DefaultShutdownGracePeriod
	}
}

func (c *Config) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("version must be 1")
	}

	parsedBaseURL, err := url.Parse(c.Server.URL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return fmt.Errorf("server.url must be an absolute URL")
	}

	if c.Server.PollInterval < 0 {
		return fmt.Errorf("server.poll_interval must be positive")
	}
	if c.Server.ShutdownGracePeriod < 0 {
		return fmt.Errorf("server.shutdown_grace_period must be positive")
	}

	if len(c.Handlers) == 0 {
		return errors.New("handlers must contain at least one handler")
	}

	seenInboxes := make(map[string]struct{}, len(c.Handlers))
	for index, handler := range c.Handlers {
		if handler.Inbox == "" {
			return fmt.Errorf("handlers[%d].inbox is required", index)
		}
		if _, exists := seenInboxes[handler.Inbox]; exists {
			return fmt.Errorf("handlers[%d].inbox duplicates slug %q", index, handler.Inbox)
		}
		seenInboxes[handler.Inbox] = struct{}{}

		if len(handler.Command) == 0 {
			return fmt.Errorf("handlers[%d].command must contain at least one argv entry", index)
		}
		if handler.Command[0] == "" {
			return fmt.Errorf("handlers[%d].command[0] must not be empty", index)
		}

		switch handler.Stdin {
		case StdinModeFullItem, StdinModeBody, StdinModeNone:
		default:
			return fmt.Errorf(
				"handlers[%d].stdin must be one of %q, %q, or %q",
				index,
				StdinModeFullItem,
				StdinModeBody,
				StdinModeNone,
			)
		}
	}

	return nil
}

func resolveAPIKey(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("server.api_key is required")
	}

	matches := envReferencePattern.FindStringSubmatch(raw)
	if len(matches) == 0 {
		if strings.HasPrefix(raw, "${") && strings.HasSuffix(raw, "}") {
			return "", errors.New("server.api_key uses unsupported environment variable syntax; expected ${UPPERCASE_NAME}")
		}
		return raw, nil
	}

	value, ok := os.LookupEnv(matches[1])
	if !ok {
		return "", fmt.Errorf("server.api_key references unset environment variable %q", matches[1])
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("server.api_key environment variable %q is empty", matches[1])
	}

	return value, nil
}

func defaultConfigPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for config path: %w", err)
		}
		base = filepath.Join(home, ".config")
	}

	return filepath.Join(base, "sigvane", "config.yaml"), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var envReferencePattern = regexp.MustCompile(`^\$\{([A-Z0-9_]+)\}$`)
