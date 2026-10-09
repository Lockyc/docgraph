package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// git quotes a non-ASCII path ("caf\303\251.md") unless core.quotePath is off;
// a quoted name opens nothing, so the file silently fell out of every check.
func TestNonASCIIPathIsScannedAndLinked(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":    "# Root\n[c](docs/café.md)\n",
		"docs/café.md": "---\ntype: reference\n---\n# Café\n",
		"notes-é.txt":  "host corehost-prod\n",
	}, []string{"README.md", "docs/café.md", "notes-é.txt"})

	found, err := LeakScan(dir, LeakConfig{Terms: []string{"corehost"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].File != "notes-é.txt" {
		t.Fatalf("leak in a non-ASCII-named file must be found, got %+v", found)
	}
	md, err := trackedMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(md, "docs/café.md") {
		t.Fatalf("trackedMD must return the unquoted path, got %q", md)
	}
}

// scanLine applies a rule per line, so ^ anchors every line; the whole-file
// prefilter must agree or a line-2 match is never scanned.
func TestLeakScanAnchoredRegexPastFirstLine(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"config.env": "# header\napi_token=abc\n",
	}, []string{"config.env"})
	found, err := LeakScan(dir, LeakConfig{Regex: []string{"^api_token="}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Line != 2 {
		t.Fatalf("anchored rule must match on line 2, got %+v", found)
	}
}

func TestAddedLinesRemovedDoubleDashLine(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{"CLAUDE.md": "a\n---\nb\n"},
		map[string]string{"CLAUDE.md": "a\n**Footgun:** x\nb\n"})
	got, err := addedLines(dir, base+".."+head, "CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if !got[2] || len(got) != 1 {
		t.Fatalf("replacing a --- line: want {2}, got %v", got)
	}
}

func TestAddedLinesNoNewlineMarker(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{"CLAUDE.md": "a\nb"},
		map[string]string{"CLAUDE.md": "a\nFootgun: x\n"})
	got, err := addedLines(dir, base+".."+head, "CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if !got[2] || len(got) != 1 {
		t.Fatalf("rewriting an unterminated last line: want {2}, got %v", got)
	}
}

func TestGraphMarkdownTerminatesOnPartOfCycle(t *testing.T) {
	fm := func(to ...string) string {
		s := "---\ntype: reference\nlinks:\n"
		for _, x := range to {
			s += "  - rel: part-of\n    to: " + x + "\n"
		}
		return s + "---\n# x\n"
	}
	dir := setupRepo(t, map[string]string{
		"docs/r.md": "---\ntype: index\n---\n# r\n",
		"docs/b.md": fm("docs/r.md", "docs/c.md"),
		"docs/c.md": fm("docs/b.md"),
	}, []string{"docs/r.md", "docs/b.md", "docs/c.md"})
	v, err := BuildGraphView(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() { done <- v.Markdown() }()
	select {
	case md := <-done:
		if !strings.Contains(md, "docs/c.md") {
			t.Fatalf("cycle member missing from hierarchy:\n%s", md)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Markdown() did not terminate on a part-of cycle")
	}
}

func TestGraphViewAtRefFromSubdirectory(t *testing.T) {
	files, track := fixtureFiles()
	dir := setupRepo(t, files, track)
	commitAll(t, dir)
	top, err := BuildGraphViewAtRef(dir, "HEAD", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := BuildGraphViewAtRef(filepath.Join(dir, "docs"), "HEAD", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Nodes) != len(top.Nodes) {
		t.Fatalf("--ref from a subdirectory must see the whole tree: %d nodes vs %d", len(sub.Nodes), len(top.Nodes))
	}
}

func TestLinkTargetWithEncodedOrBracketedSpace(t *testing.T) {
	for _, target := range []string{"docs/setup%20guide.md", "<docs/setup guide.md>", `<docs/setup guide.md> "title"`} {
		if !isLocalMd(target) {
			t.Errorf("isLocalMd(%q) = false", target)
		}
		if got := resolveTarget("README.md", target); got != "docs/setup guide.md" {
			t.Errorf("resolveTarget(%q) = %q", target, got)
		}
	}
}

// A symbol moved into a new, not-yet-added file is still defined: the bare
// doc-drift diff already includes untracked files, so the grep must too.
func TestStillDefinedInUntrackedFile(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.go": "package a\n"}, []string{"a.go"})
	if err := os.WriteFile(filepath.Join(dir, "orders.go"), []byte("func OrderManager() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !stillDefinedInCode(dir, "OrderManager") {
		t.Error("OrderManager is defined in an untracked file, want stillDefined=true")
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// (?i) folds ſ onto s and the Kelvin sign onto k; a byte-wise ASCII lowercase
// does not, so the literal prefilter must not reject such a file.
func TestLeakScanLiteralMatchesUnicodeFoldedLetters(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "the ſecret is here\n"}, []string{"a.md"})
	found, err := LeakScan(dir, LeakConfig{Terms: []string{"secret"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("(?i) term must match its Unicode case fold, got %+v", found)
	}
}

func TestLeakScanTextAnchorPastFirstLine(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.env": "# header\napi_token=abc\n"}, []string{"a.env"})
	found, err := LeakScan(dir, LeakConfig{Regex: []string{`\Aapi_token=`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Line != 2 {
		t.Fatalf(`\A rule must match on line 2, got %+v`, found)
	}
}
