package audit

import (
	"strings"
	"testing"
)

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

func TestExtractLinksSkipsCode(t *testing.T) {
	content := "real [a](x.md)\n```\ntemplate [b](fake.md)\n```\nand `inline [c](nope.md)` end\n"
	got := extractLinks(content)
	if len(got) != 1 || got[0].Target != "x.md" {
		t.Fatalf("got %v, want only {1 x.md}", got)
	}
}

// A fence closes only on a run of its own character at least as long as the
// opener with nothing after it, so a longer fence can show a shorter one.
func TestProseLinesFenceClosing(t *testing.T) {
	cases := []struct {
		name, content string
		want          []string
	}{
		{"four backticks hold three", "a\n````\n```\n[x](in.md)\n```\n````\nb", []string{"a", "b"}},
		{"four tildes hold three", "a\n~~~~\n~~~\nin\n~~~\n~~~~\nb", []string{"a", "b"}},
		{"info string never closes", "a\n```\n```bash\nin\n```\nb", []string{"a", "b"}},
		{"longer close is a close", "a\n```\nin\n`````\nb", []string{"a", "b"}},
		{"other character never closes", "a\n~~~\n```\nin\n~~~\nb", []string{"a", "b"}},
		{"mixed run is no fence", "a\n``~\nb", []string{"a", "``~", "b"}},
		{"inline triple-backtick span is no fence", "a\n```x``` y\nb", []string{"a", "```x``` y", "b"}},
	}
	for _, c := range cases {
		var got []string
		for _, l := range proseLines(c.content) {
			got = append(got, l)
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: prose = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestExtractLinksNestedFence(t *testing.T) {
	content := "````md\n```\n[ex](example.md)\n```\n````\nafter [real](real.md)\n"
	got := extractLinks(content)
	if len(got) != 1 || got[0] != (Link{6, "real.md"}) {
		t.Fatalf("got %v, want only {6 real.md}", got)
	}
}

func TestIsLocalMd(t *testing.T) {
	cases := map[string]bool{
		"x.md": true, "../a/b.md": true, "y.md#frag": true, "y.md \"T\"": true,
		"https://e.com/a.md": false, "mailto:x@y.z": false, "#frag": false,
		"/docs/x.md": true, "//host/x.md": false, "img.png": false, "": false,
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
	// A leading / resolves against the repo root, never above it.
	if got := resolveTarget("docs/services/a.md", "/docs/b.md#x"); got != "docs/b.md" {
		t.Errorf("resolveTarget = %q", got)
	}
	if got := resolveTarget("docs/a.md", "/../c.md"); got != "c.md" {
		t.Errorf("resolveTarget = %q", got)
	}
}
