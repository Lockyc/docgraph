package audit

import (
	"path/filepath"
	"sort"
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
	// Clean the target first to strip anchors/queries before checking for cross-repo
	// patterns, so a doc like docs/x.md#a:b isn't misclassified by the colon in its anchor.
	cleaned := cleanTarget(t)
	if i := strings.Index(cleaned, ":"); i > 0 && strings.Contains(cleaned[:i], "/") {
		return EdgeCrossRepo
	}
	if strings.HasSuffix(cleaned, ".md") {
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

// cycleRels are the hierarchy/lineage relations whose arcs must be acyclic. Other
// rels (see-also, depends-on, covers, …) are not part of this graph.
var cycleRels = map[string]bool{"part-of": true, "supersedes": true}

// detectCycles finds cycles in the directed graph formed by `part-of` and
// `supersedes` edges between tracked docs. Each result is one cycle as an ordered
// path of repo-relative doc paths (first node repeated implicitly). A DFS with a
// recursion stack; only tracked-doc targets form arcs (external/code/cross-repo
// and untracked targets are not nodes here).
func detectCycles(docs map[string]*Doc, trackedSet map[string]bool) [][]string {
	arcs := map[string][]string{}
	var nodes []string
	for src, d := range docs {
		nodes = append(nodes, src)
		for _, e := range d.Links {
			if !cycleRels[e.Rel] || ClassifyTarget(e.To) != EdgeDoc {
				continue
			}
			tgt := ResolveEdgeTarget(e.To)
			if trackedSet[tgt] {
				arcs[src] = append(arcs[src], tgt)
			}
		}
	}
	sort.Strings(nodes)
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var cycles [][]string
	var dfs func(n string)
	dfs = func(n string) {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range arcs[n] {
			switch color[m] {
			case gray:
				// Back edge → cycle: the slice of the stack from m to the top.
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == m {
						cyc := append([]string{}, stack[i:]...)
						cycles = append(cycles, cyc)
						break
					}
				}
			case white:
				dfs(m)
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for _, n := range nodes {
		if color[n] == white {
			dfs(n)
		}
	}
	return cycles
}
