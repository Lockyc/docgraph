package audit

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// GraphSchemaVersion stamps the JSON payload BuildGraphView produces. It is a
// stable seam: Mycelium (a separate repo) ingests this shape, so a breaking
// change to the payload must bump this constant, never silently reshape it.
const GraphSchemaVersion = 1

// GraphNode is one doc-graph node in the served view: its path plus whatever
// frontmatter it carries, if any. HasFrontmatter distinguishes "no frontmatter
// block" from "frontmatter present but every field empty" — the other fields
// alone can't, since an empty Type/Title/etc. is indistinguishable from absent.
type GraphNode struct {
	Path           string `json:"path"`
	Type           string `json:"type,omitempty"`
	Title          string `json:"title,omitempty"`
	Description    string `json:"description,omitempty"`
	Verified       string `json:"verified,omitempty"`
	Review         string `json:"review,omitempty"`
	HasFrontmatter bool   `json:"hasFrontmatter"`
}

// GraphIslands carries each graph's island list — a content-graph node with no
// inbound prose reference, or a metadata-graph node with no doc→doc edge.
type GraphIslands struct {
	Content  []string `json:"content"`
	Metadata []string `json:"metadata"`
}

// GraphView is the served two-graph payload: the content graph (prose
// findability) and the metadata graph (frontmatter structure), over the same
// node set, plus each graph's islands. It reuses BuildContentGraph and
// BuildMetadataGraph verbatim — the same computation the whole-state checks
// gate on — so the served graph and the gated graph can never diverge.
type GraphView struct {
	SchemaVersion int            `json:"schemaVersion"`
	RepoRoot      string         `json:"repoRoot"`
	Nodes         []GraphNode    `json:"nodes"`
	ContentEdges  []ContentEdge  `json:"contentEdges"`
	MetadataEdges []MetadataEdge `json:"metadataEdges"`
	Islands       GraphIslands   `json:"islands"`
}

// BuildGraphView builds both graphs once and assembles the served view. It is
// the read-only counterpart to Audit: it never gates, and mirrors Audit's
// root/ignore/trackedSet resolution so the served graph matches the gated one.
func BuildGraphView(repoRoot string, extraRoots, ignores []string) (GraphView, error) {
	tracked, err := trackedMD(repoRoot)
	if err != nil {
		return GraphView{}, err
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[f] = true
	}
	globs, err := loadIgnores(repoRoot, ignores)
	if err != nil {
		return GraphView{}, err
	}
	roots := map[string]bool{}
	for _, r := range rootCandidates {
		if trackedSet[r] {
			roots[r] = true
		}
	}
	for _, r := range extraRoots {
		r = filepath.ToSlash(filepath.Clean(r))
		if trackedSet[r] {
			roots[r] = true
		}
	}
	docs, _ := parseDocs(repoRoot, tracked, globs)
	cg := BuildContentGraph(repoRoot, tracked, trackedSet, roots, globs)
	mg := BuildMetadataGraph(docs, trackedSet)

	v := GraphView{
		SchemaVersion: GraphSchemaVersion,
		RepoRoot:      repoRoot,
		ContentEdges:  cg.Edges,
		MetadataEdges: mg.Edges,
		Islands:       GraphIslands{Content: cg.Islands(), Metadata: mg.Islands()},
	}
	for _, n := range cg.Nodes {
		gn := GraphNode{Path: n}
		if d := docs[n]; d != nil {
			gn.HasFrontmatter = true
			gn.Type, gn.Title, gn.Description = d.Type, d.Title, d.Description
			gn.Verified, gn.Review = d.Verified, d.Review
		}
		v.Nodes = append(v.Nodes, gn)
	}
	sort.Slice(v.Nodes, func(i, j int) bool { return v.Nodes[i].Path < v.Nodes[j].Path })

	// Guarantee non-nil slices so the JSON always carries the keys as empty
	// arrays rather than null — Mycelium keys off their presence.
	if v.Nodes == nil {
		v.Nodes = []GraphNode{}
	}
	if v.ContentEdges == nil {
		v.ContentEdges = []ContentEdge{}
	}
	if v.MetadataEdges == nil {
		v.MetadataEdges = []MetadataEdge{}
	}
	if v.Islands.Content == nil {
		v.Islands.Content = []string{}
	}
	if v.Islands.Metadata == nil {
		v.Islands.Metadata = []string{}
	}
	return v, nil
}

// JSON renders the versioned payload.
func (v GraphView) JSON() ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// Markdown renders the metadata hierarchy (part-of tree), cross-references,
// and the two island lists for a human or agent exploring the docs.
func (v GraphView) Markdown() string {
	var b strings.Builder
	b.WriteString("# Doc graph\n\n## Metadata hierarchy (part-of)\n\n")
	children := map[string][]string{}
	hasParent := map[string]bool{}
	for _, e := range v.MetadataEdges {
		if e.Rel == "part-of" {
			children[e.To] = append(children[e.To], e.From)
			hasParent[e.From] = true
		}
	}
	var roots []string
	for _, n := range v.Nodes {
		if n.HasFrontmatter && !hasParent[n.Path] {
			roots = append(roots, n.Path)
		}
	}
	sort.Strings(roots)
	var walk func(p string, depth int)
	walk = func(p string, depth int) {
		fmt.Fprintf(&b, "%s- %s\n", strings.Repeat("  ", depth), p)
		kids := children[p]
		sort.Strings(kids)
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	b.WriteString("\n## Cross-references\n\n")
	for _, e := range v.MetadataEdges {
		if e.Rel != "part-of" {
			fmt.Fprintf(&b, "- %s —%s→ %s\n", e.From, e.Rel, e.To)
		}
	}
	fmt.Fprintf(&b, "\n## Content-graph islands (%d)\n\n", len(v.Islands.Content))
	for _, i := range v.Islands.Content {
		fmt.Fprintf(&b, "- %s\n", i)
	}
	fmt.Fprintf(&b, "\n## Metadata-graph islands (%d)\n\n", len(v.Islands.Metadata))
	for _, i := range v.Islands.Metadata {
		fmt.Fprintf(&b, "- %s\n", i)
	}
	return b.String()
}
