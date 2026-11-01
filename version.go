package main

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var rawVersion string

// version is the semver of this build. The tracked root VERSION file is the
// single source of truth; it is embedded at compile time so the binary
// self-reports (`docgraph version`) without a build-time -ldflags step.
var version = strings.TrimSpace(rawVersion)

// modulePath is the Go module path for this version's major: Go's semantic import
// versioning puts a `/vN` suffix on every major ≥ 2. The hook's install hint is
// built from it, and TestModuleMajorMatchesVersion pins go.mod and every tracked
// `go install …@latest` site to it.
func modulePath() string {
	const base = "github.com/lockyc/docgraph"
	major, _, _ := strings.Cut(version, ".")
	if major == "0" || major == "1" {
		return base
	}
	return base + "/v" + major
}
