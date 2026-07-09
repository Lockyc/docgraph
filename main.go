package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
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
		case "leaks-rules":
			os.Exit(runLeaksRules(args[1:], os.Stdout, os.Stderr))
		case "version", "--version", "-v":
			fmt.Println("docaudit " + version)
			os.Exit(0)
		}
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

// checksFlagRemoved reports (with a migration message) whether args still use the
// removed --checks flag. docaudit enforces every check by default now — an
// allow-list of checks to *run* can't enforce, because a newly-added check is
// silently absent from every existing --checks list. Excluding a check is the
// explicit exception (--skip). Old hooks bake in `--checks …`, so a clear message
// beats flag's cryptic "flag provided but not defined".
func checksFlagRemoved(args []string, stderr io.Writer) bool {
	for _, a := range args {
		if a == "--checks" || a == "-checks" ||
			strings.HasPrefix(a, "--checks=") || strings.HasPrefix(a, "-checks=") {
			fmt.Fprintln(stderr, "docaudit: --checks was removed in v2 — all checks are enforced by default.")
			fmt.Fprintln(stderr, "  exclude one with --skip <check[,check]>, and regenerate any installed hook:")
			fmt.Fprintln(stderr, "  docaudit install-hook --force")
			return true
		}
	}
	return false
}

