package audit

import (
	"regexp"
	"sort"
	"strings"
)

// constChange is a numeric constant whose value changed across a diff.
type constChange struct{ Name, Old string }

var (
	// Definition keywords across languages, capturing the declared identifier.
	defKW = regexp.MustCompile(`(?:export\s+)?(?:default\s+)?(?:public\s+)?(?:abstract\s+)?(?:async\s+)?(?:const|let|var|function|class|interface|type|enum|def|fn|func|struct|trait)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	// NAME <sep> NUMBER, sep in {= :}, number >=2 digits.
	assignRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*[:=]\s*([0-9][0-9]+)`)

	// Distinctive-symbol sub-checks (ported verbatim from doc-drift.sh).
	reAllSnake    = regexp.MustCompile(`^[A-Z0-9_]+$`)
	reHasUpper    = regexp.MustCompile(`[A-Z]`)
	rePascalShape = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	reHasLower    = regexp.MustCompile(`[a-z]`)
	reTwoHumps    = regexp.MustCompile(`[A-Z][a-z0-9]*[A-Z]`)
)

// looksLikeSymbol reports whether s is a doc-referenceable identifier:
// SCREAMING_SNAKE_CASE or multi-word PascalCase, length >=4. camelCase locals
// and single capitalized words are excluded (they collide with doc example keys).
func looksLikeSymbol(s string) bool {
	if len(s) < 4 {
		return false
	}
	if reAllSnake.MatchString(s) && strings.Contains(s, "_") && reHasUpper.MatchString(s) {
		return true
	}
	if rePascalShape.MatchString(s) && reHasLower.MatchString(s) && reTwoHumps.MatchString(s) {
		return true
	}
	return false
}

// diffIdents runs re over each removed (want='-') or added (want='+') diff body
// line (excluding the ---/+++ headers), returning capture group 1 of every match.
func diffIdents(diff string, want byte, re *regexp.Regexp) []string {
	var ids []string
	for _, line := range strings.Split(diff, "\n") {
		if len(line) == 0 || line[0] != want {
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(line[1:], -1) {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// removedNotReadded returns distinct definition identifiers removed in the diff
// and NOT re-added (a moved/renamed-in-place symbol still exists), sorted.
func removedNotReadded(diff string) []string {
	added := map[string]bool{}
	for _, id := range diffIdents(diff, '+', defKW) {
		added[id] = true
	}
	seen := map[string]bool{}
	var gone []string
	for _, id := range diffIdents(diff, '-', defKW) {
		if added[id] || seen[id] {
			continue
		}
		seen[id] = true
		gone = append(gone, id)
	}
	sort.Strings(gone)
	return gone
}

// changedConstants returns constants assigned a numeric literal on both diff
// sides with a differing value, sorted by name.
func changedConstants(diff string) []constChange {
	old := map[string]string{}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			for _, m := range assignRe.FindAllStringSubmatch(line[1:], -1) {
				old[m[1]] = m[2]
			}
		}
	}
	newv := map[string]string{}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			for _, m := range assignRe.FindAllStringSubmatch(line[1:], -1) {
				newv[m[1]] = m[2]
			}
		}
	}
	var out []constChange
	for name, o := range old {
		if n, ok := newv[name]; ok && n != o {
			out = append(out, constChange{Name: name, Old: o})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
