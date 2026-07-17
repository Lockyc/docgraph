package audit

import "sort"

// CoversFinding is one doc whose `covers` edge points at code a pushed range
// modified, while the doc itself went untouched.
type CoversFinding struct {
	Doc   string   // repo-relative doc path
	Paths []string // changed code paths it covers, sorted
}

// CoversDrift reports docs that declare a `covers` edge onto code changed in any
// of the given ranges but were not themselves changed in that same range. It is
// the graph join `doc-drift` cannot do: DocDrift catches only what leaves a
// mechanical trace (a removed symbol, a changed literal), so a rewritten function
// whose doc describes the old behaviour in prose is invisible to it — but a
// `covers` edge declares that doc the architecture of record for the code
// regardless.
//
// Advisory by nature: it cannot judge whether the doc actually needed
// reconciling, so callers must never turn a finding into a non-zero exit.
//
// ranges mirrors FootgunDrift's: one pre-push invocation can push several refs,
// and findings dedupe across them. A doc touched in a range never fires for that
// range — editing the doc is the intended escape hatch, which is why this check
// needs no suppression mechanism. docs is an already-parsed graph (see RepoDocs);
// malformed docs are absent from it and therefore declare no edges, which is the
// `frontmatter` check's concern, not this one's.
func CoversDrift(root string, ranges []RevRange, docs map[string]*Doc) ([]CoversFinding, error) {
	byDoc := map[string]map[string]bool{}
	for _, r := range ranges {
		rng := r.Base + ".." + r.Head
		code, err := changedCode(root, rng)
		if err != nil {
			return nil, err
		}
		if len(code) == 0 {
			continue
		}
		touched, err := changedMarkdown(root, rng)
		if err != nil {
			return nil, err
		}
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
	if len(byDoc) == 0 {
		return nil, nil
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
	return out, nil
}
