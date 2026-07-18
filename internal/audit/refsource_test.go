package audit

import (
	"os/exec"
	"testing"
)

// commitAll commits everything currently staged/tracked in dir at HEAD.
func commitAll(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "x"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func fixtureFiles() (map[string]string, []string) {
	files := map[string]string{
		"README.md":      "# Root\n[a](docs/a.md)\n",
		"docs/a.md":      "---\ntype: reference\nlinks:\n  - rel: see-also\n    to: docs/b.md\n---\n# A\n",
		"docs/b.md":      "---\ntype: reference\n---\n# B\n",
		"docs/island.md": "---\ntype: reference\n---\n# Nobody links me\n",
	}
	return files, []string{"README.md", "docs/a.md", "docs/b.md", "docs/island.md"}
}

func TestRefSourceEqualsWorktreeOnCleanCommit(t *testing.T) {
	files, track := fixtureFiles()
	dir := setupRepo(t, files, track)
	commitAll(t, dir)

	// Same dir passed as the label to both, so RepoRoot matches → byte-for-byte equal.
	wt, err := BuildGraphView(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := BuildGraphViewAtRef(dir, "HEAD", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wtJSON, _ := wt.JSON()
	refJSON, _ := ref.JSON()
	if string(wtJSON) != string(refJSON) {
		t.Fatalf("ref-mode graph differs from worktree on a clean commit:\n--- worktree ---\n%s\n--- ref ---\n%s", wtJSON, refJSON)
	}
	// sanity: the island doc is present as an island (proves the graph is non-trivial)
	if len(ref.Islands.Content) == 0 {
		t.Fatalf("expected docs/island.md as a content island, got none")
	}
}

func TestRefSourceWorksOnBareRepo(t *testing.T) {
	files, track := fixtureFiles()
	dir := setupRepo(t, files, track)
	commitAll(t, dir)

	bare := t.TempDir() + "/repo.git"
	if out, err := exec.Command("git", "clone", "--bare", dir, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	v, err := BuildGraphViewAtRef(bare, "HEAD", nil, nil)
	if err != nil {
		t.Fatalf("BuildGraphViewAtRef on bare repo: %v", err)
	}
	if len(v.Nodes) != 4 {
		t.Fatalf("want 4 nodes from the bare repo, got %d", len(v.Nodes))
	}
}

func TestRefSourceIgnoresUncommittedEdits(t *testing.T) {
	files, track := fixtureFiles()
	dir := setupRepo(t, files, track)
	commitAll(t, dir)

	// Add an UNCOMMITTED new tracked-path edit that would add a node in worktree mode.
	writeFile(t, dir, "docs/new.md", "---\ntype: reference\n---\n# new\n")
	cmd := exec.Command("git", "-C", dir, "add", "docs/new.md")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	wt, _ := BuildGraphView(dir, nil, nil)               // sees docs/new.md (tracked in index)
	ref, _ := BuildGraphViewAtRef(dir, "HEAD", nil, nil) // committed only → no docs/new.md
	if len(wt.Nodes) == len(ref.Nodes) {
		t.Fatalf("expected worktree (%d) to see the uncommitted doc that ref (%d) does not", len(wt.Nodes), len(ref.Nodes))
	}
}
