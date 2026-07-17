You are installing or updating **docgraph** — a Go CLI documentation-audit gate (whole-state
doc-graph + leak checks, two diff-scoped pre-push nags — `footgun-drift` and `covers-drift` —
and a `doc-drift` **Stop hook** that blocks a turn while a tracked doc still describes code
that just changed).

GitHub: `https://github.com/lockyc/docgraph`

docgraph is a **Go CLI**, not a bundle: `install.sh` installs a *binary* via `go install`
(no `~/.docgraph` clone). This command adds the guided wiring on top — the doc-drift Claude
Stop hook, a repo's pre-push gate, and the leaks config dir.

---

## Steps

### 1. Detect repo location

Check whether the current working directory is the docgraph repo:

```bash
MODULE="github.com/lockyc/docgraph/v2"
[ -f install.sh ] && [ -f main.go ] && grep -q "^module $MODULE\$" go.mod 2>/dev/null && echo "IN_REPO" || echo "NOT_IN_REPO"
```

`MODULE` is the `go.mod` module line verbatim, `/v2` included — the same shape
`install.sh` uses. The suffix is load-bearing here too: an anchored grep for the
bare path matches no `go.mod` at major ≥2, so this step would report
`NOT_IN_REPO` from inside the checkout and clone needlessly. A major bump moves
it in lockstep with every other `@latest` site.

**If in repo:** set `REPO_DIR` to the current working directory.
**If not in repo:** clone into a temp dir and set `REPO_DIR` to it:

```bash
CLONE_DIR=$(mktemp -d) && git clone --depth 1 https://github.com/lockyc/docgraph "$CLONE_DIR/docgraph" && echo "$CLONE_DIR/docgraph"
```

If the clone fails, report the error and stop.

### 2. Check prerequisites

```bash
command -v go  >/dev/null 2>&1 && echo "go: ok"  || echo "go: MISSING"
command -v git >/dev/null 2>&1 && echo "git: ok" || echo "git: MISSING"
```

- **`go`** is a hard prerequisite (the install *is* `go install`). If MISSING and Homebrew
  is present, offer `brew install go` via AskUserQuestion; otherwise point at
  https://go.dev/dl/ and stop — do not proceed without Go.
- **`git`** is needed at runtime (docgraph shells out to it). If MISSING and Homebrew is
  present, offer `brew install git`; otherwise warn and continue.

### 3. Probe current state

So question defaults are smart:

```bash
BIN_DIR="$(go env GOBIN 2>/dev/null)"; [ -n "$BIN_DIR" ] || BIN_DIR="$(go env GOPATH 2>/dev/null)/bin"; [ -n "$BIN_DIR" ] || BIN_DIR="$HOME/go/bin"
[ -x "$BIN_DIR/docgraph" ] && echo "binary:present $("$BIN_DIR/docgraph" version 2>/dev/null)" || echo "binary:absent"
grep -qs 'docgraph doc-drift' ~/.claude/settings.json && echo "stop-hook:wired" || echo "stop-hook:missing"
[ -f "${XDG_CONFIG_HOME:-$HOME/.config}/docgraph/leaks.toml" ] && echo "leaks:present" || echo "leaks:absent"
[ -f ~/.claude/skills/docgraph/SKILL.md ] && echo "skill:present" || echo "skill:absent"
git rev-parse --is-inside-work-tree >/dev/null 2>&1 && echo "cwd:git-repo" || echo "cwd:not-a-repo"
```

Note the resolved `BIN_DIR` (usually `~/go/bin`) — reuse it in the wiring steps.

### 4. Run the core install

```bash
bash "$REPO_DIR/install.sh"
```

`install.sh` runs `go install` (from the checkout if IN_REPO, else `@latest`), seeds the
global config dir, and prints the installed binary path. If it fails, show the full output
and stop.

### 5. Ask what to set up

Use AskUserQuestion with a **multi-select** question — **"What should I set up for you?"**.
Mark as "Recommended" those not already detected as wired in step 3. Only offer the
**pre-push gate** if step 3 reported `cwd:git-repo`.

- **doc-drift Stop hook** — wires `docgraph doc-drift` into `~/.claude/settings.json` so it
  runs at the end of every Claude Code turn and blocks the turn while a tracked doc still
  describes code that just changed. (Recommended if `stop-hook:missing`.)
- **Pre-push gate (this repo)** — writes `.githooks/pre-push` in the current repo and points
  `core.hooksPath` at it, so a broken doc-graph blocks the push. (Only offer when in a repo.)
