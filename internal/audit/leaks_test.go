package audit

import (
	"strings"
	"testing"
)

func TestParseLeakRules(t *testing.T) {
	src := `# a comment
lsjc\.au
/Users/[a-z]+

!au\.lsjc\.curator      # bundle id, legit
`
	rules, err := ParseLeakRules(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.deny) != 2 {
		t.Errorf("deny = %d, want 2 (comment+blank ignored)", len(rules.deny))
	}
	if len(rules.allow) != 1 {
		t.Errorf("allow = %d, want 1", len(rules.allow))
	}
	if rules.allow[0].raw != `au\.lsjc\.curator` {
		t.Errorf("allow raw = %q, want trailing comment stripped", rules.allow[0].raw)
	}
}

func TestParseLeakRulesPureCommentAllowSkipped(t *testing.T) {
	rules, err := ParseLeakRules(strings.NewReader("!  # note\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.allow) != 0 {
		t.Errorf("allow = %d, want 0 (pure-comment allow line must not compile a rule)", len(rules.allow))
	}
}

func TestParseLeakRulesBadRegex(t *testing.T) {
	_, err := ParseLeakRules(strings.NewReader("valid\n(unclosed\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("want line-numbered regex error, got %v", err)
	}
}

func TestLeakScan(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":   "clean line\ncontact lachlan@lsjc.au here\n", // deny hit on line 2
		"src/app.rs":  "// bundle id au.lsjc.curator is fine\n",     // deny 'lsjc' covered by allow
		"secrets.env": "AWS=AKIAIOSFODNN7EXAMPLE\n",                 // built-in hit
		"logo.bin":    "\x00\x01binary lsjc.au\x00",                 // binary, skipped
		"vendor.md":   "lsjc.au appears here\n",                     // ignored via --ignore
	}, []string{"README.md", "src/app.rs", "secrets.env", "logo.bin", "vendor.md"})

	rules, err := ParseLeakRules(strings.NewReader("lsjc\\.au\n!au\\.lsjc\\.curator\n"))
	if err != nil {
		t.Fatal(err)
	}
	found, err := LeakScan(dir, rules, []string{"vendor.md"})
	if err != nil {
		t.Fatal(err)
	}
	// Expect: README.md:2 (lsjc.au), secrets.env:1 (AKIA built-in). Not app.rs
	// (allow-covered), not logo.bin (binary), not vendor.md (ignored).
	if len(found) != 2 {
		t.Fatalf("findings = %+v, want 2", found)
	}
	if found[0].File != "README.md" || found[0].Line != 2 {
		t.Errorf("finding[0] = %+v, want README.md:2", found[0])
	}
	if found[1].File != "secrets.env" || !strings.HasPrefix(found[1].Match, "AKIA") {
		t.Errorf("finding[1] = %+v, want secrets.env AKIA built-in", found[1])
	}
}

func TestLeakScanScansTrackedToolingDespiteDefaultIgnores(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		".claude/skills/foo.md": "internal note: lsjc.au\n",
		".docauditignore":       ".claude/**\n",
	}, []string{".claude/skills/foo.md", ".docauditignore"})

	rules, err := ParseLeakRules(strings.NewReader("lsjc\\.au\n"))
	if err != nil {
		t.Fatal(err)
	}
	found, err := LeakScan(dir, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	var hit bool
	for _, f := range found {
		if f.File == ".claude/skills/foo.md" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("want leak found in tracked .claude/skills/foo.md despite defaultIgnores + .docauditignore, got %+v", found)
	}
}

func TestLeakScanAllowSuppressesBuiltin(t *testing.T) {
	// A valid 36-char GitHub token so the built-in `ghp_[A-Za-z0-9]{36}` actually
	// matches — otherwise the suppression assertion below would pass vacuously.
	const token = "ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8"
	dir := setupRepo(t, map[string]string{
		"README.md": "example key " + token + "\n",
	}, []string{"README.md"})

	// Without any allow rule, the built-in must fire — proving the fixture is a
	// real built-in match, so the suppression below is meaningful.
	bare, err := LeakScan(dir, LeakRules{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bare) != 1 || bare[0].Match != token {
		t.Fatalf("built-in ghp_ pattern should match the token, got %+v", bare)
	}

	// With a `!` allow covering it, the same built-in match is suppressed.
	rules, err := ParseLeakRules(strings.NewReader("!ghp_[A-Za-z0-9]{36}\n"))
	if err != nil {
		t.Fatal(err)
	}
	found, err := LeakScan(dir, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("allow should suppress the built-in match, got %+v", found)
	}
}
