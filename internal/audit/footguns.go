package audit

import (
	"regexp"
	"strings"
)

// FootgunFinding is one newly-added footgun declaration. File/Line locate it;
// Text is the trimmed declaration line.
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
	footgunDeclLead = regexp.MustCompile(`(?i)^\s*(?:>[ \t]*)*(?:#{1,6}\s*|[-*+]\s+)?\*{0,2}footguns?\s*(?::|—|-(?:\s|$))`)
	footgunDeclBold = regexp.MustCompile(`(?i)\*\*\s*footguns?\s*(?::|—|-(?:\s|$))`)
)

func isFootgunDeclaration(line string) bool {
	return footgunDeclLead.MatchString(line) || footgunDeclBold.MatchString(line)
}

// scanDeclarations reports EVERY footgun declaration in content — one finding per
// declaration line. It does not read rationale: docgraph can't judge whether a
// stated "why" is real, so it nags on the declaration itself and leaves that
// judgement to the pusher (the finding prints the two-question test). Passing
// mentions — cross-references and bare container headings — are not declarations
// (see isFootgunDeclaration) and never flag.
func scanDeclarations(content string) []declFinding {
	var out []declFinding
	for ln, line := range strings.Split(content, "\n") {
		if isFootgunDeclaration(line) {
			out = append(out, declFinding{Line: ln + 1, Text: strings.TrimSpace(line)})
		}
	}
	return out
}
