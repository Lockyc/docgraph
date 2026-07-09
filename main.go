package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lockyc/docaudit/internal/audit"
)

type multiFlag []string

func (m *multiFlag) String() string     { return "" }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "install-hook":
			os.Exit(runInstallHook(args[1:], os.Stdout, os.Stderr))
		case "version", "--version", "-v":
			fmt.Println("docaudit " + version)
			os.Exit(0)
		}
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

// runInstallHook writes a tracked .githooks/pre-push that runs docaudit, and
// points core.hooksPath at .githooks (activated for this clone). The hook fails
// closed: if docaudit isn't installed the push is blocked, because a gate that
// silently skips when its tool is missing is a false green, not a gate.
func runInstallHook(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docaudit install-hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checks := fs.String("checks", "orphans,broken,untracked", "checks to gate (comma-separated)")
	force := fs.Bool("force", false, "overwrite an existing .githooks/pre-push")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := parseChecks(*checks); err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	root, err := audit.GitRoot(path)
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: not a git repository: %s\n", path)
		return 2
	}
	hookPath := filepath.Join(root, ".githooks", "pre-push")
	if _, err := os.Stat(hookPath); err == nil && !*force {
		fmt.Fprintf(stderr, "docaudit: %s already exists — integrate manually or pass --force\n", filepath.Join(".githooks", "pre-push"))
		return 2
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	if err := os.WriteFile(hookPath, []byte(hookScript(*checks)), 0o755); err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	if err := exec.Command("git", "-C", root, "config", "core.hooksPath", ".githooks").Run(); err != nil {
		fmt.Fprintf(stderr, "docaudit: git config core.hooksPath failed: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "installed .githooks/pre-push (checks: %s); core.hooksPath -> .githooks\n", *checks)
	return 0
}

func hookScript(checks string) string {
	return `#!/usr/bin/env bash
# docaudit pre-push gate — installed by 'docaudit install-hook'. Activated per
# clone via core.hooksPath -> .githooks. Fails closed: if docaudit can't be found
# the push is blocked (install: go install github.com/lockyc/docaudit@latest).
set -euo pipefail

# Resolve docaudit even under a minimal hook PATH. Git runs hooks with whatever
# PATH the caller had; GUI clients and some agent harnesses push with a bare PATH
# that omits ~/go/bin, so 'command -v docaudit' alone is unreliable and would
# make the gate fail-closed (blocked) purely because it couldn't see an installed
# binary. Fall back to the Go install dirs before giving up.
docaudit_bin() {
  if command -v docaudit >/dev/null 2>&1; then command -v docaudit; return; fi
  local d
  for d in "${GOBIN:-}" "${GOPATH:+${GOPATH%%:*}/bin}" "$HOME/go/bin"; do
    [ -n "$d" ] && [ -x "$d/docaudit" ] && { printf '%s\n' "$d/docaudit"; return; }
  done
  if command -v go >/dev/null 2>&1; then
    d="$(go env GOBIN 2>/dev/null)"; [ -z "$d" ] && d="$(go env GOPATH 2>/dev/null)/bin"
    [ -x "$d/docaudit" ] && { printf '%s\n' "$d/docaudit"; return; }
  fi
  return 1
}

if ! bin="$(docaudit_bin)"; then
  echo "docaudit: not found on PATH or in the Go bin dir — push blocked (fail-closed)." >&2
  echo "  install it: go install github.com/lockyc/docaudit@latest" >&2
  exit 1
fi
exec "$bin" --checks ` + checks + ` .
`
}

var checkNames = []string{"orphans", "broken", "untracked", "leaks"}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docaudit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var roots, ignores multiFlag
	fs.Var(&roots, "root", "extra root doc to start reachability from (repeatable)")
	fs.Var(&ignores, "ignore", "glob to exclude from checks (repeatable)")
	checks := fs.String("checks", "orphans,broken,untracked", "comma-separated checks to run/gate: orphans,broken,untracked")
	leaksConfig := fs.String("leaks-config", "", "path to the global leak rules file (default: $DOCAUDIT_LEAKS or os.UserConfigDir()/docaudit/leaks)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	selected, err := parseChecks(*checks)
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	root, err := audit.GitRoot(path)
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: not a git repository: %s\n", path)
		return 2
	}
	rep, err := audit.Audit(root, audit.Options{ExtraRoots: roots, Ignores: ignores})
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	var leaks []audit.LeakFinding
	if selected["leaks"] {
		cfgPath, err := resolveLeaksConfig(*leaksConfig)
		if err != nil {
			fmt.Fprintf(stderr, "docaudit: %v\n", err)
			return 2
		}
		rules, err := loadLeakRules(cfgPath)
		if err != nil {
			fmt.Fprintf(stderr, "docaudit: leaks selected but no rules file at %s — create it or pass --leaks-config\n", cfgPath)
			return 2
		}
		leaks, err = audit.LeakScan(root, rules, ignores)
		if err != nil {
			fmt.Fprintf(stderr, "docaudit: %v\n", err)
			return 2
		}
	}
	findings := printReport(stdout, rep, leaks, selected)
	if findings {
		return 1
	}
	return 0
}

func parseChecks(s string) (map[string]bool, error) {
	sel := map[string]bool{}
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		valid := false
		for _, name := range checkNames {
			if c == name {
				valid = true
			}
		}
		if !valid {
			return nil, fmt.Errorf("unknown check %q (valid: %s)", c, strings.Join(checkNames, ","))
		}
		sel[c] = true
	}
	if len(sel) == 0 {
		return nil, fmt.Errorf("no checks selected")
	}
	return sel, nil
}

