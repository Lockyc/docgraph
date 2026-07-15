package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var sampleReport = Report{
	Roots:     []string{"CLAUDE.md"},
	TrackedMD: 5,
	Reachable: 3,
	Orphans:   []string{"docs/orphan.md"},
	BrokenLinks: []BrokenLink{
		{Source: "README.md", Line: 12, Target: "docs/missing.md"},
	},
	Untracked: []string{"loose.md"},
}

var sampleLeaks = []LeakFinding{
	{File: "src.go", Line: 4, Match: "SUPERSECRETTOKEN", Pattern: "SECRET"},
}

func allChecks() map[string]bool {
	return map[string]bool{"orphans": true, "broken": true, "untracked": true, "leaks": true}
}

func TestBuildRecordLevel1CountsOnly(t *testing.T) {
	ts := time.Date(2026, 7, 9, 21, 30, 0, 0, time.UTC)
	rec := BuildRecord("run", "/repo", "2.1.0", 1, sampleReport, sampleLeaks, allChecks(), 1, ts)

	if rec.Cmd != "run" || rec.Repo != "/repo" || rec.Version != "2.1.0" || rec.Exit != 1 {
		t.Errorf("scalar fields wrong: %+v", rec)
	}
	if rec.TS != "2026-07-09T21:30:00Z" {
		t.Errorf("ts = %q, want RFC3339 UTC", rec.TS)
	}
	wantChecks := []string{"broken", "leaks", "orphans", "untracked"}
	if strings.Join(rec.Checks, ",") != strings.Join(wantChecks, ",") {
		t.Errorf("checks = %v, want sorted %v", rec.Checks, wantChecks)
	}
	if rec.Counts["orphans"] != 1 || rec.Counts["broken"] != 1 ||
		rec.Counts["untracked"] != 1 || rec.Counts["leaks"] != 1 {
		t.Errorf("counts = %v", rec.Counts)
	}
	if rec.Files != nil || rec.Findings != nil {
		t.Errorf("level 1 must carry no Files/Findings, got files=%v findings=%v", rec.Files, rec.Findings)
	}
	// The record must never carry leak match text at level 1.
	b, _ := json.Marshal(rec)
	if strings.Contains(string(b), "SUPERSECRETTOKEN") {
		t.Errorf("level 1 record leaked match text: %s", b)
	}
}

func TestBuildRecordLevel2FilesNoMatchText(t *testing.T) {
	ts := time.Date(2026, 7, 9, 21, 30, 0, 0, time.UTC)
	rec := BuildRecord("run", "/repo", "2.1.0", 1, sampleReport, sampleLeaks, allChecks(), 2, ts)

	if rec.Findings != nil {
		t.Errorf("level 2 must not carry Findings, got %v", rec.Findings)
	}
	if got := rec.Files["orphans"]; len(got) != 1 || got[0] != "docs/orphan.md" {
		t.Errorf("files.orphans = %v", got)
	}
	if got := rec.Files["untracked"]; len(got) != 1 || got[0] != "loose.md" {
		t.Errorf("files.untracked = %v", got)
	}
	// broken and leaks carry file:line (a location), never the target/match text.
	if got := rec.Files["broken"]; len(got) != 1 || got[0] != "README.md:12" {
		t.Errorf("files.broken = %v, want [README.md:12]", got)
	}
	if got := rec.Files["leaks"]; len(got) != 1 || got[0] != "src.go:4" {
		t.Errorf("files.leaks = %v, want [src.go:4]", got)
	}
	// Critically: level 2 still must NOT leak the match text.
	b, _ := json.Marshal(rec)
	if strings.Contains(string(b), "SUPERSECRETTOKEN") {
		t.Errorf("level 2 record leaked match text: %s", b)
	}
}

func TestBuildRecordLevel3FindingsIncludeMatchText(t *testing.T) {
	ts := time.Date(2026, 7, 9, 21, 30, 0, 0, time.UTC)
	rec := BuildRecord("run", "/repo", "2.1.0", 1, sampleReport, sampleLeaks, allChecks(), 3, ts)

	if rec.Files != nil {
		t.Errorf("level 3 carries Findings (the superset), not Files; got files=%v", rec.Files)
	}
	if got := rec.Findings["broken"]; len(got) != 1 || got[0] != "README.md:12 → docs/missing.md" {
		t.Errorf("findings.broken = %v", got)
	}
	if got := rec.Findings["leaks"]; len(got) != 1 || got[0] != "src.go:4 → SUPERSECRETTOKEN (SECRET)" {
		t.Errorf("findings.leaks = %v", got)
	}
	if got := rec.Findings["orphans"]; len(got) != 1 || got[0] != "docs/orphan.md" {
		t.Errorf("findings.orphans = %v", got)
	}
	// Level 3 is the deliberate sink: match text IS present here.
	b, _ := json.Marshal(rec)
	if !strings.Contains(string(b), "SUPERSECRETTOKEN") {
		t.Errorf("level 3 should include match text, absent in: %s", b)
	}
}

