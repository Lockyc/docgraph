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

func TestDeclInlineRationalePasses(t *testing.T) {
	got := decls(t, "- **Footgun:** don't touch the cache, because writes race the flush.\n")
	if len(got) != 0 {
		t.Fatalf("rationale should suppress, got %+v", got)
	}
}

// An inline suppression comment is NOT honored — docaudit reads no in-file
// marker, so a footgun declaration is silenced only by a nearby rationale.
func TestDeclInlineMarkerIsIgnored(t *testing.T) {
	got := decls(t, "- **Footgun:** terse. <!-- footgun-ok: hit in prod -->\n")
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("inline marker must not suppress (no rationale present), got %+v", got)
	}
}

func TestDeclHeadingRationaleInNextParagraph(t *testing.T) {
	// Lone heading declaration; rationale in the following paragraph → window extends.
	got := decls(t, "## Footgun — don't merge the hooks\n\nThe reason is they run on different cadences.\n")
	if len(got) != 0 {
		t.Fatalf("heading window should extend to next paragraph, got %+v", got)
	}
}

func TestDeclHeadingNoBodyIsFinding(t *testing.T) {
	got := decls(t, "## Footgun — don't merge the hooks\n\n## Next unrelated section\n\nsome text\n")
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("heading with no rationale should flag at line 1, got %+v", got)
	}
}

func TestDeclBoldedMidParagraph(t *testing.T) {
	// A bolded declaration embedded in prose is still a declaration.
	got := decls(t, "Long intro sentence. **Footgun — a trailing newline is ignored.** More prose after.\n")
	// no rationale word present → finding
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
		t.Fatalf("two bullet declarations (contiguous) → two findings, got %+v", got)
	}
}

func TestDeclMixedSiblingsInParagraph(t *testing.T) {
	// Contiguous bullets (one paragraph): A justified, B not. B must still flag.
	got := decls(t, "- **Footgun:** A, because reasons.\n- **Footgun:** B, no why.\n")
	if len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("unjustified sibling B must flag at line 2, got %+v", got)
	}
}

func TestDeclMultilineParagraphTailNoExtend(t *testing.T) {
	// An unjustified declaration that is merely the LAST line of a MULTI-line
	// paragraph must NOT absorb the following paragraph's rationale. The window
	// extends only when the paragraph is a lone line or a heading — a tight bullet
	// list is neither.
	got := decls(t, "- Some setup note.\n- **Footgun:** never use raw SQL.\n\nWe chose Postgres because it scales.\n")
	if len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("multi-line-paragraph tail must not extend into next paragraph; want finding at line 2, got %+v", got)
	}
}

func TestDeclLoneLineRationaleInNextParagraph(t *testing.T) {
	// A lone-line (non-heading) declaration owning its paragraph DOES extend into
	// the next paragraph, same as a heading — the fix must not break this.
	got := decls(t, "**Footgun:** don't merge the hooks.\n\nThe reason is they run on different cadences.\n")
	if len(got) != 0 {
		t.Fatalf("lone-line declaration window should extend to next paragraph, got %+v", got)
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
