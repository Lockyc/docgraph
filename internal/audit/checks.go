package audit

import (
	"fmt"
	"strings"
)

// Finding is one gate finding at each detail it is shown in: Report is the gate's
// report line, Location the level-2 log entry (a path or file:line, never a
// target or leak match), Detail the level-3 log entry.
type Finding struct {
	Report, Location, Detail string
}

// Check is one whole-state gate check: its --skip name, how its report section
// and footer fix read, and its findings in a completed audit.
type Check struct {
	Name     string
	Heading  string   // the report section's label, e.g. "BROKEN LINKS"
	Title    string   // what the section lists, after its count
	Fix      []string // the footer's fix lines
	Findings func(r Report, leaks []LeakFinding) []Finding
}

// Checks is every whole-state gate check, in report order — the one list the
// --skip vocabulary, the report, the footer, the exit code and the usage log all
// read. A check added here is enforced, reported and logged everywhere.
var Checks = []Check{
	{
		Name: "orphans", Heading: "ORPHANS", Title: "docs unreachable by link/path-following",
		Fix: []string{
			"  ORPHAN    → link it in from a reachable doc; or `--ignore '<glob>'` (a",
			"              .docgraphignore entry) if it is intentionally standalone; or delete it.",
		},
		Findings: func(r Report, _ []LeakFinding) []Finding { return same(r.Orphans) },
	},
	{
		Name: "broken", Heading: "BROKEN LINKS", Title: ".md targets that don't exist",
		Fix:      []string{"  BROKEN    → repair or remove the dead .md link at the shown file:line."},
		Findings: func(r Report, _ []LeakFinding) []Finding { return linkFindings(r.BrokenLinks) },
	},
	{
		Name: "anchors", Heading: "ANCHORS", Title: "#fragments no heading or id in the target produces",
		Fix: []string{
			"  ANCHOR    → point the #fragment at a heading the target has (its GitHub slug, or",
			"              a `{#id}`/`id=\"…\"` it declares), or drop the fragment.",
		},
		Findings: func(r Report, _ []LeakFinding) []Finding { return linkFindings(r.BrokenAnchors) },
	},
	{
		Name: "untracked", Heading: "UNTRACKED", Title: ".md on disk but not in git",
		Fix:      []string{"  UNTRACKED → `git add` it; or delete/ignore it."},
		Findings: func(r Report, _ []LeakFinding) []Finding { return same(r.Untracked) },
	},
	{
		Name: "frontmatter", Heading: "FRONTMATTER", Title: "malformed frontmatter or missing required `type`",
		Fix: []string{
			"  FRONTMATTER → add a `---` frontmatter block with a `type:` (every doc except a",
			"                README.md needs one); or fix malformed YAML; or `--skip frontmatter`.",
		},
		Findings: func(r Report, _ []LeakFinding) []Finding {
			var fs []Finding
			for _, f := range r.FrontmatterFindings {
				fs = append(fs, Finding{f.File + " → " + f.Detail, f.File, f.File + ": " + f.Detail})
			}
			return fs
		},
	},
	{
		Name: "edges", Heading: "EDGES", Title: "frontmatter typed edges with a missing target, or a part-of/supersedes cycle",
		Fix: []string{
			"  EDGES     → fix the frontmatter `to:` target (path is repo-root-relative), or remove the edge.",
			"              A cycle means part-of/supersedes edges form a loop — break it.",
		},
		Findings: func(r Report, _ []LeakFinding) []Finding {
			var fs []Finding
			for _, e := range r.BrokenEdges {
				line := fmt.Sprintf("%s [%s] → %s (%s)", e.Source, e.Rel, e.Target, e.Reason)
				fs = append(fs, Finding{line, e.Source, line})
			}
			for _, cyc := range r.EdgeCycles {
				path := "cycle: " + strings.Join(cyc, " → ")
				fs = append(fs, Finding{path + " → " + cyc[0], path, path})
			}
			return fs
		},
	},
	{
		Name: "leaks", Heading: "LEAKS", Title: "tree content matching a leak pattern",
		Fix: []string{
			"  LEAK      → genericise it, remove it, or add an `allow`/`allow_regex` (optionally",
			"              scoped under `[[dir]]`) to your leaks.toml if the match is legitimate.",
		},
		// Only Report and Detail carry the match text: level 2 must never log it.
		Findings: func(_ Report, leaks []LeakFinding) []Finding {
			var fs []Finding
			for _, l := range leaks {
				loc := fmt.Sprintf("%s:%d", l.File, l.Line)
				fs = append(fs, Finding{
					fmt.Sprintf("%s → %s  (%s)", loc, l.Match, l.Pattern),
					loc,
					fmt.Sprintf("%s → %s (%s)", loc, l.Match, l.Pattern),
				})
			}
			return fs
		},
	},
	{
		Name: "disconnected", Heading: "DISCONNECTED", Title: "frontmatter docs with no doc→doc edge (metadata island)",
		Fix: []string{
			"  DISCONNECTED → give the doc a frontmatter part-of/see-also/depends-on edge to a",
			"                related doc (a covers→code edge does not count); or `--skip disconnected`.",
		},
		Findings: func(r Report, _ []LeakFinding) []Finding { return same(r.Disconnected) },
	},
}

// same is the findings for a list of paths, each shown as itself at every detail.
func same(paths []string) []Finding {
	var fs []Finding
	for _, p := range paths {
		fs = append(fs, Finding{p, p, p})
	}
	return fs
}

// linkFindings is the findings for broken links or anchors: file:line → target,
// with only the location at level 2.
func linkFindings(links []BrokenLink) []Finding {
	var fs []Finding
	for _, b := range links {
		loc := fmt.Sprintf("%s:%d", b.Source, b.Line)
		line := loc + " → " + b.Target
		fs = append(fs, Finding{line, loc, line})
	}
	return fs
}

// CheckNames is the --skip vocabulary: every Checks name, in order.
func CheckNames() []string {
	names := make([]string, len(Checks))
	for i, c := range Checks {
		names[i] = c.Name
	}
	return names
}

// CheckCounts is each gate check's finding count, keyed by check name.
func CheckCounts(r Report, leaks []LeakFinding) map[string]int {
	counts := map[string]int{}
	for _, c := range Checks {
		counts[c.Name] = len(c.Findings(r, leaks))
	}
	return counts
}
