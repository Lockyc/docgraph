package audit

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// setupRepo builds a throwaway git repo in a temp dir: writes every file in
// files, then `git add`s the ones named in track (others stay untracked).
func setupRepo(t *testing.T, files map[string]string, track []string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	for _, f := range track {
		git("add", f)
	}
	return dir
}

func TestTrackedUntracked(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":     "x",
		"docs/a.md":     "x",
		"docs/loose.md": "x", // untracked
		"notmd.txt":     "x",
	}, []string{"CLAUDE.md", "docs/a.md"})

	tracked, err := trackedMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked) != 2 {
		t.Errorf("tracked = %v", tracked)
	}
	untracked, err := untrackedMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(untracked) != 1 || untracked[0] != "docs/loose.md" {
		t.Errorf("untracked = %v", untracked)
	}
}

func TestClosestBaseFindsIntegrationBranch(t *testing.T) {
	dir := setupRepo(t, map[string]string{"CLAUDE.md": "a\n"}, []string{"CLAUDE.md"})
	git := func(a ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
		return string(out)
	}
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "base")
	git("branch", "-M", "main") // make the base branch a known integration candidate
	base := trim(git("rev-parse", "HEAD"))
	git("checkout", "-b", "feature")
	writeFile(t, dir, "CLAUDE.md", "a\nb\n")
	git("add", "CLAUDE.md")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "feature work")

	got, ok := ClosestBase(dir, "feature")
	if !ok || got != base {
		t.Fatalf("want base=%s ok=true, got %q ok=%v", base, got, ok)
	}
}

func TestChangedCodeExcludesProse(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{"a.go": "package a\n", "CLAUDE.md": "intro\n"},
		map[string]string{
			"a.go":          "package a\n\nfunc New() {}\n",
			"CLAUDE.md":     "intro\nmore\n",
			"docs/x.md":     "doc\n",
			"internal/b.go": "package b\n",
		},
	)
	got, err := changedCode(dir, base+".."+head)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "internal/b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedCode = %v, want %v (prose must be excluded)", got, want)
	}
}

func TestClosestBaseFailsOpenWithNoIntegrationBranch(t *testing.T) {
	// No main/master/dev/develop/trunk branch exists → ClosestBase can resolve no
	// range and fails OPEN (the caller then runs no footgun-drift check).
	dir := setupRepo(t, map[string]string{"CLAUDE.md": "a\n"}, []string{"CLAUDE.md"})
	git := func(a ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
		return string(out)
	}
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "base")
	git("branch", "-M", "wip") // none of the integration-branch candidates exist

	got, ok := ClosestBase(dir, "wip")
	if ok || got != "" {
		t.Fatalf(`no integration branch → want ("", false), got %q %v`, got, ok)
	}
}
