package audit

import (
	"regexp"
	"regexp/syntax"
)

// maxLitSet caps how many alternative strings a required-literal set may hold,
// so a class or alternation product can't grow into a set slower to search for
// than the regexp it stands in for.
const maxLitSet = 16

// minGateLen is the shortest required literal worth gating a rule on. A shorter
// one ("=", "@") is in nearly every file, so the gate would admit everything and
// the rule is left in the alternation instead.
const minGateLen = 3

// foldExempt are the two non-ASCII runes (?i) folds onto an ASCII letter (ſ → s,
// Kelvin K → k). A literal set may drop them because mayMatch admits any file
// containing either before any substring test runs.
func foldExempt(r rune) bool { return r == 0x17F || r == 0x212A }

// requiredLiterals returns a set of ASCII-lowercased strings such that every
// match of re contains at least one of them, or false when no such set can be
// derived. A file whose lowercased text holds none of them cannot match re, which
// is what lets scanFilter skip the regexp there: the derivation must only ever be
// sound (never claim a string a match could lack), and gives up rather than guess.
func requiredLiterals(re *regexp.Regexp) ([]string, bool) {
	ast, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return nil, false
	}
	set, ok := required(ast)
	if !ok || minLen(set) < minGateLen {
		return nil, false
	}
	return set, true
}

// required: every match of re contains some element of the returned set.
func required(re *syntax.Regexp) ([]string, bool) {
	if set, ok := exact(re); ok {
		return set, true
	}
	switch re.Op {
	case syntax.OpCapture:
		return required(re.Sub[0])
	case syntax.OpPlus:
		return required(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min >= 1 {
			return required(re.Sub[0])
		}
	case syntax.OpAlternate:
		var out []string
		for _, sub := range re.Sub {
			set, ok := required(sub)
			if !ok {
				return nil, false
			}
			out = union(out, set)
			if len(out) > maxLitSet {
				return nil, false
			}
		}
		return out, true
	case syntax.OpConcat:
		// Each run of consecutive exact children is matched verbatim, so the
		// product of their sets is required; so is any one child's required set.
		// Keep whichever candidate's shortest string is longest.
		var best []string
		consider := func(set []string) {
			if set != nil && (best == nil || minLen(set) > minLen(best)) {
				best = set
			}
		}
		run := []string{""}
		for _, sub := range re.Sub {
			if set, ok := exact(sub); ok {
				if p, ok := product(run, set); ok {
					run = p
					continue
				}
				consider(run)
				run = set
				continue
			}
			consider(run)
			run = []string{""}
			if set, ok := required(sub); ok {
				consider(set)
			}
		}
		consider(run)
		return best, best != nil
	}
	return nil, false
}

// exact: the complete set of strings re matches, when it is finite, small and
// ASCII (modulo foldExempt runes), lowercased.
func exact(re *syntax.Regexp) ([]string, bool) {
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return []string{""}, true
	case syntax.OpLiteral:
		b := make([]byte, 0, len(re.Rune))
		for _, r := range re.Rune {
			if r >= 0x80 {
				return nil, false
			}
			b = append(b, lowerASCII(byte(r)))
		}
		return []string{string(b)}, true
	case syntax.OpCharClass:
		var out []string
		for i := 0; i+1 < len(re.Rune); i += 2 {
			lo, hi := re.Rune[i], re.Rune[i+1]
			if hi-lo >= maxLitSet {
				return nil, false
			}
			for r := lo; r <= hi; r++ {
				if foldExempt(r) {
					continue
				}
				if r >= 0x80 {
					return nil, false
				}
				out = union(out, []string{string(lowerASCII(byte(r)))})
				if len(out) > maxLitSet {
					return nil, false
				}
			}
		}
		return out, len(out) > 0
	case syntax.OpCapture:
		return exact(re.Sub[0])
	case syntax.OpConcat:
		out := []string{""}
		for _, sub := range re.Sub {
			set, ok := exact(sub)
			if !ok {
				return nil, false
			}
			if out, ok = product(out, set); !ok {
				return nil, false
			}
		}
		return out, true
	case syntax.OpAlternate:
		var out []string
		for _, sub := range re.Sub {
			set, ok := exact(sub)
			if !ok {
				return nil, false
			}
			out = union(out, set)
			if len(out) > maxLitSet {
				return nil, false
			}
		}
		return out, true
	}
	return nil, false
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func product(a, b []string) ([]string, bool) {
	if len(a)*len(b) > maxLitSet {
		return nil, false
	}
	var out []string
	for _, x := range a {
		for _, y := range b {
			out = union(out, []string{x + y})
		}
	}
	return out, true
}

func union(a, b []string) []string {
	for _, s := range b {
		dup := false
		for _, t := range a {
			if s == t {
				dup = true
				break
			}
		}
		if !dup {
			a = append(a, s)
		}
	}
	return a
}

func minLen(set []string) int {
	m := -1
	for _, s := range set {
		if m < 0 || len(s) < m {
			m = len(s)
		}
	}
	return m
}
