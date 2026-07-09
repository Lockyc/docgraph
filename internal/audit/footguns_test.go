package audit

import "testing"

func TestFootgunNoRationaleIsFinding(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "intro\n\n- Footgun: don't touch the cache directly.\n",
	}, []string{"CLAUDE.md"})
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "CLAUDE.md" || got[0].Line != 3 {
		t.Fatalf("want one finding at CLAUDE.md:3, got %+v", got)
	}
}

func TestFootgunWithInlineRationalePasses(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "- Footgun: don't touch the cache directly, because writes race the flush.\n",
	}, []string{"CLAUDE.md"})
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("rationale ('because') should suppress, got %+v", got)
	}
}

func TestFootgunAckMarkerPasses(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "- Footgun: terse but real. <!-- footgun-ok: hit in prod 2026 -->\n",
	}, []string{"CLAUDE.md"})
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ack marker should suppress, got %+v", got)
	}
}

func TestFootgunRationaleAcrossParagraphLines(t *testing.T) {
	// Multi-line paragraph (no blank line): rationale on a later line still counts.
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "- Footgun: don't merge these two hooks.\n  The reason is they run on different cadences.\n",
	}, []string{"CLAUDE.md"})
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("same-paragraph rationale should suppress, got %+v", got)
	}
}

func TestFootgunOnePerParagraph(t *testing.T) {
	// Two footgun tokens, one unjustified paragraph -> exactly one finding, first line.
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "Footgun one and footgun two in the same block with no why.\n",
	}, []string{"CLAUDE.md"})
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("want exactly one finding at line 1, got %+v", got)
	}
}

func TestFootgunSeparateParagraphsAreIndependent(t *testing.T) {
	// Para 1 justified, para 2 not -> one finding, in para 2.
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "- Footgun: A, because reasons.\n\n- Footgun: B, no why.\n",
	}, []string{"CLAUDE.md"})
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("want one finding at line 3, got %+v", got)
	}
}

func TestFootgunCaseAndPlural(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "Common FOOTGUNS here, no justification.\n",
	}, []string{"CLAUDE.md"})
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("case-insensitive plural should match, got %+v", got)
	}
}

func TestFootgunIgnoreLayerScope(t *testing.T) {
	// .claude/** is a doc-graph ignore -> not scanned. Untracked .md -> not scanned.
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":        "clean doc\n",
		".claude/skill.md": "Footgun: unjustified but out of scope.\n",
		"loose.md":         "Footgun: untracked, out of scope.\n",
	}, []string{"CLAUDE.md", ".claude/skill.md"}) // loose.md left untracked
	got, err := FootgunScan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ignored/untracked files must not be scanned, got %+v", got)
	}
}

func TestFootgunExtraIgnore(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"docs/notes.md": "Footgun: unjustified.\n",
	}, []string{"docs/notes.md"})
	got, err := FootgunScan(dir, []string{"docs/**"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("--ignore glob should exclude the file, got %+v", got)
	}
}
