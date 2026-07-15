package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"**/superpowers/**", "docs/superpowers/plans/x.md", true},
		{"**/superpowers/**", "docs/services/x.md", false},
		{"docs/*.md", "docs/index.md", true},
		{"docs/*.md", "docs/sub/index.md", false},
		{"**/*.tmp.md", "a/b/c.tmp.md", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.pat, c.path); got != c.want {
			t.Errorf("matchGlob(%q,%q)=%v want %v", c.pat, c.path, got, c.want)
		}
	}
}

func TestLoadIgnores(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".docgraphignore"), []byte("# c\nvendor/**\n\n"), 0644)
	globs, err := loadIgnores(dir, []string{"extra/*.md"})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, g := range globs {
		joined += g + "|"
	}
	want := "**/superpowers/**|.claude/**|.agents/**|vendor/**|extra/*.md|"
	if joined != want {
		t.Errorf("globs = %q want %q", joined, want)
	}
}
