package audit

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A frontmatter split never invents or reorders text: the body is a suffix of
// the content, and a file with no block comes back whole as its body.
func FuzzSplitFrontmatter(f *testing.F) {
	for _, s := range []string{
		"---\ntype: guide\n---\n# T\n",
		"\xef\xbb\xbf---\ntype: guide\n---\nbody",
		"--- \t\ntype: x\n---  \nbody",
		"---\r\ntype: x\r\n---\r\nbody\r\n",
		"intro\n---\nnot frontmatter\n---\n",
		"---\nunclosed\n",
		"---",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, content string) {
		fm, body, has := SplitFrontmatter(content)
		if !strings.HasSuffix(content, body) {
			t.Fatalf("body %q is not a suffix of %q", body, content)
		}
		if !has {
			if body != content || fm != "" {
				t.Fatalf("no block: want body == content and empty fm, got fm=%q body=%q", fm, body)
			}
			return
		}
		first, _, _ := strings.Cut(strings.TrimPrefix(content, "\xef\xbb\xbf"), "\n")
		if strings.TrimRight(first, " \t\r") != "---" {
			t.Fatalf("has=true but the first line %q is not a fence", first)
		}
		if !strings.Contains(content, fm) {
			t.Fatalf("fm %q is not taken from %q", fm, content)
		}
	})
}

// A link target resolves to a repo-relative path: never absolute, and a
// leading-/ target is clamped at the repo root as GitHub does for /../x.md.
func FuzzResolveTarget(f *testing.F) {
	for _, s := range [][2]string{
		{"docs/a.md", "b.md"},
		{"docs/a.md", "../b.md#x"},
		{"docs/a.md", "/docs/x.md"},
		{"docs/a.md", "/../../x.md"},
		{"a.md", "<docs/a b.md>"},
		{"a.md", "docs/a%20b.md"},
		{"a.md", "%2F..%2Fx.md"},
		{"a.md", "/"},
	} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, source, target string) {
		if !filepath.IsLocal(source) {
			return // a source is always a tracked, repo-relative path
		}
		got := resolveTarget(source, target)
		if strings.HasPrefix(got, "/") {
			t.Fatalf("resolveTarget(%q, %q) = %q is absolute", source, target, got)
		}
		if strings.HasPrefix(cleanTarget(target), "/") && (got == ".." || strings.HasPrefix(got, "../")) {
			t.Fatalf("resolveTarget(%q, %q) = %q escapes the repo root from a root-relative link", source, target, got)
		}
	})
}

// A target that names no path (blank, or only an anchor or query) is a missing
// edge, never a path that would resolve to "." and cover the whole repo.
func FuzzClassifyTargetBlank(f *testing.F) {
	for _, s := range []string{"", " ", "\t\n", "#", "  #anchor", "?q=1", "#a:b", "docs/x.md", "owner/repo:path"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, to string) {
		if strings.Contains(to, "://") || strings.HasPrefix(strings.TrimSpace(to), "mailto:") {
			return
		}
		kind := ClassifyTarget(to)
		trimmed := strings.TrimSpace(to)
		if (trimmed == "" || trimmed[0] == '#' || trimmed[0] == '?') && kind != EdgeMissing {
			t.Fatalf("ClassifyTarget(%q) = %v, want EdgeMissing", to, kind)
		}
		if kind == EdgeDoc || kind == EdgeCode {
			if strings.TrimSpace(cleanTarget(trimmed)) == "" {
				t.Fatalf("ClassifyTarget(%q) = %v for a target with no path", to, kind)
			}
		}
	})
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// The parsed added lines of a --unified=0 diff are exactly each hunk's
// +c,d range: one per '+' line, counted from c, whatever the removed lines or
// the "\ No newline" marker look like.
func FuzzParseAddedLines(f *testing.F) {
	for _, s := range []string{
		// TestAddedLinesRemovedDoubleDashLine: a removed "---" line reads "----".
		"diff --git a/CLAUDE.md b/CLAUDE.md\nindex 1..2 100644\n--- a/CLAUDE.md\n+++ b/CLAUDE.md\n@@ -2 +2 @@\n----\n+**Footgun:** x\n",
		// TestAddedLinesNoNewlineMarker.
		"--- a/CLAUDE.md\n+++ b/CLAUDE.md\n@@ -2 +2 @@\n-b\n\\ No newline at end of file\n+Footgun: x\n",
		// An added line that itself looks like a header, and two hunks.
		"--- a/x.md\n+++ b/x.md\n@@ -0,0 +1,2 @@\n++++ b/y\n+@@ -1 +1 @@\n@@ -5,2 +7,0 @@\n-a\n-b\n@@ -9 +9,3 @@ func(a+b)\n-x\n+1\n+2\n+3\n",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, diff string) {
		lines := strings.Split(diff, "\n")
		got := parseAddedLines(lines)

		want := map[int]bool{}
		wellFormed := true
		inHunk := false
		var start, declared, plus int
		closeHunk := func() {
			if !inHunk {
				return
			}
			if plus != declared {
				wellFormed = false
			}
			for i := 0; i < plus; i++ {
				want[start+i] = true
			}
		}
		totalPlus, seenHunk := 0, false
		for i, l := range lines {
			if seenHunk && strings.HasPrefix(l, "+") {
				totalPlus++
			}
			if strings.HasPrefix(l, "@@") {
				seenHunk = true
				closeHunk()
				m := hunkHeader.FindStringSubmatch(l)
				if m == nil {
					wellFormed = false
					inHunk = false
					continue
				}
				inHunk, plus = true, 0
				start, _ = strconv.Atoi(m[1])
				declared = 1
				if m[2] != "" {
					declared, _ = strconv.Atoi(m[2])
				}
				continue
			}
			if !inHunk {
				continue
			}
			switch {
			case strings.HasPrefix(l, "+"):
				plus++
			case strings.HasPrefix(l, "-"), strings.HasPrefix(l, `\`):
			default:
				// --unified=0 has no context lines; a trailing "" from the final
				// newline is the only one git emits.
				if l != "" || i != len(lines)-1 {
					wellFormed = false
				}
			}
		}
		closeHunk()

		if len(got) > totalPlus {
			t.Fatalf("%d added lines reported for %d '+' lines in %q", len(got), totalPlus, diff)
		}
		if !wellFormed {
			return
		}
		if len(got) != len(want) {
			t.Fatalf("parseAddedLines(%q) = %v, want %v", diff, got, want)
		}
		for n := range want {
			if !got[n] {
				t.Fatalf("parseAddedLines(%q) = %v, want %v", diff, got, want)
			}
		}
	})
}
