package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LogConfig is the decoded [log] table of the global config.toml. Logging is
// opt-in: absent config → zero value → Active() false, so CI, fresh clones, and
// contributors never log without opting in. Separate from LeakConfig on purpose —
// leaks.toml is a dedicated rules file that may be synced on its own; logging is
// orthogonal.
type LogConfig struct {
	Enabled bool   `toml:"enabled"`
	Level   int    `toml:"level"` // 1 counts / 2 +paths / 3 +findings(incl leak match text)
	Path    string `toml:"path"`  // optional override; default $XDG_STATE_HOME/docgraph/usage.jsonl
}

// Active reports whether a run should be logged: enabled with a valid level.
func (c LogConfig) Active() bool { return c.Enabled && c.Level >= 1 && c.Level <= 3 }

// UsageRecord is one JSONL line per docgraph run. Level gates the optional detail:
// Files (paths, level ≥2) and Findings (paths + broken targets + leak MATCH TEXT,
// level 3) are the escalating tiers. Cmd is the seam for a future `docgraph drift`
// run to log through the same file with the same shape.
type UsageRecord struct {
	TS       string              `json:"ts"`
	Version  string              `json:"version"`
	Repo     string              `json:"repo"`
	Cmd      string              `json:"cmd"`
	Checks   []string            `json:"checks"`
	Exit     int                 `json:"exit"`
	Counts   map[string]int      `json:"counts"`
	Files    map[string][]string `json:"files,omitempty"`
	Findings map[string][]string `json:"findings,omitempty"`
}

// BuildRecord assembles a UsageRecord at the given level from a completed audit.
// Level 1 is counts only. Leak match text appears ONLY at level 3 — levels 1 and 2
// never write a leak Match, keeping the log out of the sensitive-string sink the
// leaks check exists to prevent.
func BuildRecord(cmd, repo, version string, exit int, rep Report, leaks []LeakFinding, sel map[string]bool, level int, now time.Time) UsageRecord {
	var checks []string
	for name := range sel {
		if sel[name] {
			checks = append(checks, name)
		}
	}
	sort.Strings(checks)

	counts := map[string]int{}
	if sel["orphans"] {
		counts["orphans"] = len(rep.Orphans)
	}
	if sel["broken"] {
		counts["broken"] = len(rep.BrokenLinks)
	}
	if sel["untracked"] {
		counts["untracked"] = len(rep.Untracked)
	}
	if sel["leaks"] {
		counts["leaks"] = len(leaks)
	}
	if sel["frontmatter"] {
		counts["frontmatter"] = len(rep.FrontmatterFindings)
	}
	if sel["edges"] {
		counts["edges"] = len(rep.BrokenEdges) + len(rep.EdgeCycles)
	}
	if sel["disconnected"] {
		counts["disconnected"] = len(rep.Disconnected)
	}

	rec := UsageRecord{
		TS:      now.Format(time.RFC3339),
		Version: version,
		Repo:    repo,
		Cmd:     cmd,
		Checks:  checks,
		Exit:    exit,
		Counts:  counts,
	}

	switch {
	case level >= 3:
		// Level 3 — the deliberate sink: paths plus broken targets and leak MATCH
		// text. The README warns this turns the log into the sensitive-string sink
		// the leaks check exists to prevent.
		f := map[string][]string{}
		if sel["orphans"] {
			f["orphans"] = append([]string{}, rep.Orphans...)
		}
		if sel["untracked"] {
			f["untracked"] = append([]string{}, rep.Untracked...)
		}
		if sel["broken"] {
			for _, b := range rep.BrokenLinks {
				f["broken"] = append(f["broken"], fmt.Sprintf("%s:%d → %s", b.Source, b.Line, b.Target))
			}
		}
		if sel["leaks"] {
			for _, l := range leaks {
				f["leaks"] = append(f["leaks"], fmt.Sprintf("%s:%d → %s (%s)", l.File, l.Line, l.Match, l.Pattern))
			}
		}
		if sel["frontmatter"] {
			for _, ff := range rep.FrontmatterFindings {
				f["frontmatter"] = append(f["frontmatter"], fmt.Sprintf("%s: %s", ff.File, ff.Detail))
			}
		}
		if sel["edges"] {
			for _, e := range rep.BrokenEdges {
				f["edges"] = append(f["edges"], fmt.Sprintf("%s [%s] → %s (%s)", e.Source, e.Rel, e.Target, e.Reason))
			}
			for _, cyc := range rep.EdgeCycles {
				f["edges"] = append(f["edges"], "cycle: "+strings.Join(cyc, " → "))
			}
		}
		if sel["disconnected"] {
			f["disconnected"] = append([]string{}, rep.Disconnected...)
		}
		rec.Findings = f
	case level == 2:
		// Level 2 — paths only. broken/leaks carry file:line (a location), never the
		// target or match text: the match string is what must not be logged here.
		f := map[string][]string{}
		if sel["orphans"] {
			f["orphans"] = append([]string{}, rep.Orphans...)
		}
		if sel["untracked"] {
			f["untracked"] = append([]string{}, rep.Untracked...)
		}
		if sel["broken"] {
			for _, b := range rep.BrokenLinks {
				f["broken"] = append(f["broken"], fmt.Sprintf("%s:%d", b.Source, b.Line))
			}
		}
		if sel["leaks"] {
			for _, l := range leaks {
				f["leaks"] = append(f["leaks"], fmt.Sprintf("%s:%d", l.File, l.Line))
			}
		}
		if sel["frontmatter"] {
			for _, ff := range rep.FrontmatterFindings {
				f["frontmatter"] = append(f["frontmatter"], ff.File)
			}
		}
		if sel["edges"] {
			for _, e := range rep.BrokenEdges {
				f["edges"] = append(f["edges"], e.Source)
			}
			for _, cyc := range rep.EdgeCycles {
				f["edges"] = append(f["edges"], "cycle: "+strings.Join(cyc, " → "))
			}
		}
		if sel["disconnected"] {
			f["disconnected"] = append([]string{}, rep.Disconnected...)
		}
		rec.Files = f
	}

	return rec
}

// LogPath resolves the usage-log file: DOCGRAPH_LOG env > cfgPath (config [log].path)
// > $XDG_STATE_HOME/docgraph/usage.jsonl (default ~/.local/state/... — XDG *state*,
// not config, matching the machine's ~/.local/state ledger convention).
func LogPath(cfgPath string) (string, error) {
	if env := os.Getenv("DOCGRAPH_LOG"); env != "" {
		return env, nil
	}
	if cfgPath != "" {
		return cfgPath, nil
	}
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "docgraph", "usage.jsonl"), nil
}

// LogRun appends rec as one newline-terminated JSON line to path, creating parent
// dirs as needed. Best-effort by contract: the caller ignores the returned error so
// a gate's exit code is never decided by whether the log is writable. A single
// O_APPEND write keeps concurrent runs' lines from interleaving (atomic under
// PIPE_BUF for the count/path tiers).
func LogRun(path string, rec UsageRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}
