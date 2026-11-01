package audit

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCoversOf(t *testing.T) {
	docs := map[string]*Doc{
		"docs/auth.md":  {Links: []Edge{{Rel: "covers", To: "src/auth/login.go"}}},
		"docs/dir.md":   {Links: []Edge{{Rel: "covers", To: "src/auth/"}}}, // covers a directory
		"docs/other.md": {Links: []Edge{{Rel: "covers", To: "src/db/x.go"}}},
		"docs/ref.md":   {Links: []Edge{{Rel: "see-also", To: "src/auth/login.go"}}}, // not a covers edge
		"docs/near.md":  {Links: []Edge{{Rel: "covers", To: "src/au"}}},              // a path PREFIX of src/auth, not a parent dir
	}
	// A file covered directly by auth.md and by-directory by dir.md.
	got := CoversOf(docs, "src/auth/login.go")
	if !reflect.DeepEqual(got, []string{"docs/auth.md", "docs/dir.md"}) {
		t.Errorf("CoversOf(login.go) = %v, want [docs/auth.md docs/dir.md]", got)
	}
	// A file only under the covered directory.
	if got := CoversOf(docs, "src/auth/logout.go"); !reflect.DeepEqual(got, []string{"docs/dir.md"}) {
		t.Errorf("CoversOf(logout.go) = %v, want [docs/dir.md]", got)
	}
	// Nothing covers this.
	if got := CoversOf(docs, "src/nope.go"); len(got) != 0 {
		t.Errorf("CoversOf(nope) = %v, want empty", got)
	}
	// A covers target that is a string prefix of the file's path but NOT a parent
	// directory of it must not match: "src/au" does not cover "src/auth/login.go".
	// Directory containment is a path-segment relationship, which is what the "/"
	// in the prefix test enforces — drop it and near.md claims every path under a
	// sibling whose name merely starts the same way, nagging on unrelated docs.
	for _, src := range CoversOf(docs, "src/auth/login.go") {
		if src == "docs/near.md" {
			t.Error("CoversOf matched a bare string prefix (src/au) as a covering directory of src/auth/login.go")
		}
	}
}

// A covers edge to the repo root (`.` or `./`) covers every path in the repo.
func TestCoversOfRepoRoot(t *testing.T) {
	docs := map[string]*Doc{
		"CLAUDE.md": {Links: []Edge{{Rel: "covers", To: "."}}},
		"docs/a.md": {Links: []Edge{{Rel: "covers", To: "./"}}},
	}
	for _, target := range []string{"main.go", "src/auth/login.go"} {
		if got := CoversOf(docs, target); !reflect.DeepEqual(got, []string{"CLAUDE.md", "docs/a.md"}) {
			t.Errorf("CoversOf(%s) = %v, want [CLAUDE.md docs/a.md]", target, got)
		}
	}
}

func TestIndexMarkdown(t *testing.T) {
	docs := map[string]*Doc{
		"docs/run.md": {Type: "runbook", Title: "Restore", Description: "recover it"},
		"docs/a.md":   {Type: "reference", Title: "API", Heading: "Ignored"}, // explicit title overrides the H1
		"docs/h.md":   {Type: "reference", Heading: "From H1"},               // no title → falls back to the body H1
		"docs/z.md":   {Type: "reference"},                                   // no title, no H1 → falls back to path
	}
	out := IndexMarkdown(docs, "")
	// runbook is earlier than reference in CoreTypes order.
	if !strings.Contains(out, "## runbook") || !strings.Contains(out, "## reference") {
		t.Fatalf("missing type headings:\n%s", out)
	}
	if strings.Index(out, "## runbook") > strings.Index(out, "## reference") {
		t.Error("runbook heading should precede reference (CoreTypes order)")
	}
	if !strings.Contains(out, "[Restore](docs/run.md) — recover it") {
		t.Errorf("runbook entry missing/incorrect:\n%s", out)
	}
	if !strings.Contains(out, "[API](docs/a.md)") {
		t.Errorf("explicit title should override the body H1:\n%s", out)
	}
	if !strings.Contains(out, "[From H1](docs/h.md)") {
		t.Errorf("titleless doc should fall back to its body H1:\n%s", out)
	}
	if !strings.Contains(out, "[docs/z.md](docs/z.md)") {
		t.Errorf("doc with neither title nor H1 should fall back to its path:\n%s", out)
	}
}

