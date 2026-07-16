package audit

import (
	"encoding/json"
	"strings"
)

// SchemaJSON returns the JSON Schema (draft 2020-12) describing a valid docgraph
// frontmatter object, stamped with the given tool version. It is the artifact
// `docgraph schema` emits and that every non-owning consumer (compositor,
// Mycelium, …) conforms to instead of re-encoding the vocabulary.
//
// The volatile vocabularies (CoreTypes, CoreRels) are read from their single
// source in frontmatter.go — surfaced as advisory x-docgraph-core-* arrays and
// woven into the field descriptions — so the schema cannot drift from what the
// code understands. type/rel are deliberately NOT enum-restricted: custom values
// are allowed (tolerate-unknown), so a strict validator must still accept them.
func SchemaJSON(version string) []byte {
	doc := map[string]any{
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"$id":      "https://github.com/lockyc/docgraph/schema",
		"title":    "docgraph document frontmatter",
		"$comment": "docgraph schema v" + version,
		"type":     "object",
		"properties": map[string]any{
			"type": map[string]any{
				"type":        "string",
				"description": "doc kind; core: " + strings.Join(CoreTypes, ", ") + " (custom allowed)",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "index label; omit unless it should differ from the doc's H1, which is the default",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "one-line summary shown beside the doc in the generated index",
			},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"verified": map[string]any{
				"type":        "string",
				"description": "date the doc was last verified against reality (YYYY-MM-DD)",
			},
			"review": map[string]any{
				"type":        "string",
				"description": "per-doc staleness cadence override, e.g. 90d",
			},
			"links": map[string]any{
				"type":  "array",
				"items": map[string]any{"$ref": "#/$defs/edge"},
			},
		},
		"required":             []any{"type"},
		"additionalProperties": true,
		"$defs": map[string]any{
			"edge": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rel": map[string]any{
						"type":        "string",
						"description": "relationship role; core: " + strings.Join(CoreRels, ", ") + " (custom allowed)",
					},
					"to":   map[string]any{"type": "string"},
					"note": map[string]any{"type": "string"},
				},
				"required":             []any{"rel", "to"},
				"additionalProperties": false,
			},
		},
		"x-docgraph-core-types": CoreTypes,
		"x-docgraph-core-rels":  CoreRels,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return append(b, '\n')
}
