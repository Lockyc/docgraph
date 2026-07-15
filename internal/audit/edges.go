package audit

import (
	"path/filepath"
	"strings"
)

// EdgeKind is a frontmatter edge target's inferred category — inferred from the
// target string, never declared. Only EdgeDoc and EdgeCode are checked for
// existence; EdgeExternal is unverifiable and EdgeCrossRepo is deferred to
// Mycelium (docgraph sees only one repo).
type EdgeKind int

const (
	EdgeDoc       EdgeKind = iota // internal .md doc — existence-checked AND feeds reachability
	EdgeCode                      // internal non-.md path — existence-checked only
	EdgeExternal                  // URL / mailto — not checked
	EdgeCrossRepo                 // owner/repo:path — deferred (never a finding)
)

// ClassifyTarget infers an edge target's kind. Internal targets are treated as
// repo-root-relative (see ResolveEdgeTarget). Order matters: external (has a
// scheme) is ruled out first, then cross-repo (an owner/repo: form with no
// scheme), then .md vs other for the internal cases.
func ClassifyTarget(to string) EdgeKind {
	t := strings.TrimSpace(to)
	if strings.Contains(t, "://") || strings.HasPrefix(t, "mailto:") {
		return EdgeExternal
	}
	if i := strings.Index(t, ":"); i > 0 && strings.Contains(t[:i], "/") {
		return EdgeCrossRepo
	}
	if strings.HasSuffix(cleanTarget(t), ".md") {
		return EdgeDoc
	}
	return EdgeCode
}

// ResolveEdgeTarget returns the repo-root-relative, slash-cleaned path for an
// internal (EdgeDoc/EdgeCode) target. Unlike markdown links, a frontmatter edge
// target is resolved against the REPO ROOT, not the source doc's directory — it
// is a structured reference, not inline prose. Anchors/queries are stripped.
func ResolveEdgeTarget(to string) string {
	return filepath.ToSlash(filepath.Clean(cleanTarget(strings.TrimSpace(to))))
}
