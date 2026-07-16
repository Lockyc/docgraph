package audit

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// CoreTypes is the advisory core vocabulary for a doc's `type`. Custom types are
// allowed (tolerate-unknown) — this is what docgraph understands, not a closed
// set. Single source for the type vocabulary: the emitted JSON Schema and any
// future validation derive from it, never restate it.
var CoreTypes = []string{"runbook", "architecture", "reference", "decision", "guide", "index"}

// CoreRels is the advisory core vocabulary for an edge's `rel`. Custom rels are
// allowed and treated as opaque cross-references. Single source for the rel
// vocabulary.
var CoreRels = []string{"covers", "part-of", "supersedes", "depends-on", "runbook-for", "see-also", "source"}

// Doc is a parsed doc frontmatter block — a node in the doc graph. Unknown keys
// (domain/ops fields like service/host/severity) land in Extra, not rejected:
// the core schema is generic; domains extend it via permissive extra keys.
type Doc struct {
	Type        string   `yaml:"type"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Verified    string   `yaml:"verified"`
	Review      string   `yaml:"review"`
	Links       []Edge   `yaml:"links"`
	// Heading is the body's first H1, derived at parse time — NOT a frontmatter
	// key (`yaml:"-"`, so a `heading:` key lands in Extra and can never set it).
	// A doc's title already exists as its H1; a `title:` field restating it is a
	// shadow that drifts, so Title is an override for when the index label should
	// differ, and Heading is the default.
	Heading string         `yaml:"-"`
	Extra   map[string]any `yaml:",inline"`
}

// Edge is a labeled, typed link from a Doc — the "label the link" model. Rel is
// the relationship role (see CoreRels); To is the target (a doc path, code path,
// URL, or owner/repo:… cross-repo reference — kind inferred, not declared); Note
// is an optional human label carried with the edge (what OKF's bare resource:
// cannot express).
type Edge struct {
	Rel  string `yaml:"rel"`
	To   string `yaml:"to"`
	Note string `yaml:"note"`
}

// SplitFrontmatter separates a leading YAML frontmatter block from the body. A
// block exists only when the file's very first line is exactly `---`; it ends at
// the next line that is exactly `---`. Returns the frontmatter YAML (without the
// fences), the remaining body, and whether a block was present. A `---` anywhere
// but the first line is a markdown horizontal rule, not frontmatter.
func SplitFrontmatter(content string) (fm, body string, has bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", content, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			fm = strings.Join(lines[1:i], "\n")
			body = strings.Join(lines[i+1:], "\n")
			return fm, body, true
		}
	}
	// Opening fence with no close: treat as no frontmatter (a lone --- line).
	return "", content, false
}

// ParseFrontmatter splits and decodes a doc's leading YAML frontmatter into a
// Doc. Returns (nil, nil) when there is no frontmatter block — plain docs are
// valid. Returns (nil, err) when a block is present but its YAML is malformed. A
// well-formed block with no `type` decodes to a Doc with an empty Type; whether
// that is a finding is the caller's decision, not the parser's.
func ParseFrontmatter(content string) (*Doc, error) {
	fm, body, has := SplitFrontmatter(content)
	if !has {
		return nil, nil
	}
	var d Doc
	if err := yaml.Unmarshal([]byte(fm), &d); err != nil {
		return nil, err
	}
	d.Heading = firstHeading(body)
	return &d, nil
}

// firstHeading returns the text of the body's first ATX H1 (`# Title`), or "" if
// there is none. Fenced blocks are skipped so a shell comment (`# install`) in a
// code sample is never mistaken for the title — the same fence model
// extractLinks uses. Deliberately strict: the line must begin with "# " (no
// indent, exactly one #), because an indented line is a code block and a
// setext heading is out of scope.
func firstHeading(body string) string {
	var inFence bool
	var fenceChar byte
	for _, raw := range strings.Split(body, "\n") {
		if m := fenceRe.FindString(strings.TrimSpace(raw)); m != "" {
			if !inFence {
				inFence, fenceChar = true, m[0]
			} else if m[0] == fenceChar {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(raw, "# ") {
			return strings.TrimSpace(raw[2:])
		}
	}
	return ""
}
