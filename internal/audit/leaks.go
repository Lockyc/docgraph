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
		if pat == "" || strings.HasPrefix(pat, "#") {
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
		`AKIA[0-9A-Z]{16}`,                   // AWS access key id
		`ghp_[A-Za-z0-9]{36}`,                // GitHub personal access token
		`xox[baprs]-[A-Za-z0-9-]{10,}`,       // Slack token
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

// LeakScan walks every git-tracked file and reports every line span matching a
// deny rule (user rules + built-ins) that no allow rule covers. Binary files are
// skipped. Scope is governed by git tracking, not the doc-graph ignore layers: a
// tracked file ships publicly, so it is in-scope regardless of defaultIgnores or
// .docauditignore (both are doc-graph-scoped and do not apply here) — a tracked
// .claude/ config still ships and is exactly where owner-specific strings hide.
// Only the explicit extraIgnores (the CLI --ignore flag) narrows the scan, as a
// per-run escape hatch. History is never read.
func LeakScan(repoRoot string, rules LeakRules, extraIgnores []string) ([]LeakFinding, error) {
	deny := append(append([]LeakRule{}, rules.deny...), builtinLeakRules()...)
	scanRules := LeakRules{deny: deny, allow: rules.allow}
	files, err := gitLines(repoRoot, "ls-files")
	if err != nil {
		return nil, err
	}
	var findings []LeakFinding
	for _, f := range files {
		if matchesIgnore(f, extraIgnores) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if err != nil || looksBinary(b) {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			findings = append(findings, scanLine(f, i+1, line, scanRules)...)
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

// LeakConfig is the decoded global leaks.toml. Deny terms and their exceptions
// live ONLY here (never in a repo): a committed deny/allow list would re-leak the
// terms it names. Top-level fields apply to every scanned repo; [[dir]] sections
// scope exceptions to files under an absolute path.
type LeakConfig struct {
	Terms      []string  `toml:"terms"`       // literal, case-insensitive deny
	Regex      []string  `toml:"regex"`       // Go-regexp deny (opt into (?i) yourself)
	Allow      []string  `toml:"allow"`       // literal global allow
	AllowRegex []string  `toml:"allow_regex"` // regexp global allow
	Dir        []DirRule `toml:"dir"`         // per-directory exceptions
}

// DirRule scopes exceptions to files whose absolute path is under Path.
type DirRule struct {
	Path       string   `toml:"path"`        // absolute directory key
	Ignore     []string `toml:"ignore"`      // path globs (relative to Path) to skip
	Allow      []string `toml:"allow"`       // literal allow, scoped to this subtree
	AllowRegex []string `toml:"allow_regex"` // regexp allow, scoped to this subtree
}

// matcher is a compiled deny/allow term. Literal terms compile to a
// case-insensitive quoted regexp; regex terms compile as-is. raw is shown in findings.
type matcher struct {
	re  *regexp.Regexp
	raw string
}

func literalMatcher(s string) (matcher, bool) {
	if strings.TrimSpace(s) == "" {
		return matcher{}, false
	}
	return matcher{re: regexp.MustCompile("(?i)" + regexp.QuoteMeta(s)), raw: s}, true
}

func regexMatcher(s string) (matcher, bool, error) {
	if strings.TrimSpace(s) == "" {
		return matcher{}, false, nil
	}
	re, err := regexp.Compile(s)
	if err != nil {
		return matcher{}, false, err
	}
	return matcher{re: re, raw: s}, true, nil
}

// builtinLeakMatchers are the always-on generic secret shapes — a small baseline
// net, not the primary feature. Suppressible by any allow/ignore.
func builtinLeakMatchers() []matcher {
	pats := []string{
		`-----BEGIN [A-Z ]*PRIVATE KEY-----`,
		`AKIA[0-9A-Z]{16}`,
		`ghp_[A-Za-z0-9]{36}`,
		`xox[baprs]-[A-Za-z0-9-]{10,}`,
	}
	ms := make([]matcher, 0, len(pats))
	for _, p := range pats {
		ms = append(ms, matcher{re: regexp.MustCompile(p), raw: p})
	}
	return ms
}

type compiledDir struct {
	path   string // cleaned absolute
	ignore []string
	allow  []matcher
}

type compiledLeaks struct {
	deny  []matcher // global terms + regex + built-ins
	allow []matcher // global allow + allow_regex
	dirs  []compiledDir
}

// compile turns a LeakConfig into matchers. Literal entries never error; a bad
// regexp in any regex field is a fatal config error.
func (c LeakConfig) compile() (compiledLeaks, error) {
	var cl compiledLeaks
	addLit := func(dst *[]matcher, ss []string) {
		for _, s := range ss {
			if m, ok := literalMatcher(s); ok {
				*dst = append(*dst, m)
			}
		}
	}
	addRe := func(dst *[]matcher, ss []string, what string) error {
		for _, s := range ss {
			m, ok, err := regexMatcher(s)
			if err != nil {
				return fmt.Errorf("%s %q: %v", what, s, err)
			}
			if ok {
				*dst = append(*dst, m)
			}
		}
		return nil
	}
	addLit(&cl.deny, c.Terms)
	if err := addRe(&cl.deny, c.Regex, "leaks regex"); err != nil {
		return compiledLeaks{}, err
	}
	cl.deny = append(cl.deny, builtinLeakMatchers()...)
	addLit(&cl.allow, c.Allow)
	if err := addRe(&cl.allow, c.AllowRegex, "leaks allow_regex"); err != nil {
		return compiledLeaks{}, err
	}
	for _, d := range c.Dir {
		cd := compiledDir{path: filepath.Clean(d.Path), ignore: d.Ignore}
		addLit(&cd.allow, d.Allow)
		if err := addRe(&cd.allow, d.AllowRegex, fmt.Sprintf("leaks [[dir]] %q allow_regex", d.Path)); err != nil {
			return compiledLeaks{}, err
		}
		cl.dirs = append(cl.dirs, cd)
	}
	return cl, nil
}
