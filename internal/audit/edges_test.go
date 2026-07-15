package audit

import "testing"

func TestClassifyTarget(t *testing.T) {
	cases := map[string]EdgeKind{
		"docs/services/a.md":         EdgeDoc,
		"a.md#section":               EdgeDoc,
		"scripts/x.sh":               EdgeCode,
		"internal/audit/audit.go":    EdgeCode,
		"https://example.com/a":      EdgeExternal,
		"mailto:x@y.z":               EdgeExternal,
		"homelab/docs:services/x.md": EdgeCrossRepo,
		"owner/repo:path/to/thing":   EdgeCrossRepo,
	}
	for in, want := range cases {
		if got := ClassifyTarget(in); got != want {
			t.Errorf("ClassifyTarget(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolveEdgeTarget(t *testing.T) {
	// Root-relative: NOT resolved against any source dir.
	if got := ResolveEdgeTarget("docs/../scripts/x.sh"); got != "scripts/x.sh" {
		t.Errorf("ResolveEdgeTarget = %q", got)
	}
	if got := ResolveEdgeTarget("docs/a.md#frag"); got != "docs/a.md" {
		t.Errorf("ResolveEdgeTarget = %q", got)
	}
}

func TestDetectCyclesFindsAPartOfCycle(t *testing.T) {
	docs := map[string]*Doc{
		"a.md": {Links: []Edge{{Rel: "part-of", To: "b.md"}}},
		"b.md": {Links: []Edge{{Rel: "part-of", To: "a.md"}}},
		"c.md": {Links: []Edge{{Rel: "see-also", To: "a.md"}}}, // see-also is NOT a hierarchy arc
	}
	tracked := map[string]bool{"a.md": true, "b.md": true, "c.md": true}
	cycles := detectCycles(docs, tracked)
	if len(cycles) == 0 {
		t.Fatal("no cycle detected, want the a.md<->b.md part-of cycle")
	}
}

func TestDetectCyclesAcyclicIsClean(t *testing.T) {
	docs := map[string]*Doc{
		"a.md": {Links: []Edge{{Rel: "part-of", To: "b.md"}}},
		"b.md": {Links: []Edge{{Rel: "supersedes", To: "c.md"}}},
		"c.md": {},
	}
	tracked := map[string]bool{"a.md": true, "b.md": true, "c.md": true}
	if got := detectCycles(docs, tracked); len(got) != 0 {
		t.Errorf("cycles = %v, want none", got)
	}
}
