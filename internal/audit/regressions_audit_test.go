package audit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string) func(a ...string) string {
	t.Helper()
	return func(a ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, a...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
		return strings.TrimSpace(string(out))
	}
}

// A new-branch push of a trunk (dev, first push) must not resolve to an empty
// head..head range just because the pushed branch is itself a candidate.
func TestPushBaseSkipsThePushedBranchItself(t *testing.T) {
	dir := setupRepo(t, map[string]string{"CLAUDE.md": "a\n"}, []string{"CLAUDE.md"})
	git := gitIn(t, dir)
	git("commit", "-qm", "base")
	git("branch", "-M", "main")
	base := git("rev-parse", "HEAD")
	git("checkout", "-qb", "dev")
	writeFile(t, dir, "CLAUDE.md", "a\nb\n")
	git("commit", "-qam", "dev work")
	head := git("rev-parse", "HEAD")

	got, ok := PushBase(dir, head)
	if !ok || got != base {
		t.Fatalf("PushBase = %q ok=%v, want %s (main's tip, not dev's own)", got, ok, base)
	}
	// ClosestBase keeps its trunk signal for doc-drift: on dev, the base is HEAD.
	if cb, _ := ClosestBase(dir, head); cb != head {
		t.Fatalf("ClosestBase = %q, want head %s", cb, head)
	}
}

func sessionFindings(t *testing.T, files map[string]string, edits ...string) []DocDriftFinding {
	t.Helper()
	track := make([]string, 0, len(files))
	for f := range files {
		track = append(track, f)
	}
	dir := setupRepo(t, files, track)
	git := gitIn(t, dir)
	git("commit", "-qm", "init")
	base := git("rev-parse", "HEAD")
	for i, content := range edits {
		writeFile(t, dir, "c.go", content)
		git("commit", "-qam", "edit"+string(rune('0'+i)))
	}
	d, err := SessionDocDriftDiff(dir, []string{base + "..HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := d.Findings()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// A constant changed and changed back across the session's commits has not
// drifted: the doc showing the original value is correct.
func TestSessionScanIgnoresARevertedConstant(t *testing.T) {
	f := sessionFindings(t,
		map[string]string{"c.go": "package c\nconst MAX_RETRIES = 10\n", "README.md": "MAX_RETRIES is 10\n"},
		"package c\nconst MAX_RETRIES = 20\n",
		"package c\nconst MAX_RETRIES = 10\n",
	)
	if len(f) != 0 {
		t.Fatalf("want no findings for a reverted constant, got %+v", f)
	}
}

// A constant changed twice reports the session's original value, not an
// intermediate one.
func TestSessionScanReportsTheOriginalValue(t *testing.T) {
	f := sessionFindings(t,
		map[string]string{"c.go": "package c\nconst MAX_RETRIES = 10\n", "README.md": "MAX_RETRIES is 10\n"},
		"package c\nconst MAX_RETRIES = 20\n",
		"package c\nconst MAX_RETRIES = 30\n",
	)
	if len(f) != 1 || f[0].Kind != ValueDrift || f[0].Old != "10" {
		t.Fatalf("want one ValueDrift with Old=10, got %+v", f)
	}
}

// A definition edited in one commit and removed in a later one is gone.
func TestSessionScanCatchesRemovalAfterEdit(t *testing.T) {
	f := sessionFindings(t,
		map[string]string{"c.go": "package c\nfunc FooBar(a int) {}\n", "README.md": "call FooBar\n"},
		"package c\nfunc FooBar(a, b int) {}\n",
		"package c\n",
	)
	if len(f) != 1 || f[0].Symbol != "FooBar" || f[0].Kind != Dangling {
		t.Fatalf("want FooBar dangling, got %+v", f)
	}
}

// A link that escapes the repo, or differs from the tracked path only by case,
// is broken on a fresh clone however this machine's filesystem answers.
func TestBrokenLinksAreJudgedAgainstTheRepo(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md": "[sib](../outside.md) [case](Docs/A.md) [ok](docs/a.md)\n",
		"docs/a.md": "---\ntype: guide\n---\nx\n",
	}, []string{"README.md", "docs/a.md"})
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, b := range rep.BrokenLinks {
		got[b.Target] = true
	}
	if !got["../outside.md"] || !got["Docs/A.md"] || got["docs/a.md"] || len(got) != 2 {
		t.Fatalf("broken = %+v, want exactly ../outside.md and Docs/A.md", rep.BrokenLinks)
	}
}

// A link to an untracked-but-present doc is the `untracked` check's finding,
// not a broken link.
func TestLinkToUntrackedDocIsNotBroken(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":   "[new](docs/new.md)\n",
		"docs/new.md": "---\ntype: guide\n---\nx\n",
	}, []string{"README.md"})
	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.BrokenLinks) != 0 {
		t.Fatalf("broken = %+v, want none", rep.BrokenLinks)
	}
}

func TestSplitFrontmatterToleratesBOMAndFenceWhitespace(t *testing.T) {
	for _, src := range []string{
		"\xef\xbb\xbf---\ntype: guide\n---\n# T\n",
		"--- \ntype: guide\n---\t\n# T\n",
	} {
		d, err := ParseFrontmatter(src)
		if err != nil || d == nil || d.Type != "guide" {
			t.Errorf("ParseFrontmatter(%q) = %+v, %v; want type guide", src, d, err)
		}
	}
}
