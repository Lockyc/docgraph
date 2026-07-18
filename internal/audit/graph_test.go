package audit

import (
	"reflect"
	"testing"
)

func TestContentGraphIslands(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":         "root\n[reached](docs/reached.md)\n[refs](docs/refs.md)\n",
		"docs/reached.md":   "x\n",
		"docs/mentioned.md": "y\n",
		"docs/refs.md":      "see `docs/mentioned.md`\n",
		"docs/island.md":    "nobody points here\n",
	}, []string{"README.md", "docs/reached.md", "docs/mentioned.md", "docs/refs.md", "docs/island.md"})

	tracked, err := trackedMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[f] = true
	}
	roots := map[string]bool{"README.md": true}
	g := BuildContentGraph(dir, tracked, trackedSet, roots, nil)

	got := g.Islands()
	want := []string{"docs/island.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("islands = %v, want %v (edges: %+v)", got, want, g.Edges)
	}
}

// TestContentGraphFrontmatterEdgeNotMention asserts the Task 3 pivot invariant
// directly at the BuildContentGraph level (graph_test.go's peers exercise the
// function directly rather than through Audit): a.md's ONLY reference to
// docs/target.md is a bare (no "./") frontmatter doc->doc edge
// ("to: docs/target.md"); a.md's body has no prose reference at all. Before
// the Task 3 fix, BuildContentGraph scans the doc's whole raw content —
// frontmatter included — with mentionsPath, and the raw YAML text
// "to: docs/target.md" satisfies mentionsPath's word-boundary check (the byte
// before "docs/target.md" is a space, not a path-word byte), so it wrongly
// creates a "mention" content edge and docs/target.md is not an island. This
// is the bare-target case TestFrontmatterEdgeAloneIsIsland (audit_test.go)
// deliberately does NOT cover: that test uses "./docs/leaf.md" specifically so
// the preceding "/" defeats the same word-boundary check, which means it
// passes on the unfixed code too and doesn't discriminate the bug this test
// targets.
func TestContentGraphFrontmatterEdgeNotMention(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":      "root\n[a](docs/a.md)\n",
		"docs/a.md":      "---\ntype: reference\nlinks: [{rel: see-also, to: docs/target.md}]\n---\nno prose reference here\n",
		"docs/target.md": "---\ntype: reference\n---\nnothing points here in prose\n",
	}, []string{"README.md", "docs/a.md", "docs/target.md"})

	tracked, err := trackedMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[f] = true
	}
	roots := map[string]bool{"README.md": true}
	g := BuildContentGraph(dir, tracked, trackedSet, roots, nil)

	islands := g.Islands()
	found := false
	for _, is := range islands {
		if is == "docs/target.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("docs/target.md should be a content island: a bare frontmatter edge is not a content edge, islands=%v edges=%+v", islands, g.Edges)
	}
}

func TestContentGraphMutualClusterNotIsland(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md": "root\n",
		"docs/a.md": "[b](b.md)\n", // a -> b
		"docs/b.md": "[a](a.md)\n", // b -> a; neither is a root, nothing else links them
	}, []string{"README.md", "docs/a.md", "docs/b.md"})

	tracked, err := trackedMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[f] = true
	}
	roots := map[string]bool{"README.md": true}
	g := BuildContentGraph(dir, tracked, trackedSet, roots, nil)

	// Each has an inbound edge from the other, so island rule (zero inbound) flags neither.
	if got := g.Islands(); len(got) != 0 {
		t.Fatalf("mutual cluster should not be islands, got %v", got)
	}
}
