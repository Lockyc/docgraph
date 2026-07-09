# docaudit

[![Release](https://img.shields.io/github/v/release/Lockyc/docaudit?sort=semver&label=release)](https://github.com/Lockyc/docaudit/releases/latest)
![Built with Go](https://img.shields.io/badge/built%20with-Go-00ADD8?logo=go&logoColor=white)
[![License](https://img.shields.io/github/license/Lockyc/docaudit)](LICENSE)

Audits a repo's **agent-facing documentation graph** — the docs an AI agent
navigates by grep + following `[x](y.md)` links, not the rendered site a human
browses — **and** scans tracked file content for leaked owner-specific/secret
strings. It **enforces every check by default** and exits non-zero on any
finding, so it drops into a pre-push hook or CI without a wrapper. You *exclude*
a check explicitly (`--skip`); nothing is opt-in, because an opt-in check
enforces nothing.

**Scope: project *documents*, not project *content*** (for the doc-graph
checks). Those checks audit the docs that explain the project (`docs/`,
`CLAUDE.md`, config-dir READMEs), not content a site framework routes and renders
(an Astro/MkDocs content collection, a seed-data corpus) — exclude those per-repo
via `.docauditignore`. The `leaks` check is broader: it scans *all* tracked files
(see below).

## Checks

All four run by default. Exclude one with `--skip <check[,check]>` (e.g. a
nav-driven MkDocs repo runs `--skip orphans`). A newly-added check is enforced
everywhere automatically — there is no run-list to update.

1. **Orphans** — a tracked doc not reachable from the entry points.
   Reachability follows both markdown links *and* bare/inline-code path mentions
   (`` `docs/x.md` ``), because an agent follows either. Every real `.md` is
   audited — including docs outside `docs/` (e.g. a config-dir README); only
   Claude Code tooling under `.claude/` and untracked scratch are excluded, as
   those aren't documentation.
2. **Broken links** — a `[x](y.md)` whose target doesn't exist (renamed/moved/
   deleted, link not updated). Checked across all tracked `.md`.
3. **Untracked** — a `.md` on disk but not in git (a forgotten `git add`) —
   absent from clones, the built site, and any mirror.
4. **Leaks** — tracked file *content* matching a configured leak pattern (see
   below). Scans file content rather than the doc graph.

### `leaks` — the content scan

Scans **tracked file content** (working tree only, never git history) for
owner-specific / secret strings, to catch them before a repo goes public. Runs
by default like the other checks; `--skip leaks` turns it off.

Scope is governed by **git tracking**, not the doc-graph ignore layers: every
`git ls-files` entry is scanned (so `.gitignore` governs what's excluded), and
the `defaultIgnores`/`.docauditignore` used by the orphans/broken/untracked
checks do **not** narrow the leak pass — only an explicit `--ignore` glob does.
A tracked `.claude/` config still ships in a public clone, so it stays in
scope even though it's excluded from the doc-graph checks.

Patterns come from a **global** rules file — never committed to a repo, because a
per-repo deny list would itself enumerate your sensitive terms. Resolution order:
`--leaks-config <path>` → `$DOCAUDIT_LEAKS` → `os.UserConfigDir()/docaudit/leaks`
(e.g. `~/.config/docaudit/leaks`).

Format — one Go regexp per line; `!` prefixes an allow-exception that suppresses a
deny match it covers; `#` and blank lines are ignored; a ` #` trailing comment is
stripped:

    lsjc\.au
    /Users/[a-z]+
    !au\.lsjc\.curator      # bundle id — meant to ship

A small built-in set of unambiguous secret shapes (PEM private-key headers, AWS
`AKIA…`, GitHub `ghp_…`, Slack `xox…` tokens) always runs and is suppressible with
`!` allow lines.

**Config handling** (leaks runs by default, incl. in CI, which has no global file):
- **No config file** → scans with the built-in patterns only and prints a
  warning; **not** fatal. Baseline secret detection is always on; define your
  global file (or pass `--leaks-config`) to enforce your own footprint patterns —
  they take effect wherever that file exists (your local pre-push).
- **Malformed config** (a bad regex) → exit 2 (fail-closed). A broken config is a
  real bug, not the common "not set up yet" case, so it fails loudly rather than
  silently degrading.

**Known gaps:** no git-history scan (rewriting history is the owner's call — use the
manual leak-audit skill), no per-rule messages.

## Install

```bash
go install github.com/lockyc/docaudit@latest   # or: just install
```

Stdlib only — no dependencies. Requires `git` on PATH.

## Usage

```bash
docaudit [path]                     # path defaults to the current directory; enforces all checks
docaudit --root wiki/Home.md        # add an extra entry point (repeatable)
docaudit --ignore 'vendor/**'       # exclude a glob from checks (repeatable)
docaudit --skip orphans             # exclude a check (comma-separated; e.g. nav-driven MkDocs)
docaudit --skip leaks               # exclude the content leak scan
docaudit --leaks-config <path>      # override the global leak rules file
docaudit version                    # print version (also --version, -v)
```

Exit codes: `0` clean · `1` findings in an enforced check · `2` usage / not a git repo / malformed leak config.

> **v2 breaking change:** the `--checks` (include) flag was removed. docaudit now
> enforces every check by default; use `--skip` to exclude one, and regenerate any
> installed hook with `docaudit install-hook --force`. A stray `--checks` prints a
> migration message and exits 2.

On a finding, the output is self-describing: below the findings it prints what
docaudit is, that its non-zero exit is what aborts a pre-push, what a finding
means, and how to remediate each category. A reader who sees only a failed push —
human or agent — should not have to reverse-engineer the gate. Clean/CI runs stay
terse.

### Install as a pre-push gate

```bash
docaudit install-hook [path]                # gate: enforce ALL checks (default)
docaudit install-hook --skip orphans        # nav-driven repos (no orphan gate)
docaudit install-hook --force               # regenerate an existing hook (e.g. after upgrading)
```

The generated hook runs a bare `docaudit .`, so a check added in a later version
is enforced without regenerating the hook. It writes a tracked `.githooks/pre-push`
and sets `core.hooksPath -> .githooks` for this clone (other clones activate it
with `git config core.hooksPath .githooks`). Refuses to clobber an existing
`.githooks/pre-push` (pass `--force`, or integrate into it — e.g. call docaudit
from an existing `make lint`). Fails **closed**: a missing `docaudit` blocks the
push, because a gate that skips when its tool is absent is a false green, not a
gate.

### Doc models and when to `--skip orphans`

The orphan check assumes a **prose-linked** doc graph (entry docs link/mention
their way through `docs/`). That fits most repos. Two exceptions:

- **Nav-driven MkDocs sites** (a `docs/` with no `nav:` block — MkDocs
  auto-builds the sidebar from the file tree, pages never cross-link). Every
  page is a prose-orphan by design. Gate these with `--skip orphans`.
- Repos with genuinely unreferenced design docs will report real orphans — link
  them from `CLAUDE.md`/`README`, or accept and exclude with `--skip orphans`.

### Entry points (roots)

Reachability starts from whichever of `CLAUDE.md`, `README.md`, `AGENTS.md`
(repo root) and `docs/index.md` are tracked, plus any `--root` you add. This
covers both a whole doc repo and a project whose docs are `CLAUDE.md` + `docs/`.

### Ignoring paths

`**/superpowers/**` is ignored by default for the doc-graph checks
(intentionally-untracked scratch). Add more via a `.docauditignore` file
(gitignore syntax, `#` comments) or repeatable `--ignore` globs. Globs support
`**` (any number of path segments), `*`, `?`. Note the leak scan honors only
`--ignore` (not the default/`.docauditignore` layers) — see the leaks section.

## Known gaps

Anchor validity (`y.md#missing`), external-URL liveness, raw `<a href>` in HTML
blocks, per-section `index.md` implicit-nav, and repo-specific conventions are
out of scope. Markdown-link extraction (used for broken-link detection and link
edges) skips fenced/inline code so example paths aren't treated as real links;
the orphan *reachability* pass, by contrast, does read inline-code path mentions
(that's how it follows `` `docs/x.md` ``).

## Development

```bash
just test    # go test ./...
just build   # go build -o docaudit .
just install # go install . -> ~/go/bin/docaudit
just gate    # gofmt check + vet + tests (pre-release gate)
```

Work lands on the `dev` integration branch; `main` is the release branch and only
fast-forwards to a tagged release. Branch feature/fix work off `dev`, run `just gate`
before merging. Releases follow [semantic versioning](https://semver.org): the root
`VERSION` file is the single source of truth (embedded into the binary via `go:embed`),
and `just release` tags `v<VERSION>` and publishes the matching GitHub release. Because
consumers `go install …@latest`, a release moves everyone's pinned tool — keep `main`
releasable.

**Deploy is automatic on this repo's host.** A tracked `.githooks/post-commit`
(and `post-merge`) runs `go install .`, so `~/go/bin/docaudit` always matches the
latest commit — no manual `just install` step to forget, and no stale binary
gating pushes with old logic. Activate in a fresh clone with
`git config core.hooksPath .githooks`.

The installed binary must be reachable when a hook fires: the pre-push hook
`install-hook` generates resolves docaudit via PATH **and** the Go bin dir
(`$GOBIN` / `$GOPATH/bin` / `~/go/bin`), because git runs hooks with the caller's
PATH — GUI clients and sandboxed agents often push with a bare PATH that omits
`~/go/bin`, and a `command -v`-only hook would then fail-closed for the wrong
reason (tool present but unseen).

Layout: `main.go` is a thin CLI (flags → audit → report → exit code);
`internal/audit/` holds the logic (link parsing, glob-ignore, git wrappers, the
`Audit` orchestrator, and the leak scanner). See `CLAUDE.md` for design invariants.
