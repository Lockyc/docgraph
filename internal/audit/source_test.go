package audit

import (
	"reflect"
	"testing"
)

func TestWorktreeSourceMatchesLegacyHelpers(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":     "# Root\n",
		"docs/a.md":     "---\ntype: reference\n---\n# A\n",
		"docs/loose.md": "untracked\n", // not added → not tracked
	}, []string{"README.md", "docs/a.md"})

	src := worktreeSource{root: dir}

	got, err := src.tracked()
	if err != nil {
		t.Fatal(err)
	}
	want, err := trackedMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked() = %v, want %v", got, want)
	}
	if src.label() != dir {
		t.Fatalf("label() = %q, want %q", src.label(), dir)
	}
	content, err := src.read("docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	legacy, _ := readFile(dir, "docs/a.md")
	if content != legacy {
		t.Fatalf("read() = %q, want %q", content, legacy)
	}
}
