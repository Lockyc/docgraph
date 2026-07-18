package audit

import (
	"reflect"
	"testing"
)

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
		"CLAUDE.md":     "---\ntype: index\n---\n[a](docs/good.md) [b](docs/bad.md) [c](docs/plain.md)\n",
		"docs/good.md":  "---\ntype: reference\n---\nok\n",
		"docs/bad.md":   "---\ntitle: no type here\n---\nbody\n",
		"docs/plain.md": "no frontmatter at all\n",
		"docs/broke.md": "---\ntype: [oops\n---\nx\n",
	}, []string{"CLAUDE.md", "docs/good.md", "docs/bad.md", "docs/plain.md", "docs/broke.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	// Expect: bad.md (missing type) + broke.md (malformed) + plain.md (no block at
	// all, non-README — required under the Task 5 rule). good.md is clean; CLAUDE.md
	// carries a real block so it isn't incidental noise in a test about the other cases.
	got := map[string]string{}
	for _, f := range rep.FrontmatterFindings {
		got[f.File] = f.Detail
	}
	if len(got) != 3 {
		t.Fatalf("findings = %v, want exactly bad.md + broke.md + plain.md", rep.FrontmatterFindings)
	}
	if _, ok := got["docs/bad.md"]; !ok {
		t.Error("missing finding for docs/bad.md (no type)")
	}
	if _, ok := got["docs/broke.md"]; !ok {
		t.Error("missing finding for docs/broke.md (malformed)")
	}
	if _, ok := got["docs/plain.md"]; !ok {
		t.Error("missing finding for docs/plain.md (no frontmatter block, non-README)")
	}
}

// TestFrontmatterRequiredExceptReadme is the Task 5 pivot: a frontmatter block
// is now required on every tracked, non-ignored doc EXCEPT a README.md (any
// directory, matched by basename) — GitHub renders leading YAML as a metadata
// table in every directory view, so READMEs stay exempt everywhere.
func TestFrontmatterRequiredExceptReadme(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md":     "# Root\n[x](docs/x.md)\n",
		"sub/README.md": "# Sub, no frontmatter\n",
		"docs/x.md":     "# X, no frontmatter\n",
	}, []string{"README.md", "sub/README.md", "docs/x.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range rep.FrontmatterFindings {
		got = append(got, f.File)
	}
	want := []string{"docs/x.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frontmatter findings = %v, want %v", got, want)
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

func TestAuditReportsCycle(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md": "[a](a.md) [b](b.md)\n",
		"a.md":      "---\ntype: reference\nlinks: [{rel: part-of, to: b.md}]\n---\n",
		"b.md":      "---\ntype: reference\nlinks: [{rel: part-of, to: a.md}]\n---\n",
	}, []string{"CLAUDE.md", "a.md", "b.md"})
	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(rep.EdgeCycles) == 0 {
		t.Fatal("Audit found no cycle, want the a.md<->b.md part-of cycle")
	}
}

// TestFrontmatterEdgeAloneIsIsland asserts the Task 3 pivot invariant: a
// frontmatter typed edge (`links: [{rel: ..., to: ...}]`) is NOT a content
// edge. hub.md is root-reachable; leaf.md is linked nowhere by markdown link
// or path-mention — only referenced via hub.md's frontmatter see-also edge —
// so under the content-graph island rule leaf.md has zero inbound content
// edges and must be an orphan. (Superseded from a pre-pivot pair of tests
// that asserted the opposite — that a frontmatter edge alone made a doc
// reachable, back when reachability was one union BFS over links, mentions,
// AND frontmatter edges. Frontmatter edges now feed the metadata graph
// instead, Task 4.)
//
// The "./" prefix on the edge target is deliberate, not cosmetic: without it,
// the raw "to: docs/leaf.md" YAML text in the frontmatter block itself would
// satisfy mentionsPath's word-boundary check (preceding char is a space), so
// leaf.md would pick up a *mention* content edge purely by coincidence of the
// YAML syntax — passing the test for the wrong reason. "./docs/leaf.md" defeats
// that: ResolveEdgeTarget's filepath.Clean still resolves the frontmatter edge
// to "docs/leaf.md", but the raw text's "/" immediately before "docs/leaf.md"
// is a path-word byte, so mentionsPath's boundary check fails on it and no
// accidental mention edge is created. This isolates the assertion to the one
// mechanism under test: frontmatter edges don't feed the content graph.
func TestFrontmatterEdgeAloneIsIsland(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"CLAUDE.md":    "[hub](docs/hub.md)\n",
		"docs/hub.md":  "---\ntype: index\nlinks: [{rel: see-also, to: ./docs/leaf.md}]\n---\nhub body\n",
		"docs/leaf.md": "---\ntype: reference\n---\nleaf body\n",
	}, []string{"CLAUDE.md", "docs/hub.md", "docs/leaf.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	found := false
	for _, o := range rep.Orphans {
		if o == "docs/leaf.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("docs/leaf.md should be an orphan: a frontmatter see-also edge is not a content edge, orphans=%v", rep.Orphans)
	}
}
