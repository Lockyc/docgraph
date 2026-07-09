package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// noCfg returns a leaks-config path guaranteed not to exist, so a test that isn't
// about leak rules stays deterministic (built-in patterns only) instead of picking
// up the dev machine's real ~/.config/docaudit/leaks.
func noCfg(dir string) string { return filepath.Join(dir, "no-leaks-cfg") }

func mkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(p, c string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	write("CLAUDE.md", "[i](docs/index.md)\n")
	write("docs/index.md", "[gone](missing.md)\n")
	git := func(a ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	git("init")
	git("add", "CLAUDE.md", "docs/index.md")
	return dir
}

func TestRunFindingsExit1(t *testing.T) {
	dir := mkRepo(t)
	var out, errb bytes.Buffer
	code := run([]string{"--leaks-config", noCfg(dir), dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("BROKEN LINKS (1)")) {
		t.Errorf("missing broken-links section:\n%s", out.String())
	}
}

func mkOrphanRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(p, c string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	write("CLAUDE.md", "hub with no links\n")
	write("docs/orphan.md", "unreferenced\n")
	git := func(a ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	git("init")
	git("add", "CLAUDE.md", "docs/orphan.md")
	return dir
}

func TestRunSkipExcludesOrphans(t *testing.T) {
	dir := mkOrphanRepo(t)
	var out, errb bytes.Buffer
	code := run([]string{"--skip", "orphans", "--leaks-config", noCfg(dir), dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (orphans skipped)\n%s", code, out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("ORPHANS")) {
		t.Errorf("ORPHANS section shown despite --skip orphans:\n%s", out.String())
	}
}

func TestRunEnforcesOrphansByDefault(t *testing.T) {
	dir := mkOrphanRepo(t)
	var out, errb bytes.Buffer
	code := run([]string{"--leaks-config", noCfg(dir), dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (orphan gated by default)", code)
	}
}

func TestRunSkipInvalidExit2(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--skip", "bogus", t.TempDir()}, &out, &errb); code != 2 {
		t.Fatalf("exit = %d, want 2 for invalid check name", code)
	}
}

// The removed --checks flag must fail loudly with a migration message rather than
// flag's cryptic "flag provided but not defined", because old hooks bake in --checks.
func TestRunChecksFlagRemovedExit2(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--checks", "orphans", t.TempDir()}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for removed --checks flag", code)
	}
	if !strings.Contains(errb.String(), "--checks was removed") || !strings.Contains(errb.String(), "--skip") {
		t.Errorf("want a --checks migration message pointing at --skip, got: %s", errb.String())
	}
}

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func TestInstallHookDefaultEnforcesAll(t *testing.T) {
	dir := gitInit(t)
	var out, errb bytes.Buffer
	if code := runInstallHook([]string{dir}, &out, &errb); code != 0 {
		t.Fatalf("exit=%d\n%s", code, errb.String())
	}
	hook := filepath.Join(dir, ".githooks", "pre-push")
	b, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	// Default hook runs a bare `docaudit .` — no check selection — so a
	// newly-added check is enforced automatically without regenerating the hook.
	if !strings.Contains(string(b), `exec "$bin" .`) {
		t.Errorf("default hook should run bare `docaudit .`:\n%s", b)
	}
	if strings.Contains(string(b), "--checks") || strings.Contains(string(b), "--skip") {
		t.Errorf("default hook should carry no check flags:\n%s", b)
	}
	// The hook must resolve docaudit even under a minimal PATH (git runs hooks
	// with the caller's PATH; GUI clients / agent harnesses often lack ~/go/bin).
	// Guard the Go-bin fallback so it can't regress to `command -v` only.
	if !strings.Contains(string(b), "$HOME/go/bin") || !strings.Contains(string(b), "docaudit_bin") {
		t.Errorf("hook lost its minimal-PATH fallback (would fail-closed when docaudit isn't on PATH):\n%s", b)
	}
	if fi, _ := os.Stat(hook); fi.Mode()&0o100 == 0 {
		t.Error("hook not executable")
	}
	hp, _ := exec.Command("git", "-C", dir, "config", "core.hooksPath").Output()
	if strings.TrimSpace(string(hp)) != ".githooks" {
		t.Errorf("core.hooksPath = %q, want .githooks", strings.TrimSpace(string(hp)))
	}
}

func TestInstallHookSkip(t *testing.T) {
	dir := gitInit(t)
	var out, errb bytes.Buffer
	if code := runInstallHook([]string{"--skip", "orphans", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit=%d\n%s", code, errb.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `--skip orphans`) {
		t.Errorf("hook missing --skip orphans:\n%s", b)
	}
}

func TestInstallHookRefusesExisting(t *testing.T) {
	dir := gitInit(t)
	os.MkdirAll(filepath.Join(dir, ".githooks"), 0o755)
	os.WriteFile(filepath.Join(dir, ".githooks", "pre-push"), []byte("#existing\n"), 0o755)
	var out, errb bytes.Buffer
	if code := runInstallHook([]string{dir}, &out, &errb); code != 2 {
		t.Fatalf("exit=%d, want 2 (refuse existing without --force)", code)
	}
}

func TestRunNotAGitRepoExit2(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{t.TempDir()}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// Absent leak config is NON-fatal: leaks runs by default (incl. CI, which has no
// global file), so it degrades to built-in patterns + a warning, not exit 2.
func TestRunLeaksAbsentConfigNonFatal(t *testing.T) {
	dir := t.TempDir()
	write := func(p, c string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	write("README.md", "nothing sensitive here\n")
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	var out, errb bytes.Buffer
	code := run([]string{"--leaks-config", noCfg(dir), dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (absent config is non-fatal, no findings)\n%s", code, out.String())
	}
	if !strings.Contains(errb.String(), "no leak rules file") || !strings.Contains(errb.String(), "built-in") {
		t.Errorf("want a warning about the absent config + built-in fallback, got: %s", errb.String())
	}
}

// Built-in secret patterns enforce even with no config file at all.
func TestRunLeaksBuiltinsWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(p, c string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	write("README.md", "hello\n")
	write("secrets.env", "AWS=AKIAIOSFODNN7EXAMPLE\n")
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "README.md", "secrets.env").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	var out, errb bytes.Buffer
	code := run([]string{"--leaks-config", noCfg(dir), dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (built-in AWS pattern fires without a config)\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("LEAKS (1)")) {
		t.Errorf("missing LEAKS section from a built-in match:\n%s", out.String())
	}
}

func TestInstallHookIgnorePassthrough(t *testing.T) {
	dir := gitInit(t)
	var out, errb bytes.Buffer
	code := runInstallHook([]string{"--ignore", "**/*_test.go", "--force", dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errb.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "--ignore '**/*_test.go'") {
		t.Errorf("hook missing --ignore passthrough:\n%s", b)
	}
}

func TestRunLeaksBadRegexExit2(t *testing.T) {
	dir := gitInit(t)
	cfg := filepath.Join(dir, "leaks.toml")
	os.WriteFile(cfg, []byte("regex = ['(unclosed']\n"), 0o644)
	var out, errb bytes.Buffer
	code := run([]string{"--leaks-config", cfg, dir}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (bad regex in config)\n%s", code, errb.String())
	}
	if strings.Contains(errb.String(), "built-in secret patterns only") {
		t.Errorf("a bad-regex config must not degrade to built-ins: %s", errb.String())
	}
	if !strings.Contains(errb.String(), "leaks regex") {
		t.Errorf("want the regex error surfaced, got: %s", errb.String())
	}
}

func TestRunLeaksMalformedTomlExit2(t *testing.T) {
	dir := gitInit(t)
	cfg := filepath.Join(dir, "leaks.toml")
	os.WriteFile(cfg, []byte("this is not = valid = toml\n"), 0o644)
	var out, errb bytes.Buffer
	code := run([]string{"--leaks-config", cfg, dir}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (malformed TOML)\n%s", code, errb.String())
	}
	if strings.Contains(errb.String(), "built-in secret patterns only") {
		t.Errorf("a malformed config must not degrade to built-ins: %s", errb.String())
	}
}

// leaks is enforced by DEFAULT — no opt-in flag needed for a user-configured
// pattern to gate.
func TestRunEnforcesLeaksByDefault(t *testing.T) {
	dir := t.TempDir()
	write := func(p, c string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	write("README.md", "reach us at admin@lsjc.au today\n")
	cfg := filepath.Join(dir, "leaks.toml")
	os.WriteFile(cfg, []byte("terms = [\"lsjc.au\"]\n"), 0o644)
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	var out, errb bytes.Buffer
	code := run([]string{"--leaks-config", cfg, dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (leak present, enforced by default)\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("LEAKS (1)")) {
		t.Errorf("missing LEAKS section:\n%s", out.String())
	}
}
