package audit

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type BrokenLink struct {
	Source string
	Line   int
	Target string
}

// FrontmatterFinding is a per-doc well-formedness problem: a malformed YAML
// frontmatter block, or a block present without the required `type` field.
type FrontmatterFinding struct {
	File   string
	Detail string
}

// BrokenEdge is a frontmatter typed edge whose internal target (a doc or code
// path) does not exist. External (URL) and cross-repo targets are never broken
// edges — they are unverifiable-here by design.
type BrokenEdge struct {
	Source string
	Rel    string
	Target string
	Reason string
}

type Report struct {
	Roots               []string
	TrackedMD           int
	Reachable           int
	Orphans             []string
	BrokenLinks         []BrokenLink
	Untracked           []string
	FrontmatterFindings []FrontmatterFinding
	BrokenEdges         []BrokenEdge
	EdgeCycles          [][]string
}

func (r Report) HasFindings() bool {
	return len(r.Orphans) > 0 || len(r.BrokenLinks) > 0 || len(r.Untracked) > 0 ||
		len(r.FrontmatterFindings) > 0 || len(r.BrokenEdges) > 0 || len(r.EdgeCycles) > 0
}

type Options struct {
	ExtraRoots []string
	Ignores    []string
}

var rootCandidates = []string{"CLAUDE.md", "README.md", "AGENTS.md", "docs/index.md"}

// isPathWordByte reports whether b can be part of a path segment — used to
// require a segment boundary before a path mention (so "mydocs/x.md" does not
// count as a mention of "docs/x.md").
func isPathWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.' || b == '/' || b == '-' || b == '_':
		return true
	}
	return false
}

// mentionsPath reports whether content references the repo-relative path at a
// segment boundary (bare or inside inline code), not just as a markdown link.
func mentionsPath(content, path string) bool {
	for from := 0; ; {
		i := strings.Index(content[from:], path)
		if i < 0 {
			return false
		}
		i += from
		if i == 0 || !isPathWordByte(content[i-1]) {
			return true
		}
		from = i + 1
	}
}

// parseDocs reads and parses the frontmatter of every non-ignored tracked .md,
// returning a cache of the successfully-parsed docs (keyed by repo-relative
// slash path; malformed docs are omitted from the cache) plus well-formedness
// findings: malformed YAML, or a block present with no `type`. Files with no
// frontmatter block are valid and produce neither a cache entry nor a finding.
func parseDocs(repoRoot string, tracked, globs []string) (map[string]*Doc, []FrontmatterFinding) {
	docs := map[string]*Doc{}
	var findings []FrontmatterFinding
	for _, f := range tracked {
		if matchesIgnore(f, globs) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		d, err := ParseFrontmatter(string(content))
		if err != nil {
			findings = append(findings, FrontmatterFinding{File: f, Detail: "malformed frontmatter: " + err.Error()})
			continue
		}
		if d == nil {
			continue // no frontmatter block — valid
		}
		if strings.TrimSpace(d.Type) == "" {
			findings = append(findings, FrontmatterFinding{File: f, Detail: "frontmatter present but missing required field: type"})
		}
		docs[f] = d
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].File < findings[j].File })
	return docs, findings
}

// brokenEdges reports frontmatter edges whose internal (doc/code) target is
// absent on disk. EdgeExternal and EdgeCrossRepo targets are skipped by design.
func brokenEdges(repoRoot string, docs map[string]*Doc) []BrokenEdge {
	var out []BrokenEdge
	for src, d := range docs {
		for _, e := range d.Links {
			switch ClassifyTarget(e.To) {
			case EdgeDoc, EdgeCode:
				target := ResolveEdgeTarget(e.To)
				if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(target))); err != nil {
					out = append(out, BrokenEdge{Source: src, Rel: e.Rel, Target: target, Reason: "target does not exist"})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func Audit(repoRoot string, opts Options) (Report, error) {
	tracked, err := trackedMD(repoRoot)
	if err != nil {
		return Report{}, err
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[f] = true
	}
	globs, err := loadIgnores(repoRoot, opts.Ignores)
	if err != nil {
		return Report{}, err
	}

	var roots []string
	for _, r := range rootCandidates {
		if trackedSet[r] {
			roots = append(roots, r)
		}
	}
	for _, r := range opts.ExtraRoots {
		r = filepath.ToSlash(filepath.Clean(r))
		if trackedSet[r] {
			roots = append(roots, r)
		}
	}

	docs, fmFindings := parseDocs(repoRoot, tracked, globs)

	rootSet := map[string]bool{}
	for _, r := range roots {
		rootSet[r] = true
	}
	cg := BuildContentGraph(repoRoot, tracked, trackedSet, rootSet, globs)
	orphans := cg.Islands()

	// Broken links: every tracked (non-ignored) md, any .md target missing on disk.
	var broken []BrokenLink
	for _, f := range tracked {
		if matchesIgnore(f, globs) {
			continue
		}
		content, err := readFile(repoRoot, f)
		if err != nil {
			continue
		}
		for _, link := range extractLinks(content) {
			if !isLocalMd(link.Target) {
				continue
			}
			resolved := resolveTarget(f, link.Target)
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(resolved))); err != nil {
				broken = append(broken, BrokenLink{Source: f, Line: link.Line, Target: resolved})
			}
		}
	}

	untrackedList, err := untrackedMD(repoRoot)
	if err != nil {
		return Report{}, err
	}
	var untracked []string
	for _, f := range untrackedList {
		if !matchesIgnore(f, globs) {
			untracked = append(untracked, f)
		}
	}

	// orphans is already sorted by cg.Islands().
	sort.Strings(untracked)
	sort.Slice(broken, func(i, j int) bool {
		if broken[i].Source != broken[j].Source {
			return broken[i].Source < broken[j].Source
		}
		return broken[i].Line < broken[j].Line
	})

	brokenEdgeFindings := brokenEdges(repoRoot, docs)
	edgeCycles := detectCycles(docs, trackedSet)

	return Report{
		Roots:               roots,
		TrackedMD:           len(tracked),
		Reachable:           len(cg.Nodes) - len(orphans),
		Orphans:             orphans,
		BrokenLinks:         broken,
		Untracked:           untracked,
		FrontmatterFindings: fmFindings,
		BrokenEdges:         brokenEdgeFindings,
		EdgeCycles:          edgeCycles,
	}, nil
}
