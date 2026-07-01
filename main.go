package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"git.lsjc.au/lachlan/docaudit/internal/audit"
)

type multiFlag []string

func (m *multiFlag) String() string     { return "" }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "install-hook" {
		os.Exit(runInstallHook(args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

// runInstallHook writes a tracked .githooks/pre-push that runs docaudit, and
// points core.hooksPath at .githooks (activated for this clone). The hook fails
// closed by default (a missing docaudit blocks the push); --soft warns and
// allows, which suits repos cloned where docaudit may be absent (CI, public
// contributors).
func runInstallHook(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docaudit install-hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checks := fs.String("checks", "orphans,broken,untracked", "checks to gate (comma-separated)")
	force := fs.Bool("force", false, "overwrite an existing .githooks/pre-push")
	soft := fs.Bool("soft", false, "fail open (warn + allow) if docaudit isn't installed")
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
	if err := os.WriteFile(hookPath, []byte(hookScript(*checks, *soft)), 0o755); err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	if err := exec.Command("git", "-C", root, "config", "core.hooksPath", ".githooks").Run(); err != nil {
		fmt.Fprintf(stderr, "docaudit: git config core.hooksPath failed: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "installed .githooks/pre-push (checks: %s%s); core.hooksPath -> .githooks\n",
		*checks, map[bool]string{true: ", soft", false: ""}[*soft])
	return 0
}

func hookScript(checks string, soft bool) string {
	missing := `  echo "docaudit not installed (go install git.lsjc.au/lachlan/docaudit@latest)" >&2
  exit 1`
	if soft {
		missing = `  echo "docaudit not installed — skipping docs gate (go install git.lsjc.au/lachlan/docaudit@latest)" >&2
  exit 0`
	}
	return `#!/usr/bin/env bash
# docaudit pre-push gate — installed by ` + "`docaudit install-hook`" + `.
# Activated per clone via core.hooksPath -> .githooks.
set -euo pipefail
command -v docaudit >/dev/null || {
` + missing + `
}
exec docaudit --checks ` + checks + ` .
`
}

var checkNames = []string{"orphans", "broken", "untracked"}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docaudit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var roots, ignores multiFlag
	fs.Var(&roots, "root", "extra root doc to start reachability from (repeatable)")
	fs.Var(&ignores, "ignore", "glob to exclude from checks (repeatable)")
	checks := fs.String("checks", "orphans,broken,untracked", "comma-separated checks to run/gate: orphans,broken,untracked")
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
	findings := printReport(stdout, rep, selected)
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

// printReport prints the selected sections and reports whether any selected
// category has findings.
func printReport(w io.Writer, r audit.Report, sel map[string]bool) bool {
	fmt.Fprintf(w, "roots: %v\n", r.Roots)
	fmt.Fprintf(w, "tracked .md: %d   reachable: %d\n\n", r.TrackedMD, r.Reachable)

	findings := false
	if sel["orphans"] {
		fmt.Fprintf(w, "ORPHANS (%d) — docs unreachable by link/path-following:\n", len(r.Orphans))
		for _, o := range r.Orphans {
			fmt.Fprintf(w, "  %s\n", o)
		}
		fmt.Fprintln(w)
		findings = findings || len(r.Orphans) > 0
	}
	if sel["broken"] {
		fmt.Fprintf(w, "BROKEN LINKS (%d) — .md targets that don't exist:\n", len(r.BrokenLinks))
		for _, b := range r.BrokenLinks {
			fmt.Fprintf(w, "  %s:%d → %s\n", b.Source, b.Line, b.Target)
		}
		fmt.Fprintln(w)
		findings = findings || len(r.BrokenLinks) > 0
	}
	if sel["untracked"] {
		fmt.Fprintf(w, "UNTRACKED (%d) — .md on disk but not in git:\n", len(r.Untracked))
		for _, u := range r.Untracked {
			fmt.Fprintf(w, "  %s\n", u)
		}
		fmt.Fprintln(w)
		findings = findings || len(r.Untracked) > 0
	}
	if !findings {
		fmt.Fprintln(w, "clean ✓")
	}
	return findings
}
