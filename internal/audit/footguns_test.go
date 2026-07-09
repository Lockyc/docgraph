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

func TestDeclMarkerPasses(t *testing.T) {
	got := decls(t, "- **Footgun:** terse. <!-- footgun-ok: hit in prod -->\n")
	if len(got) != 0 {
		t.Fatalf("marker should suppress, got %+v", got)
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
