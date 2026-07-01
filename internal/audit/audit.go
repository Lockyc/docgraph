package audit

import (
	"os"
	"path/filepath"
	"sort"
)

type BrokenLink struct {
	Source string
	Line   int
	Target string
}

type Report struct {
	Roots       []string
	TrackedMD   int
	Reachable   int
	Orphans     []string
	BrokenLinks []BrokenLink
	Untracked   []string
}

func (r Report) HasFindings() bool {
	return len(r.Orphans) > 0 || len(r.BrokenLinks) > 0 || len(r.Untracked) > 0
}

type Options struct {
	ExtraRoots []string
	Ignores    []string
}

var rootCandidates = []string{"CLAUDE.md", "README.md", "AGENTS.md", "docs/index.md"}

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

	read := func(rel string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			return "", false
		}
		return string(b), true
	}

	// BFS reachability from roots, following only links to tracked .md files.
	reachable := map[string]bool{}
	var queue []string
	enqueue := func(f string) {
		if !reachable[f] {
			reachable[f] = true
			queue = append(queue, f)
		}
	}
	for _, r := range roots {
		enqueue(r)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		content, ok := read(cur)
		if !ok {
			continue
		}
		for _, link := range extractLinks(content) {
			if !isLocalMd(link.Target) {
				continue
			}
			if resolved := resolveTarget(cur, link.Target); trackedSet[resolved] {
				enqueue(resolved)
			}
		}
	}

	// Broken links: every tracked (non-ignored) md, any .md target missing on disk.
	var broken []BrokenLink
	for _, f := range tracked {
		if matchesIgnore(f, globs) {
			continue
		}
		content, ok := read(f)
		if !ok {
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

	var orphans []string
	for _, f := range tracked {
		if !reachable[f] && !matchesIgnore(f, globs) {
			orphans = append(orphans, f)
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

	sort.Strings(orphans)
	sort.Strings(untracked)
	sort.Slice(broken, func(i, j int) bool {
		if broken[i].Source != broken[j].Source {
			return broken[i].Source < broken[j].Source
		}
		return broken[i].Line < broken[j].Line
	})

	return Report{
		Roots:       roots,
		TrackedMD:   len(tracked),
		Reachable:   len(reachable),
		Orphans:     orphans,
		BrokenLinks: broken,
		Untracked:   untracked,
	}, nil
}
