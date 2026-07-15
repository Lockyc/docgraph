package audit

import (
	"path/filepath"
	"sort"
	"strings"
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
