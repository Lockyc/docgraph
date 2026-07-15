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
	Type        string         `yaml:"type"`
	Title       string         `yaml:"title"`
	Description string         `yaml:"description"`
	Tags        []string       `yaml:"tags"`
	Verified    string         `yaml:"verified"`
	Review      string         `yaml:"review"`
	Links       []Edge         `yaml:"links"`
	Extra       map[string]any `yaml:",inline"`
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
	fm, _, has := SplitFrontmatter(content)
	if !has {
		return nil, nil
	}
	var d Doc
	if err := yaml.Unmarshal([]byte(fm), &d); err != nil {
		return nil, err
	}
	return &d, nil
}
