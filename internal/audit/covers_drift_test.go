package audit

import (
	"reflect"
	"testing"
)

// coversDocs parses the given repo's docs the way the views do.
func coversDocs(t *testing.T, dir string) map[string]*Doc {
	t.Helper()
	docs, err := RepoDocs(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

func TestCoversDriftFiresWhenGoverningDocUntouched(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/auth.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/auth.go\n---\n\n# Auth\n",
			"src/auth.go":  "package auth\n",
		},
		map[string]string{"src/auth.go": "package auth\n\nfunc Login() {}\n"},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []CoversFinding{{Doc: "docs/auth.md", Paths: []string{"src/auth.go"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CoversDrift = %+v, want %+v", got, want)
	}
}

func TestCoversDriftSilentWhenDocTouched(t *testing.T) {
	// Editing the doc IS the escape hatch — it is the desired behaviour, not a loophole.
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/auth.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/auth.go\n---\n\n# Auth\n",
			"src/auth.go":  "package auth\n",
		},
		map[string]string{
			"src/auth.go":  "package auth\n\nfunc Login() {}\n",
			"docs/auth.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/auth.go\n---\n\n# Auth\n\nLogin exists.\n",
		},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("doc was touched — want no findings, got %+v", got)
	}
}

func TestCoversDriftSilentWithNoCoversEdges(t *testing.T) {
	// The edge IS the opt-in: a repo declaring none gets nothing.
	dir, base, head := commitRepo(t,
		map[string]string{"docs/auth.md": "---\ntype: reference\n---\n\n# Auth\n", "src/auth.go": "package auth\n"},
		map[string]string{"src/auth.go": "package auth\n\nfunc Login() {}\n"},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("no covers edges — want no findings, got %+v", got)
	}
}

func TestCoversDriftMatchesDirectoryEdge(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/a.md":           "---\ntype: reference\nlinks:\n  - rel: covers\n    to: internal/audit\n---\n\n# A\n",
			"internal/audit/x.go": "package audit\n",
		},
		map[string]string{"internal/audit/x.go": "package audit\n\nfunc F() {}\n"},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []CoversFinding{{Doc: "docs/a.md", Paths: []string{"internal/audit/x.go"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("directory edge should cover files under it: got %+v, want %+v", got, want)
	}
}

func TestCoversDriftSilentWhenCoveredPathUnchanged(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/a.md":    "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/other.go\n---\n\n# A\n",
			"src/a.go":     "package a\n",
			"src/other.go": "package a\n",
		},
		map[string]string{"src/a.go": "package a\n\nfunc F() {}\n"},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("covered path did not change — want no findings, got %+v", got)
	}
}

func TestCoversDriftIgnoresDocTargetEdge(t *testing.T) {
	// A covers edge onto a .md can never fire: changedCode excludes prose.
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/a.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: docs/b.md\n---\n\n# A\n",
			"docs/b.md": "---\ntype: reference\n---\n\n# B\n",
			"src/a.go":  "package a\n",
		},
		map[string]string{"docs/b.md": "---\ntype: reference\n---\n\n# B\n\nmore\n"},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("doc-target covers edge must never fire, got %+v", got)
	}
}

func TestCoversDriftGroupsPathsUnderOneDoc(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/a.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src\n---\n\n# A\n",
			"src/b.go":  "package s\n",
			"src/a.go":  "package s\n",
		},
		map[string]string{
			"src/b.go": "package s\n\nfunc B() {}\n",
			"src/a.go": "package s\n\nfunc A() {}\n",
		},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []CoversFinding{{Doc: "docs/a.md", Paths: []string{"src/a.go", "src/b.go"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CoversDrift = %+v, want %+v (one finding, paths sorted)", got, want)
	}
}

func TestCoversDriftSortsByDoc(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/zeta.md":  "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/z.go\n---\n\n# Z\n",
			"docs/alpha.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/a.go\n---\n\n# A\n",
			"src/z.go":      "package s\n",
			"src/a.go":      "package s\n",
		},
		map[string]string{
			"src/z.go": "package s\n\nfunc Z() {}\n",
			"src/a.go": "package s\n\nfunc A() {}\n",
		},
	)
	got, err := CoversDrift(dir, []RevRange{{Base: base, Head: head}}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []CoversFinding{
		{Doc: "docs/alpha.md", Paths: []string{"src/a.go"}},
		{Doc: "docs/zeta.md", Paths: []string{"src/z.go"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings must be sorted by Doc: got %+v, want %+v", got, want)
	}
}

func TestCoversDriftDedupesAcrossRanges(t *testing.T) {
	// The same doc/path surfacing in two pushed ranges is ONE finding, not two.
	dir, base, head := commitRepo(t,
		map[string]string{
			"docs/a.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/a.go\n---\n\n# A\n",
			"src/a.go":  "package s\n",
		},
		map[string]string{"src/a.go": "package s\n\nfunc A() {}\n"},
	)
	r := RevRange{Base: base, Head: head}
	got, err := CoversDrift(dir, []RevRange{r, r}, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []CoversFinding{{Doc: "docs/a.md", Paths: []string{"src/a.go"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate ranges must dedupe: got %+v, want %+v", got, want)
	}
}
