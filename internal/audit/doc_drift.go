package audit

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// DocHit is one doc location referencing a stale symbol or value.
type DocHit struct {
	File string
	Line int
	Text string
}

// gitDiff returns the unified diff of `git diff <spec>`. spec may be a base SHA
// (diffs base vs the WORKING TREE — committed and uncommitted), "HEAD"
// (uncommitted only), or "base..head" (committed only).
func gitDiff(root, spec string) (string, error) {
	out, err := exec.Command("git", "-C", root, "diff", spec).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// stillDefinedInCode reports whether sym appears (whole-word) anywhere in tracked
// NON-doc files. A fixed-string word match; a regex alternation backtracks
// catastrophically on a large tree.
func stillDefinedInCode(root, sym string) bool {
	return exec.Command("git", "-C", root, "grep", "-qwF", "--", sym,
		"--", ".", ":!*.md", ":!*.mdx").Run() == nil
}

// gitGrepHits runs `git grep -n -F -w` with the given pathspec args and parses
// file:line:text, capping at max. git grep exit 1 (no match) yields (nil, nil).
func gitGrepHits(root string, pathspec []string, needle string, max int) ([]DocHit, error) {
	args := append([]string{"-C", root, "grep", "-n", "-F", "-w", "--", needle, "--"}, pathspec...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var hits []DocHit
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		j := strings.IndexByte(line[i+1:], ':')
		if j < 0 {
			continue
		}
		j += i + 1
		ln, e := strconv.Atoi(line[i+1 : j])
		if e != nil {
			continue
		}
		hits = append(hits, DocHit{
			File: filepath.ToSlash(line[:i]),
			Line: ln,
			Text: strings.TrimSpace(line[j+1:]),
		})
		if len(hits) >= max {
			break
		}
	}
	return hits, nil
}

// docGrepSymbol returns up to 5 doc locations naming sym.
func docGrepSymbol(root, sym string) ([]DocHit, error) {
	return gitGrepHits(root, []string{"*.md", "*.mdx"}, sym, 5)
}

// docGrepValue returns up to 5 doc locations that carry old, but only within docs
// that also NAME the symbol (word match) — the anchored-drift signature.
func docGrepValue(root, name, old string) ([]DocHit, error) {
	named, err := exec.Command("git", "-C", root, "grep", "-l", "-F", "-w", "--", name,
		"--", "*.md", "*.mdx").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var hits []DocHit
	for _, d := range strings.Split(strings.TrimSpace(string(named)), "\n") {
		if d == "" {
			continue
		}
		h, err := gitGrepHits(root, []string{filepath.ToSlash(d)}, old, 5)
		if err != nil {
			return nil, err
		}
		hits = append(hits, h...)
		if len(hits) >= 5 {
			hits = hits[:5]
			break
		}
	}
	return hits, nil
}

// DriftKind distinguishes the two mechanical staleness classes.
type DriftKind int

const (
	Dangling   DriftKind = iota // symbol definition removed, doc still names it
	ValueDrift                  // constant value changed, doc still shows the old literal
)

// DocDriftFinding groups one stale symbol with the doc locations referencing it.
// Old is the previous literal (ValueDrift only).
type DocDriftFinding struct {
	Symbol string
	Kind   DriftKind
	Old    string
	Hits   []DocHit
}

// DocDrift scans `git diff <spec>` for (A) definitions removed on this change set
// that survive nowhere in code yet are still named in a tracked doc, and (B)
// constants whose value changed while a doc still names the symbol and shows the
// old literal. spec is passed straight to `git diff` (see gitDiff).
func DocDrift(root, spec string) ([]DocDriftFinding, error) {
	diff, err := gitDiff(root, spec)
	if err != nil {
		return nil, err
	}
	if diff == "" {
		return nil, nil
	}
	var out []DocDriftFinding

	// (A) dangling references
	n := 0
	for _, sym := range removedNotReadded(diff) {
		if !looksLikeSymbol(sym) || stillDefinedInCode(root, sym) {
			continue
		}
		if n++; n > 40 {
			break
		}
		hits, err := docGrepSymbol(root, sym)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			out = append(out, DocDriftFinding{Symbol: sym, Kind: Dangling, Hits: hits})
		}
	}

	// (B) anchored value drift
	m := 0
	for _, c := range changedConstants(diff) {
		if !looksLikeSymbol(c.Name) {
			continue
		}
		if m++; m > 40 {
			break
		}
		hits, err := docGrepValue(root, c.Name, c.Old)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			out = append(out, DocDriftFinding{Symbol: c.Name, Kind: ValueDrift, Old: c.Old, Hits: hits})
		}
	}
	return out, nil
}
