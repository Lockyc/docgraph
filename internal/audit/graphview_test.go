package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildGraphViewJSON exercises the whole assembly path — content graph,
// metadata graph, both island lists — and asserts the JSON payload round-trips
// with every documented key present, even when a list happens to be empty.
func TestBuildGraphViewJSON(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md": "# Root\n[a](docs/a.md)\n",
		"docs/a.md": "---\ntype: reference\nlinks:\n  - rel: see-also\n    to: docs/b.md\n---\n# A\n",
		"docs/b.md": "---\ntype: reference\n---\n# B\n", // reached only via a's content link + metadata edge
	}, []string{"README.md", "docs/a.md", "docs/b.md"})

	v, err := BuildGraphView(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.SchemaVersion != GraphSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", v.SchemaVersion, GraphSchemaVersion)
	}
	if v.RepoRoot != dir {
		t.Fatalf("repoRoot = %q, want %q", v.RepoRoot, dir)
	}
	b, err := v.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("JSON does not round-trip: %v", err)
	}
	for _, k := range []string{"schemaVersion", "nodes", "contentEdges", "metadataEdges", "islands"} {
		if _, ok := round[k]; !ok {
			t.Fatalf("JSON missing key %q", k)
		}
	}
	islands, ok := round["islands"].(map[string]any)
	if !ok {
		t.Fatalf("islands is not an object: %v", round["islands"])
	}
	for _, k := range []string{"content", "metadata"} {
		if _, ok := islands[k]; !ok {
			t.Fatalf("islands missing key %q", k)
		}
	}
}

// TestBuildGraphViewNonNilSlicesWhenEmpty covers the JSON-robustness detail
// directly: a repo with no frontmatter and no links at all must still emit
// empty arrays (not JSON null) for every list-shaped key, since Mycelium keys
// off their presence.
func TestBuildGraphViewNonNilSlicesWhenEmpty(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md": "# Root\nno links, no frontmatter\n",
	}, []string{"README.md"})

	v, err := BuildGraphView(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.ContentEdges == nil || v.MetadataEdges == nil {
		t.Fatalf("edge slices must be non-nil, got contentEdges=%v metadataEdges=%v", v.ContentEdges, v.MetadataEdges)
	}
	if v.Islands.Content == nil || v.Islands.Metadata == nil {
		t.Fatalf("island slices must be non-nil, got %+v", v.Islands)
	}
	b, err := v.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round["contentEdges"] == nil {
		t.Fatalf("contentEdges must not be JSON null")
	}
	if round["metadataEdges"] == nil {
		t.Fatalf("metadataEdges must not be JSON null")
	}
	islands := round["islands"].(map[string]any)
	if islands["content"] == nil {
		t.Fatalf("islands.content must not be JSON null")
	}
	if islands["metadata"] == nil {
		t.Fatalf("islands.metadata must not be JSON null")
	}
}

// TestGraphViewMarkdownShowsHierarchyAndIslands checks the human render covers
// the part-of tree, a cross-reference, and both island sections.
func TestGraphViewMarkdownShowsHierarchyAndIslands(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":      "# Root\n[p](docs/parent.md)\n",
		"docs/parent.md": "---\ntype: index\nlinks:\n  - rel: see-also\n    to: docs/child.md\n---\n# Parent\n[c](docs/child.md)\n",
		"docs/child.md":  "---\ntype: reference\nlinks:\n  - rel: part-of\n    to: docs/parent.md\n---\n# Child\n",
	}, []string{"README.md", "docs/parent.md", "docs/child.md"})

	v, err := BuildGraphView(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := v.Markdown()
	for _, want := range []string{
		"## Metadata hierarchy (part-of)",
		"docs/parent.md",
		"docs/child.md",
		"## Cross-references",
		"see-also",
		"## Content-graph islands",
		"## Metadata-graph islands",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}
