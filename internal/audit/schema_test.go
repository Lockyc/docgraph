package audit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func toStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		out[i], _ = e.(string)
	}
	return out
}

func TestSchemaJSONValidAndStamped(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(SchemaJSON("2.0.0"), &m); err != nil {
		t.Fatalf("SchemaJSON is not valid JSON: %v", err)
	}
	if m["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", m["$schema"])
	}
	if m["type"] != "object" {
		t.Errorf("top type = %v, want object", m["type"])
	}
	if c, _ := m["$comment"].(string); !strings.Contains(c, "2.0.0") {
		t.Errorf("$comment = %v, want a version stamp", m["$comment"])
	}
}

func TestSchemaJSONRequiredAndPermissive(t *testing.T) {
	var m map[string]any
	_ = json.Unmarshal(SchemaJSON("x"), &m)
	if req := toStrings(m["required"]); !reflect.DeepEqual(req, []string{"type"}) {
		t.Errorf("required = %v, want [type]", m["required"])
	}
	if m["additionalProperties"] != true {
		t.Errorf("additionalProperties = %v, want true (permissive extras)", m["additionalProperties"])
	}
}

func TestSchemaJSONCoreVocabExtensions(t *testing.T) {
	var m map[string]any
	_ = json.Unmarshal(SchemaJSON("x"), &m)
	if got := toStrings(m["x-docgraph-core-types"]); !reflect.DeepEqual(got, CoreTypes) {
		t.Errorf("x-docgraph-core-types = %v, want %v", got, CoreTypes)
	}
	if got := toStrings(m["x-docgraph-core-rels"]); !reflect.DeepEqual(got, CoreRels) {
		t.Errorf("x-docgraph-core-rels = %v, want %v", got, CoreRels)
	}
}

func TestSchemaJSONEdgeShape(t *testing.T) {
	var m map[string]any
	_ = json.Unmarshal(SchemaJSON("x"), &m)
	defs, ok := m["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("$defs missing")
	}
	edge, ok := defs["edge"].(map[string]any)
	if !ok {
		t.Fatalf("$defs.edge missing")
	}
	if req := toStrings(edge["required"]); !reflect.DeepEqual(req, []string{"rel", "to"}) {
		t.Errorf("edge.required = %v, want [rel to]", edge["required"])
	}
	if edge["additionalProperties"] != false {
		t.Errorf("edge.additionalProperties = %v, want false", edge["additionalProperties"])
	}
}

func TestSchemaJSONNoEnumRestriction(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(SchemaJSON("x"), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	props := m["properties"].(map[string]any)
	typ := props["type"].(map[string]any)
	if _, ok := typ["enum"]; ok {
		t.Error("type must not be enum-restricted (custom types are allowed)")
	}
	defs := m["$defs"].(map[string]any)
	edge := defs["edge"].(map[string]any)
	rel := edge["properties"].(map[string]any)["rel"].(map[string]any)
	if _, ok := rel["enum"]; ok {
		t.Error("rel must not be enum-restricted (custom rels are allowed)")
	}
}
