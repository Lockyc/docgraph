package audit

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type LeakRule struct {
	re    *regexp.Regexp
	raw   string
	allow bool
}

type LeakRules struct {
	deny  []LeakRule
	allow []LeakRule
}

type LeakFinding struct {
	File    string
	Line    int
	Match   string
	Pattern string
}

// stripInlineComment removes an unescaped " #" (space-hash) trailing comment and
// everything after it. A literal '#' in a pattern must not be space-preceded.
func stripInlineComment(s string) string {
	if i := strings.Index(s, " #"); i >= 0 {
		return s[:i]
	}
	return s
}

// ParseLeakRules parses the gitignore-style leak rules format: one Go regexp per
// line; a leading '!' marks an allow-exception; '#' and blank lines are ignored;
// a " #" trailing comment is stripped. A bad regex is a line-numbered error.
func ParseLeakRules(r io.Reader) (LeakRules, error) {
	var rules LeakRules
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allow := false
		if strings.HasPrefix(line, "!") {
			allow = true
			line = strings.TrimSpace(line[1:])
		}
		pat := strings.TrimSpace(stripInlineComment(line))
		if pat == "" {
			continue
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return LeakRules{}, fmt.Errorf("leaks rules line %d: bad regex %q: %v", n, pat, err)
		}
		rule := LeakRule{re: re, raw: pat, allow: allow}
		if allow {
			rules.allow = append(rules.allow, rule)
		} else {
			rules.deny = append(rules.deny, rule)
		}
	}
	if err := sc.Err(); err != nil {
		return LeakRules{}, err
	}
	return rules, nil
}
