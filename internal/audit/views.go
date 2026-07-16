package audit

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RepoDocs parses the frontmatter of every non-ignored tracked .md, returning the
// doc-graph nodes keyed by repo-relative slash path. It is the single read path
// the read-only views (covers/index/stale) share with the Audit check pass —
// malformed docs are omitted (their well-formedness is the `frontmatter` check's
// concern, not a view's).
func RepoDocs(repoRoot string, ignores []string) (map[string]*Doc, error) {
	tracked, err := trackedMD(repoRoot)
	if err != nil {
		return nil, err
	}
	globs, err := loadIgnores(repoRoot, ignores)
	if err != nil {
		return nil, err
	}
	docs, _ := parseDocs(repoRoot, tracked, globs)
	return docs, nil
}

// CoversOf returns the docs (sorted, repo-relative) that declare a `covers` edge
// resolving to target, or to a directory that contains target. target is
// normalized repo-root-relative; a doc covering a directory covers every path
// under it.
func CoversOf(docs map[string]*Doc, target string) []string {
	want := filepath.ToSlash(filepath.Clean(strings.TrimSpace(target)))
	var out []string
	for src, d := range docs {
		for _, e := range d.Links {
			if e.Rel != "covers" {
				continue
			}
			if k := ClassifyTarget(e.To); k != EdgeDoc && k != EdgeCode {
				continue
			}
			cov := ResolveEdgeTarget(e.To)
			if cov == want || strings.HasPrefix(want, cov+"/") {
				out = append(out, src)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// IndexMarkdown renders a markdown index of the doc graph: docs grouped by type
// (core types first in their canonical CoreTypes order, then custom types
// alphabetically), each listed with its label and description. The label is the
// doc's `title` if set, else its body H1, else its path — so a doc gets a
// readable index entry without restating its own heading in frontmatter. Only
// docs with frontmatter appear — they are the graph nodes. Intended to be
// redirected into an index.md.
func IndexMarkdown(docs map[string]*Doc) string {
	byType := map[string][]string{}
	for src, d := range docs {
		t := d.Type
		if strings.TrimSpace(t) == "" {
			t = "(untyped)"
		}
		byType[t] = append(byType[t], src)
	}
	seen := map[string]bool{}
	var order []string
	for _, t := range CoreTypes {
		if _, ok := byType[t]; ok {
			order = append(order, t)
			seen[t] = true
		}
	}
	var rest []string
	for t := range byType {
		if !seen[t] {
			rest = append(rest, t)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)

	var b strings.Builder
	b.WriteString("# Index\n")
	for _, t := range order {
		fmt.Fprintf(&b, "\n## %s\n\n", t)
		srcs := byType[t]
		sort.Strings(srcs)
		for _, src := range srcs {
			d := docs[src]
			title := d.Title
			if strings.TrimSpace(title) == "" {
				title = d.Heading
			}
			if strings.TrimSpace(title) == "" {
				title = src
			}
			if strings.TrimSpace(d.Description) != "" {
				fmt.Fprintf(&b, "- [%s](%s) — %s\n", title, src, d.Description)
			} else {
				fmt.Fprintf(&b, "- [%s](%s)\n", title, src)
			}
		}
	}
	return b.String()
}

// StaleFinding is a doc whose `verified` date is older than its staleness
// threshold (its own `review:` cadence if set, else the caller's default).
type StaleFinding struct {
	File      string
	Verified  string
	AgeDays   int
	Threshold int
}

// StaleDocs reports docs whose verified date is older than their threshold as of
// now. Threshold is the doc's `review:` cadence (e.g. "90d") if set and parseable,
// else defaultDays. Docs with no `verified`, or an unparseable verified/review,
// are skipped — malformed metadata is the frontmatter check's concern, not a view's.
func StaleDocs(docs map[string]*Doc, now time.Time, defaultDays int) []StaleFinding {
	var out []StaleFinding
	for src, d := range docs {
		v := strings.TrimSpace(d.Verified)
		if v == "" {
			continue
		}
		vt, err := time.Parse("2006-01-02", v)
		if err != nil {
			continue
		}
		threshold := defaultDays
		if days, ok := parseReviewDays(d.Review); ok {
			threshold = days
		}
		age := int(now.Sub(vt).Hours() / 24)
		if age > threshold {
			out = append(out, StaleFinding{File: src, Verified: v, AgeDays: age, Threshold: threshold})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// parseReviewDays parses a `review` cadence like "90d" or "2w" into days. Returns
// (0,false) for empty, missing-unit, or otherwise unrecognized forms.
func parseReviewDays(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	switch s[len(s)-1] {
	case 'd':
		return n, true
	case 'w':
		return n * 7, true
	default:
		return 0, false
	}
}
