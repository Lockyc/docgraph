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
