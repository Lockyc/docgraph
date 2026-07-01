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

func TestAuditPathMentionReachability(t *testing.T) {
	// A doc referenced only by an inline-code path (not a markdown link) is
	// still reachable — an agent follows bare paths too.
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":      "see `docs/design.md` for the design.\n",
		"docs/design.md": "design\n",
	}, []string{"CLAUDE.md", "docs/design.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Orphans) != 0 {
		t.Errorf("path-mentioned doc should be reachable, got %v", rep.Orphans)
	}
}

func TestAuditExcludesToolingNotRealDocs(t *testing.T) {
	// Skill files under .claude/ are runtime tooling, not docs — excluded.
	// A real doc outside docs/ (a config-dir README) IS a document and must be
	// audited, so if unreachable it's a genuine orphan.
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":                     "hub with no links\n",
		"docs/lonely.md":                "unreferenced doc\n",
		"monitoring/README.md":          "a real doc outside docs/\n",
		".claude/skills/foo/rules/x.md": "skill file, not a doc\n",
	}, []string{"CLAUDE.md", "docs/lonely.md", "monitoring/README.md", ".claude/skills/foo/rules/x.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := rep.Orphans
	if len(got) != 2 || got[0] != "docs/lonely.md" || got[1] != "monitoring/README.md" {
		t.Errorf("orphans = %v, want [docs/lonely.md monitoring/README.md] (skill file excluded, real docs kept)", got)
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
