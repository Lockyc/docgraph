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
