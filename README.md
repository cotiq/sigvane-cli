# Sigvane CLI

[![GitHub Release](https://img.shields.io/github/v/release/cotiq/sigvane-cli)](https://github.com/cotiq/sigvane-cli/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/cotiq/sigvane-cli)](https://goreportcard.com/report/github.com/cotiq/sigvane-cli)
[![License](https://img.shields.io/github/license/cotiq/sigvane-cli)](LICENSE)

Sigvane is a hosted GitHub webhook inbox. Sigvane CLI polls your inbox and runs a command on your machine for each
event.

Learn more about Sigvane at [sigvane.com](https://sigvane.com). For CLI setup and full documentation, see
[docs.sigvane.com](https://docs.sigvane.com).

## How it works

GitHub -> Sigvane inbox -> Sigvane CLI -> your command

## Why

Use Sigvane CLI when you want to process webhook events without exposing your machine to the public internet.

- No public endpoints to deploy or maintain
- Works locally or behind a firewall
- Run a command, script, or automation workflow for each incoming event

## Install

Choose one of the following installation options.

### Download a release package

If you are not familiar with Go, use this option. No Go installation is required.

1. Download the latest release package for your platform from the
   [GitHub Releases page](https://github.com/cotiq/sigvane-cli/releases/latest).
2. Extract the archive.
3. Move `sigvane` to a directory on your `PATH`.
4. Run `sigvane version` to confirm the installation.

For detailed platform-specific installation steps, see
[CLI getting started](https://docs.sigvane.com/cli/getting-started/).

### Install with Go

Requires Go 1.26.2 or newer.

```bash
go install github.com/cotiq/sigvane-cli/cmd/sigvane@latest
```

## Quick start

This example assumes you already have a Sigvane inbox and API key. It uses a simple handler that prints each event
JSON to your terminal.

1. Generate a starter config:

```bash
sigvane config init
```

2. Edit `~/.config/sigvane/config.yaml` so it looks like this:

```yaml
version: 1

server:
  url: https://api.sigvane.com
  api_key: ${SIGVANE_API_KEY}
  poll_interval: 5s
  shutdown_grace_period: 30s

handlers:
  - inbox: github-repo
    command: ["/bin/sh", "-c", "cat"]
    stdin: full_item
```

Replace `github-repo` with your inbox slug.

Set your API key as an environment variable:

  ```bash
  export SIGVANE_API_KEY='replace-me'
  ```

Or configure it directly in the config file.

3. Check the config:

```bash
sigvane config check
```

4. Poll once:

```bash
sigvane inbox poll --once
```

If the inbox has items, the CLI prints the event JSON in your terminal. If nothing prints, trigger a new event and run
the command again.

## Commands

- `sigvane config init` writes a starter config file.
- `sigvane config check` validates the resolved config without making network calls.
- `sigvane inbox poll` polls configured inboxes and runs their handlers.
- `sigvane inbox poll --once` drains currently available items and exits.
- `sigvane inbox poll <inbox-slug>` polls one configured inbox.
- `sigvane state reset <inbox-slug>` resets the saved cursor for one inbox.
- `sigvane version` prints build metadata.

## Build from source

Requires Go 1.26.2 or newer.

From the repository root:

```bash
go build -o ./bin/sigvane ./cmd/sigvane
```

## License

Sigvane CLI is licensed under the [Apache License 2.0](LICENSE).