- **Leaks config** — seeds `~/.config/docgraph/` and explains how to populate `leaks.toml`.
  (Recommended if `leaks:absent`.)
- **`docgraph` skill** — installs the skill that teaches an agent to reach for `docgraph
  covers <path>` when it needs the doc governing a file. Without it the gates still fire,
  but the read-only views go unused because nothing advertises them. (Recommended if
  `skill:absent`.)

### 6. Wire the doc-drift Stop hook (if selected)

Wired as the **absolute** `$BIN_DIR/docgraph doc-drift` (typically `~/go/bin/docgraph
doc-drift`), so it resolves under the hook's minimal PATH. Idempotent — never duplicate or
disturb another tool's Stop hooks:

1. If step 3 reported `stop-hook:wired`, skip this step (already present).
2. Otherwise merge, seeding an empty object if the file is absent:

```bash
[ -f ~/.claude/settings.json ] || echo '{"hooks":{}}' > ~/.claude/settings.json
jq --arg cmd "$HOME/go/bin/docgraph doc-drift" \
   '.hooks.Stop += [{"hooks":[{"type":"command","command":$cmd}]}]' \
   ~/.claude/settings.json > /tmp/docgraph-settings.json && mv /tmp/docgraph-settings.json ~/.claude/settings.json
```

Substitute the real `$BIN_DIR` into `--arg cmd` if it isn't `~/go/bin`. Report whether the
hook was newly added or already present.

### 7. Wire the pre-push gate (if selected)

From inside the target repo:

```bash
docgraph install-hook          # writes .githooks/pre-push and sets core.hooksPath -> .githooks
```

The generated hook runs the whole-state gate plus both advisory riders — `footgun-drift`
and `covers-drift`. Add `--skip orphans` for a nav-driven MkDocs repo, `--ignore '<glob>'`
to bake in an exclusion, or `--no-footgun-drift` / `--no-covers-drift` to omit a rider.
Neither rider can block a push, and `covers-drift` is silent in a repo with no `covers:`
edges, so both are safe to leave in. Report the result.

### 8. Seed the leaks config (if selected)

```bash
mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}/docgraph"
```

Do **not** write any rules — leak rules are owner-sensitive and live only in this global
file by design (a per-repo deny-list would itself be the leak). Tell the user to populate
`~/.config/docgraph/leaks.toml` (schema in the README's leaks section) and that `docgraph
leaks-rules` exports the active rules for a `git filter-repo` history scrub.

### 9. Install the `docgraph` skill (if selected)

```bash
mkdir -p ~/.claude/skills/docgraph
```

Read `$REPO_DIR/.claude/skills/docgraph/SKILL.md` and write it verbatim to
`~/.claude/skills/docgraph/SKILL.md` (overwrite — the repo copy is the source of truth).
Report whether it was newly installed or updated.

### 10. Self-install this command

So `/docgraph:install` is available globally in future Claude Code sessions:

```bash
mkdir -p ~/.claude/commands/docgraph
```

Read `$REPO_DIR/.claude/commands/docgraph/install.md` and write it verbatim to
`~/.claude/commands/docgraph/install.md`.

### 11. Summary

Print three sections:

**Installed**
- `docgraph <version> → <BIN_DIR>/docgraph ✓` (use the version from step 3/4).
- Each wired item with its target and status: doc-drift Stop hook
  (`~/.claude/settings.json` — wired / already present / skipped), pre-push gate
  (`.githooks/pre-push` — installed / skipped), leaks config (`~/.config/docgraph/` —
  seeded / skipped), `docgraph` skill (`~/.claude/skills/docgraph/` — installed /
  updated / skipped).

**Reload**
- Restart Claude Code if the Stop hook or the skill was wired — hooks and skills take
  effect on the next session.
- Ensure `<BIN_DIR>` is on your `PATH` if the installer warned about it.

**Next steps**
- Run `docgraph .` in any repo to audit its doc-graph; the pre-push gate does this
  automatically on push.
- `docgraph covers <path>` names the doc governing a file — the skill teaches agents to
  reach for it. It answers only in repos whose docs declare `covers:` edges.
- Update any time by re-running `/docgraph:install` (or `go install
  github.com/lockyc/docgraph/v2@latest`) — nothing auto-updates the binary.
- `DOC_DRIFT_OFF=1` disables the Stop hook for a repo that doesn't use the
  anchored-symbol convention; `DOCGRAPH_FOOTGUN_OFF=1` disables the footgun nag and
  `DOCGRAPH_COVERS_OFF=1` the covers nag.
