package audit

import (
	"os"
	"path/filepath"
	"sort"
)

// ContentEdge is a directed prose reference in the content graph. Kind is "link"
// (a markdown [x](y.md)) or "mention" (a bare/inline-code path reference).
type ContentEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
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
		// Content-graph edges are prose references only — strip any leading
		// frontmatter block before scanning so a frontmatter doc->doc edge
		// (metadata graph, Task 4) never leaks in as a link or mention here.
		_, body, _ := SplitFrontmatter(content)
		for _, link := range extractLinks(body) {
			if !isLocalMd(link.Target) {
				continue
			}
			if to := resolveTarget(f, link.Target); trackedSet[to] {
				add(f, to, "link")
			}
		}
		for _, to := range g.Nodes {
			if to != f && mentionsPath(body, to) {
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

// isMetadataEdge reports whether e joins two docs structurally. It is an edge to
// a tracked doc (EdgeDoc) whose rel is neither `covers` (code ownership) nor
// `source` (external provenance) — the two rels that by nature point outside the
// doc→doc structure. This operationalizes the spec's part-of/supersedes/
// see-also/depends-on set while also admitting runbook-for and custom doc→doc
// rels, and excluding covers/source even when they happen to target a .md.
func isMetadataEdge(e Edge) bool {
	if ClassifyTarget(e.To) != EdgeDoc {
		return false
	}
	return e.Rel != "covers" && e.Rel != "source"
}

// MetadataEdge is a frontmatter doc→doc relationship in the metadata graph.
type MetadataEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"`
	Note string `json:"note,omitempty"`
}

// MetadataGraph is the structural-placement layer: docs carrying frontmatter,
// joined by doc→doc typed edges. Its island rule flags a frontmatter doc that
// declares no place in the structure (zero doc→doc edges in OR out).
type MetadataGraph struct {
	Nodes  []string
	Edges  []MetadataEdge
	degree map[string]int
}

// BuildMetadataGraph builds the doc→doc edge graph over docs that carry
// frontmatter. Degree counts both directions; self-edges are ignored.
func BuildMetadataGraph(docs map[string]*Doc, trackedSet map[string]bool) MetadataGraph {
	g := MetadataGraph{degree: map[string]int{}}
	for src := range docs {
		g.Nodes = append(g.Nodes, src)
	}
	sort.Strings(g.Nodes)
	seen := map[MetadataEdge]bool{}
	for _, src := range g.Nodes {
		for _, e := range docs[src].Links {
			if !isMetadataEdge(e) {
				continue
			}
			to := ResolveEdgeTarget(e.To)
			if to == src || !trackedSet[to] {
				continue
			}
			me := MetadataEdge{From: src, To: to, Rel: e.Rel, Note: e.Note}
			if seen[me] {
				continue
			}
			seen[me] = true
			g.Edges = append(g.Edges, me)
			g.degree[src]++
			g.degree[to]++
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

// Islands returns frontmatter nodes with zero doc→doc edges (either direction).
func (g MetadataGraph) Islands() []string {
	var out []string
	for _, n := range g.Nodes {
		if g.degree[n] == 0 {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
