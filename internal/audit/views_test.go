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
}

func TestIndexMarkdown(t *testing.T) {
	docs := map[string]*Doc{
		"docs/run.md": {Type: "runbook", Title: "Restore", Description: "recover it"},
		"docs/a.md":   {Type: "reference", Title: "API"},
		"docs/z.md":   {Type: "reference"}, // no title → falls back to path
	}
	out := IndexMarkdown(docs)
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
	if !strings.Contains(out, "[docs/z.md](docs/z.md)") {
		t.Errorf("titleless doc should fall back to its path:\n%s", out)
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
}
