package audit

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FootgunFinding is one "footgun" mention whose enclosing paragraph carries
// neither a rationale signal nor an <!-- footgun-ok --> acknowledgment marker.
type FootgunFinding struct {
	File string
	Line int    // 1-based line of the first footgun token in the paragraph
	Text string // that line, trimmed, for context in the report
}

// footgunToken matches the word "footgun"/"footguns", case-insensitive.
var footgunToken = regexp.MustCompile(`(?i)\bfootguns?\b`)

// footgunAckMarker matches the explicit acknowledgment comment (optional reason
// after the name): <!-- footgun-ok --> or <!-- footgun-ok: hit in prod -->.
var footgunAckMarker = regexp.MustCompile(`(?i)<!--\s*footgun-ok\b`)

// footgunRationaleSignals are the conservative built-in phrases that count as a
// documented rationale in a footgun's paragraph. Deliberately narrow: loose
// connectives (so/since/thus) fire everywhere and would neuter the check. This
// is the single source of truth for the rationale vocabulary — do not restate it.
var footgunRationaleSignals = regexp.MustCompile(
	`(?i)(because|otherwise|so that|the reason|would break|re-?litigat|the trap)`)

// FootgunScan reports every "footgun" mention whose enclosing paragraph lacks a
// rationale signal or an ack marker. Scope is tracked .md under the doc-graph
// ignore layers (defaultIgnores + .docauditignore + --ignore) — the same set as
// the orphan check, NOT the leaks git-tracking scope: an agent skill file under
// .claude/ isn't house documentation, so its "footgun" usage is out of scope.
func FootgunScan(repoRoot string, extraIgnores []string) ([]FootgunFinding, error) {
	tracked, err := trackedMD(repoRoot)
	if err != nil {
		return nil, err
	}
	globs, err := loadIgnores(repoRoot, extraIgnores)
	if err != nil {
		return nil, err
	}
	var findings []FootgunFinding
	for _, f := range tracked {
		if matchesIgnore(f, globs) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		findings = append(findings, scanFootguns(f, string(b))...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

// scanFootguns groups content into paragraphs (maximal runs of contiguous
// non-blank lines) and emits one finding per paragraph that mentions "footgun"
// without a rationale signal or ack marker, anchored to the paragraph's first
// footgun line.
func scanFootguns(file, content string) []FootgunFinding {
	lines := strings.Split(content, "\n")
	var out []FootgunFinding
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
		para := lines[start:i]
		joined := strings.Join(para, "\n")
		if !footgunToken.MatchString(joined) {
			continue
		}
		if footgunRationaleSignals.MatchString(joined) || footgunAckMarker.MatchString(joined) {
			continue
		}
		for j, ln := range para {
			if footgunToken.MatchString(ln) {
				out = append(out, FootgunFinding{File: file, Line: start + j + 1, Text: strings.TrimSpace(ln)})
				break
			}
		}
	}
	return out
}
