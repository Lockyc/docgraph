package audit

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*.golden.json from the current output")

// TestGraphJSONGolden pins the whole graph --json payload Mycelium consumes, so a
// renamed key, a null list or a reordering shows up as a diff in review rather
// than slipping past assertions that look at a few keys. Regenerate with
// `go test ./internal/audit -run TestGraphJSONGolden -update` and review the diff.
func TestGraphJSONGolden(t *testing.T) {
	fm := func(typ string, links ...string) string {
		s := "---\ntype: " + typ + "\n"
		if len(links) > 0 {
			s += "links:\n"
			for _, l := range links {
				rel, to, _ := strings.Cut(l, " ")
				s += "  - rel: " + rel + "\n    to: " + to + "\n"
			}
		}
		return s + "---\n"
	}
	cases := map[string]map[string]string{
		"graph": {
			"README.md":     "# Root\n[guide](docs/guide.md) and [index](docs/index.md)\n",
			"docs/index.md": fm("index") + "# Index\n[arch](arch.md)\n",
			"docs/guide.md": fm("guide", "part-of docs/index.md", "see-also docs/ref.md", "covers main.go") + "# Guide\n",
			"docs/arch.md":  fm("architecture", "part-of docs/guide.md") + "# Arch\n",
			"docs/ref.md": strings.Replace(fm("reference", "source https://example.com/spec"), "---\n",
				"---\ntitle: Reference\ndescription: The spec, summarised\nverified: 2026-01-02\nreview: 90d\n", 1) + "# Ref\n",
			"docs/loop-a.md":    fm("decision", "part-of docs/loop-b.md") + "# Loop A\n",
			"docs/loop-b.md":    fm("decision", "part-of docs/loop-a.md") + "# Loop B\n",
			"docs/island.md":    "# Island\nno links, no frontmatter\n",
			"docs/meta-only.md": fm("reference") + "# Meta only\n",
			"main.go":           "package main\n",
		},
		"graph-empty": {
			"README.md": "# Root\n",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			var track []string
			for p := range files {
				track = append(track, p)
			}
			dir := setupRepo(t, files, track)
			v, err := BuildGraphView(dir, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			v.RepoRoot = "REPO_ROOT"
			got, err := v.JSON()
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := filepath.Join("testdata", name+".golden.json")
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("graph --json payload differs from %s (rerun with -update if intended)\n--- got\n%s", golden, got)
			}
		})
	}
}
