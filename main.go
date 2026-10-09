package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/lockyc/docgraph/v3/internal/audit"
)

// notARepo reports a GitRoot failure, appending git's own explanation when it
// has one. The case that needs it: a linked worktree whose `.git` gitdir pointer
// no longer resolves (main repo moved or pruned, or unreachable from a sandbox)
// — the bare message reads as "docgraph can't run in a worktree", which is wrong
// and unactionable, while git's stderr names the dangling pointer target.
func notARepo(stderr io.Writer, path string, err error) int {
	if msg := strings.TrimSpace(fmt.Sprint(err)); msg != "" && !strings.Contains(msg, "exit status") {
		fmt.Fprintf(stderr, "docgraph: not a git repository: %s (%s)\n", path, msg)
	} else {
		fmt.Fprintf(stderr, "docgraph: not a git repository: %s\n", path)
	}
	return 2
}

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
		case "footgun-drift":
			os.Exit(runFootgunDrift(args[1:], os.Stdout, os.Stderr))
		case "covers-drift":
			os.Exit(runCoversDrift(args[1:], os.Stdin, os.Stdout, os.Stderr))
		case "doc-drift":
			os.Exit(runDocDrift(args[1:], os.Stdin, os.Stdout, os.Stderr))
		case "schema":
			os.Exit(runSchema(os.Stdout))
		case "covers":
			os.Exit(runCovers(args[1:], os.Stdout, os.Stderr))
		case "index":
			os.Exit(runIndex(args[1:], os.Stdout, os.Stderr))
		case "stale":
			os.Exit(runStale(args[1:], os.Stdout, os.Stderr))
		case "graph":
			os.Exit(runGraph(args[1:], os.Stdout, os.Stderr))
		case "version", "--version", "-v":
			fmt.Println("docgraph " + version)
			os.Exit(0)
		}
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

// checksFlagRemoved reports (with a migration message) whether args still use the
// removed --checks flag. docgraph enforces every check by default now — an
// allow-list of checks to *run* can't enforce, because a newly-added check is
// silently absent from every existing --checks list. Excluding a check is the
// explicit exception (--skip). Old hooks bake in `--checks …`, so a clear message
// beats flag's cryptic "flag provided but not defined".
func checksFlagRemoved(args []string, stderr io.Writer) bool {
	for _, a := range args {
		if a == "--checks" || a == "-checks" ||
			strings.HasPrefix(a, "--checks=") || strings.HasPrefix(a, "-checks=") {
			fmt.Fprintln(stderr, "docgraph: --checks was removed in v2 — all checks are enforced by default.")
			fmt.Fprintln(stderr, "  exclude one with --skip <check[,check]>, and regenerate any installed hook:")
			fmt.Fprintln(stderr, "  docgraph install-hook --force")
			return true
		}
	}
	return false
}

