package audit

import (
	"sort"
	"strconv"
)

// RevRange is a base..head pair to examine (base exclusive).
type RevRange struct {
	Base, Head string
}

// FootgunDrift returns every footgun declaration ADDED in any of the given
// ranges whose window lacks a rationale/marker. A declaration counts only if its
// anchor line is in that range's added-line set for the file (content read at
// Head). Findings are deduped by (file, line) and sorted.
func FootgunDrift(root string, ranges []RevRange) ([]FootgunFinding, error) {
	seen := map[string]bool{}
	var out []FootgunFinding
	for _, r := range ranges {
		rng := r.Base + ".." + r.Head
		files, err := changedMarkdown(root, rng)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			added, err := addedLines(root, rng, f)
			if err != nil {
				return nil, err
			}
			content, ok := fileAtRev(root, r.Head, f)
			if !ok {
				continue // deleted at head
			}
			for _, d := range scanDeclarations(content) {
				if !added[d.Line] {
					continue
				}
				key := f + ":" + strconv.Itoa(d.Line)
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, FootgunFinding{File: f, Line: d.Line, Text: d.Text})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}
