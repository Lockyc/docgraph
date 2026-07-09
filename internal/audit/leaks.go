package audit

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

func builtinLeakRules() []LeakRule {
	pats := []string{
		`-----BEGIN [A-Z ]*PRIVATE KEY-----`, // PEM private key header
		`AKIA[0-9A-Z]{16}`,                    // AWS access key id
		`ghp_[A-Za-z0-9]{36}`,                 // GitHub personal access token
		`xox[baprs]-[A-Za-z0-9-]{10,}`,        // Slack token
	}
	rs := make([]LeakRule, 0, len(pats))
	for _, p := range pats {
		rs = append(rs, LeakRule{re: regexp.MustCompile(p), raw: p})
	}
	return rs
}

// looksBinary reports whether a head chunk contains a NUL byte.
func looksBinary(b []byte) bool {
	if len(b) > 8000 {
		b = b[:8000]
	}
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// LeakScan walks tracked files and reports every line span matching a deny rule
// (user rules + built-ins) that no allow rule covers. Binary and ignored files
// (defaults + .docauditignore + extraIgnores) are skipped. History is never read.
func LeakScan(repoRoot string, rules LeakRules, extraIgnores []string) ([]LeakFinding, error) {
	rules.deny = append(rules.deny, builtinLeakRules()...)
	globs, err := loadIgnores(repoRoot, extraIgnores)
	if err != nil {
		return nil, err
	}
	files, err := gitLines(repoRoot, "ls-files")
	if err != nil {
		return nil, err
	}
	var findings []LeakFinding
	for _, f := range files {
		if matchesIgnore(f, globs) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if err != nil || looksBinary(b) {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			findings = append(findings, scanLine(f, i+1, line, rules)...)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Match < findings[j].Match
	})
	return findings, nil
}

// scanLine returns findings for one line: deny matches not covered by an allow
// span. A deny span [s,e) is covered iff some allow rule matches a span [as,ae)
// with as<=s && ae>=e (e.g. `lsjc` inside an allowed `au.lsjc.curator`).
func scanLine(file string, lineNo int, line string, rules LeakRules) []LeakFinding {
	var allowSpans [][]int
	for _, a := range rules.allow {
		allowSpans = append(allowSpans, a.re.FindAllStringIndex(line, -1)...)
	}
	covered := func(s, e int) bool {
		for _, sp := range allowSpans {
			if sp[0] <= s && sp[1] >= e {
				return true
			}
		}
		return false
	}
	var out []LeakFinding
	seen := map[string]bool{}
	for _, d := range rules.deny {
		for _, loc := range d.re.FindAllStringIndex(line, -1) {
			if covered(loc[0], loc[1]) {
				continue
			}
			m := line[loc[0]:loc[1]]
			key := fmt.Sprintf("%d:%s:%s", loc[0], m, d.raw)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, LeakFinding{File: file, Line: lineNo, Match: m, Pattern: d.raw})
		}
	}
	return out
}
