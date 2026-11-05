package audit

import (
	"os"
	"os/exec"
	"reflect"
	"testing"
)

// A doc is stale by fact when code it covers changed after the day it was
// verified: a later commit counts, a same-day one does not, and a doc with no
// verified date or no covers edge is never judged.
func TestCodeMovedDocs(t *testing.T) {
	cover := func(verified, to string) string {
		v := ""
		if verified != "" {
			v = "verified: " + verified + "\n"
		}
		return "---\ntype: reference\n" + v + "links:\n  - rel: covers\n    to: " + to + "\n---\n# D\n"
	}
	dir := setupRepo(t, map[string]string{
		"docs/moved.md":    cover("2026-03-01", "src/a.go"),
		"docs/sameday.md":  cover("2026-03-10", "src/b.go"),
		"docs/dir.md":      cover("2026-03-01", "src"),
		"docs/fresh.md":    cover("2026-04-01", "src/a.go"),
		"docs/noverify.md": cover("", "src/a.go"),
		"src/a.go":         "package src\n",
		"src/b.go":         "package src\n",
	}, nil)
	commit := func(date, msg string) {
		t.Helper()
		cmd := exec.Command("git", "-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", msg)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date+"T12:00:00Z", "GIT_COMMITTER_DATE="+date+"T12:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}
	}
	gitIn(t, dir)("add", "-A")
	commit("2026-01-01", "base")
	writeFile(t, dir, "src/a.go", "package src\n// changed\n")
	gitIn(t, dir)("add", "-A")
	commit("2026-03-10", "change a")
	writeFile(t, dir, "src/b.go", "package src\n// changed\n")
	gitIn(t, dir)("add", "-A")
	commit("2026-03-10", "change b")
	writeFile(t, dir, "src/notes.md", "prose under a covered dir is not code\n")
	gitIn(t, dir)("add", "-A")
	commit("2026-03-20", "notes")

	got, err := CodeMovedDocs(dir, coversDocs(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []CodeMovedFinding{
		{File: "docs/dir.md", Verified: "2026-03-01", Changed: "2026-03-10", Path: "src/b.go"},
		{File: "docs/moved.md", Verified: "2026-03-01", Changed: "2026-03-10", Path: "src/a.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CodeMovedDocs = %+v\nwant %+v", got, want)
	}
}
