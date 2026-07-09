package audit

import (
	"regexp"
	"strings"
)

// FootgunFinding is one newly-added footgun declaration whose window lacks a
// nearby rationale signal. File/Line locate it; Text is the trimmed declaration
// line.
type FootgunFinding struct {
	File string
	Line int
	Text string
}

// declFinding is scanDeclarations' per-content result (no file — the diff layer
// adds it). Line is 1-based.
type declFinding struct {
	Line int
	Text string
}

// A footgun DECLARATION (footgun as the subject being introduced), not a passing
// mention: line-leading `Footgun:`/`—` after optional markdown markers, OR a
// bolded `**Footgun:`/`—` anywhere. Cross-references ("see the X footgun") and
// bare container headings ("## Footguns", no delimiter) deliberately do NOT match.
var (
	footgunDeclLead = regexp.MustCompile(`(?i)^\s*(?:>[ \t]*)*(?:#{1,6}\s*|[-*+]\s+)?\*{0,2}footguns?\s*[:—-]`)
	footgunDeclBold = regexp.MustCompile(`(?i)\*\*\s*footguns?\s*[:—-]`)
	footgunHeading  = regexp.MustCompile(`^\s*#`)
	// Conservative built-in rationale vocabulary — single source of truth; do not
	// restate in docs. Narrow on purpose: loose connectives fire everywhere.
	footgunRationaleSignals = regexp.MustCompile(
		`(?i)(because|otherwise|so that|the reason|would break|re-?litigat|the trap)`)
)

func isFootgunDeclaration(line string) bool {
	return footgunDeclLead.MatchString(line) || footgunDeclBold.MatchString(line)
}

// scanDeclarations reports every footgun declaration whose window lacks a
// nearby rationale signal. A declaration's window starts at its own line
// and is bounded by the NEXT declaration in the same paragraph (so a justified
// sibling in a tight bullet list cannot suppress an unjustified neighbour). If
// the declaration owns the tail of its paragraph and that paragraph is a lone
// line or a heading, the window extends into the next paragraph — so a heading
// declaration sees the explanation that follows it.
func scanDeclarations(content string) []declFinding {
	lines := strings.Split(content, "\n")
	type para struct{ start, end int } // end exclusive
	var paras []para
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		start := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			i++
		}
		paras = append(paras, para{start, i})
	}
	paraOf := make(map[int]int)
	for pi, p := range paras {
		for l := p.start; l < p.end; l++ {
			paraOf[l] = pi
		}
	}
	var out []declFinding
	for ln, line := range lines {
		if !isFootgunDeclaration(line) {
			continue
		}
		pi := paraOf[ln]
		p := paras[pi]
		boundEnd := p.end
		for k := ln + 1; k < p.end; k++ {
			if isFootgunDeclaration(lines[k]) {
				boundEnd = k
				break
			}
		}
		text := strings.Join(lines[ln:boundEnd], "\n")
		if boundEnd == p.end {
			single := boundEnd-ln == 1
			heading := footgunHeading.MatchString(line)
			if (single || heading) && pi+1 < len(paras) {
				np := paras[pi+1]
				text += "\n" + strings.Join(lines[np.start:np.end], "\n")
			}
		}
		if footgunRationaleSignals.MatchString(text) {
			continue
		}
		out = append(out, declFinding{Line: ln + 1, Text: strings.TrimSpace(line)})
	}
	return out
}