func resolveLeaksConfig(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if env := os.Getenv("DOCAUDIT_LEAKS"); env != "" {
		return env, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "docaudit", "leaks"), nil
}

func loadLeakRules(path string) (audit.LeakRules, error) {
	f, err := os.Open(path)
	if err != nil {
		return audit.LeakRules{}, err
	}
	defer f.Close()
	return audit.ParseLeakRules(f)
}

// printReport prints the selected sections and reports whether any selected
// category has findings. The output is written to be self-describing: a reader
// (often a fresh agent seeing only a failed `git push`) should learn from the
// text alone what docaudit is, that a finding is a doc-graph — not code —
// problem, why a non-zero exit aborts a push, and how to remediate or bypass.
// The banner prints always; the explain-and-remediate footer only on findings,
// so green/CI runs stay terse.
func printReport(w io.Writer, r audit.Report, leaks []audit.LeakFinding, sel map[string]bool) bool {
	fmt.Fprintln(w, "docaudit — audits the agent-facing doc graph: every tracked .md should be reachable")
	fmt.Fprintln(w, "by an agent following links/path-mentions from a root doc. With --checks leaks it also")
	fmt.Fprintln(w, "scans tracked file content for configured leak patterns.")
	fmt.Fprintf(w, "roots: %v   tracked .md: %d   reachable: %d\n\n", r.Roots, r.TrackedMD, r.Reachable)

	if sel["orphans"] {
		fmt.Fprintf(w, "ORPHANS (%d) — docs unreachable by link/path-following:\n", len(r.Orphans))
		for _, o := range r.Orphans {
			fmt.Fprintf(w, "  %s\n", o)
		}
		fmt.Fprintln(w)
	}
	if sel["broken"] {
		fmt.Fprintf(w, "BROKEN LINKS (%d) — .md targets that don't exist:\n", len(r.BrokenLinks))
		for _, b := range r.BrokenLinks {
			fmt.Fprintf(w, "  %s:%d → %s\n", b.Source, b.Line, b.Target)
		}
		fmt.Fprintln(w)
	}
	if sel["untracked"] {
		fmt.Fprintf(w, "UNTRACKED (%d) — .md on disk but not in git:\n", len(r.Untracked))
		for _, u := range r.Untracked {
			fmt.Fprintf(w, "  %s\n", u)
		}
		fmt.Fprintln(w)
	}
	if sel["leaks"] {
		fmt.Fprintf(w, "LEAKS (%d) — tree content matching a leak pattern:\n", len(leaks))
		for _, l := range leaks {
			fmt.Fprintf(w, "  %s:%d → %s  (%s)\n", l.File, l.Line, l.Match, l.Pattern)
		}
		fmt.Fprintln(w)
	}

	orphans := sel["orphans"] && len(r.Orphans) > 0
	broken := sel["broken"] && len(r.BrokenLinks) > 0
	untracked := sel["untracked"] && len(r.Untracked) > 0
	leaksFound := sel["leaks"] && len(leaks) > 0
	if !orphans && !broken && !untracked && !leaksFound {
		fmt.Fprintln(w, "clean ✓")
		return false
	}

	n := 0
	if sel["orphans"] {
		n += len(r.Orphans)
	}
	if sel["broken"] {
		n += len(r.BrokenLinks)
	}
	if sel["untracked"] {
		n += len(r.Untracked)
	}
	if sel["leaks"] {
		n += len(leaks)
	}
	printFailureFooter(w, n, orphans, broken, untracked, leaksFound)
	return true
}

// printFailureFooter explains, in plain text, why docaudit is exiting non-zero
// and how to act on it — so nobody has to reverse-engineer the gate from a bare
// "failed to push some refs". Only the fix lines for checks that actually have
// findings are shown.
func printFailureFooter(w io.Writer, n int, orphans, broken, untracked, leaks bool) {
	bar := strings.Repeat("─", 82)
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "docaudit: %d finding(s) in gated checks → exiting non-zero.\n", n)
	fmt.Fprintln(w, "Its intended use is a pre-push gate, so if a git push just failed, this is why: the")
	fmt.Fprintln(w, "non-zero exit aborted the push. A finding is a doc-graph problem (a doc an agent")
	fmt.Fprintln(w, "can't reach, a dead .md link, an untracked .md) — not a code problem.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Fix the findings listed above:")
	if orphans {
		fmt.Fprintln(w, "  ORPHAN    → link it in from a reachable doc; or `--ignore '<glob>'` (a")
		fmt.Fprintln(w, "              .docauditignore entry) if it is intentionally standalone; or delete it.")
	}
	if broken {
		fmt.Fprintln(w, "  BROKEN    → repair or remove the dead .md link at the shown file:line.")
	}
	if untracked {
		fmt.Fprintln(w, "  UNTRACKED → `git add` it; or delete/ignore it.")
	}
	if leaks {
		fmt.Fprintln(w, "  LEAK      → genericise it, remove it, or add a `!` allow-exception to your")
		fmt.Fprintln(w, "              leak rules file if the match is legitimate.")
	}
	fmt.Fprintln(w, bar)
}
