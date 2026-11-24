package audit

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	atxHeadingRe   = regexp.MustCompile(`^ {0,3}#{1,6}(?:[ \t]+(.*?))?[ \t]*$`)
	atxClosingRe   = regexp.MustCompile(`(?:^|[ \t]+)#+[ \t]*$`)
	setextRe       = regexp.MustCompile(`^ {0,3}(?:=+|-+)[ \t]*$`)
	headingAttrRe  = regexp.MustCompile(`[ \t]*\{:?([^{}]*)\}[ \t]*$`)
	mdImageRe      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdLinkRe       = regexp.MustCompile(`\[([^\]]*)\](?:\([^)]*\)|\[[^\]]*\])`)
	htmlTagRe      = regexp.MustCompile(`<[^>]*>`)
	underscoreEmRe = regexp.MustCompile(`(^|[^\p{L}\p{N}_])_+([^_]+?)_+([^\p{L}\p{N}_]|$)`)
	htmlOpenTagRe  = regexp.MustCompile(`<[A-Za-z][^>]*>`)
	htmlIdAttrRe   = regexp.MustCompile(`(?i)(?:^|\s)(?:id|name)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

// headingText is a heading's text as the renderer displays it, which is what
// GitHub and Forgejo slug: inline markup (code, emphasis, links, images, HTML
// tags, backslash escapes) and an ATX closing sequence are dropped, their text
// kept.
func headingText(s string) string {
	s = atxClosingRe.ReplaceAllString(s, "")
	s = mdImageRe.ReplaceAllString(s, "$1")
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = underscoreEmRe.ReplaceAllString(s, "$1$2$3")
	s = strings.NewReplacer("`", "", "*", "", "~~", "", `\`, "").Replace(s)
	return strings.TrimSpace(s)
}

// slugify renders heading text into the id GitHub and Forgejo give it
// (github-slugger): lowercase, every character that is not a letter, number,
// mark, hyphen, underscore or space dropped, each space a hyphen — runs are
// not collapsed.
func slugify(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// slugger numbers repeated slugs the way github-slugger does: the second
// "setup" is "setup-1", and a generated suffix never reuses a slug already
// taken.
type slugger map[string]int

func (s slugger) next(slug string) string {
	out := slug
	for {
		if _, taken := s[out]; !taken {
			break
		}
		s[slug]++
		out = slug + "-" + strconv.Itoa(s[slug])
	}
	s[out] = 0
	return out
}

// explicitID splits a trailing heading attribute block (`{#id}`, `{#id .cls}`,
// kramdown's `{: #id}`) off heading text. A block with no #id is left in the
// text, as GitHub leaves it.
func explicitID(text string) (rest, id string) {
	m := headingAttrRe.FindStringSubmatchIndex(text)
	if m == nil {
		return text, ""
	}
	for _, f := range strings.Fields(text[m[2]:m[3]]) {
		if len(f) > 1 && f[0] == '#' {
			return text[:m[0]], f[1:]
		}
	}
	return text, ""
}

// docAnchors is every fragment a markdown file answers to, lowercased: each
// heading's rendered slug (duplicates numbered), each explicit heading id, and
// each HTML id/name attribute. Frontmatter, fenced code and inline code produce
// none. Indented code is not recognised as code: an ATX heading there is
// rejected by its indent, but an HTML id or a setext underline still counts, so
// a link to one can pass when it should not.
func docAnchors(content string) map[string]bool {
	_, body, _ := SplitFrontmatter(content)
	ids := map[string]bool{}
	slugs := slugger{}
	addHeading := func(raw string) {
		text, id := explicitID(raw)
		if id != "" {
			ids[strings.ToLower(id)] = true
		}
		if slug := slugify(headingText(text)); slug != "" {
			ids[slugs.next(slug)] = true
		}
	}
	prevIdx, prev := -2, ""
	for i, raw := range proseLines(body) {
		line := strings.TrimRight(raw, "\r")
		switch {
		case setextRe.MatchString(line) && prevIdx == i-1 && strings.TrimSpace(prev) != "" && !atxHeadingRe.MatchString(prev):
			addHeading(strings.TrimSpace(prev))
			prevIdx = -2 // an underline closes its heading; it is no paragraph line
			continue
		case atxHeadingRe.MatchString(line):
			addHeading(atxHeadingRe.FindStringSubmatch(line)[1])
		}
		for _, tag := range htmlOpenTagRe.FindAllString(stripInlineCode(line), -1) {
			for _, m := range htmlIdAttrRe.FindAllStringSubmatch(tag, -1) {
				if v := m[1] + m[2]; v != "" {
					ids[strings.ToLower(v)] = true
				}
			}
		}
		prevIdx, prev = i, line
	}
	return ids
}

// linkFragment returns the decoded #fragment of a link destination, if it has
// a non-empty one. The <…> form and a trailing " title" are handled as
// cleanTarget handles them.
func linkFragment(target string) (string, bool) {
	if rest, ok := strings.CutPrefix(target, "<"); ok {
		if i := strings.IndexByte(rest, '>'); i >= 0 {
			target = rest[:i]
		}
	} else if i := strings.IndexAny(target, " \t"); i >= 0 {
		target = target[:i]
	}
	i := strings.IndexByte(target, '#')
	if i < 0 || i == len(target)-1 {
		return "", false
	}
	frag := target[i+1:]
	if dec, err := url.PathUnescape(frag); err == nil {
		frag = dec
	}
	return frag, true
}

// hasAnchor reports whether a file with the given anchors answers to frag.
// Matching ignores case, as GitHub's fragment navigation does, and #top always
// resolves (the HTML spec scrolls to the top of the page).
func hasAnchor(anchors map[string]bool, frag string) bool {
	f := strings.ToLower(frag)
	return f == "top" || anchors[f]
}