func TestLogPathPrecedence(t *testing.T) {
	// env DOCGRAPH_LOG wins over the config path.
	t.Setenv("DOCGRAPH_LOG", "/env/usage.jsonl")
	got, err := LogPath("/cfg/usage.jsonl")
	if err != nil || got != "/env/usage.jsonl" {
		t.Fatalf("env should win: got %q, %v", got, err)
	}

	// With no env, the config path wins over the default.
	t.Setenv("DOCGRAPH_LOG", "")
	got, err = LogPath("/cfg/usage.jsonl")
	if err != nil || got != "/cfg/usage.jsonl" {
		t.Fatalf("config path should win over default: got %q, %v", got, err)
	}

	// With neither, default to $XDG_STATE_HOME/docgraph/usage.jsonl.
	t.Setenv("XDG_STATE_HOME", "/state")
	got, err = LogPath("")
	if err != nil || got != filepath.FromSlash("/state/docgraph/usage.jsonl") {
		t.Fatalf("XDG_STATE default wrong: got %q, %v", got, err)
	}
}

func TestLogRunAppendsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "usage.jsonl") // parent dir does not exist yet
	ts := time.Date(2026, 7, 9, 21, 30, 0, 0, time.UTC)

	r1 := BuildRecord("run", "/a", "2.1.0", 0, Report{}, nil, allChecks(), 1, ts)
	r2 := BuildRecord("run", "/b", "2.1.0", 1, sampleReport, sampleLeaks, allChecks(), 1, ts)
	if err := LogRun(path, r1); err != nil {
		t.Fatal(err)
	}
	if err := LogRun(path, r2); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 appended lines, got %d: %q", len(lines), b)
	}
	var back UsageRecord
	if err := json.Unmarshal([]byte(lines[1]), &back); err != nil {
		t.Fatalf("second line not valid JSON: %v", err)
	}
	if back.Repo != "/b" || back.Exit != 1 {
		t.Errorf("round-trip wrong: %+v", back)
	}
}

func TestLogRunBadPathErrsButDoesNotPanic(t *testing.T) {
	// Parent is a regular file, so MkdirAll under it must fail — LogRun returns the
	// error (the caller swallows it; a gate never fails because the log is unwritable).
	dir := t.TempDir()
	blocker := filepath.Join(dir, "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "usage.jsonl") // afile/usage.jsonl — afile is not a dir
	if err := LogRun(path, UsageRecord{}); err == nil {
		t.Error("want an error writing under a non-dir parent, got nil")
	}
}

func TestBuildRecordCountsNewChecks(t *testing.T) {
	rep := Report{
		FrontmatterFindings: []FrontmatterFinding{{File: "docs/a.md", Detail: "missing type"}},
		BrokenEdges:         []BrokenEdge{{Source: "docs/a.md", Rel: "covers", Target: "x.sh", Reason: "target does not exist"}},
		EdgeCycles:          [][]string{{"a.md", "b.md"}},
	}
	sel := map[string]bool{"frontmatter": true, "edges": true}
	rec := BuildRecord("run", "/r", "9", 1, rep, nil, sel, 1, time.Unix(0, 0).UTC())
	if rec.Counts["frontmatter"] != 1 {
		t.Errorf("counts[frontmatter] = %d, want 1", rec.Counts["frontmatter"])
	}
	if rec.Counts["edges"] != 2 { // 1 broken + 1 cycle
		t.Errorf("counts[edges] = %d, want 2 (broken+cycles)", rec.Counts["edges"])
	}
}

func TestBuildRecordLevel3FindsNewChecks(t *testing.T) {
	rep := Report{
		FrontmatterFindings: []FrontmatterFinding{{File: "docs/a.md", Detail: "missing type"}},
		BrokenEdges:         []BrokenEdge{{Source: "docs/a.md", Rel: "covers", Target: "x.sh", Reason: "target does not exist"}},
		EdgeCycles:          [][]string{{"a.md", "b.md"}},
	}
	sel := map[string]bool{"frontmatter": true, "edges": true}
	rec := BuildRecord("run", "/r", "9", 1, rep, nil, sel, 3, time.Unix(0, 0).UTC())
	if len(rec.Findings["frontmatter"]) != 1 {
		t.Errorf("findings[frontmatter] = %v", rec.Findings["frontmatter"])
	}
	if len(rec.Findings["edges"]) != 2 { // broken + cycle
		t.Errorf("findings[edges] = %v, want 2 entries", rec.Findings["edges"])
	}
}
