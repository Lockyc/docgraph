package audit

import "testing"

func decls(t *testing.T, content string) []declFinding {
	t.Helper()
	return scanDeclarations(content)
}

func TestDeclBareBulletIsFinding(t *testing.T) {
	got := decls(t, "- **Footgun:** don't touch the cache directly.\n")
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("want one finding at line 1, got %+v", got)
	}
}

// Every footgun declaration flags — a rationale word in the same line does NOT
// suppress it. docgraph can't judge whether a stated "why" is real, so it nags
// on the declaration regardless and leaves the judgement to the pusher.
func TestDeclRationaleDoesNotSuppress(t *testing.T) {
	got := decls(t, "- **Footgun:** don't touch the cache, because writes race the flush.\n")
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("rationale must NOT suppress; want one finding at line 1, got %+v", got)
	}
}

// No in-file marker suppresses a declaration — there is no rationale escape and
// no annotation escape. Suppression is only DOCGRAPH_FOOTGUN_OFF / --no-footgun-drift.
func TestDeclInlineMarkerIsIgnored(t *testing.T) {
	got := decls(t, "- **Footgun:** terse. <!-- footgun-ok: hit in prod -->\n")
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("inline marker must not suppress, got %+v", got)
	}
}

// A heading declaration flags at its own line — there is no window that reaches
// into following prose to excuse it (the whole rationale-window machinery is gone).
func TestDeclHeadingIsFinding(t *testing.T) {
	got := decls(t, "## Footgun — don't merge the hooks\n\nThe reason is they run on different cadences.\n")
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("heading declaration should flag at line 1, got %+v", got)
	}
}

func TestDeclBoldedMidParagraph(t *testing.T) {
	// A bolded declaration embedded in prose is still a declaration.
	got := decls(t, "Long intro sentence. **Footgun — a trailing newline is ignored.** More prose after.\n")
	if len(got) != 1 {
		t.Fatalf("bolded mid-paragraph declaration should be checked, got %+v", got)
	}
}

func TestCrossReferenceIsNotDeclaration(t *testing.T) {
	got := decls(t, "See the probe-env footgun in the *Conventions & footguns* section below.\n")
	if len(got) != 0 {
		t.Fatalf("a cross-reference is not a declaration, got %+v", got)
	}
}

func TestContainerHeadingIsNotDeclaration(t *testing.T) {
	got := decls(t, "## Footguns\n\nsome intro with no delimiter after the word\n")
	if len(got) != 0 {
		t.Fatalf("bare container heading is not a declaration, got %+v", got)
	}
}

func TestDeclOnePerDeclaration(t *testing.T) {
	got := decls(t, "- **Footgun:** A, no why.\n- **Footgun:** B, no why.\n")
	if len(got) != 2 {
		t.Fatalf("two bullet declarations → two findings, got %+v", got)
	}
}

// A rationale word on one sibling suppresses nothing — both flag. (Guards that
// removing the suppression didn't leave a residual per-line rationale check.)
func TestDeclSiblingsBothFlag(t *testing.T) {
	got := decls(t, "- **Footgun:** A, because reasons.\n- **Footgun:** B, no why.\n")
	if len(got) != 2 || got[0].Line != 1 || got[1].Line != 2 {
		t.Fatalf("both siblings must flag at lines 1 and 2, got %+v", got)
	}
}

func TestDeclHyphenCompoundNotDeclaration(t *testing.T) {
	// "footgun-drift"/"Footgun-free" are hyphenated compounds, not declarations:
	// the hyphen is part of the word, not a Footgun-delimiter.
	got := decls(t, "footgun-drift is a diff-scoped subcommand.\n")
	if len(got) != 0 {
		t.Fatalf("hyphenated compound is not a declaration, got %+v", got)
	}
}

func TestDeclSpacedHyphenIsDeclaration(t *testing.T) {
	// A genuine spaced-hyphen separator ("Footgun - ...") is still a declaration —
	// narrowing the delimiter must not drop this real form.
	got := decls(t, "**Footgun - never use raw SQL.**\n")
	if len(got) != 1 {
		t.Fatalf("spaced-hyphen separator is a declaration, got %+v", got)
	}
}
