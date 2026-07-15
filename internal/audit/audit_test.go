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

func TestFrontmatterFindings(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":     "[a](docs/good.md) [b](docs/bad.md) [c](docs/plain.md)\n",
		"docs/good.md":  "---\ntype: reference\n---\nok\n",
		"docs/bad.md":   "---\ntitle: no type here\n---\nbody\n",
		"docs/plain.md": "no frontmatter at all\n",
		"docs/broke.md": "---\ntype: [oops\n---\nx\n",
	}, []string{"CLAUDE.md", "docs/good.md", "docs/bad.md", "docs/plain.md", "docs/broke.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	// Expect: bad.md (missing type) + broke.md (malformed). good.md and plain.md are clean.
	got := map[string]string{}
	for _, f := range rep.FrontmatterFindings {
		got[f.File] = f.Detail
	}
	if len(got) != 2 {
		t.Fatalf("findings = %v, want exactly bad.md + broke.md", rep.FrontmatterFindings)
	}
	if _, ok := got["docs/bad.md"]; !ok {
		t.Error("missing finding for docs/bad.md (no type)")
	}
	if _, ok := got["docs/broke.md"]; !ok {
		t.Error("missing finding for docs/broke.md (malformed)")
	}
}

func TestBrokenEdges(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "[a](docs/a.md)\n",
		"docs/a.md": "---\ntype: runbook\nlinks:\n" +
			"  - {rel: covers, to: scripts/real.sh}\n" +
			"  - {rel: covers, to: scripts/missing.sh}\n" +
			"  - {rel: depends-on, to: docs/gone.md}\n" +
			"  - {rel: source, to: https://example.com/x}\n" +
			"  - {rel: depends-on, to: homelab/docs:services/y.md}\n" +
			"---\nbody\n",
		"scripts/real.sh": "#!/bin/sh\n",
	}, []string{"CLAUDE.md", "docs/a.md", "scripts/real.sh"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	// Broken: scripts/missing.sh (code, absent) + docs/gone.md (doc, absent).
	// NOT broken: scripts/real.sh (exists), the https URL (external), and the
	// homelab/docs:... cross-repo ref (deferred).
	if len(rep.BrokenEdges) != 2 {
		t.Fatalf("BrokenEdges = %+v, want exactly missing.sh + gone.md", rep.BrokenEdges)
	}
	got := map[string]bool{}
	for _, e := range rep.BrokenEdges {
		got[e.Target] = true
	}
	if !got["scripts/missing.sh"] || !got["docs/gone.md"] {
		t.Errorf("BrokenEdges targets = %v, want scripts/missing.sh + docs/gone.md", got)
	}
}
