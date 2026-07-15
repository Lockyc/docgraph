package audit

import (
	"reflect"
	"strings"
	"testing"
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
