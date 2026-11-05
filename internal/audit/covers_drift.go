package audit

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// CoversFinding is one doc whose `covers` edge points at code a change set
// modified, while the doc itself went untouched.
type CoversFinding struct {
	Doc   string   // repo-relative doc path
	Paths []string // changed code paths it covers, sorted
	Lines []DocHit // doc lines whose code names an identifier the change touched (PointCovers)
}

// identRe matches a code identifier; pointIdent decides which ones are
// distinctive enough to point at a doc line.
var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// pointIdent keeps identifiers with an upper-case letter or an inner underscore:
// a plain lowercase word (user, return, token) appears in every code block of
// every doc, so pointing on one points at everything.
func pointIdent(id string) bool {
	return len(id) >= 4 && (strings.ContainsAny(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || strings.Contains(id[1:], "_"))
}

// changeIdents maps each file in a unified diff to the distinctive identifiers
// the change added or removed: per hunk, those whose count differs between its
// removed and added lines, so a token a restyled or reflowed line merely carries
// over is not a change. A hunk that changed one also contributes its header's
// enclosing-function context, which names the function a change sits in even
// when no changed line does.
func changeIdents(diff string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	var cur map[string]bool
	var context string
	minus, plus := map[string]int{}, map[string]int{}
	minusLines, plusLines := map[string]bool{}, map[string]bool{}
	count := func(s string, into map[string]int, lines map[string]bool) {
		lines[strings.TrimSpace(s)] = true
		for _, id := range identRe.FindAllString(s, -1) {
			if pointIdent(id) {
				into[id]++
			}
		}
	}
	// A definition line that differs between the sides changed what it defines
	// (a constant's value, a signature) even though its name is on both.
	defs := func(lines, other map[string]bool) (found bool) {
		for l := range lines {
			if other[l] {
				continue
			}
			for _, m := range defKW.FindAllStringSubmatch(l, -1) {
				if pointIdent(m[1]) {
					cur[m[1]], found = true, true
				}
			}
		}
		return found
	}
	flush := func() {
		changed := false
		if cur != nil {
			changed = defs(plusLines, minusLines)
			changed = defs(minusLines, plusLines) || changed
			for id, n := range plus {
				if minus[id] != n {
					cur[id], changed = true, true
				}
			}
			for id, n := range minus {
				if plus[id] != n {
					cur[id], changed = true, true
				}
			}
			if changed {
				for _, id := range identRe.FindAllString(context, -1) {
					if pointIdent(id) {
						cur[id] = true
					}
				}
			}
		}
		minus, plus, context = map[string]int{}, map[string]int{}, ""
		minusLines, plusLines = map[string]bool{}, map[string]bool{}
	}
	setFile := func(p string) {
		if out[p] == nil {
			out[p] = map[string]bool{}
		}
		cur = out[p]
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			flush()
			if p := strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/"); p != "/dev/null" {
				setFile(p)
			}
		case strings.HasPrefix(line, "--- "):
			// The +++ header that follows names the file; a deletion's is /dev/null,
			// so key it by the old path instead.
			flush()
			if p := strings.TrimPrefix(strings.TrimPrefix(line, "--- "), "a/"); p != "/dev/null" {
				setFile(p)
			}
		case strings.HasPrefix(line, "@@"):
			flush()
			if i := strings.Index(line[2:], "@@"); i >= 0 {
				context = line[2+i+2:]
			}
		case strings.HasPrefix(line, "+"):
			count(line[1:], plus, plusLines)
		case strings.HasPrefix(line, "-"):
			count(line[1:], minus, minusLines)
		}
	}
	flush()
	return out
}

// CoversChangeDiff returns the unified diff of paths over a change set: the
// commits revArgs selects (`git log -p`, none when revArgs is nil) followed by the
// working tree against HEAD, with an untracked path rendered as all-added.
func CoversChangeDiff(root string, revArgs []string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	var b strings.Builder
	if revArgs != nil {
		args := append(append([]string{"log", "--first-parent", "--no-merges", "-p", "--format="}, revArgs...), "--")
		out, err := GitCmd(root, append(args, paths...)...).Output()
		if err != nil {
			return "", gitError(err)
		}
		b.Write(out)
	}
	out, err := GitCmd(root, append([]string{"diff", "HEAD", "--"}, paths...)...).Output()
	if err != nil {
		return "", gitError(err)
	}
	b.Write(out)
	untracked, err := gitLines(root, append([]string{"ls-files", "--others", "--exclude-standard", "--"}, paths...)...)
	if err != nil {
		return "", err
	}
	for _, p := range untracked {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		b.WriteString("+++ b/" + p + "\n")
		for _, l := range strings.Split(string(content), "\n") {
			b.WriteString("+" + l + "\n")
		}
	}
	return b.String(), nil
}

// RangeChangeDiff is CoversChangeDiff for a pushed range: `git diff spec -- paths`.
func RangeChangeDiff(root, spec string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	out, err := GitCmd(root, append([]string{"diff", spec, "--"}, paths...)...).Output()
	if err != nil {
		return "", gitError(err)
	}
	return string(out), nil
}

// maxPointedLines caps the lines listed per doc; past it the nag is no longer a
// short list to check.
const maxPointedLines = 8