// runInstallHook writes a tracked .githooks/pre-push that runs docaudit, and
// points core.hooksPath at .githooks (activated for this clone). The hook fails
// closed: if docaudit isn't installed the push is blocked, because a gate that
// silently skips when its tool is missing is a false green, not a gate.
func runInstallHook(args []string, stdout, stderr io.Writer) int {
	if checksFlagRemoved(args, stderr) {
		return 2
	}
	fs := flag.NewFlagSet("docaudit install-hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	skip := fs.String("skip", "", "checks to EXCLUDE from the gate, comma-separated (default: none — all enforced)")
	var ignores multiFlag
	fs.Var(&ignores, "ignore", "path glob to exclude from the gated scan (repeatable)")
	force := fs.Bool("force", false, "overwrite an existing .githooks/pre-push")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := parseSkip(*skip); err != nil {
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
	if err := os.WriteFile(hookPath, []byte(hookScript(*skip, ignores)), 0o755); err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	if err := exec.Command("git", "-C", root, "config", "core.hooksPath", ".githooks").Run(); err != nil {
		fmt.Fprintf(stderr, "docaudit: git config core.hooksPath failed: %v\n", err)
		return 2
	}
	if *skip == "" {
		fmt.Fprintln(stdout, "installed .githooks/pre-push (enforcing all checks); core.hooksPath -> .githooks")
	} else {
		fmt.Fprintf(stdout, "installed .githooks/pre-push (enforcing all checks except %s); core.hooksPath -> .githooks\n", *skip)
	}
	return 0
}

// runLeaksRules exports the global leak config as a git-filter-repo --replace-text
// rules file on stdout (rules only — filter-repo has no comment syntax), with
// warnings + a drop summary on stderr. It is NON-destructive: it reads only the
// TOML, never git history; the actual history rewrite is a separate, external
// `git filter-repo --replace-text` step. Config resolution and the absent/malformed
// exit contract mirror the leaks scan.
func runLeaksRules(args []string, stdout, stderr io.Writer) int {
	if checksFlagRemoved(args, stderr) {
		return 2
	}
	fs := flag.NewFlagSet("docaudit leaks-rules", flag.ContinueOnError)
	fs.SetOutput(stderr)
	leaksConfig := fs.String("leaks-config", "", "path to the global leaks.toml (default: $DOCAUDIT_LEAKS or $XDG_CONFIG_HOME/docaudit/leaks.toml, else ~/.config/docaudit/leaks.toml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfgPath, err := resolveLeaksConfig(*leaksConfig)
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	cfg, err := loadLeakConfig(cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		// Absent config is NOT fatal (same stance as the scan): the global rules file
		// is the normal machine-local-only artifact, absent in CI / fresh clones.
		fmt.Fprintf(stderr, "docaudit: no leak rules file at %s — nothing to export.\n", cfgPath)
		return 0
	} else if err != nil {
		fmt.Fprintf(stderr, "docaudit: leaks config %s: %v\n", cfgPath, err)
		return 2
	}
	// Malformed regex / non-absolute [[dir]] path aren't caught by TOML decode —
	// validate via the same compile the scan runs. Fatal, like the scan.
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "docaudit: leaks config %s: %v\n", cfgPath, err)
		return 2
	}
	lines, dropped := audit.ReplaceTextRules(cfg)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	if dropped.Allows > 0 || dropped.Dirs > 0 {
		fmt.Fprintf(stderr, "docaudit: leaks-rules ignores %d allow/allow_regex and %d [[dir]] rule(s) — filter-repo\n", dropped.Allows, dropped.Dirs)
		fmt.Fprintln(stderr, "  rewrites by content across all paths/history, so exceptions and dir-scoping do not")
		fmt.Fprintln(stderr, "  apply. Review the rewrite result.")
	}
	return 0
}

func hookScript(skip string, ignores []string) string {
	args := ""
	if skip != "" {
		args += " --skip " + skip
	}
	for _, g := range ignores {
		args += " --ignore '" + g + "'"
	}
	runLine := `exec "$bin"` + args + ` .`
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
` + runLine + `
`
}

var checkNames = []string{"orphans", "broken", "untracked", "leaks"}

func run(args []string, stdout, stderr io.Writer) int {
	if checksFlagRemoved(args, stderr) {
		return 2
	}
	fs := flag.NewFlagSet("docaudit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var roots, ignores multiFlag
	fs.Var(&roots, "root", "extra root doc to start reachability from (repeatable)")
	fs.Var(&ignores, "ignore", "glob to exclude from checks (repeatable)")
	skip := fs.String("skip", "", "checks to EXCLUDE, comma-separated (default: none — all enforced: orphans,broken,untracked,leaks)")
	leaksConfig := fs.String("leaks-config", "", "path to the global leaks.toml (default: $DOCAUDIT_LEAKS or $XDG_CONFIG_HOME/docaudit/leaks.toml, else ~/.config/docaudit/leaks.toml)")
	config := fs.String("config", "", "path to the global config.toml, holding [log] (default: $DOCAUDIT_CONFIG or $XDG_CONFIG_HOME/docaudit/config.toml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	selected, err := parseSkip(*skip)
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	if len(selected) == 0 {
		fmt.Fprintln(stderr, "docaudit: every check skipped — nothing is being enforced")
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
		cfg, err := loadLeakConfig(cfgPath)
		if errors.Is(err, os.ErrNotExist) {
			// Absent config is NOT fatal: leaks runs by default (incl. CI, which has
			// no machine-local config), so a hard-fail would brick every push there.
			// The config is the sole source of rules — with none, the scan is a no-op,
			// and the warning nudges the owner to define their footprint file.
			fmt.Fprintf(stderr, "docaudit: no leak rules file at %s — the leaks check has no rules, so nothing is scanned;\n", cfgPath)
			fmt.Fprintln(stderr, "  add one (or pass --leaks-config) to define your leak patterns.")
			cfg = audit.LeakConfig{}
		} else if err != nil {
			// Present-but-malformed TOML IS fatal: a real config bug, not "not set up yet".
			fmt.Fprintf(stderr, "docaudit: leaks config %s: %v\n", cfgPath, err)
			return 2
		}
		leaks, err = audit.LeakScan(root, cfg, ignores)
		if err != nil {
			// A bad regexp in an otherwise-valid config surfaces here — also fatal.
			fmt.Fprintf(stderr, "docaudit: leaks config %s: %v\n", cfgPath, err)
			return 2
		}
	}
	findings := printReport(stdout, rep, leaks, selected)
	exit := 0
	if findings {
		exit = 1
	}
	maybeLog(*config, "run", root, exit, rep, leaks, selected, stderr)
	return exit
}

// maybeLog appends one usage record for this run when logging is opted in. It is
// best-effort and side-channel: it never changes the exit code and never returns an
// error. DOCAUDIT_NO_LOG short-circuits it. A malformed config.toml is warned about
// and disables logging — NOT fatal, unlike a malformed leaks.toml: logging is
// auxiliary, so a log-config typo must not block a push.
func maybeLog(configFlag, cmd, root string, exit int, rep audit.Report, leaks []audit.LeakFinding, sel map[string]bool, stderr io.Writer) {
	if os.Getenv("DOCAUDIT_NO_LOG") != "" {
		return
	}
	cfgPath, err := resolveConfig(configFlag)
	if err != nil {
		return
	}
	logCfg, err := loadLogConfig(cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		return // absent config → logging silently off (the normal CI/clone state).
	} else if err != nil {
		fmt.Fprintf(stderr, "docaudit: config %s: %v — logging disabled (non-fatal)\n", cfgPath, err)
		return
	}
	if !logCfg.Active() {
		return
	}
	logPath, err := audit.LogPath(logCfg.Path)
	if err != nil {
		return
	}
	rec := audit.BuildRecord(cmd, root, version, exit, rep, leaks, sel, logCfg.Level, time.Now())
	_ = audit.LogRun(logPath, rec) // best-effort: a gate never fails because the log is unwritable.
}

// resolveConfig resolves the global config.toml: --config > $DOCAUDIT_CONFIG >
// $XDG_CONFIG_HOME/docaudit/config.toml (else ~/.config/...). Same XDG discipline
// as resolveLeaksConfig — never os.UserConfigDir() (wrong on macOS for a CLI tool).
func resolveConfig(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if env := os.Getenv("DOCAUDIT_CONFIG"); env != "" {
		return env, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "docaudit", "config.toml"), nil
}

// loadLogConfig decodes the [log] table of config.toml. An absent file returns
// os.ErrNotExist (the caller treats it as silently-off); a malformed file returns a
// decode error the caller warns on without failing the run.
func loadLogConfig(path string) (audit.LogConfig, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return audit.LogConfig{}, err
	}
	var fc struct {
		Log audit.LogConfig `toml:"log"`
	}
	_, err := toml.DecodeFile(path, &fc)
	return fc.Log, err
}

// parseSkip returns the set of checks to RUN: every check by default, minus the
// comma-separated names in s. An unknown name is an error. Enforcement is the
// default; skipping is the explicit, per-repo exception (e.g. a nav-driven MkDocs
// repo skips orphans). A newly-added check is enforced everywhere automatically —
// nobody has to remember to add it to a run-list.
func parseSkip(s string) (map[string]bool, error) {
	sel := map[string]bool{}
	for _, name := range checkNames {
		sel[name] = true
	}
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
		delete(sel, c)
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
	// XDG, not os.UserConfigDir(): docaudit is a CLI tool, and os.UserConfigDir()
	// returns ~/Library/Application Support on macOS (Apple's GUI-app convention),
	// which is the wrong home for a dev tool. Honor $XDG_CONFIG_HOME, else ~/.config.
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "docaudit", "leaks.toml"), nil
}

func loadLeakConfig(path string) (audit.LeakConfig, error) {
	var cfg audit.LeakConfig
	_, err := toml.DecodeFile(path, &cfg)
	return cfg, err
}

// printReport prints the sections for the checks being run and reports whether any
// has findings. The output is written to be self-describing: a reader (often a
// fresh agent seeing only a failed `git push`) should learn from the text alone
// what docaudit is, what a finding means, why a non-zero exit aborts a push, and
// how to remediate. The banner prints always; the explain-and-remediate footer
// only on findings, so green/CI runs stay terse.
func printReport(w io.Writer, r audit.Report, leaks []audit.LeakFinding, sel map[string]bool) bool {
	fmt.Fprintln(w, "docaudit — enforces agent-facing repo hygiene: doc-graph reachability")
	fmt.Fprintln(w, "(orphans/broken/untracked .md) plus a content scan for configured leak patterns.")
	fmt.Fprintln(w, "All checks run by default; exclude one with --skip. Reads the doc graph and file content.")
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
	fmt.Fprintln(w, "non-zero exit aborted the push. A finding is a repo-hygiene problem — a doc an agent")
	fmt.Fprintln(w, "can't reach, a dead .md link, an untracked .md, or a configured leak pattern matched")
	fmt.Fprintln(w, "in tracked content.")
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
		fmt.Fprintln(w, "  LEAK      → genericise it, remove it, or add an `allow`/`allow_regex` (optionally")
		fmt.Fprintln(w, "              scoped under `[[dir]]`) to your leaks.toml if the match is legitimate.")
	}
	fmt.Fprintln(w, bar)
}
