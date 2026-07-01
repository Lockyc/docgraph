package audit

import "testing"

func TestExtractLinks(t *testing.T) {
	content := "see [a](x.md) and\n[b](../y.md#frag)\n\n[ref]: z/w.md\n"
	got := extractLinks(content)
	want := []Link{{1, "x.md"}, {2, "../y.md#frag"}, {4, "z/w.md"}}
	if len(got) != len(want) {
		t.Fatalf("got %d links, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestIsLocalMd(t *testing.T) {
	cases := map[string]bool{
		"x.md": true, "../a/b.md": true, "y.md#frag": true, "y.md \"T\"": true,
		"https://e.com/a.md": false, "mailto:x@y.z": false, "#frag": false,
		"/abs/site.md": false, "img.png": false, "": false,
	}
	for in, want := range cases {
		if got := isLocalMd(in); got != want {
			t.Errorf("isLocalMd(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolveTarget(t *testing.T) {
	if got := resolveTarget("docs/services/a.md", "../infrastructure/b.md#x"); got != "docs/infrastructure/b.md" {
		t.Errorf("resolveTarget = %q", got)
	}
	if got := resolveTarget("CLAUDE.md", "docs/index.md"); got != "docs/index.md" {
		t.Errorf("resolveTarget = %q", got)
	}
}