// PointCovers fills each finding's Lines with the doc lines whose code — an
// inline code span, or a line inside a fenced block — names an identifier the
// diff changed in one of the finding's paths. Prose mentions are left out: a doc
// referring to code names it in code. An unreadable doc is left unpointed.
func PointCovers(root string, fs []CoversFinding, diff string) []CoversFinding {
	idents := changeIdents(diff)
	for i := range fs {
		want := map[string]bool{}
		for _, p := range fs[i].Paths {
			for id := range idents[p] {
				want[id] = true
			}
		}
		content, err := readFile(root, fs[i].Doc)
		if err != nil || len(want) == 0 {
			continue
		}
		fs[i].Lines = pointedLines(fs[i].Doc, content, want)
	}
	return fs
}

// maxIdentLines is how many of a doc's code lines an identifier may name and
// still point anywhere: past it the identifier is the doc's subject (a namespace,
// the main class), named on every other line, not one claim to check.
const maxIdentLines = 3

func pointedLines(doc, content string, want map[string]bool) []DocHit {
	prose := map[int]bool{}
	for i := range proseLines(content) {
		prose[i] = true
	}
	lines := strings.Split(content, "\n")
	byLine := make([][]string, len(lines)) // the wanted identifiers each line's code names
	freq := map[string]int{}
	for i, line := range lines {
		code := line // inside a fenced block, the whole line is code
		if prose[i] {
			code = inlineCode(line)
		} else if fenceRe.MatchString(strings.TrimSpace(line)) {
			continue // the fence marker line itself
		}
		seen := map[string]bool{}
		for _, id := range identRe.FindAllString(code, -1) {
			if want[id] && !seen[id] {
				seen[id] = true
				byLine[i] = append(byLine[i], id)
				freq[id]++
			}
		}
	}
	var hits []DocHit
	for i, ids := range byLine {
		for _, id := range ids {
			if freq[id] <= maxIdentLines {
				hits = append(hits, DocHit{File: doc, Line: i + 1, Text: strings.TrimSpace(lines[i])})
				break
			}
		}
		if len(hits) == maxPointedLines {
			break
		}
	}
	return hits
}

// inlineCode returns the text inside a line's `code spans`, space-joined.
func inlineCode(s string) string {
	parts := strings.Split(s, "`")
	var b strings.Builder
	for i := 1; i < len(parts); i += 2 {
		b.WriteString(parts[i])
		b.WriteByte(' ')
	}
	return b.String()
}

// CoversDrift reports docs that declare a `covers` edge onto code changed in any
// of the given ranges but were not themselves changed in that same range. It is
// the graph join `doc-drift`'s symbol scan cannot do: that scan catches only what
// leaves a mechanical trace (a removed symbol, a changed literal), so a rewritten
// function whose doc describes the old behaviour in prose is invisible to it — but
// a `covers` edge declares that doc the architecture of record for the code
// regardless.
//
// It cannot judge whether the doc actually needed reconciling, so a finding is a
// prompt to read the doc, never a verdict on it.
//
// ranges mirrors FootgunDrift's: one pre-push invocation can push several refs,
// and findings dedupe across them. A doc touched in a range never fires for that
// range. docs is an already-parsed graph (see RepoDocs); malformed docs are absent
// from it and therefore declare no edges, which is the `frontmatter` check's
// concern, not this one's.
func CoversDrift(root string, ranges []RevRange, docs map[string]*Doc) ([]CoversFinding, error) {
	byDoc := map[string]map[string]bool{}
	for _, r := range ranges {
		code, touched, err := SpecChanges(root, r.Base+".."+r.Head)
		if err != nil {
			return nil, err
		}
		joinCovers(byDoc, docs, code, touched)
	}
	return coversFindings(byDoc), nil
}

// CoversOfChanges is the same join over one already-collected change set: the
// code paths changed and the markdown touched anywhere in it. A doc touched
// anywhere in the set clears every finding against it, whichever commit of the
// set changed the code.
func CoversOfChanges(docs map[string]*Doc, code, touched []string) []CoversFinding {
	byDoc := map[string]map[string]bool{}
	joinCovers(byDoc, docs, code, touched)
	return coversFindings(byDoc)
}

func joinCovers(byDoc map[string]map[string]bool, docs map[string]*Doc, code, touched []string) {
	touchedSet := make(map[string]bool, len(touched))
	for _, d := range touched {
		touchedSet[d] = true
	}
	for _, path := range code {
		for _, doc := range CoversOf(docs, path) {
			if touchedSet[doc] {
				continue
			}
			if byDoc[doc] == nil {
				byDoc[doc] = map[string]bool{}
			}
			byDoc[doc][path] = true
		}
	}
}

func coversFindings(byDoc map[string]map[string]bool) []CoversFinding {
	if len(byDoc) == 0 {
		return nil
	}
	out := make([]CoversFinding, 0, len(byDoc))
	for doc, pathSet := range byDoc {
		paths := make([]string, 0, len(pathSet))
		for p := range pathSet {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		out = append(out, CoversFinding{Doc: doc, Paths: paths})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Doc < out[j].Doc })
	return out
}
