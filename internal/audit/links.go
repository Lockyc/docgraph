package audit

import (
	"iter"
	"net/url"
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
var fenceRe = regexp.MustCompile("^(`{3,}|~{3,})(.*)$")

// fenceMarker reports a line that opens a fence: its run (``` or ~~~, three or
// more of one character) and the info string after it. A backtick run whose info
// string holds a backtick is an inline code span, not a fence (CommonMark).
func fenceMarker(line string) (run, info string, ok bool) {
	m := fenceRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil || (m[1][0] == '`' && strings.ContainsRune(m[2], '`')) {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// proseLines yields each line outside fenced code blocks with its 0-based index.
// A fence closes only on a run of its own character at least as long as the
// opener with nothing after it, so a ```` block can show a ``` example and
// ```bash inside a ``` block is content, not a close.
func proseLines(content string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		open := "" // the opening run while inside a fence
		for i, raw := range strings.Split(content, "\n") {
			run, info, ok := fenceMarker(raw)
			switch {
			case open == "" && ok:
				open = run
				continue
			case open != "":
				if ok && info == "" && run[0] == open[0] && len(run) >= len(open) {
					open = ""
				}
				continue
			}
			if !yield(i, raw) {
				return
			}
		}
	}
}

// extractLinks returns every markdown link target with its 1-based line number,
// covering inline [x](target) and reference-style [label]: target. Links inside
// fenced code blocks (``` / ~~~) and inline code spans (`...`) are skipped, so
// illustrative/template paths in examples don't count as real links.
func extractLinks(content string) []Link {
	var links []Link
	for i, raw := range proseLines(content) {
		line := stripInlineCode(raw)
		for _, m := range inlineLinkRe.FindAllStringSubmatch(line, -1) {
			links = append(links, Link{Line: i + 1, Target: m[1]})
		}
		if m := refLinkRe.FindStringSubmatch(line); m != nil {
			links = append(links, Link{Line: i + 1, Target: m[1]})
		}
	}
	return links
}

// stripInlineCode blanks out `...` spans so example links inside them are not
// matched, while preserving column-independent link syntax outside the spans.
func stripInlineCode(s string) string {
	var b strings.Builder
	inCode := false
	for _, r := range s {
		switch {
		case r == '`':
			inCode = !inCode
			b.WriteByte(' ')
		case inCode:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cleanTarget reduces a link destination to a path: it unwraps the <…> form
// (which may hold spaces), strips any #anchor, ?query, or " title" suffix, and
// percent-decodes, so docs/a%20b.md names the file "docs/a b.md" as GitHub does.
func cleanTarget(target string) string {
	if rest, ok := strings.CutPrefix(target, "<"); ok {
		if i := strings.IndexByte(rest, '>'); i >= 0 {
			target = rest[:i]
		}
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			target = target[:i]
		}
	} else if i := strings.IndexAny(target, "#? \t"); i >= 0 {
		target = target[:i]
	}
	if dec, err := url.PathUnescape(target); err == nil {
		target = dec
	}
	return target
}

// isLocalMd reports whether target is an intra-repo link to a .md file. A
// root-relative /docs/x.md is one (GitHub resolves it against the repo root); a
// protocol-relative //host/x.md is not.
func isLocalMd(target string) bool {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return false
	}
	t := cleanTarget(target)
	if t == "" || strings.HasPrefix(t, "//") {
		return false
	}
	return strings.HasSuffix(t, ".md")
}

// resolveTarget resolves target relative to the source file's directory — or,
// for a leading-/ target, the repo root — and returns a /-separated, cleaned,
// repo-relative path.
func resolveTarget(sourceRel, target string) string {
	t := cleanTarget(target)
	if strings.HasPrefix(t, "/") {
		return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(t)), "/")
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(sourceRel), t)))
}
