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

func TestParseLeakRulesBadRegex(t *testing.T) {
	_, err := ParseLeakRules(strings.NewReader("valid\n(unclosed\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("want line-numbered regex error, got %v", err)
	}
}
