package audit

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// leakFuzzSeeds are rule/line pairs: every rule the required-literal tests pin,
// each with a line it matches, plus the folds and encodings the prefilter has
// missed before (ſ, Kelvin K, a BOM, non-ASCII before the match, ^/$ per line).
var leakFuzzSeeds = [][2]string{
	{`(?-i)[rs]k_live_[0-9A-Za-z]{20,}`, "x sk_live_ABCDEFGHIJKLMNOPQRSTU"},
	{`\bbearer [A-Za-z0-9._~+/-]{20,}`, "Authorization: BEARER abcdefghijklmnopqrstuv"},
	{`\b[a-z][a-z0-9+.-]*://[^\s/:@'"]+:[^\s/@'"]{3,}@`, "HTTPS://user:hunter2@host"},
	{`(?-i)\b[A-Z0-9_]*(PASSWORD|PASSWD|SECRET|TOKEN|API_KEY)=['"]?[^\s'"]{11,}`, "API_KEY='abcdefghijklm'"},
	{`acme[\s_-]*widgetry`, "ACME  Widgetry"},
	{`192\.0\.2\.\d{1,3}`, "host 192.0.2.4"},
	{`(?:ab)+cdef`, "ABABcdef"},
	{`(?:secret)?_value`, "x_VALUE"},
	{`secret[0-9]+|token[a-z]*`, "TOKENabc"},
	{`secret_key=[a-z]+`, "the ſecret_key=abcdef here"},
	{`kelvin[0-9]+`, "Kelvin42"},
	{`token=[a-z]+`, "\xef\xbb\xbftoken=abc"},
	{`straße`, "STRASSE straße"},
	{`^secret$`, "intro\nsecret\nouter"},
	{`internal\.example`, "İİİİ internal.example"},
}

// Every match of a gated rule contains one of its required literals once
// lowercased, unless it holds a fold rune the substring gate can't see — those
// files the scan filter admits before any substring test.
func FuzzRequiredLiteralsSound(f *testing.F) {
	for _, s := range leakFuzzSeeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, rule, line string) {
		re, err := regexp.Compile("(?i)" + rule)
		if err != nil {
			return
		}
		lits, ok := requiredLiterals(re)
		if !ok {
			return
		}
		for _, m := range re.FindAllString(line, -1) {
			if strings.ContainsAny(m, "ſK") {
				continue
			}
			low := strings.ToLower(m)
			if !slices.ContainsFunc(lits, func(l string) bool { return strings.Contains(low, l) }) {
				t.Fatalf("rule %q: match %q contains none of %q", rule, m, lits)
			}
		}
	})
}

// The scan filter never rejects a file in which the per-line scan finds a leak:
// whatever strategy a rule is put under (term literal, gated regex, alternation),
// mayMatch must admit every text scanLine would report on.
func FuzzScanFilterNeverSkipsAMatch(f *testing.F) {
	for _, s := range leakFuzzSeeds {
		f.Add(s[0], s[1], false)
		f.Add(s[0], "padding\n"+s[1]+"\ntrailer", false)
	}
	f.Add("internal.example", "İİİİ internal.EXAMPLE", true)
	f.Add("secret", "ſecret", true)
	f.Fuzz(func(t *testing.T, rule, text string, literal bool) {
		if !utf8.ValidString(rule) { // the TOML decoder refuses an invalid-UTF-8 term
			return
		}
		var m matcher
		var ok bool
		if literal {
			m, ok = literalMatcher(rule)
		} else {
			var err error
			m, ok, err = regexMatcher(rule)
			if err != nil {
				return
			}
		}
		if !ok {
			return
		}
		deny := []matcher{m}
		filter := newScanFilter(deny)
		for i, line := range strings.Split(text, "\n") {
			if found := scanLine("f", i+1, line, deny, nil); len(found) > 0 && !filter.mayMatch(text) {
				t.Fatalf("rule %q (literal=%v): scanLine finds %q on line %d but the filter skips the text %q",
					rule, literal, found[0].Match, i+1, text)
			}
		}
	})
}
