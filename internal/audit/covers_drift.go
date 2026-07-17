package audit

import "sort"

// CoversFinding is one doc whose `covers` edge points at code this change set
// modified, while the doc itself went untouched.
type CoversFinding struct {
	Doc   string   // repo-relative doc path
	Paths []string // changed code paths it covers, sorted
}

// CoversDrift reports docs that declare a `covers` edge onto code changed in
// spec but were not themselves changed in spec. It is the graph join `doc-drift`
// cannot do: DocDrift catches only what leaves a mechanical trace (a removed
// symbol, a changed literal), so a rewritten function whose doc describes the old
// behaviour in prose is invisible to it — but a `covers` edge declares that doc
// the architecture of record for the code regardless.
//
// Advisory by nature: it cannot judge whether the doc actually needed
// reconciling, so callers must never turn a finding into a non-zero exit.
//
// A doc touched in spec never fires — editing the doc is the intended escape
// hatch, which is why this check needs no suppression mechanism. docs is an
// already-parsed graph (see RepoDocs); malformed docs are absent from it and
// therefore declare no edges, which is the `frontmatter` check's concern, not
// this one's.
func CoversDrift(root, spec string, docs map[string]*Doc) ([]CoversFinding, error) {
	code, err := changedCode(root, spec)
	if err != nil {
		return nil, err
	}
	if len(code) == 0 {
		return nil, nil
	}
	touched, err := changedMarkdown(root, spec)
	if err != nil {
		return nil, err
	}
	touchedSet := make(map[string]bool, len(touched))
	for _, d := range touched {
		touchedSet[d] = true
	}

	byDoc := map[string][]string{}
	for _, path := range code {
		for _, doc := range CoversOf(docs, path) {
			if touchedSet[doc] {
				continue
			}
			byDoc[doc] = append(byDoc[doc], path)
		}
	}
	out := make([]CoversFinding, 0, len(byDoc))
	for doc, paths := range byDoc {
		sort.Strings(paths)
		out = append(out, CoversFinding{Doc: doc, Paths: paths})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Doc < out[j].Doc })
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
