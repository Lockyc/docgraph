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
	BrokenAnchors       []BrokenLink // Target is the resolved path plus #fragment
	Untracked           []string
	FrontmatterFindings []FrontmatterFinding
	BrokenEdges         []BrokenEdge
	EdgeCycles          [][]string
	Disconnected        []string
}

func (r Report) HasFindings() bool {
	for _, n := range CheckCounts(r, nil) {
		if n > 0 {
			return true
		}
	}
	return false
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
// findings: malformed YAML, a block present with no `type`, or (except any
// README.md, matched by basename) no frontmatter block at all.
func parseDocs(repoRoot string, tracked, globs []string) (map[string]*Doc, []FrontmatterFinding) {
	return parseDocsFrom(worktreeSource{root: repoRoot}, tracked, globs)
}

func parseDocsFrom(src fileSource, tracked, globs []string) (map[string]*Doc, []FrontmatterFinding) {
	docs := map[string]*Doc{}
	var findings []FrontmatterFinding
	for _, f := range tracked {
		if matchesIgnore(f, globs) {
			continue
		}
		content, err := src.read(f)
		if err != nil {
			continue
		}
		d, err := ParseFrontmatter(content)
		if err != nil {
			findings = append(findings, FrontmatterFinding{File: f, Detail: "malformed frontmatter: " + err.Error()})
			continue
		}
		if d == nil {
			if filepath.Base(f) != "README.md" {
				findings = append(findings, FrontmatterFinding{
					File:   f,
					Detail: "frontmatter block required (add a --- block with a type:)",
				})
			}
			continue // no frontmatter block; README is exempt
		}
		if strings.TrimSpace(d.Type) == "" {
			findings = append(findings, FrontmatterFinding{File: f, Detail: "frontmatter present but missing required field: type"})
		}
		docs[f] = d
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].File < findings[j].File })
	return docs, findings
}

// brokenEdges reports frontmatter edges whose internal (doc/code) target is not
// in the repo, judged by membership as the broken-links check judges: a fresh
// clone's verdict on any filesystem, so a target outside the repo, a case-only
// match or a file git doesn't track is broken. inRepo is the repo's markdown
// (tracked plus untracked); tracked is every tracked file. EdgeExternal and
// EdgeCrossRepo targets are skipped by design.
func brokenEdges(docs map[string]*Doc, inRepo map[string]bool, tracked []string) []BrokenEdge {
	var out []BrokenEdge
	for src, d := range docs {
		for _, e := range d.Links {
			// The emitted schema requires both; yaml decoding is lenient, so a typo'd
			// key arrives as an empty field rather than an error.
			if strings.TrimSpace(e.Rel) == "" {
				out = append(out, BrokenEdge{Source: src, Rel: e.Rel, Target: e.To, Reason: "missing rel"})
			}
			switch ClassifyTarget(e.To) {
			case EdgeMissing:
				out = append(out, BrokenEdge{Source: src, Rel: e.Rel, Target: e.To, Reason: "missing to"})
			case EdgeDoc, EdgeCode:
				target := ResolveEdgeTarget(e.To)
				reason := ""
				switch {
				case target == ".." || strings.HasPrefix(target, "../"):
					reason = "target escapes the repo"
				case ClassifyTarget(e.To) == EdgeDoc && !inRepo[target]:
					reason = "target does not exist"
				case ClassifyTarget(e.To) == EdgeCode && !codeInRepo(target, tracked):
					reason = "target does not exist"
				}
				if reason != "" {
					out = append(out, BrokenEdge{Source: src, Rel: e.Rel, Target: target, Reason: reason})
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
	in, err := resolveGraphInputs(worktreeSource{root: repoRoot}, opts.ExtraRoots, opts.Ignores)
	if err != nil {
		return Report{}, err
	}
	tracked, trackedSet, roots, rootSet, globs := in.tracked, in.trackedSet, in.roots, in.rootSet, in.globs

	docs, fmFindings := parseDocs(repoRoot, tracked, globs)

	cg := BuildContentGraph(repoRoot, tracked, trackedSet, rootSet, globs)
	orphans := cg.Islands()

	untrackedList, err := untrackedMD(repoRoot)
	if err != nil {
		return Report{}, err
	}

	// Broken links: every tracked (non-ignored) md, any .md target that is not a
	// repo doc present on disk. Membership is the exact-case git path, so the
	// verdict is a fresh clone's on any filesystem: a link escaping the repo, or
	// matching a doc only case-insensitively, is broken even where os.Stat finds it.
	// An untracked target is the `untracked` check's finding, not a broken link.
	inRepo := make(map[string]bool, len(trackedSet)+len(untrackedList))
	for f := range trackedSet {
		inRepo[f] = true
	}
	for _, f := range untrackedList {
		inRepo[f] = true
	}
	//
	// Broken anchors: a #fragment, same-file or on a link whose target exists,
	// that no heading or HTML id in that target produces. A missing target is
	// only the broken-links finding.
	var broken, badAnchors []BrokenLink
	anchorCache := map[string]map[string]bool{}
	anchorsOf := func(rel string) map[string]bool {
		if a, ok := anchorCache[rel]; ok {
			return a
		}
		content, _ := readFile(repoRoot, rel)
		a := docAnchors(content)
		anchorCache[rel] = a
		return a
	}
	for _, f := range tracked {
		if matchesIgnore(f, globs) {
			continue
		}
		content, err := readFile(repoRoot, f)
		if err != nil {
			continue
		}
		for _, link := range extractLinks(content) {
			frag, hasFrag := linkFragment(link.Target)
			if strings.HasPrefix(link.Target, "#") {
				if hasFrag && !hasAnchor(anchorsOf(f), frag) {
					badAnchors = append(badAnchors, BrokenLink{Source: f, Line: link.Line, Target: f + "#" + frag})
				}
				continue
			}
			if !isLocalMd(link.Target) {
				continue
			}
			resolved := resolveTarget(f, link.Target)
			_, statErr := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(resolved)))
			if !inRepo[resolved] || statErr != nil {
				broken = append(broken, BrokenLink{Source: f, Line: link.Line, Target: resolved})
				continue
			}
			if hasFrag && !hasAnchor(anchorsOf(resolved), frag) {
				badAnchors = append(badAnchors, BrokenLink{Source: f, Line: link.Line, Target: resolved + "#" + frag})
			}
		}
	}

	var untracked []string
	for _, f := range untrackedList {
		if !matchesIgnore(f, globs) {
			untracked = append(untracked, f)
		}
	}

	// orphans is already sorted by cg.Islands().
	sort.Strings(untracked)
	for _, s := range [][]BrokenLink{broken, badAnchors} {
		sort.SliceStable(s, func(i, j int) bool {
			if s[i].Source != s[j].Source {
				return s[i].Source < s[j].Source
			}
			return s[i].Line < s[j].Line
		})
	}

	allTracked, err := gitLines(repoRoot, "ls-files")
	if err != nil {
		return Report{}, err
	}
	brokenEdgeFindings := brokenEdges(docs, inRepo, allTracked)
	edgeCycles := detectCycles(docs, trackedSet)

	mg := BuildMetadataGraph(docs, trackedSet)

	return Report{
		Roots:               roots,
		TrackedMD:           len(tracked),
		Reachable:           len(cg.Nodes) - len(orphans),
		Orphans:             orphans,
		BrokenLinks:         broken,
		BrokenAnchors:       badAnchors,
		Untracked:           untracked,
		FrontmatterFindings: fmFindings,
		BrokenEdges:         brokenEdgeFindings,
		EdgeCycles:          edgeCycles,
		Disconnected:        mg.Islands(),
	}, nil
}
