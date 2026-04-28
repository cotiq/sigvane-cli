// Package version exposes build-time version metadata for the Sigvane CLI.
//
// Version, Commit, and Date are populated at link time via -ldflags by GoReleaser
// and default to "dev" / "unknown" for plain `go build` invocations.
package version

// Version is the semantic version of this build (e.g. "0.1.0"), or "dev" for unreleased builds.
var Version = "dev"

// Commit is the short git commit hash this binary was built from, or "unknown".
var Commit = "unknown"

// Date is the build date in RFC3339 format, or "unknown".
var Date = "unknown"
