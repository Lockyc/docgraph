package audit

import (
	"os"
	"os/exec"
	"path/filepath"
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
