package audit

import (
	"os/exec"
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

func TestDocDriftDanglingReference(t *testing.T) {
	// base defines OldWidget and a doc names it; head removes the def, doc unchanged.
	dir, base, _ := commitRepo(t,
		map[string]string{"x.go": "type OldWidget struct{}\n", "CLAUDE.md": "We use OldWidget.\n"},
		map[string]string{"x.go": "package x\n"},
	)
	got, err := DocDrift(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Symbol != "OldWidget" || got[0].Kind != Dangling {
		t.Fatalf("want one Dangling OldWidget finding, got %+v", got)
	}
	if len(got[0].Hits) != 1 || got[0].Hits[0].File != "CLAUDE.md" {
		t.Fatalf("want a CLAUDE.md hit, got %+v", got[0].Hits)
	}
}

func TestDocDriftStillDefinedElsewhereNotFlagged(t *testing.T) {
	dir, base, _ := commitRepo(t,
		map[string]string{"a.go": "type OldWidget struct{}\n", "b.go": "type OldWidget struct{}\n", "CLAUDE.md": "OldWidget\n"},
		map[string]string{"a.go": "package x\n"}, // removed from a.go; b.go still has it
	)
	got, err := DocDrift(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("symbol still defined in b.go -> not dangling, got %+v", got)
	}
}

func TestDocDriftFiltersUndistinctiveSymbol(t *testing.T) {
	dir, base, _ := commitRepo(t,
		map[string]string{"x.go": "func handleClick() {}\n", "CLAUDE.md": "handleClick\n"},
		map[string]string{"x.go": "package x\n"},
	)
	got, err := DocDrift(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("camelCase handleClick is not distinctive -> filtered, got %+v", got)
	}
}

func TestDocDriftValueDrift(t *testing.T) {
	dir, base, _ := commitRepo(t,
		map[string]string{"cfg.go": "const MAX_ROOTS = 1000\n", "CLAUDE.md": "MAX_ROOTS is 1000.\n"},
		map[string]string{"cfg.go": "const MAX_ROOTS = 2000\n"},
	)
	got, err := DocDrift(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Symbol != "MAX_ROOTS" || got[0].Kind != ValueDrift || got[0].Old != "1000" {
		t.Fatalf("want one ValueDrift MAX_ROOTS old=1000, got %+v", got)
	}
}

func TestDocDriftValueReconciledNotFlagged(t *testing.T) {
	dir, base, _ := commitRepo(t,
		map[string]string{"cfg.go": "const MAX_ROOTS = 1000\n", "CLAUDE.md": "MAX_ROOTS is 1000.\n"},
		map[string]string{"cfg.go": "const MAX_ROOTS = 2000\n", "CLAUDE.md": "MAX_ROOTS is 2000.\n"},
	)
	got, err := DocDrift(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("doc updated to new value -> no drift, got %+v", got)
	}
}

// TestDocDriftIgnoresMarkdownOnlyChange regression-tests the code-diff scope: a
// commit that ONLY edits a .md file (removing a line naming a distinctive
// symbol that is never defined in any tracked code file) must produce zero
// findings. Before the fix, gitDiff had no pathspec, so the removed doc line
// "the type QueueManager did things" was misparsed by defKW as a REMOVED CODE
// DEFINITION of QueueManager; since QueueManager is (rightly) undefined in any
// .go file, stillDefinedInCode said "gone", and b.md's surviving mention of
// QueueManager turned prose-editing into a false dangling-reference finding.
func TestDocDriftIgnoresMarkdownOnlyChange(t *testing.T) {
	dir, base, _ := commitRepo(t,
		map[string]string{
			"a.md": "intro\nthe type QueueManager did things\n",
			"b.md": "QueueManager is mentioned here too.\n",
		},
		map[string]string{
			"a.md": "intro\n", // doc prose trimmed; no code anywhere ever defined QueueManager
		},
	)
	got, err := DocDrift(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a doc-only edit must not be mistaken for code drift, got %+v", got)
	}
}

// Non-.md prose formats (CHANGELOG.txt/.rst/.adoc/…) are excluded from the code
// side too (nonCodePathspec) — a def-keyword-shaped English sentence trimmed
// from such a file must not be read as a removed code definition and blocked.
func TestDocDriftIgnoresNonMarkdownProseChange(t *testing.T) {
	dir, base, _ := commitRepo(t,
		map[string]string{
			"CHANGELOG.txt": "Changelog\n\nAdded class OrderManager for orders.\n",
			"CLAUDE.md":     "The OrderManager handles orders.\n", // named in a real doc
		},
		map[string]string{
			"CHANGELOG.txt": "Changelog\n", // prose line trimmed; no code ever defined OrderManager
		},
	)
	got, err := DocDrift(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a non-.md prose edit must not be mistaken for code drift, got %+v", got)
	}
}

func TestDocDriftIncludesUncommittedWorkingTree(t *testing.T) {
	// base committed with def + doc; remove the def in the WORKING TREE, no commit.
	dir := setupRepo(t, map[string]string{
		"x.go": "type OldWidget struct{}\n", "CLAUDE.md": "We use OldWidget.\n",
	}, []string{"x.go", "CLAUDE.md"})
	git := func(a ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "base")
	writeFile(t, dir, "x.go", "package x\n") // uncommitted removal
	got, err := DocDrift(dir, "HEAD")        // diff worktree vs HEAD
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Symbol != "OldWidget" {
		t.Fatalf("uncommitted removal must be flagged, got %+v", got)
	}
}
