package audit

import (
	"regexp"
	"strings"
)

// Dropped counts config rules that leaks-rules cannot express as git-filter-repo
// --replace-text lines. filter-repo rewrites by content across every path in all
// history, so it has no concept of an allowed span or a path-scoped exception.
type Dropped struct {
	Allows int // global allow + allow_regex, plus every [[dir]] allow/allow_regex
	Dirs   int // number of [[dir]] sections
}

// ReplaceTextRules translates a LeakConfig's DENY vocabulary into git-filter-repo
// --replace-text lines. Each line is `regex:<pat>`, using filter-repo's default
// ***REMOVED*** replacement. terms are regexp-escaped and made case-insensitive
// ((?i)); regex entries also get a (?i) prefix unless they already opt out with a
// leading (?-i) — mirroring regexMatcher's case-folding (leaks.go:60). A leading
// (?-i) is stripped rather than emitted verbatim: filter-repo compiles regex: lines
// with Python re, which rejects the bare (?-i) flag-clear that Go/RE2 accepts, so
// the opt-out is normalized to a plain case-sensitive pattern instead. Output is
// deterministic (terms then regex, in config order) and de-duped; whitespace-only
// entries are skipped, matching the scan.
//
// allow / allow_regex / [[dir]] rules have no filter-repo equivalent, so they are
// dropped and counted in Dropped. docaudit reads only this config — never history.
func ReplaceTextRules(cfg LeakConfig) (lines []string, dropped Dropped) {
	seen := map[string]bool{}
	add := func(line string) {
		if seen[line] {
			return
		}
		seen[line] = true
		lines = append(lines, line)
	}
	for _, t := range cfg.Terms {
		if strings.TrimSpace(t) == "" {
			continue
		}
		add("regex:(?i)" + regexp.QuoteMeta(t))
	}
	for _, r := range cfg.Regex {
		if strings.TrimSpace(r) == "" {
			continue
		}
		if strings.HasPrefix(r, "(?-i)") {
			// A leading (?-i) is docaudit's documented case-sensitive opt-out. But
			// git-filter-repo compiles with Python re, which REJECTS a bare (?-i)
			// flag-clear that Go/RE2 accepts — emitting the pattern verbatim aborts the
			// whole rewrite. Strip the flag and emit a plain case-sensitive rule, which
			// is Python-valid and matches the scan's semantics for a leading opt-out.
			add("regex:" + strings.TrimPrefix(r, "(?-i)"))
		} else {
			add("regex:(?i)" + r)
		}
	}
	dropped.Allows = countNonEmpty(cfg.Allow) + countNonEmpty(cfg.AllowRegex)
	dropped.Dirs = len(cfg.Dir)
	for _, d := range cfg.Dir {
		dropped.Allows += countNonEmpty(d.Allow) + countNonEmpty(d.AllowRegex)
	}
	return lines, dropped
}

func countNonEmpty(ss []string) int {
	n := 0
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			n++
		}
	}
	return n
}

// Validate reports whether cfg is well-formed — every regexp compiles and every
// [[dir]] path is absolute — by reusing the same compile step the leaks scan runs
// (leaks.go:105). leaks-rules treats a malformed config as fatal, exactly like the
// scan, so it validates before emitting.
func (c LeakConfig) Validate() error {
	_, err := c.compile()
	return err
}
