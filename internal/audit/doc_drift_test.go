package audit

import (
	"reflect"
	"testing"
)

func TestLooksLikeSymbol(t *testing.T) {
	yes := []string{"MAX_ROOTS", "OldWidget", "HTTPServer", "A_BC"}
	no := []string{"handleClick", "Window", "Math", "foo", "abc", "X1", "lowercase_only"}
	for _, s := range yes {
		if !looksLikeSymbol(s) {
			t.Errorf("looksLikeSymbol(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeSymbol(s) {
			t.Errorf("looksLikeSymbol(%q) = true, want false", s)
		}
	}
}

func TestRemovedNotReadded(t *testing.T) {
	diff := "" +
		"--- a/x.go\n+++ b/x.go\n" +
		"-type OldWidget struct{}\n" +
		"-func Helper() {}\n" +
		"+func Helper() {}\n" // Helper re-added -> not gone
	got := removedNotReadded(diff)
	want := []string{"OldWidget"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removedNotReadded = %v, want %v", got, want)
	}
}

func TestChangedConstants(t *testing.T) {
	diff := "" +
		"-const MAX_ROOTS = 1000\n" +
		"+const MAX_ROOTS = 2000\n" +
		"-UNCHANGED = 5\n+UNCHANGED = 5\n" // same value -> not changed
	got := changedConstants(diff)
	want := []constChange{{Name: "MAX_ROOTS", Old: "1000"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedConstants = %v, want %v", got, want)
	}
}

func TestStillDefinedInCode(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"a.go":      "type KeepWidget struct{}\n",
		"CLAUDE.md": "KeepWidget and GhostWidget are mentioned.\n",
	}, []string{"a.go", "CLAUDE.md"})
	if !stillDefinedInCode(dir, "KeepWidget") {
		t.Error("KeepWidget is in code, want stillDefined=true")
	}
	if stillDefinedInCode(dir, "GhostWidget") {
		t.Error("GhostWidget is only in a doc, want stillDefined=false")
	}
}

func TestDocGrepSymbol(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "line one\nwe use OldWidget here\n",
		"a.go":      "OldWidget\n", // code hit must be ignored (docs only)
	}, []string{"CLAUDE.md", "a.go"})
	hits, err := docGrepSymbol(dir, "OldWidget")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].File != "CLAUDE.md" || hits[0].Line != 2 {
		t.Fatalf("want one CLAUDE.md:2 hit, got %+v", hits)
	}
}

func TestDocGrepValue(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "MAX_ROOTS is 1000 by default\n",
		"other.md":  "1000 appears but MAX_ROOTS does not\n", // NOT named here? it is -> still counts
	}, []string{"CLAUDE.md"})
	hits, err := docGrepValue(dir, "MAX_ROOTS", "1000")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].File != "CLAUDE.md" {
		t.Fatalf("want one CLAUDE.md hit (other.md untracked), got %+v", hits)
	}
}

// TestDocGrepSymbolNoMatch drives gitGrepHits' git-grep-exit-1 branch: no
// tracked doc names the symbol, so `git grep -n -F -w` exits 1 and must
// yield (nil, nil), not an error.
func TestDocGrepSymbolNoMatch(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "this doc mentions nothing special\n",
	}, []string{"CLAUDE.md"})
	hits, err := docGrepSymbol(dir, "GhostSymbol")
	if err != nil {
		t.Fatalf("docGrepSymbol err = %v, want nil", err)
	}
	if len(hits) != 0 {
		t.Fatalf("docGrepSymbol hits = %+v, want none", hits)
	}
}

// TestDocGrepValueNoMatch drives docGrepValue's own `git grep -l` listing
// exit-1 branch: no tracked doc names the symbol at all, so the listing grep
// exits 1 before ever searching for the value, and must yield (nil, nil).
func TestDocGrepValueNoMatch(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "this doc names no relevant symbol\n",
	}, []string{"CLAUDE.md"})
	hits, err := docGrepValue(dir, "GhostSymbol", "1000")
	if err != nil {
		t.Fatalf("docGrepValue err = %v, want nil", err)
	}
	if len(hits) != 0 {
		t.Fatalf("docGrepValue hits = %+v, want none", hits)
	}
}
