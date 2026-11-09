package audit

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestRequiredLiterals(t *testing.T) {
	cases := []struct {
		rule string
		want []string // nil = no gate
	}{
		{`(?-i)[rs]k_live_[0-9A-Za-z]{20,}`, []string{"rk_live_", "sk_live_"}},
		{`\bbearer [A-Za-z0-9._~+/-]{20,}`, []string{"bearer "}},
		{`\b[a-z][a-z0-9+.-]*://[^\s/:@'"]+:[^\s/@'"]{3,}@`, []string{"://"}},
		{`(?-i)\b[A-Z0-9_]*(PASSWORD|PASSWD|SECRET|TOKEN|API_KEY)=['"]?[^\s'"]{11,}`,
			[]string{"api_key=", "passwd=", "password=", "secret=", "token="}},
		{`acme[\s_-]*widgetry`, []string{"widgetry"}},
		{`10\.20\.30\.\d{1,3}`, []string{"10.20.30."}},
		{`(?:ab)+cdef`, []string{"cdef"}},
		{`(?:xyz)+`, []string{"xyz"}},
		{`foo|ba`, nil},        // a branch too short to gate on
		{`[a-z]+@[a-z]+`, nil}, // nothing longer than "@" is required
		{`(?:secret)?_value`, []string{"_value"}},
		{`(?:ab)*`, nil},
		{`straße`, nil}, // non-ASCII: byte-wise lowercase is not Unicode folding
	}
	for _, c := range cases {
		got, ok := requiredLiterals(regexp.MustCompile("(?i)" + c.rule))
		if c.want == nil {
			if ok {
				t.Errorf("%s: want no gate, got %q", c.rule, got)
			}
			continue
		}
		slices.Sort(got)
		if !ok || !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q (ok=%v), want %q", c.rule, got, ok, c.want)
		}
	}
}

// Soundness: every match of a gated rule contains one of its required literals
// once lowercased. Each input below is a real match in a casing or form the
// rule's source never wrote.
func TestRequiredLiteralsAreContainedInEveryMatch(t *testing.T) {
	cases := map[string][]string{
		`(?-i)[rs]k_live_[0-9A-Za-z]{20,}`:                                          {"rk_live_abcdefghijklmnopqrstu", "x sk_live_ABCDEFGHIJKLMNOPQRSTU"},
		`\bbearer [A-Za-z0-9._~+/-]{20,}`:                                           {"BEARER abcdefghijklmnopqrstuv", "Bearer x.y.z-aaaaaaaaaaaaaaaaaa"},
		`\b[a-z][a-z0-9+.-]*://[^\s/:@'"]+:[^\s/@'"]{3,}@`:                          {"HTTPS://user:hunter2@host", "pg+ssl://a:bbbb@c"},
		`(?-i)\b[A-Z0-9_]*(PASSWORD|PASSWD|SECRET|TOKEN|API_KEY)=['"]?[^\s'"]{11,}`: {"DB_PASSWD=abcdefghijkl", "API_KEY='abcdefghijklm'"},
		`acme[\s_-]*widgetry`:                                                       {"ACME  Widgetry", "acme_widgetry"},
	}
	for rule, inputs := range cases {
		re := regexp.MustCompile("(?i)" + rule)
		lits, ok := requiredLiterals(re)
		if !ok {
			t.Fatalf("%s: expected a gate", rule)
		}
		for _, in := range inputs {
			m := re.FindString(in)
			if m == "" {
				t.Fatalf("%s: fixture %q does not match", rule, in)
			}
			low := strings.ToLower(m)
			if !slices.ContainsFunc(lits, func(l string) bool { return strings.Contains(low, l) }) {
				t.Errorf("%s: match %q contains none of %q", rule, m, lits)
			}
		}
	}
}

// A gated rule must still catch a casing the config never wrote.
func TestLeakScanGatedRegexSurvivesCasing(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "intro\nAuthorization: BEARER abcdefghijklmnopqrstuvwxyz\n"}, []string{"a.md"})
	found, err := LeakScan(dir, LeakConfig{Regex: []string{`\bbearer [A-Za-z0-9]{20,}`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Line != 2 {
		t.Fatalf("gated regex must match in any casing, got %+v", found)
	}
}

// (?i) folds ſ onto s: a gated rule's substring test can't see that, so the fold
// guard must admit the file.
func TestLeakScanGatedRegexMatchesUnicodeFoldedLetters(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "the ſecret_key=abcdef here\n"}, []string{"a.md"})
	found, err := LeakScan(dir, LeakConfig{Regex: []string{`secret_key=[a-z]+`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("gated regex must match its Unicode case fold, got %+v", found)
	}
}

// A gated rule runs only on lines holding its literal: an earlier occurrence that
// doesn't match must not end the search before the line that does.
func TestLeakScanGatedRegexFindsLaterOccurrence(t *testing.T) {
	body := "see https://example.com/docs\nplain line\nurl = postgres://admin:hunter22@db\n"
	dir := setupRepo(t, map[string]string{"a.md": body}, []string{"a.md"})
	found, err := LeakScan(dir, LeakConfig{Regex: []string{`\b[a-z][a-z0-9+.-]*://[^\s/:@]+:[^\s/@]{3,}@`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Line != 3 {
		t.Fatalf("gated regex must match the later line, got %+v", found)
	}
}

// Lowercasing non-ASCII text can shift byte offsets, so such a file takes the
// whole-file path — a match after the shift must still be found.
func TestLeakScanGatedRegexNonASCIIFile(t *testing.T) {
	body := "İİİİ Straße\nurl = postgres://admin:hunter22@db\n"
	dir := setupRepo(t, map[string]string{"a.md": body}, []string{"a.md"})
	found, err := LeakScan(dir, LeakConfig{Regex: []string{`\b[a-z][a-z0-9+.-]*://[^\s/:@]+:[^\s/@]{3,}@`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Line != 2 {
		t.Fatalf("gated regex must match in a non-ASCII file, got %+v", found)
	}
}
