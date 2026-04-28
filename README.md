# Sigvane CLI

Sigvane CLI is a small Go worker that polls one or more Sigvane inboxes and runs a local command for each inbox item.

Use it to connect hosted Sigvane event delivery to local scripts, services, and automation.

Product information is available at [sigvane.com](https://sigvane.com). CLI setup and product documentation are
available at [docs.sigvane.com](https://docs.sigvane.com).

## Commands

- `sigvane config init` writes a starter config file.
- `sigvane config check` validates the resolved config without making network calls.
- `sigvane inbox poll` polls configured inboxes and runs their handlers.
- `sigvane inbox poll --once` drains currently available items and exits.
- `sigvane inbox poll <inbox-slug>` polls one configured inbox.
- `sigvane state reset <inbox-slug>` resets the saved cursor for one inbox.
- `sigvane version` prints build metadata.

## Install

Requires Go 1.26.2 or newer.

```bash
go install github.com/cotiq/sigvane-cli/cmd/sigvane@latest
```

## Build From Source

From the repository root:

```bash
go build -o ./bin/sigvane ./cmd/sigvane
```

## Documentation

See [CLI getting started](https://docs.sigvane.com/cli/getting-started/) for installation, configuration, and polling
instructions.

## License

Sigvane CLI is licensed under the [Apache License 2.0](LICENSE).
