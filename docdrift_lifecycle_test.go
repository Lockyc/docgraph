package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// lifecycleSyms are the code symbols the doc-drift lifecycle model removes,
// restores and mentions.
var lifecycleSyms = []string{"AlphaWidget", "BetaWidget"}

// driftModel is what a bare doc-drift Stop should remember between turns: the
// findings its last nag carried and whether the last tool error was already
// raised. The repo is on a non-integration branch with no session, so every
// Stop scans the working tree against HEAD.
type driftModel struct {
	dir   string
	index string

	committed map[string]bool // defined in x.go at HEAD
	defined   map[string]bool // defined in the working-tree x.go
	mentioned map[string]bool // named in the working-tree CLAUDE.md
	broken    bool            // git cannot read the index

	lastNag map[string]bool // findings the last nag carried
	errSeen bool            // the current tool error was already raised
}

func (m *driftModel) git(t *rapid.T, a ...string) {
	if out, err := exec.Command("git", append([]string{"-C", m.dir}, a...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", a, err, out)
	}
}

func (m *driftModel) write(t *rapid.T) {
	code, doc := "package x\n", "Docs.\n"
	for _, s := range lifecycleSyms {
		if m.defined[s] {
			code += "type " + s + " struct{}\n"
		}
		if m.mentioned[s] {
			doc += "Uses " + s + ".\n"
		}
	}
	for p, c := range map[string]string{"x.go": code, "CLAUDE.md": doc} {
		if err := os.WriteFile(filepath.Join(m.dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// findings is what a full scan reports: a symbol removed since HEAD that the
// doc still names.
func (m *driftModel) findings() map[string]bool {
	out := map[string]bool{}
	for _, s := range lifecycleSyms {
		if m.committed[s] && !m.defined[s] && m.mentioned[s] {
			out[s] = true
		}
	}
	return out
}

// stop ends a turn: it runs the Stop hook and checks its exit against the model.
func (m *driftModel) stop(t *rapid.T) {
	var errb bytes.Buffer
	got := runDocDrift([]string{m.dir}, strings.NewReader(""), &bytes.Buffer{}, &errb)
	want := 0
	if m.broken {
		want = 2
		if m.errSeen {
			want = 1
		}
		m.errSeen = true
	} else {
		m.errSeen = false
		f := m.findings()
		for s := range f {
			if !m.lastNag[s] {
				want = 2
			}
		}
		m.lastNag = f
	}
	if got != want {
		t.Fatalf("Stop exit %d, want %d (committed=%v defined=%v mentioned=%v broken=%v)\nstderr:\n%s",
			got, want, m.committed, m.defined, m.mentioned, m.broken, errb.String())
	}
}

// turn is one agent turn: a change, then the Stop hook.
func (m *driftModel) turn(change func(*rapid.T)) func(*rapid.T) {
	return func(t *rapid.T) {
		change(t)
		m.stop(t)
	}
}

func (m *driftModel) toggle(set map[string]bool) func(*rapid.T) {
	return m.turn(func(t *rapid.T) {
		s := rapid.SampledFrom(lifecycleSyms).Draw(t, "sym")
		set[s] = !set[s]
		m.write(t)
	})
}

// TestDocDriftStopLifecycle checks the Stop hook's promise under any order of
// turns: a Stop blocks (2) exactly when the current findings include one the
// last nag did not carry, or a tool error not yet raised; a repeated error
// exits 1; anything else exits 0.
func TestDocDriftStopLifecycle(t *testing.T) {
	if f := flag.Lookup("rapid.checks"); f != nil && f.Value.String() == f.DefValue {
		// Each check builds a git repo and runs doc-drift every turn; the gate
		// keeps it short and `just soak` raises it.
		_ = flag.Set("rapid.checks", "10")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("DOCGRAPH_COVERS_OFF", "1")
	rapid.Check(t, func(rt *rapid.T) {
		dir, err := os.MkdirTemp(t.TempDir(), "drift")
		if err != nil {
			rt.Fatal(err)
		}
		m := &driftModel{dir: dir, index: filepath.Join(dir, ".git", "index"),
			committed: map[string]bool{}, defined: map[string]bool{}, mentioned: map[string]bool{}}
		for _, s := range lifecycleSyms {
			m.committed[s], m.defined[s] = true, true
			m.mentioned[s] = rapid.Bool().Draw(rt, "mention "+s)
		}
		m.git(rt, "init", "-q")
		m.git(rt, "checkout", "-qb", "wip") // no integration branch: the base is HEAD
		m.write(rt)
		m.git(rt, "add", ".")
		m.git(rt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base")

		rt.Repeat(map[string]func(*rapid.T){
			"editCode": m.toggle(m.defined),
			"editDoc":  m.toggle(m.mentioned),
			"commit": m.turn(func(t *rapid.T) {
				if m.broken {
					t.Skip("git cannot commit without its index")
				}
				m.git(t, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qam", "step", "--allow-empty")
				for _, s := range lifecycleSyms {
					m.committed[s] = m.defined[s]
				}
			}),
			"toggleGitError": m.turn(func(t *rapid.T) {
				mode := os.FileMode(0)
				if m.broken {
					mode = 0o644
				}
				if err := os.Chmod(m.index, mode); err != nil {
					t.Fatal(err)
				}
				m.broken = !m.broken
			}),
			"idle": m.stop,
		})
	})
}

// A doc edit after a nag, at the same HEAD and code diff, that names another
// removed symbol must block: on a trunk the next commit empties the diff and the
// finding is otherwise never raised.
func TestDocDriftSameHeadDocEditBlocks(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := setupRepoMain(t, map[string]string{
		"x.go": "type AlphaWidget struct{}\ntype BetaWidget struct{}\n", "CLAUDE.md": "Uses AlphaWidget.\n",
	})
	if out, err := exec.Command("git", "-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	run := func() int { return runDocDrift([]string{dir}, strings.NewReader(""), io.Discard, io.Discard) }

	os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n"), 0o644)
	if code := run(); code != 2 {
		t.Fatalf("first nag must block -> want 2, got %d", code)
	}
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Uses AlphaWidget.\nUses BetaWidget.\n"), 0o644)
	if code := run(); code != 2 {
		t.Fatalf("same HEAD and code diff, new stale doc reference -> want 2, got %d", code)
	}
}
