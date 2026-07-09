package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// LeakConfig is the decoded global leaks.toml. Deny terms and their exceptions
// live ONLY here (never in a repo): a committed deny/allow list would re-leak the
// terms it names. Top-level fields apply to every scanned repo; [[dir]] sections
// scope exceptions to files under an absolute path.
type LeakConfig struct {
	Terms      []string  `toml:"terms"`       // literal, case-insensitive deny
	Regex      []string  `toml:"regex"`       // regexp deny (case-insensitive like terms; opt out per-pattern with (?-i))
	Allow      []string  `toml:"allow"`       // literal global allow
	AllowRegex []string  `toml:"allow_regex"` // regexp global allow (also case-insensitive)
	Dir        []DirRule `toml:"dir"`         // per-directory exceptions
}

// DirRule scopes exceptions to files whose absolute path is under Path.
type DirRule struct {
	Path       string   `toml:"path"`        // absolute directory key (a leading ~/ is expanded)
	Ignore     []string `toml:"ignore"`      // path globs (relative to Path) to skip
	Allow      []string `toml:"allow"`       // literal allow, scoped to this subtree
	AllowRegex []string `toml:"allow_regex"` // regexp allow, scoped to this subtree
}

// LeakFinding is one deny match not covered by an allow span.
type LeakFinding struct {
	File    string
	Line    int
	Match   string
	Pattern string
}

// matcher is a compiled deny/allow term. Both literal and regex terms compile
// case-insensitively (a leak must be caught in any casing); raw is the source
// string, shown in findings.
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

// regexMatcher compiles a user regexp case-insensitively by default: in a
// leak-prevention gate a false negative from a casing mismatch (a footprint term
// written `Nucleus` slipping a `nucleus` pattern) is the cardinal sin, so regex
// matches `terms`' case-folding. A pattern that genuinely needs case sensitivity
// opts out with an inline (?-i).
func regexMatcher(s string) (matcher, bool, error) {
	if strings.TrimSpace(s) == "" {
		return matcher{}, false, nil
	}
	re, err := regexp.Compile("(?i)" + s)
	if err != nil {
		return matcher{}, false, err
	}
	return matcher{re: re, raw: s}, true, nil
}

// builtinLeakMatchers are the always-on generic secret shapes — a small baseline
// net, not the primary feature. Suppressible by any allow/ignore. They stay
// case-sensitive: these are fixed-case token shapes (an AWS key id is uppercase,
// a `ghp_` prefix lowercase), so folding case would only add false positives.
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

// expandDirPath resolves a [[dir]] path key to a cleaned absolute path, expanding
// a leading ~/ to the home dir. A non-absolute path is a config error, not a
// silent no-op: dir-scoping matches by absolute-path containment, so a relative
// or unexpanded-~ path would quietly never apply — and a silently-dead exclusion
// in a gate is exactly what trains people to reach for --no-verify.
func expandDirPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("must be an absolute path")
	}
	return p, nil
}

// compile turns a LeakConfig into matchers. Literal entries never error; a bad
// regexp in any regex field, or a non-absolute [[dir]] path, is a fatal config error.
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
		path, err := expandDirPath(d.Path)
		if err != nil {
			return compiledLeaks{}, fmt.Errorf("leaks [[dir]] path %q: %v", d.Path, err)
		}
		cd := compiledDir{path: path, ignore: d.Ignore}
		addLit(&cd.allow, d.Allow)
		if err := addRe(&cd.allow, d.AllowRegex, fmt.Sprintf("leaks [[dir]] %q allow_regex", d.Path)); err != nil {
			return compiledLeaks{}, err
		}
		cl.dirs = append(cl.dirs, cd)
	}
	return cl, nil
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

// relUnder reports whether abs is within dir (or equals it) and returns abs
// relative to dir as a slash path, for glob matching.
func relUnder(abs, dir string) (string, bool) {
	if abs == dir {
		return ".", true
	}
	if !strings.HasPrefix(abs, dir+string(os.PathSeparator)) {
		return "", false
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// LeakScan walks every git-tracked, non-binary file and reports deny matches not
// covered by an allow span. Scope is git tracking (a tracked file ships publicly),
// not the doc-graph ignore layers. extraIgnores (--ignore CLI globs) and each
// applicable [[dir]].ignore drop a file entirely; global + dir-scoped allows
// suppress matches. History is never read. A bad regexp in the config is an error.
func LeakScan(repoRoot string, cfg LeakConfig, extraIgnores []string) ([]LeakFinding, error) {
	cl, err := cfg.compile()
	if err != nil {
		return nil, err
	}
	files, err := gitLines(repoRoot, "ls-files")
	if err != nil {
		return nil, err
	}
	var findings []LeakFinding
	for _, f := range files {
		if matchesIgnore(f, extraIgnores) {
			continue
		}
		abs := filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(f)))
		var dirAllows []matcher
		skip := false
		for _, d := range cl.dirs {
			rel, under := relUnder(abs, d.path)
			if !under {
				continue
			}
			if matchesIgnore(rel, d.ignore) {
				skip = true
				break
			}
			dirAllows = append(dirAllows, d.allow...)
		}
		if skip {
			continue
		}
		allow := cl.allow
		if len(dirAllows) > 0 {
			allow = append(append([]matcher{}, cl.allow...), dirAllows...)
		}
		b, err := os.ReadFile(abs)
		if err != nil || looksBinary(b) {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			findings = append(findings, scanLine(f, i+1, line, cl.deny, allow)...)
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
// span. A deny span [s,e) is covered iff some allow rule matches [as,ae) with
// as<=s && ae>=e (e.g. `lsjc` inside an allowed `au.lsjc.curator`).
func scanLine(file string, lineNo int, line string, deny, allow []matcher) []LeakFinding {
	var allowSpans [][]int
	for _, a := range allow {
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
	for _, d := range deny {
		for _, loc := range d.re.FindAllStringIndex(line, -1) {
			if loc[0] == loc[1] { // defensive: skip any zero-width match
				continue
			}
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
