package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lockyc/docgraph/v3/internal/audit"
)

// fullReport has findings in every gate check, so the report, the exit decision
// and the usage-log tiers can be pinned for each check at once.
func fullReport() (audit.Report, []audit.LeakFinding) {
	return audit.Report{
			Roots: []string{"CLAUDE.md"}, TrackedMD: 9, Reachable: 7,
			Orphans:             []string{"docs/lost.md"},
			BrokenLinks:         []audit.BrokenLink{{Source: "README.md", Line: 3, Target: "gone.md"}},
			BrokenAnchors:       []audit.BrokenLink{{Source: "README.md", Line: 4, Target: "docs/a.md#nope"}},
			Untracked:           []string{"notes.md"},
			FrontmatterFindings: []audit.FrontmatterFinding{{File: "docs/b.md", Detail: "missing required `type`"}},
			BrokenEdges:         []audit.BrokenEdge{{Source: "docs/c.md", Rel: "covers", Target: "src/x.go", Reason: "not in the repo"}},
			EdgeCycles:          [][]string{{"docs/d.md", "docs/e.md"}},
			Disconnected:        []string{"docs/f.md"},
		}, []audit.LeakFinding{
			{File: "src/y.go", Line: 7, Match: "SECRET", Pattern: "secret"},
		}
}

func allSelected() map[string]bool {
	sel := map[string]bool{}
	for _, name := range checkNames {
		sel[name] = true
	}
	return sel
}

// A finding in any one gate check alone fails the gate: the exit decision covers
// every check, not a hand-kept subset.
func TestPrintReportFailsOnAnySingleCheck(t *testing.T) {
	full, leaks := fullReport()
	for _, name := range checkNames {
		sel := map[string]bool{name: true}
		var out bytes.Buffer
		if !printReport(&out, full, leaks, sel) {
			t.Errorf("%s alone with a finding: printReport reported clean\n%s", name, out.String())
		}
	}
	var out bytes.Buffer
	if printReport(&out, full, leaks, map[string]bool{}) {
		t.Errorf("nothing selected: printReport reported findings\n%s", out.String())
	}
}

// The report, footer and log tiers for a run with a finding in every check are
// pinned byte for byte in testdata, so moving them onto one table changes nothing.
func TestReportAndRecordGolden(t *testing.T) {
	full, leaks := fullReport()
	var out bytes.Buffer
	printReport(&out, full, leaks, allSelected())
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var recs bytes.Buffer
	for _, level := range []int{1, 2, 3} {
		b, _ := json.Marshal(audit.BuildRecord("run", "/repo", "0.0.0", 1, full, leaks, allSelected(), level, now))
		recs.Write(append(b, '\n'))
	}
	for name, got := range map[string][]byte{"report.golden": out.Bytes(), "records.golden": recs.Bytes()} {
		path := filepath.Join("testdata", name)
		if os.Getenv("UPDATE_GOLDEN") != "" {
			os.MkdirAll("testdata", 0o755)
			os.WriteFile(path, got, 0o644)
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", name, got, want)
		}
	}
}
