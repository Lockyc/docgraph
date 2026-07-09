package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	code := run([]string{dir}, &out, &errb)
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

func TestRunChecksExcludesOrphans(t *testing.T) {
	dir := mkOrphanRepo(t)
	var out, errb bytes.Buffer
	code := run([]string{"--checks", "broken,untracked", dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (orphans not selected)\n%s", code, out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("ORPHANS")) {
		t.Errorf("ORPHANS section shown despite not being selected:\n%s", out.String())
	}
}

func TestRunChecksDefaultGatesOrphans(t *testing.T) {
	dir := mkOrphanRepo(t)
	var out, errb bytes.Buffer
	code := run([]string{dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (orphan gated by default)", code)
	}
}

func TestRunChecksInvalidExit2(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--checks", "bogus", t.TempDir()}, &out, &errb); code != 2 {
		t.Fatalf("exit = %d, want 2 for invalid check name", code)
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

func TestInstallHook(t *testing.T) {
	dir := gitInit(t)
	var out, errb bytes.Buffer
	if code := runInstallHook([]string{"--checks", "broken,untracked", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit=%d\n%s", code, errb.String())
	}
	hook := filepath.Join(dir, ".githooks", "pre-push")
	b, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "--checks broken,untracked") {
		t.Errorf("hook missing checks:\n%s", b)
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

func TestRunLeaksNoConfigExit2(t *testing.T) {
	dir := gitInit(t)
	var out, errb bytes.Buffer
	// Force a nonexistent config path so this never depends on the dev machine's
	// real ~/.config/docaudit/leaks.
	code := run([]string{"--checks", "leaks", "--leaks-config", filepath.Join(dir, "nope"), dir}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (leaks selected, no rules file)\n%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "no rules file") {
		t.Errorf("want a 'no rules file' message, got: %s", errb.String())
	}
}

func TestRunLeaksBadRegexExit2(t *testing.T) {
	dir := gitInit(t)
	cfg := filepath.Join(dir, "leaks.rules")
	os.WriteFile(cfg, []byte("(unclosed\n"), 0o644)
	var out, errb bytes.Buffer
	code := run([]string{"--checks", "leaks", "--leaks-config", cfg, dir}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (malformed rules file)\n%s", code, errb.String())
	}
	if strings.Contains(errb.String(), "no rules file") {
		t.Errorf("an existing-but-malformed rules file must not be reported as missing: %s", errb.String())
	}
	if !strings.Contains(errb.String(), "bad regex") {
		t.Errorf("want the parse error surfaced, got: %s", errb.String())
	}
}

func TestRunLeaksFindingExit1(t *testing.T) {
	dir := t.TempDir()
	write := func(p, c string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	write("README.md", "reach us at admin@lsjc.au today\n")
	cfg := filepath.Join(dir, "leaks.rules")
	os.WriteFile(cfg, []byte("lsjc\\.au\n"), 0o644)
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	var out, errb bytes.Buffer
	code := run([]string{"--checks", "leaks", "--leaks-config", cfg, dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (leak present)\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("LEAKS (1)")) {
		t.Errorf("missing LEAKS section:\n%s", out.String())
	}
}