// runInstallHook writes a tracked .githooks/pre-push that runs docgraph, and
// points core.hooksPath at .githooks (activated for this clone). The hook fails
// closed: if docgraph isn't installed the push is blocked, because a gate that
// silently skips when its tool is missing is a false green, not a gate.
func runInstallHook(args []string, stdout, stderr io.Writer) int {
	if checksFlagRemoved(args, stderr) {
		return 2
	}
	fs := flag.NewFlagSet("docgraph install-hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	skip := fs.String("skip", "", "checks to EXCLUDE from the gate, comma-separated (default: none — all enforced)")
	var ignores multiFlag
	fs.Var(&ignores, "ignore", "path glob to exclude from the gated scan (repeatable)")
	force := fs.Bool("force", false, "overwrite an existing .githooks/pre-push")
	noFootgun := fs.Bool("no-footgun-drift", false, "omit the diff-scoped footgun-drift check from the generated hook")
	noCovers := fs.Bool("no-covers-drift", false, "omit the diff-scoped covers-drift check from the generated hook")
	positional, perr := parseArgs(fs, args)
	if perr != nil {
	}
	if _, err := parseSkip(*skip); err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	path := "."
	if len(positional) > 0 {
		path = positional[0]
	}
	root, err := audit.GitRoot(path)
	if err != nil {
		return notARepo(stderr, path, err)
	}
	hookPath := filepath.Join(root, ".githooks", "pre-push")
	if _, err := os.Stat(hookPath); err == nil && !*force {
		fmt.Fprintf(stderr, "docgraph: %s already exists — integrate manually or pass --force\n", filepath.Join(".githooks", "pre-push"))
		return 2
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	if err := os.WriteFile(hookPath, []byte(hookScript(*skip, ignores, *noFootgun, *noCovers)), 0o755); err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	if err := audit.GitCmd(root, "config", "core.hooksPath", ".githooks").Run(); err != nil {
		fmt.Fprintf(stderr, "docgraph: git config core.hooksPath failed: %v\n", err)
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
	fs := flag.NewFlagSet("docgraph leaks-rules", flag.ContinueOnError)
	fs.SetOutput(stderr)
	leaksConfig := fs.String("leaks-config", "", "path to the global leaks.toml (default: $DOCGRAPH_LEAKS or $XDG_CONFIG_HOME/docgraph/leaks.toml, else ~/.config/docgraph/leaks.toml)")
	if _, perr := parseArgs(fs, args); perr != nil {
		return 2
	}
	cfgPath, err := resolveLeaksConfig(*leaksConfig)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	cfg, err := loadLeakConfig(cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		// Absent config is NOT fatal (same stance as the scan): the global rules file
		// is the normal machine-local-only artifact, absent in CI / fresh clones.
		fmt.Fprintf(stderr, "docgraph: no leak rules file at %s — nothing to export.\n", cfgPath)
		return 0
	} else if err != nil {
		fmt.Fprintf(stderr, "docgraph: leaks config %s: %v\n", cfgPath, err)
		return 2
	}
	// Malformed regex / non-absolute [[dir]] path aren't caught by TOML decode —
	// validate via the same compile the scan runs. Fatal, like the scan.
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "docgraph: leaks config %s: %v\n", cfgPath, err)
		return 2
	}
	lines, dropped := audit.ReplaceTextRules(cfg)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	if dropped.Allows > 0 || dropped.Dirs > 0 {
		fmt.Fprintf(stderr, "docgraph: leaks-rules ignores %d allow/allow_regex and %d [[dir]] rule(s) — filter-repo\n", dropped.Allows, dropped.Dirs)
		fmt.Fprintln(stderr, "  rewrites by content across all paths/history, so exceptions and dir-scoping do not")
		fmt.Fprintln(stderr, "  apply. Review the rewrite result.")
	}
	return 0
}

// docgraphBinFunc returns the docgraph_bin() shell function the generated hook
// uses to resolve docgraph even under a minimal hook PATH. Git runs hooks with
// whatever PATH the caller had; GUI clients and some agent harnesses push with a
// bare PATH that omits ~/go/bin, so 'command -v docgraph' alone is unreliable and
// would make the gate fail-closed (blocked) purely because it couldn't see an
// installed binary. Fall back to the Go install dirs before giving up. Both the
// whole-state check and footgun-drift share this one resolution — do not retype
// it inline a second time.
func docgraphBinFunc() string {
	return `docgraph_bin() {
  if command -v docgraph >/dev/null 2>&1; then command -v docgraph; return; fi
  local d
  for d in "${GOBIN:-}" "${GOPATH:+${GOPATH%%:*}/bin}" "$HOME/go/bin"; do
    [ -n "$d" ] && [ -x "$d/docgraph" ] && { printf '%s\n' "$d/docgraph"; return; }
  done
  if command -v go >/dev/null 2>&1; then
    d="$(go env GOBIN 2>/dev/null)"; [ -z "$d" ] && d="$(go env GOPATH 2>/dev/null)/bin"
    [ -x "$d/docgraph" ] && { printf '%s\n' "$d/docgraph"; return; }
  fi
  return 1
}`
}

// hookScript generates the tracked .githooks/pre-push gate. It runs the
// whole-state check (`docgraph .`) and then the diff-scoped riders — unless
// noFootgun, `docgraph footgun-drift`; unless noCovers, `docgraph covers-drift`
// — each fed git's pre-push stdin (ref lines: local/remote SHA pairs for what's
// being pushed) so they can scope themselves to the pushed commit range. The
// whole-state line is a plain command, not `exec` — `exec` would replace the
// shell process, so a failing `docgraph .` would never reach the rider lines
// below it. Under `set -e` a non-zero exit from the whole-state command aborts
// the script (and the push) immediately, so its fail-closed behavior is
// unchanged. Both rider lines are ADVISORY (`|| true`, and each subcommand exits
// 0 on findings): they print a nag but never block.
func hookScript(skip string, ignores []string, noFootgun, noCovers bool) string {
	args := ""
	if skip != "" {
		args += " --skip " + skip
	}
	for _, g := range ignores {
		args += " --ignore '" + g + "'"
	}
	stateLine := `"$bin"` + args + ` .`
	footgun := ""
	if !noFootgun {
		// Advisory, never blocks: footgun-drift exits 0 on findings, and `|| true`
		// swallows even an operational error so this line can never abort a push.
		footgun = `
# Diff-scoped ADVISORY nag: footgun declarations ADDED in the pushed range. Never blocks.
printf '%s' "$refs" | "$bin" footgun-drift . || true`
	}
	covers := ""
	if !noCovers {
		// Advisory on the same terms as footgun-drift above: covers-drift exits 0 on
		// findings, and `|| true` swallows even an operational error.
		covers = `
# Diff-scoped ADVISORY nag: docs whose 'covers' edge points at code this push changed. Never blocks.
printf '%s' "$refs" | "$bin" covers-drift . || true`
	}
	return `#!/usr/bin/env bash
# docgraph pre-push gate — installed by 'docgraph install-hook'. Activated per
# clone via core.hooksPath -> .githooks. Fails closed: if docgraph can't be found
# the push is blocked (install: go install github.com/lockyc/docgraph/v3@latest).
set -euo pipefail
refs="$(cat)"   # git feeds pre-push ref lines on stdin; captured before running anything

` + docgraphBinFunc() + `

if ! bin="$(docgraph_bin)"; then
  echo "docgraph: not found on PATH or in the Go bin dir — push blocked (fail-closed)." >&2
  echo "  install it: go install github.com/lockyc/docgraph/v3@latest" >&2
  exit 1
fi
` + stateLine + footgun + covers + `
`
}

var checkNames = []string{"orphans", "broken", "untracked", "leaks", "frontmatter", "edges", "disconnected"}

func run(args []string, stdout, stderr io.Writer) int {
	if checksFlagRemoved(args, stderr) {
		return 2
	}
	fs := flag.NewFlagSet("docgraph", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var roots, ignores multiFlag
	fs.Var(&roots, "root", "extra root doc to start reachability from (repeatable)")
	fs.Var(&ignores, "ignore", "glob to exclude from checks (repeatable)")
	skip := fs.String("skip", "", "checks to EXCLUDE, comma-separated (default: none — all enforced: orphans,broken,untracked,leaks,frontmatter,edges,disconnected)")
	leaksConfig := fs.String("leaks-config", "", "path to the global leaks.toml (default: $DOCGRAPH_LEAKS or $XDG_CONFIG_HOME/docgraph/leaks.toml, else ~/.config/docgraph/leaks.toml)")
	config := fs.String("config", "", "path to the global config.toml, holding [log] (default: $DOCGRAPH_CONFIG or $XDG_CONFIG_HOME/docgraph/config.toml)")
	positional, perr := parseArgs(fs, args)
	if perr != nil {
	}
	selected, err := parseSkip(*skip)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	if len(selected) == 0 {
		fmt.Fprintln(stderr, "docgraph: every check skipped — nothing is being enforced")
	}
	path := "."
	if len(positional) > 0 {
		path = positional[0]
	}
	root, err := audit.GitRoot(path)
	if err != nil {
		return notARepo(stderr, path, err)
	}
	rep, err := audit.Audit(root, audit.Options{ExtraRoots: roots, Ignores: ignores})
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	var leaks []audit.LeakFinding
	if selected["leaks"] {
		cfgPath, err := resolveLeaksConfig(*leaksConfig)
		if err != nil {
			fmt.Fprintf(stderr, "docgraph: %v\n", err)
			return 2
		}
		cfg, err := loadLeakConfig(cfgPath)
		if errors.Is(err, os.ErrNotExist) {
			// Absent config is NOT fatal: leaks runs by default (incl. CI, which has
			// no machine-local config), so a hard-fail would brick every push there.
			// The config is the sole source of rules — with none, the scan is a no-op,
			// and the warning nudges the owner to define their footprint file.
			fmt.Fprintf(stderr, "docgraph: no leak rules file at %s — the leaks check has no rules, so nothing is scanned;\n", cfgPath)
			fmt.Fprintln(stderr, "  add one (or pass --leaks-config) to define your leak patterns.")
			cfg = audit.LeakConfig{}
		} else if err != nil {
			// Present-but-malformed TOML IS fatal: a real config bug, not "not set up yet".
			fmt.Fprintf(stderr, "docgraph: leaks config %s: %v\n", cfgPath, err)
			return 2
		}
		leaks, err = audit.LeakScan(root, cfg, ignores)
		if err != nil {
			// A bad regexp in an otherwise-valid config surfaces here — also fatal.
			fmt.Fprintf(stderr, "docgraph: leaks config %s: %v\n", cfgPath, err)
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
// error. DOCGRAPH_NO_LOG short-circuits it. A malformed config.toml is warned about
// and disables logging — NOT fatal, unlike a malformed leaks.toml: logging is
// auxiliary, so a log-config typo must not block a push.
func maybeLog(configFlag, cmd, root string, exit int, rep audit.Report, leaks []audit.LeakFinding, sel map[string]bool, stderr io.Writer) {
	if os.Getenv("DOCGRAPH_NO_LOG") != "" {
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
		fmt.Fprintf(stderr, "docgraph: config %s: %v — logging disabled (non-fatal)\n", cfgPath, err)
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

// resolveConfig resolves the global config.toml: --config > $DOCGRAPH_CONFIG >
// $XDG_CONFIG_HOME/docgraph/config.toml (else ~/.config/...). Same XDG discipline
// as resolveLeaksConfig — never os.UserConfigDir() (wrong on macOS for a CLI tool).
func resolveConfig(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if env := os.Getenv("DOCGRAPH_CONFIG"); env != "" {
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
	return filepath.Join(dir, "docgraph", "config.toml"), nil
}

// loadLogConfig decodes the [log] table of config.toml. An absent file returns
// os.ErrNotExist (the caller treats it as silently-off); a malformed file returns a
// decode error the caller warns on without failing the run. An absent `level`
// defaults to 1 (counts only), so `enabled = true` alone logs at the safe tier.
func loadLogConfig(path string) (audit.LogConfig, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return audit.LogConfig{}, err
	}
	var fc struct {
		Log audit.LogConfig `toml:"log"`
	}
	fc.Log.Level = 1
	_, err := toml.DecodeFile(path, &fc)
	return fc.Log, err
}

// parseSkip returns the set of checks to RUN: every check by default, minus the
// comma-separated names in s. An unknown name is an error. Enforcement is the
// default; skipping is the explicit, per-repo exception (e.g. a nav-driven MkDocs
// repo skips orphans). A newly-added check is enforced everywhere automatically —
// nobody has to remember to add it to a run-list.
// parseArgs parses flags that appear ANYWHERE in args, returning the positional
// arguments. Go's flag package stops at the first non-flag argument, so a plain
// fs.Parse turns `docgraph . --skip leaks` into path="." plus two ignored
// strings: the skip is silently dropped and the check it named runs anyway. A
// gate flag that quietly does nothing is the exact failure this tool exists to
// prevent, and it reads as "docgraph ignored me" rather than as a usage error.
// Re-parsing what follows each positional lets flag itself decide which tokens
// are flag values, so `--ignore x` is never mistaken for a positional.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
}

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
	if env := os.Getenv("DOCGRAPH_LEAKS"); env != "" {
		return env, nil
	}
	// XDG, not os.UserConfigDir(): docgraph is a CLI tool, and os.UserConfigDir()
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
	return filepath.Join(dir, "docgraph", "leaks.toml"), nil
}

// loadLeakConfig decodes the global leaks.toml, rejecting any key the schema
// doesn't recognize. toml.DecodeFile silently ignores unknown keys by
// default, which is exactly the failure mode groups introduced: a typo like
// "ignore_group" for "ignore_groups" decodes with err == nil and a
// zero-value field, so a [[dir]] silently falls back to the DefaultGroup
// default instead of the named group the owner meant — inverting which deny
// class a blanket ignore leaves live. Checking MetaData.Undecoded() turns
// that into the same fatal-config path a malformed TOML file already takes.
func loadLeakConfig(path string) (audit.LeakConfig, error) {
	var cfg audit.LeakConfig
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return cfg, fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
	}
	return cfg, nil
}

// printReport prints the outcome of the checks being run and reports whether any
// has findings. A clean run is a single terse line, so a green pre-push gate does
// not bury the terminal in headers nobody reads. Only on findings does the output
// turn self-describing — a reader (often a fresh agent seeing only a failed `git
// push`) should then learn from the text alone what docgraph is, what a finding
// means, why a non-zero exit aborts a push, and how to remediate: banner, the
// sections that actually have findings, and the explain-and-remediate footer.
func printReport(w io.Writer, r audit.Report, leaks []audit.LeakFinding, sel map[string]bool) bool {
	orphans := sel["orphans"] && len(r.Orphans) > 0
	broken := sel["broken"] && len(r.BrokenLinks) > 0
	untracked := sel["untracked"] && len(r.Untracked) > 0
	frontmatter := sel["frontmatter"] && len(r.FrontmatterFindings) > 0
	edges := sel["edges"] && (len(r.BrokenEdges) > 0 || len(r.EdgeCycles) > 0)
	leaksFound := sel["leaks"] && len(leaks) > 0
	disconnected := sel["disconnected"] && len(r.Disconnected) > 0
	if !orphans && !broken && !untracked && !frontmatter && !edges && !leaksFound && !disconnected {
		fmt.Fprintf(w, "docgraph: clean ✓ (%d tracked .md, %d reachable, 0 findings)\n", r.TrackedMD, r.Reachable)
		return false
	}

	fmt.Fprintln(w, "docgraph — enforces agent-facing repo hygiene across two doc graphs: the")
	fmt.Fprintln(w, "content graph (findability — orphans flags an island no prose reference reaches)")
	fmt.Fprintln(w, "and the metadata graph (structure — disconnected flags a frontmatter doc with no")
	fmt.Fprintln(w, "doc→doc edge). Frontmatter is required on every doc except README. Plus broken")
	fmt.Fprintln(w, "typed-edge/link targets, untracked .md, and a content scan for leak patterns.")
	fmt.Fprintln(w, "All checks run by default; exclude one with --skip. Reads the doc graph and file content.")
	fmt.Fprintf(w, "roots: %v   tracked .md: %d   reachable: %d\n\n", r.Roots, r.TrackedMD, r.Reachable)

	if orphans {
		fmt.Fprintf(w, "ORPHANS (%d) — docs unreachable by link/path-following:\n", len(r.Orphans))
		for _, o := range r.Orphans {
			fmt.Fprintf(w, "  %s\n", o)
		}
		fmt.Fprintln(w)
	}
	if broken {
		fmt.Fprintf(w, "BROKEN LINKS (%d) — .md targets that don't exist:\n", len(r.BrokenLinks))
		for _, b := range r.BrokenLinks {
			fmt.Fprintf(w, "  %s:%d → %s\n", b.Source, b.Line, b.Target)
		}
		fmt.Fprintln(w)
	}
	if untracked {
		fmt.Fprintf(w, "UNTRACKED (%d) — .md on disk but not in git:\n", len(r.Untracked))
		for _, u := range r.Untracked {
			fmt.Fprintf(w, "  %s\n", u)
		}
		fmt.Fprintln(w)
	}
	if frontmatter {
		fmt.Fprintf(w, "FRONTMATTER (%d) — malformed frontmatter or missing required `type`:\n", len(r.FrontmatterFindings))
		for _, f := range r.FrontmatterFindings {
			fmt.Fprintf(w, "  %s → %s\n", f.File, f.Detail)
		}
		fmt.Fprintln(w)
	}
	if edges {
		fmt.Fprintf(w, "EDGES (%d) — frontmatter typed edges with a missing target, or a part-of/supersedes cycle:\n", len(r.BrokenEdges)+len(r.EdgeCycles))
		for _, e := range r.BrokenEdges {
			fmt.Fprintf(w, "  %s [%s] → %s (%s)\n", e.Source, e.Rel, e.Target, e.Reason)
		}
		for _, cyc := range r.EdgeCycles {
			fmt.Fprintf(w, "  cycle: %s → %s\n", strings.Join(cyc, " → "), cyc[0])
		}
		fmt.Fprintln(w)
	}
	if leaksFound {
		fmt.Fprintf(w, "LEAKS (%d) — tree content matching a leak pattern:\n", len(leaks))
		for _, l := range leaks {
			fmt.Fprintf(w, "  %s:%d → %s  (%s)\n", l.File, l.Line, l.Match, l.Pattern)
		}
		fmt.Fprintln(w)
	}
	if disconnected {
		fmt.Fprintf(w, "DISCONNECTED (%d) — frontmatter docs with no doc→doc edge (metadata island):\n", len(r.Disconnected))
		for _, d := range r.Disconnected {
			fmt.Fprintf(w, "  %s\n", d)
		}
		fmt.Fprintln(w)
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
	if sel["frontmatter"] {
		n += len(r.FrontmatterFindings)
	}
	if sel["edges"] {
		n += len(r.BrokenEdges) + len(r.EdgeCycles)
	}
	if sel["leaks"] {
		n += len(leaks)
	}
	if sel["disconnected"] {
		n += len(r.Disconnected)
	}
	printFailureFooter(w, n, orphans, broken, untracked, frontmatter, edges, leaksFound, disconnected)
	return true
}

// printFailureFooter explains, in plain text, why docgraph is exiting non-zero
// and how to act on it — so nobody has to reverse-engineer the gate from a bare
// "failed to push some refs". Only the fix lines for checks that actually have
// findings are shown.
func printFailureFooter(w io.Writer, n int, orphans, broken, untracked, frontmatter, edges, leaks, disconnected bool) {
	bar := strings.Repeat("─", 82)
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "docgraph: %d finding(s) in gated checks → exiting non-zero.\n", n)
	fmt.Fprintln(w, "Its intended use is a pre-push gate, so if a git push just failed, this is why: the")
	fmt.Fprintln(w, "non-zero exit aborted the push. A finding is a repo-hygiene problem — a doc an agent")
	fmt.Fprintln(w, "can't reach, a dead .md link, an untracked .md, malformed/incomplete frontmatter, a")
	fmt.Fprintln(w, "frontmatter edge pointing at a missing target, a configured leak pattern matched in")
	fmt.Fprintln(w, "tracked content, or a frontmatter doc with no place in the metadata structure.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Fix the findings listed above:")
	if orphans {
		fmt.Fprintln(w, "  ORPHAN    → link it in from a reachable doc; or `--ignore '<glob>'` (a")
		fmt.Fprintln(w, "              .docgraphignore entry) if it is intentionally standalone; or delete it.")
	}
	if broken {
		fmt.Fprintln(w, "  BROKEN    → repair or remove the dead .md link at the shown file:line.")
	}
	if untracked {
		fmt.Fprintln(w, "  UNTRACKED → `git add` it; or delete/ignore it.")
	}
	if frontmatter {
		fmt.Fprintln(w, "  FRONTMATTER → add a `---` frontmatter block with a `type:` (every doc except a")
		fmt.Fprintln(w, "                README.md needs one); or fix malformed YAML; or `--skip frontmatter`.")
	}
	if edges {
		fmt.Fprintln(w, "  EDGES     → fix the frontmatter `to:` target (path is repo-root-relative), or remove the edge.")
		fmt.Fprintln(w, "              A cycle means part-of/supersedes edges form a loop — break it.")
	}
	if leaks {
		fmt.Fprintln(w, "  LEAK      → genericise it, remove it, or add an `allow`/`allow_regex` (optionally")
		fmt.Fprintln(w, "              scoped under `[[dir]]`) to your leaks.toml if the match is legitimate.")
	}
	if disconnected {
		fmt.Fprintln(w, "  DISCONNECTED → give the doc a frontmatter part-of/see-also/depends-on edge to a")
		fmt.Fprintln(w, "                related doc (a covers→code edge does not count); or `--skip disconnected`.")
	}
	fmt.Fprintln(w, bar)
}

// runFootgunDrift checks only footgun declarations ADDED in a range. With
// --range it uses that range; otherwise it reads pre-push ref lines from stdin
// (`<localref> <localsha> <remoteref> <remotesha>`), deriving remotesha..localsha
// per ref (a new branch — zero remotesha — falls back to the closest base).
func runFootgunDrift(args []string, stdout, stderr io.Writer) int {
	if os.Getenv("DOCGRAPH_FOOTGUN_OFF") != "" {
		return 0
	}
	fs := flag.NewFlagSet("docgraph footgun-drift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rangeFlag := fs.String("range", "", "explicit base..head to check (else read pre-push stdin)")
	positional, perr := parseArgs(fs, args)
	if perr != nil {
	}
	path := "."
	if len(positional) > 0 {
		path = positional[0]
	}
	root, err := audit.GitRoot(path)
	if err != nil {
		return notARepo(stderr, path, err)
	}
	var ranges []audit.RevRange
	if *rangeFlag != "" {
		b, h, ok := splitRange(*rangeFlag)
		if !ok {
			fmt.Fprintf(stderr, "docgraph: bad --range %q (want base..head)\n", *rangeFlag)
			return 2
		}
		ranges = []audit.RevRange{{Base: b, Head: h}}
	} else {
		ranges = rangesFromPrePushStdin(os.Stdin, root)
	}
	if len(ranges) == 0 {
		return 0
	}
	findings, err := audit.FootgunDrift(root, ranges)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	if len(findings) == 0 {
		return 0
	}
	printFootgunDrift(stdout, findings)
	// Advisory, NOT a gate: exit 0 so a footgun declaration never aborts the push.
	// docgraph can't judge whether an added declaration is a real footgun, so it
	// nags (the two-question test) and trusts the pusher to double-check — rather
	// than blocking on something it can't evaluate and training a --no-verify habit.
	return 0
}

func splitRange(s string) (string, string, bool) {
	i := strings.Index(s, "..")
	if i < 0 {
		return "", "", false
	}
	b, h := s[:i], s[i+2:]
	if b == "" || h == "" {
		return "", "", false
	}
	return b, h, true
}

const zeroSHA = "0000000000000000000000000000000000000000"

// rangesFromPrePushStdin parses git's pre-push stdin into ranges. Deletions
// (zero local sha) are skipped; a new branch (zero remote sha) falls back to the
// closest base.
func rangesFromPrePushStdin(r io.Reader, root string) []audit.RevRange {
	var out []audit.RevRange
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		localSHA, remoteSHA := f[1], f[3]
		if localSHA == zeroSHA {
			continue // deletion
		}
		if remoteSHA == zeroSHA {
			if base, ok := audit.ClosestBase(root, localSHA); ok {
				out = append(out, audit.RevRange{Base: base, Head: localSHA})
			}
			continue
		}
		out = append(out, audit.RevRange{Base: remoteSHA, Head: localSHA})
	}
	return out
}

// printFootgunDrift renders findings with the two-question remediation. This is
// advisory (the caller exits 0): the message exists to prompt a double-check, not
// to justify a block.
// footgunEchoRunes caps how much of a declaration line is echoed. The file:line
// is what makes a finding actionable — you open the file to judge it, since no
// terminal echo tells you whether a stated rationale is real. The line itself is
// only there to say WHICH declaration, so it is trimmed to a recognisable head.
// Untrimmed it dominated the output: on one real push, nine findings echoed 6.1k
// characters of CLAUDE.md prose (single lines up to 2k) around 270 characters of
// actual payload — a per-push tax on the reader, and on an agent's context when
// the agent is the one pushing.
const footgunEchoRunes = 100

// echoLine trims a declaration line to footgunEchoRunes, rune-safe.
func echoLine(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= footgunEchoRunes {
		return string(r)
	}
	return string(r[:footgunEchoRunes]) + "…"
}

func printFootgunDrift(w io.Writer, fs []audit.FootgunFinding) {
	bar := strings.Repeat("─", 82)
	fmt.Fprintf(w, "FOOTGUNS (%d) added in this push — ADVISORY, the push was NOT blocked. Verify each\n", len(fs))
	fmt.Fprintln(w, "is a real footgun (a trap hit, a tempting-but-wrong path, or a re-litigated")
	fmt.Fprintln(w, "decision, recorded WITH its why) and at the right level — invariant → CLAUDE.md,")
	fmt.Fprintln(w, "deep rationale → docs/, human prose → README. Open each to judge it:")
	for _, f := range fs {
		fmt.Fprintf(w, "  %s:%d → %s\n", f.File, f.Line, echoLine(f.Text))
	}
	fmt.Fprintln(w, "A bug you just fixed is not a footgun: its story belongs in the commit message,")
	fmt.Fprintln(w, "and the doc keeps at most the surviving rule. Reword any note-just-in-case as a")
	fmt.Fprintln(w, "plain note, or drop it — a follow-up commit is fine, docgraph did not hold the push.")
	fmt.Fprintln(w, bar)
}

// runCoversDrift is the diff-scoped ADVISORY pre-push subcommand: it nags when a
// pushed range changed code a doc declares it `covers` while that doc went
// untouched, and exits 0 on a finding. It is the push-time pass over the whole
// pushed range; doc-drift raises the same join at Stop, scoped to the session.
func runCoversDrift(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if os.Getenv("DOCGRAPH_COVERS_OFF") != "" {
		return 0
	}
	fs := flag.NewFlagSet("docgraph covers-drift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rangeFlag := fs.String("range", "", "explicit base..head to check (else read pre-push stdin)")
	positional, perr := parseArgs(fs, args)
	if perr != nil {
	}
	path := "."
	if len(positional) > 0 {
		path = positional[0]
	}
	root, err := audit.GitRoot(path)
	if err != nil {
		return notARepo(stderr, path, err)
	}
	var ranges []audit.RevRange
	if *rangeFlag != "" {
		b, h, ok := splitRange(*rangeFlag)
		if !ok {
			fmt.Fprintf(stderr, "docgraph: bad --range %q (want base..head)\n", *rangeFlag)
			return 2
		}
		ranges = []audit.RevRange{{Base: b, Head: h}}
	} else {
		ranges = rangesFromPrePushStdin(stdin, root)
	}
	if len(ranges) == 0 {
		return 0
	}
	// An error from either call is a TOOL error, not a finding: stderr + exit 2.
	// The advisory-never-blocks rule governs findings, not bugs — swallowing a
	// genuine failure would hide it forever. The hook line's `|| true` still keeps
	// even this from aborting a push.
	docs, err := audit.RepoDocs(root, nil)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	findings, err := audit.CoversDrift(root, ranges, docs)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	if len(findings) == 0 {
		return 0
	}
	printCoversDrift(stdout, findings)
	return 0
}

// runDocDrift is the Stop-hook subcommand: it flags dangling doc references and
// anchored value drift over the branch's working-tree-inclusive diff, plus covers
// drift over what this session changed, and BLOCKS the Stop (exit 2, message on
// stderr) on any finding not already raised. Contrast footgun-drift, which is
// advisory. Bare invocation resolves the diff base and applies the once-per-finding
// loop-guards; --range runs a deterministic, guard-free check over that one spec.
func runDocDrift(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if os.Getenv("DOC_DRIFT_OFF") != "" {
		return 0
	}
	fs := flag.NewFlagSet("docgraph doc-drift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rangeFlag := fs.String("range", "", "explicit git-diff spec (base, base..head) — bypasses the loop-guard")
	positional, perr := parseArgs(fs, args)
	if perr != nil {
	}
	path := "."
	if len(positional) > 0 {
		path = positional[0]
	}
	payload := readStopPayload(stdin)

	root, err := audit.GitRoot(path)
	if err != nil {
		return 0 // not a work-tree (e.g. bare dotfiles repo) -> no-op
	}

	if spec := *rangeFlag; spec != "" {
		findings, err := audit.DocDrift(root, spec)
		if err != nil {
			fmt.Fprintf(stderr, "docgraph: %v\n", err)
			return 2
		}
		var covers []audit.CoversFinding
		if os.Getenv("DOCGRAPH_COVERS_OFF") == "" {
			code, md, err := audit.SpecChanges(root, spec)
			covers = coversOf(root, code, md, err, stderr)
		}
		return reportDocDrift(stderr, findings, covers)
	}

	h, err := audit.GitCmd(root, "rev-parse", "HEAD").Output()
	if err != nil {
		// Unborn HEAD (a freshly `git init`'d repo, no commits yet): there is no
		// commit to diff against, and `git diff HEAD` on one exits 128 — a real
		// git error, not a doc-drift finding. Blocking the Stop on that would gate
		// every turn during repo bootstrap. --range mode is unaffected: an
		// explicit ref is the caller's responsibility.
		return 0
	}
	head := strings.TrimSpace(string(h))

	var fresh []audit.DocDriftFinding
	// Already nagged at this HEAD? Then every symbol-scan finding is suppressed by
	// the guard, so resolving the base and running the diff+greps is pure waste on
	// a hook that fires once per turn. Skip it — the same decision, taken before
	// the work instead of after it.
	if docDriftNaggedAt(root) != head {
		findings, err := audit.DocDrift(root, docDriftDiffBase(root, head))
		if err != nil {
			fmt.Fprintf(stderr, "docgraph: %v\n", err)
			return 2
		}
		if len(findings) > 0 {
			seen := docDriftNaggedKeys(root)
			docDriftRecordNag(root, head, findings)
			if !docDriftAllSeen(findings, seen) {
				fresh = findings
			}
		}
	}
	return reportDocDrift(stderr, fresh, sessionCoversDrift(root, head, payload, stderr))
}

// reportDocDrift prints whatever was found and returns the Stop hook's exit code.
func reportDocDrift(w io.Writer, findings []audit.DocDriftFinding, covers []audit.CoversFinding) int {
	if len(findings) == 0 && len(covers) == 0 {
		return 0
	}
	if len(findings) > 0 {
		printDocDrift(w, findings)
	}
	if len(covers) > 0 {
		fmt.Fprint(w, coversDriftMessage(
			"doc-drift: you changed code these docs declare they cover, and didn't touch the docs:\n",
			"Read the parts of each doc that describe what you changed.\n",
			"These won't block again — carry on.\n",
			covers))
	}
	return 2
}

// coversOf joins a collected change set against the repo's covers edges. A
// failure anywhere here is a docgraph or git problem, not drift: it is reported
// and swallowed, because blocking on it would wedge every turn until fixed.
func coversOf(root string, code, md []string, err error, stderr io.Writer) []audit.CoversFinding {
	if err == nil && len(code) == 0 {
		return nil
	}
	var docs map[string]*audit.Doc
	if err == nil {
		docs, err = audit.RepoDocs(root, nil)
	}
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: covers check skipped: %v\n", err)
		return nil
	}
	return audit.CoversOfChanges(docs, code, md)
}

// stopPayload is the part of the Stop hook's stdin JSON doc-drift reads.
type stopPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

// readStopPayload drains stdin and decodes it; anything unparseable (a manual
// run, an empty pipe) is the zero payload.
func readStopPayload(r io.Reader) stopPayload {
	var p stopPayload
	b, _ := io.ReadAll(r)
	_ = json.Unmarshal(b, &p)
	return p
}

// sessionCoversDrift returns the covers findings in what THIS session changed
// that the session has not already been told about.
//
// The change set is the working tree plus the first-parent, non-merge commits
// since the HEAD seen at this session's previous Stop. On a session's first Stop
// there is no previous HEAD, so it is the commits since the session started (the
// transcript's first timestamp); the commit date floor stays on for later Stops
// too, so fast-forwarding onto someone else's older commits never counts. A
// branch-wide diff is the wrong scope: an integration branch can sit hundreds of
// commits past its merge-base, and an agent committing as it goes leaves the
// working tree empty by the time it stops. Concurrent sessions share the branch
// and often the checkout, so a changed path counts only if this session (or one of
// its subagents) edited it with an edit tool or committed it — see
// readSessionWork. Without a session id (a manual run) the change set is the
// working tree alone, unfiltered.
//
// Each (doc, path) pair blocks once per session; the seen set accumulates, so a
// file the agent keeps editing does not re-nag every turn.
func sessionCoversDrift(root, head string, p stopPayload, stderr io.Writer) []audit.CoversFinding {
	if os.Getenv("DOCGRAPH_COVERS_OFF") != "" {
		return nil
	}
	st, known := readCoversSession(root, p.SessionID)
	if !known && p.SessionID != "" {
		st.since = transcriptStart(p.TranscriptPath)
	}
	code, md, err := audit.WorktreeChanges(root)
	if err == nil && p.SessionID != "" {
		if rev := sessionRevs(root, head, st); rev != nil {
			cc, cm, cerr := audit.CommitChanges(root, rev...)
			code, md, err = append(code, cc...), append(md, cm...), cerr
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: covers check skipped: %v\n", err)
		return nil
	}
	findings := coversOf(root, code, md, nil, stderr)
	var work sessionWork
	var mine map[string]bool
	filtered := false
	if len(findings) > 0 && p.SessionID != "" {
		if w, ok := readSessionWork(p.TranscriptPath); ok {
			work, mine, filtered = w, committedPaths(root, w.commits), true
		}
	}

	var fresh []audit.CoversFinding
	for _, f := range findings {
		var paths []string
		for _, path := range f.Paths {
			if filtered && !mine[path] && !bytes.Contains(work.edited, []byte("/"+path+"\n")) {
				continue // someone else's change: this session neither edited nor committed it
			}
			if k := f.Doc + "\t" + path; !st.seen[k] {
				st.seen[k] = true
				paths = append(paths, path)
			}
		}
		if len(paths) > 0 {
			fresh = append(fresh, audit.CoversFinding{Doc: f.Doc, Paths: paths})
		}
	}
	st.head = head
	writeCoversSession(root, p.SessionID, st, !known)
	return fresh
}

// sessionRevs is the git-log selection for the session's commits since its last
// Stop, or nil for none.
func sessionRevs(root, head string, st coversSession) []string {
	var rev []string
	if st.since != "" {
		rev = append(rev, "--since="+st.since)
	}
	switch {
	case st.head == head:
		return nil
	case st.head != "" && audit.GitCmd(root, "merge-base", "--is-ancestor", st.head, head).Run() == nil:
		return append(rev, st.head+".."+head)
	case st.since != "":
		// First Stop, or HEAD moved off the previous one (rebase, branch switch):
		// the date floor alone bounds the walk.
		return append(rev, head)
	}
	return nil
}

// sessionWork is what a session's transcript says it changed itself: the raw
// path inputs of its edit-tool calls, and the commits its tool output shows it
// making. Reading, grepping or prompting about a file is not changing it.
type sessionWork struct {
	edited  []byte   // edit-tool file_path/notebook_path inputs, newline-joined
	commits []string // shas from `git commit`-style "[branch sha] subject" output
}

// editTools are the tool calls whose path input is a file the session wrote.
var editTools = map[string]bool{"Edit": true, "MultiEdit": true, "Write": true, "NotebookEdit": true}

// commitLine matches the summary line `git commit`, `cherry-pick` and `revert`
// print for a commit they create: "[branch sha] subject" or "[branch (root-commit) sha]".
var commitLine = regexp.MustCompile(`\[[^\]\s"\\]+(?: \([a-z-]+\))? ([0-9a-f]{7,40})\] `)

// readSessionWork reads a session's own transcript and its subagents', or
// returns ok=false when the transcript can't be read. A changed path the
// session neither edited nor committed was changed by someone else sharing the
// branch or the checkout, so it is not this session's to reconcile.
func readSessionWork(transcript string) (w sessionWork, ok bool) {
	files := []string{transcript}
	sub, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "*.jsonl"))
	files = append(files, sub...)
	useMarker, resultMarker := []byte(`"type":"tool_use"`), []byte(`"type":"tool_result"`)
	for i, name := range files {
		f, err := os.Open(name)
		if err != nil {
			if i == 0 {
				return w, false
			}
			continue
		}
		r := bufio.NewReader(f)
		for {
			line, err := r.ReadBytes('\n')
			if bytes.Contains(line, resultMarker) {
				for _, m := range commitLine.FindAllSubmatch(line, -1) {
					w.commits = append(w.commits, string(m[1]))
				}
			}
			if bytes.Contains(line, useMarker) {
				var rec struct {
					Message struct {
						Content json.RawMessage `json:"content"`
					} `json:"message"`
				}
				var items []struct {
					Type  string `json:"type"`
					Name  string `json:"name"`
					Input struct {
						FilePath     string `json:"file_path"`
						NotebookPath string `json:"notebook_path"`
					} `json:"input"`
				}
				if json.Unmarshal(line, &rec) == nil && json.Unmarshal(rec.Message.Content, &items) == nil {
					for _, it := range items {
						if it.Type == "tool_use" && editTools[it.Name] {
							w.edited = append(w.edited, it.Input.FilePath+"\n"+it.Input.NotebookPath+"\n"...)
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}
	return w, true
}

// committedPaths returns the paths the given commits changed. A sha the repo
// doesn't know (another repo's commit, or one rewritten since) is skipped.
func committedPaths(root string, shas []string) map[string]bool {
	out := map[string]bool{}
	for _, sha := range shas {
		b, err := audit.GitCmd(root, "show", "--no-renames", "--name-only", "--format=", sha+"^{commit}", "--").Output()
		if err != nil {
			continue
		}
		for _, p := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if p != "" {
				out[p] = true
			}
		}
	}
	return out
}

// transcriptStart returns the first timestamp in a JSONL transcript, formatted
// for git's --since, or "" when none is found.
func transcriptStart(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for i := 0; i < 50; i++ {
		line, err := r.ReadBytes('\n')
		var rec struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.Timestamp != "" {
			if t, perr := time.Parse(time.RFC3339Nano, rec.Timestamp); perr == nil {
				return t.UTC().Format("2006-01-02 15:04:05 +0000")
			}
		}
		if err != nil {
			return ""
		}
	}
	return ""
}

// coversSession is one session's covers-drift state in one repo.
type coversSession struct {
	head  string          // HEAD at the session's previous Stop
	since string          // session start, git --since form; "" if unknown
	seen  map[string]bool // "doc\tpath" pairs already raised
}

// coversSessionMaxAge bounds how long a session's state file outlives its last
// write; a session idle this long is not coming back to the same HEAD.
const coversSessionMaxAge = 14 * 24 * time.Hour

// coversSessionPath keys a session's state by the repo's shared git dir, not the
// worktree root: a session that moves into a linked worktree is still one
// session, and a pair raised in the main checkout must not block again there.
func coversSessionPath(root, session string) string {
	if session == "" {
		session = "\x00no-session"
	}
	if b, err := audit.GitCmd(root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		root = strings.TrimSpace(string(b))
	}
	sum := sha256.Sum256([]byte(session))
	return docDriftStatePath(root, ".session-"+hex.EncodeToString(sum[:])[:16])
}

func readCoversSession(root, session string) (coversSession, bool) {
	st := coversSession{seen: map[string]bool{}}
	p := coversSessionPath(root, session)
	if p == "" {
		return st, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return st, false
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	st.head, st.since, _ = strings.Cut(lines[0], "\t")
	for _, k := range lines[1:] {
		st.seen[k] = true
	}
	return st, true
}

// writeCoversSession persists st. Best-effort, like every doc-drift state write.
// A session's first write also prunes state files no session has touched in
// coversSessionMaxAge, so the directory does not grow without bound.
func writeCoversSession(root, session string, st coversSession, first bool) {
	p := coversSessionPath(root, session)
	if p == "" {
		return
	}
	dir := filepath.Dir(p)
	_ = os.MkdirAll(dir, 0o755)
	if first {
		if ents, err := os.ReadDir(dir); err == nil {
			for _, e := range ents {
				if info, err := e.Info(); err == nil && strings.Contains(e.Name(), ".session-") && time.Since(info.ModTime()) > coversSessionMaxAge {
					_ = os.Remove(filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	lines := []string{st.head + "\t" + st.since}
	for k := range st.seen {
		lines = append(lines, k)
	}
	_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
}

// docDriftDiffBase resolves what to `git diff` against: the closest integration
// branch's merge-base if this work sits ahead of it, else "HEAD" (on a trunk
// branch the change set IS the working tree — uncommitted only). Memoized per
// (repo, HEAD), because audit.ClosestBase costs a merge-base + a rev-list per
// integration-branch candidate — a dozen git subprocesses, ~85% of a warm
// doc-drift run, re-derived on every Stop hook to recompute an answer that only
// changes when HEAD does.
//
// Staleness is bounded and fails SAFE. The memo can only go stale when an
// integration branch advances while HEAD stays put (i.e. a fetch), and the
// remembered base is then an ANCESTOR of the true one — a superset diff, so
// doc-drift over-reports rather than missing drift. The next commit re-keys it.
func docDriftDiffBase(root, head string) string {
	if base, ok := readDocDriftBase(root, head); ok {
		return base
	}
	spec := "HEAD"
	if base, ok := audit.ClosestBase(root, "HEAD"); ok && base != "" && base != head {
		spec = base
	}
	writeDocDriftBase(root, head, spec)
	return spec
}

// docDriftStatePath is the per-repo state file for suffix — "" is the nag marker,
// ".base" the memoized diff base — or "" when no state dir resolves.
func docDriftStatePath(root, suffix string) string {
	dir := docDriftStateDir()
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+suffix)
}

// readDocDriftBase returns the base memoized for head, if the stored entry is
// keyed to that exact HEAD.
func readDocDriftBase(root, head string) (string, bool) {
	p := docDriftStatePath(root, ".base")
	if p == "" {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	h, spec, ok := strings.Cut(strings.TrimSpace(string(b)), " ")
	if !ok || h != head || spec == "" {
		return "", false
	}
	return spec, true
}

// writeDocDriftBase memoizes spec against head. Best-effort: a state dir that
// can't be written just means the next run re-resolves.
func writeDocDriftBase(root, head, spec string) {
	p := docDriftStatePath(root, ".base")
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(head+" "+spec), 0o644)
}

// docDriftNagLines returns the nag marker's lines — the HEAD last nagged at,
// then one docDriftKey per finding that nag carried — or nil for never
// (including an unresolvable state dir, which must never suppress).
func docDriftNagLines(root string) []string {
	p := docDriftStatePath(root, "")
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// docDriftNaggedAt returns the HEAD this repo was last nagged at, or "" for never.
func docDriftNaggedAt(root string) string {
	if l := docDriftNagLines(root); len(l) > 0 {
		return l[0]
	}
	return ""
}

// docDriftNaggedKeys returns the findings the last nag carried.
func docDriftNaggedKeys(root string) map[string]bool {
	seen := map[string]bool{}
	if l := docDriftNagLines(root); len(l) > 1 {
		for _, k := range l[1:] {
			seen[k] = true
		}
	}
	return seen
}

// docDriftKey identifies a finding across commits: the symbol and what went
// stale about it, never a line number, which any unrelated edit shifts.
func docDriftKey(f audit.DocDriftFinding) string {
	return fmt.Sprintf("%d\t%s\t%s", f.Kind, f.Symbol, f.Old)
}

// docDriftAllSeen reports whether every finding was already in the last nag.
// A long-lived branch diffs against a distant merge-base, so a finding one
// agent judged intentional would otherwise re-block every later commit's agent;
// only a finding the last nag did not carry blocks again.
func docDriftAllSeen(fs []audit.DocDriftFinding, seen map[string]bool) bool {
	for _, f := range fs {
		if !seen[docDriftKey(f)] {
			return false
		}
	}
	return true
}

// docDriftRecordNag records head and the findings as nagged. The HEAD line makes
// a repeat Stop at the same HEAD a no-op without scanning; the finding keys let
// a later HEAD stay silent when it carries nothing new (docDriftAllSeen). The
// keys are replaced, not accumulated, so a finding that is fixed and later
// recurs blocks again.
func docDriftRecordNag(root, head string, fs []audit.DocDriftFinding) {
	p := docDriftStatePath(root, "")
	if p == "" {
		return
	}
	lines := []string{head}
	for _, f := range fs {
		lines = append(lines, docDriftKey(f))
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
}

// docDriftStateDir resolves $XDG_STATE_HOME/docgraph/doc-drift (default
// ~/.local/state/...), matching the usage-log XDG-state convention.
func docDriftStateDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "docgraph", "doc-drift")
}

// printDocDrift renders findings grouped by kind, with the reconcile guidance.
// Self-contained prose — no machine-local path references.
func printDocDrift(w io.Writer, fs []audit.DocDriftFinding) {
	var dangling, value []audit.DocDriftFinding
	for _, f := range fs {
		if f.Kind == audit.Dangling {
			dangling = append(dangling, f)
		} else {
			value = append(value, f)
		}
	}
	fmt.Fprintln(w, "doc-drift: this branch changed code that tracked docs still describe the old way.")
	fmt.Fprintln(w, "Fix each in THIS change set by the smallest edit that makes the doc true again —")
	fmt.Fprintln(w, "swap the value, rename the symbol, or DELETE the sentence that now describes")
	fmt.Fprintln(w, "nothing — or confirm it is intentional history. Never add an account of what")
	fmt.Fprintln(w, "changed or why: that belongs in the commit message, not the doc.")
	if len(dangling) > 0 {
		fmt.Fprintln(w, "  Dangling references (symbol deleted, doc still names it):")
		for _, f := range dangling {
			fmt.Fprintf(w, "    • '%s' (definition removed on this branch) still referenced in:\n", f.Symbol)
			for _, h := range f.Hits {
				fmt.Fprintf(w, "        %s:%d: %s\n", h.File, h.Line, h.Text)
			}
		}
	}
	if len(value) > 0 {
		fmt.Fprintln(w, "  Anchored value drift (doc names the constant but shows its old value):")
		for _, f := range value {
			fmt.Fprintf(w, "    • '%s' changed value (old literal %s); doc names the symbol but still shows %s:\n", f.Symbol, f.Old, f.Old)
			for _, h := range f.Hits {
				fmt.Fprintf(w, "        %s:%d: %s\n", h.File, h.Line, h.Text)
			}
		}
	}
	fmt.Fprintln(w, "This catches only anchored/symbol cases — for paraphrased values or reversed")
	fmt.Fprintln(w, "decisions, run a semantic doc sweep before finishing: `docgraph covers <path>`")
	fmt.Fprintln(w, "names the docs that govern a file you changed, including ones no symbol drift")
	fmt.Fprintln(w, "points at. Already reconciled, or is it framed history? Stop again — these")
	fmt.Fprintln(w, "findings won't block again; only a new one will.")
}

// coversDriftMessage renders a covers-drift nag: lead names the occasion, read
// says what to read, tail says what happens next. The guidance between them is
// shared, so the pre-push and Stop-hook nags steer to the same smallest edit.
func coversDriftMessage(lead, read, tail string, fs []audit.CoversFinding) string {
	var b strings.Builder
	b.WriteString(lead)
	for _, f := range fs {
		fmt.Fprintf(&b, "  • %s covers:\n", f.Doc)
		for _, p := range f.Paths {
			fmt.Fprintf(&b, "      %s\n", p)
		}
	}
	b.WriteString(read)
	b.WriteString("Now false → correct or cut the false lines, nothing more; what changed and why\n")
	b.WriteString("goes in the commit message, not the doc. Still accurate → do nothing: an edit\n")
	b.WriteString("made only to quiet this is doc bloat. ")
	b.WriteString(tail)
	return b.String()
}

func printCoversDrift(w io.Writer, fs []audit.CoversFinding) {
	fmt.Fprint(w, coversDriftMessage(
		"COVERS-DRIFT: this push changes code that a doc declares it covers,\nbut the doc itself is untouched:\n",
		"Read each against the change.\n",
		"Advisory — the push is not blocked.\n",
		fs))
}

// runSchema prints the JSON Schema describing docgraph frontmatter, stamped with
// this build's version. It is how non-owning consumers (compositor, Mycelium)
// obtain the vocabulary they must conform to without re-encoding it. Read-only —
// never part of the pre-push gate.
func runSchema(stdout io.Writer) int {
	stdout.Write(audit.SchemaJSON(version))
	return 0
}

// runCovers prints the docs that document the given repo-root-relative path (via
// a frontmatter `covers` edge, directly or by covering a parent directory). A
// read-only view — never part of the gate.
func runCovers(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docgraph covers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var ignores multiFlag
	fs.Var(&ignores, "ignore", "glob to exclude (repeatable)")
	positional, perr := parseArgs(fs, args)
	if perr != nil {
	}
	if len(positional) < 1 {
		fmt.Fprintln(stderr, "docgraph: usage: docgraph covers <repo-relative-path>")
		return 2
	}
	root, err := audit.GitRoot(".")
	if err != nil {
		return notARepo(stderr, ".", err)
	}
	docs, err := audit.RepoDocs(root, ignores)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	for _, src := range audit.CoversOf(docs, positional[0]) {
		fmt.Fprintln(stdout, src)
	}
	return 0
}

// runIndex prints a generated markdown index of the doc graph to stdout (redirect
// it to an index.md). A read-only view — never part of the gate.
func runIndex(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docgraph index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var ignores multiFlag
	fs.Var(&ignores, "ignore", "glob to exclude (repeatable)")
	if _, perr := parseArgs(fs, args); perr != nil {
		return 2
	}
	root, err := audit.GitRoot(".")
	if err != nil {
		return notARepo(stderr, ".", err)
	}
	docs, err := audit.RepoDocs(root, ignores)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, audit.IndexMarkdown(docs))
	return 0
}

// runStale prints docs whose `verified` date is older than their staleness
// threshold (per-doc `review:` cadence, else --older-than). A read-only view —
// never part of the gate.
func runStale(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docgraph stale", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var ignores multiFlag
	fs.Var(&ignores, "ignore", "glob to exclude (repeatable)")
	olderThan := fs.Int("older-than", 180, "default staleness threshold in days (a per-doc `review:` cadence overrides it)")
	if _, perr := parseArgs(fs, args); perr != nil {
		return 2
	}
	root, err := audit.GitRoot(".")
	if err != nil {
		return notARepo(stderr, ".", err)
	}
	docs, err := audit.RepoDocs(root, ignores)
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	for _, s := range audit.StaleDocs(docs, time.Now(), *olderThan) {
		fmt.Fprintf(stdout, "%s (verified %s — %dd old, threshold %dd)\n", s.File, s.Verified, s.AgeDays, s.Threshold)
	}
	return 0
}

// runGraph prints the doc graph — both the content and metadata graphs — as
// human-readable markdown by default, or a stable JSON payload with --json (the
// seam Mycelium ingests). --ref <ref> reads the committed state at that ref from
// the object store instead of the working tree (bare-repo capable). A read-only
// view — never part of the gate; always 0.
func runGraph(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docgraph graph", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var roots, ignores multiFlag
	fs.Var(&roots, "root", "extra root doc (repeatable)")
	fs.Var(&ignores, "ignore", "glob to exclude (repeatable)")
	asJSON := fs.Bool("json", false, "emit the graph as JSON (schemaVersion-stamped)")
	ref := fs.String("ref", "", "read the graph from this git ref (e.g. HEAD, dev); works on a bare repo, reads committed state not the working tree")
	if _, perr := parseArgs(fs, args); perr != nil {
		return 2
	}

	var v audit.GraphView
	var err error
	if *ref != "" {
		// Ref mode: read from the object store at *ref via the cwd's git dir.
		// Do NOT call GitRoot — rev-parse --show-toplevel fails on a bare repo.
		v, err = audit.BuildGraphViewAtRef(".", *ref, roots, ignores)
	} else {
		root, gerr := audit.GitRoot(".")
		if gerr != nil {
			return notARepo(stderr, ".", gerr)
		}
		v, err = audit.BuildGraphView(root, roots, ignores)
	}
	if err != nil {
		fmt.Fprintf(stderr, "docgraph: %v\n", err)
		return 2
	}
	if *asJSON {
		b, err := v.JSON()
		if err != nil {
			fmt.Fprintf(stderr, "docgraph: %v\n", err)
			return 2
		}
		stdout.Write(b)
		fmt.Fprintln(stdout)
		return 0
	}
	fmt.Fprint(stdout, v.Markdown())
	return 0
}