// With a dest, links resolve from dest's directory, the page opens with a
// `type: index` block, and dest is not listed in itself.
func TestIndexMarkdownForDest(t *testing.T) {
	docs := map[string]*Doc{
		"CLAUDE.md":        {Type: "architecture", Heading: "Notes"},
		"docs/a.md":        {Type: "reference", Heading: "A"},
		"docs/sub/b.md":    {Type: "guide", Heading: "B"},
		"docs/index.md":    {Type: "index", Heading: "Index"},
		"other/runbook.md": {Type: "runbook", Heading: "R"},
	}
	out := IndexMarkdown(docs, "docs/index.md")
	if !strings.HasPrefix(out, "---\ntype: index\n---\n\n# Index\n") {
		t.Errorf("missing leading type: index block:\n%s", out)
	}
	for _, want := range []string{"[Notes](../CLAUDE.md)", "[A](a.md)", "[B](sub/b.md)", "[R](../other/runbook.md)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(index.md)") || strings.Contains(out, "## index") {
		t.Errorf("dest should not list itself:\n%s", out)
	}
}

// The page `index --for docs/index.md` generates passes the whole-state audit
// when written to docs/index.md.
func TestAuditPassesGeneratedIndex(t *testing.T) {
	files := map[string]string{
		"README.md":     "# Proj\n",
		"CLAUDE.md":     "---\ntype: architecture\nlinks:\n  - rel: part-of\n    to: docs/index.md\n---\n\n# Notes\n",
		"docs/index.md": "placeholder\n",
		"docs/a.md":     "---\ntype: reference\nlinks:\n  - rel: part-of\n    to: docs/index.md\n---\n\n# A\n",
		"docs/sub/b.md": "---\ntype: guide\nlinks:\n  - rel: part-of\n    to: docs/index.md\n---\n\n# B\n",
	}
	var track []string
	for f := range files {
		track = append(track, f)
	}
	dir := setupRepo(t, files, track)
	docs, err := RepoDocs(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "docs/index.md", IndexMarkdown(docs, "docs/index.md"))
	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.HasFindings() {
		t.Errorf("generated index should pass the audit: %+v", rep)
	}
}

func TestParseReviewDays(t *testing.T) {
	cases := map[string]struct {
		days int
		ok   bool
	}{"90d": {90, true}, "2w": {14, true}, "": {0, false}, "90": {0, false}, "-1d": {0, false}, "xd": {0, false}}
	for in, want := range cases {
		days, ok := parseReviewDays(in)
		if days != want.days || ok != want.ok {
			t.Errorf("parseReviewDays(%q) = (%d,%v), want (%d,%v)", in, days, ok, want.days, want.ok)
		}
	}
}

func TestStaleDocs(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	docs := map[string]*Doc{
		"fresh.md":    {Verified: "2026-07-01"},               // 14d old, under default
		"old.md":      {Verified: "2026-01-01"},               // ~195d old, over default 180
		"tight.md":    {Verified: "2026-06-01", Review: "7d"}, // ~44d old, over its own 7d
		"noverify.md": {Type: "runbook"},                      // no verified → skipped
		"baddate.md":  {Verified: "not-a-date"},               // unparseable → skipped
		// An unparseable `review` (a unit-less "90" is the plausible typo) falls back
		// to the caller's default, NOT to a zero threshold — zero would report every
		// verified doc stale, turning one typo into a repo-wide false flood.
		"badreview.md": {Verified: "2026-07-01", Review: "90"}, // 14d old, under the 180d default
	}
	got := StaleDocs(docs, now, 180)
	files := map[string]bool{}
	for _, s := range got {
		files[s.File] = true
	}
	if !files["old.md"] || !files["tight.md"] {
		t.Fatalf("stale = %+v, want old.md + tight.md", got)
	}
	if files["fresh.md"] || files["noverify.md"] || files["baddate.md"] {
		t.Errorf("stale wrongly includes a fresh/unverified/bad-date doc: %+v", got)
	}
	if files["badreview.md"] {
		t.Errorf("an unparseable `review` must fall back to the default threshold, not zero: %+v", got)
	}
}
