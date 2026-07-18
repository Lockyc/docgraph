package audit

import (
	"os"
	"path/filepath"
	"sort"
)

// ContentEdge is a directed prose reference in the content graph. Kind is "link"
// (a markdown [x](y.md)) or "mention" (a bare/inline-code path reference).
type ContentEdge struct {
	From string
	To   string
	Kind string
}

// ContentGraph is the findability layer: tracked docs joined by prose references
// (markdown .md links ∪ path-mentions). Its island rule enforces that every
// non-root doc is reachable by something following a reference to it.
type ContentGraph struct {
	Nodes   []string
	Edges   []ContentEdge
	roots   map[string]bool
	inbound map[string]int
}

// readFile reads a repo-relative file as a string.
func readFile(repoRoot, rel string) (string, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	return string(b), err
}

// BuildContentGraph reads each non-ignored tracked doc and records a directed
// edge for every markdown .md link and every path-mention to another tracked
// doc. Self-edges (a doc referencing itself) are ignored. This is the single
// content-edge computation — both the orphans check and the graph view consume
// it, so the graph is never rebuilt a second way.
func BuildContentGraph(repoRoot string, tracked []string, trackedSet, roots map[string]bool, globs []string) ContentGraph {
	g := ContentGraph{roots: roots, inbound: map[string]int{}}
	seen := map[ContentEdge]bool{}
	add := func(from, to, kind string) {
		if from == to {
			return
		}
		e := ContentEdge{From: from, To: to, Kind: kind}
		if seen[e] {
			return
		}
		seen[e] = true
		g.Edges = append(g.Edges, e)
		g.inbound[to]++
	}
	for _, f := range tracked {
		if matchesIgnore(f, globs) {
			continue
		}
		g.Nodes = append(g.Nodes, f)
	}
	for _, f := range g.Nodes {
		content, err := readFile(repoRoot, f)
		if err != nil {
			continue
		}
		for _, link := range extractLinks(content) {
			if !isLocalMd(link.Target) {
				continue
			}
			if to := resolveTarget(f, link.Target); trackedSet[to] {
				add(f, to, "link")
			}
		}
		for _, to := range g.Nodes {
			if to != f && mentionsPath(content, to) {
				add(f, to, "mention")
			}
		}
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		return g.Edges[i].To < g.Edges[j].To
	})
	return g
}

// Islands returns the non-root nodes with zero inbound content edges, sorted.
func (g ContentGraph) Islands() []string {
	var out []string
	for _, n := range g.Nodes {
		if g.roots[n] {
			continue
		}
		if g.inbound[n] == 0 {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
