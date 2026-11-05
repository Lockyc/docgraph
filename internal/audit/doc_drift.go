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

	// Distinctive-symbol sub-checks.
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

// removedNotReadded returns distinct definition identifiers removed and NOT
// re-added (a moved/renamed-in-place symbol still exists), sorted. patches are
// applied oldest first and a symbol's last event decides, so one edited in one
// commit and deleted in a later one is gone, and one deleted then restored is not.
func removedNotReadded(patches ...string) []string {
	gone := map[string]bool{}
	for _, p := range patches {
		added := map[string]bool{}
		for _, id := range diffIdents(p, '+', defKW) {
			added[id] = true
		}
		for _, id := range diffIdents(p, '-', defKW) {
			if !added[id] {
				gone[id] = true
			}
		}
		for id := range added {
			delete(gone, id)
		}
	}
	out := make([]string, 0, len(gone))
	for id := range gone {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// diffAssigns maps each constant assigned a numeric literal on the want ('-' or
// '+') side of a diff to its value; the last assignment wins.
func diffAssigns(diff string, want byte) map[string]string {
	vals := map[string]string{}
	for _, line := range strings.Split(diff, "\n") {
		if len(line) == 0 || line[0] != want || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		for _, m := range assignRe.FindAllStringSubmatch(line[1:], -1) {
			vals[m[1]] = m[2]
		}
	}
	return vals
}

// changedConstants returns constants whose numeric value differs between the
// first value the patches remove and the last value they leave, sorted by name.
// patches are oldest first, so a value changed and changed back is unchanged, and
// Old is the value before the first change, never an intermediate one.
func changedConstants(patches ...string) []constChange {
	first, last := map[string]string{}, map[string]string{}
	for _, p := range patches {
		old, newv := diffAssigns(p, '-'), diffAssigns(p, '+')
		for name, v := range old {
			if _, ok := first[name]; !ok {
				first[name] = v
			}
			if _, ok := newv[name]; !ok {
				delete(last, name)
			}
		}
		for name, v := range newv {
			last[name] = v
		}
	}
	var out []constChange
	for name, o := range first {
		if n, ok := last[name]; ok && n != o {
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

// nonCodePathspec excludes tracked doc/prose formats from the CODE side of the
// diff and the still-defined grep. A definition or constant that changed only
// inside prose is the prose being edited, not code drifting from a doc — and
// def-keyword-shaped English (e.g. "…the type OrderManager…" in a CHANGELOG) or
// a fenced code example must not be read as a real definition. gitDiff and
// stillDefinedInCode MUST share this exact set: they answer "what code changed?"
// and "does this symbol still exist in code?", so a mismatch would report a
// symbol as gone from a file class the diff never scanned. Docs are only ever
// searched for *references* by the doc-GREP side (docGrepSymbol/docGrepValue),
// which scans *.md/*.mdx directly, on purpose.
var nonCodePathspec = []string{".", ":!*.md", ":!*.mdx", ":!*.txt", ":!*.rst", ":!*.adoc", ":!*.markdown"}

// ignoreExcludes turns the repo's doc-graph ignore layers (defaultIgnores +
// .docgraphignore) into git exclude pathspecs, appended to BOTH sides of
// doc-drift: an ignored tree is declared not agent-facing (frozen archives, seed
// data, generated exports), so it is neither a doc that can go stale nor code
// whose removed definitions a doc should stop describing.
func ignoreExcludes(root string) ([]string, error) {
	globs, err := loadIgnores(root, nil)
	if err != nil {
		return nil, err
	}
	excl := make([]string, len(globs))
	for i, g := range globs {
		excl[i] = ":(exclude,glob)" + g
	}
	return excl, nil
}

// gitDiff returns the unified diff of the CODE side of `git diff <spec>`, scoped
// by nonCodePathspec. spec may be a base SHA (diffs base vs the WORKING TREE —
// committed and uncommitted), "HEAD" (uncommitted only), or "base..head"
// (committed only).
func gitDiff(root, spec string, excl ...string) (string, error) {
	args := append(append([]string{"diff", spec, "--"}, nonCodePathspec...), excl...)
	out, err := GitCmd(root, args...).Output()
	if err != nil {
		return "", gitError(err)
	}
	return string(out), nil
}

// stillDefinedInCode reports whether sym appears (whole-word) anywhere in tracked
// or untracked, non-ignored code — the file set the bare doc-drift diff covers,
// scoped by nonCodePathspec. A fixed-string word match; a regex alternation
// backtracks catastrophically on a large tree.
func stillDefinedInCode(root, sym string, excl ...string) bool {
	args := append(append([]string{"grep", "--untracked", "-qwF", "--", sym, "--"}, nonCodePathspec...), excl...)
	return GitCmd(root, args...).Run() == nil
}

// gitGrepHits runs `git grep -n -F -w` with the given pathspec args and parses
// file:line:text, capping at max. git grep exit 1 (no match) yields (nil, nil).
func gitGrepHits(root string, pathspec []string, needle string, max int) ([]DocHit, error) {
	args := append([]string{"grep", "-n", "-F", "-w", "--", needle, "--"}, pathspec...)
	out, err := GitCmd(root, args...).Output()
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
func docGrepSymbol(root, sym string, excl ...string) ([]DocHit, error) {
	return gitGrepHits(root, append([]string{"*.md", "*.mdx"}, excl...), sym, 5)
}

// docGrepValue returns up to 5 doc locations that carry old, but only within docs
// that also NAME the symbol (word match) — the anchored-drift signature.
func docGrepValue(root, name, old string, excl ...string) ([]DocHit, error) {
	args := append([]string{"grep", "-l", "-F", "-w", "--", name, "--", "*.md", "*.mdx"}, excl...)
	named, err := GitCmd(root, args...).Output()
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
	d, err := DocDriftDiff(root, spec)
	if err != nil {
		return nil, err
	}
	return d.Findings()
}

// CodeDiff is the code-side diff doc-drift scans, held so a caller can decide
// from Text whether the doc greps are worth running before it runs them. patches
// is the same diff split per commit, oldest first; Text is their concatenation.
type CodeDiff struct {
	Text    string
	patches []string
	root    string
	excl    []string
}

// DocDriftDiff collects the code-side `git diff <spec>`, scoped by
// nonCodePathspec and the repo's ignore layers.
func DocDriftDiff(root, spec string) (CodeDiff, error) {
	excl, err := ignoreExcludes(root)
	if err != nil {
		return CodeDiff{}, err
	}
	diff, err := gitDiff(root, spec, excl...)
	if err != nil {
		return CodeDiff{}, err
	}
	return CodeDiff{Text: diff, patches: []string{diff}, root: root, excl: excl}, nil
}

// SessionDocDriftDiff collects the code-side patches of the commits revArgs
// selects — first-parent, non-merge, the same selection CommitChanges makes for
// covers drift — followed by the uncommitted diff against HEAD. It is the trunk's
// change set: there the merge-base is HEAD itself, so a plain diff would see only
// what the session has not yet committed.
func SessionDocDriftDiff(root string, revArgs []string) (CodeDiff, error) {
	excl, err := ignoreExcludes(root)
	if err != nil {
		return CodeDiff{}, err
	}
	args := append([]string{"log", "--reverse", "--first-parent", "--no-merges", "-p", "--format=%x00"}, revArgs...)
	args = append(append(append(args, "--"), nonCodePathspec...), excl...)
	committed, err := GitCmd(root, args...).Output()
	if err != nil {
		return CodeDiff{}, gitError(err)
	}
	worktree, err := gitDiff(root, "HEAD", excl...)
	if err != nil {
		return CodeDiff{}, err
	}
	var patches []string
	for _, p := range strings.Split(string(committed), "\x00") {
		if strings.TrimSpace(p) != "" {
			patches = append(patches, p)
		}
	}
	patches = append(patches, worktree)
	return CodeDiff{Text: strings.Join(patches, ""), patches: patches, root: root, excl: excl}, nil
}

// Findings runs DocDrift's two scans over the collected diff.
func (d CodeDiff) Findings() ([]DocDriftFinding, error) {
	root, excl := d.root, d.excl
	if strings.TrimSpace(d.Text) == "" {
		return nil, nil
	}
	var out []DocDriftFinding

	// (A) dangling references
	n := 0
	for _, sym := range removedNotReadded(d.patches...) {
		if !looksLikeSymbol(sym) || stillDefinedInCode(root, sym, excl...) {
			continue
		}
		if n++; n > 40 {
			break
		}
		hits, err := docGrepSymbol(root, sym, excl...)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			out = append(out, DocDriftFinding{Symbol: sym, Kind: Dangling, Hits: hits})
		}
	}

	// (B) anchored value drift
	m := 0
	for _, c := range changedConstants(d.patches...) {
		if !looksLikeSymbol(c.Name) {
			continue
		}
		if m++; m > 40 {
			break
		}
		hits, err := docGrepValue(root, c.Name, c.Old, excl...)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			out = append(out, DocDriftFinding{Symbol: c.Name, Kind: ValueDrift, Old: c.Old, Hits: hits})
		}
	}
	return out, nil
}
