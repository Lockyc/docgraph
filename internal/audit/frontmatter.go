package audit

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
