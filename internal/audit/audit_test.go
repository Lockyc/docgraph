package audit

import "testing"

func TestAudit(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":          "root links to [index](docs/index.md)\n",
		"docs/index.md":      "[svc](services/a.md) and [dead](services/gone.md)\n",
		"docs/services/a.md": "leaf\n",
		"docs/orphan.md":     "nobody links here\n",
		"docs/loose.md":      "untracked file\n", // not added
	}, []string{"CLAUDE.md", "docs/index.md", "docs/services/a.md", "docs/orphan.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := rep.Orphans; len(got) != 1 || got[0] != "docs/orphan.md" {
		t.Errorf("orphans = %v", got)
	}
	if got := rep.BrokenLinks; len(got) != 1 || got[0].Source != "docs/index.md" || got[0].Target != "docs/services/gone.md" {
		t.Errorf("broken = %v", got)
	}
	if got := rep.Untracked; len(got) != 1 || got[0] != "docs/loose.md" {
		t.Errorf("untracked = %v", got)
	}
	if !rep.HasFindings() {
		t.Error("expected findings")
	}
}

func TestAuditIgnore(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":                   "[i](docs/index.md)\n",
		"docs/index.md":               "hub\n",
		"docs/superpowers/specs/s.md": "scratch orphan\n",
	}, []string{"CLAUDE.md", "docs/index.md", "docs/superpowers/specs/s.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Orphans) != 0 {
		t.Errorf("superpowers scratch should be ignored, got %v", rep.Orphans)
	}
}
