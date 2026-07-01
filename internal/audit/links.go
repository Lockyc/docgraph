package audit

import (
	"path/filepath"
	"regexp"
	"strings"
)

type Link struct {
	Line   int
	Target string
}

var inlineLinkRe = regexp.MustCompile(`\]\(([^)]+)\)`)
var refLinkRe = regexp.MustCompile(`^\s*\[[^\]]+\]:\s+(\S+)`)

// extractLinks returns every markdown link target with its 1-based line number,
// covering inline [x](target) and reference-style [label]: target.
func extractLinks(content string) []Link {
	var links []Link
	for i, line := range strings.Split(content, "\n") {
		for _, m := range inlineLinkRe.FindAllStringSubmatch(line, -1) {
			links = append(links, Link{Line: i + 1, Target: m[1]})
		}
		if m := refLinkRe.FindStringSubmatch(line); m != nil {
			links = append(links, Link{Line: i + 1, Target: m[1]})
		}
	}
	return links
}

// cleanTarget strips any #anchor, ?query, or " title" suffix.
func cleanTarget(target string) string {
	if i := strings.IndexAny(target, "#? \t"); i >= 0 {
		target = target[:i]
	}
	return target
}

// isLocalMd reports whether target is an intra-repo link to a .md file.
func isLocalMd(target string) bool {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return false
	}
	t := cleanTarget(target)
	if t == "" || strings.HasPrefix(t, "/") {
		return false
	}
	return strings.HasSuffix(t, ".md")
}

// resolveTarget resolves target relative to the source file's directory and
// returns a /-separated, cleaned, repo-relative path.
func resolveTarget(sourceRel, target string) string {
	dir := filepath.Dir(sourceRel)
	return filepath.ToSlash(filepath.Clean(filepath.Join(dir, cleanTarget(target))))
}
