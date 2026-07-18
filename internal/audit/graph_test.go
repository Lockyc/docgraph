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
