# docaudit

Audits a repo's **agent-facing documentation graph** — the docs an AI agent
navigates by grep + following `[x](y.md)` links, not the rendered site a human
browses. Flags three things and exits non-zero on any finding, so it drops into
a pre-push hook or CI without a wrapper.

**Scope: project *documents*, not project *content*.** It audits the docs that
explain the project (`docs/`, `CLAUDE.md`, config-dir READMEs). It is *not* for
content a site framework routes and renders (an Astro/MkDocs content collection,
a seed-data corpus) — exclude those per-repo via `.docauditignore`.

## Checks

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

## Install

```bash
go install git.lsjc.au/lachlan/docaudit@latest   # or: just install
```

Stdlib only — no dependencies. Requires `git` on PATH.

## Usage

```bash
docaudit [path]                     # path defaults to the current directory
docaudit --root wiki/Home.md        # add an extra entry point (repeatable)
docaudit --ignore 'vendor/**'       # exclude a glob from checks (repeatable)
docaudit --checks broken,untracked  # run/gate a subset (default: all three)
```

Exit codes: `0` clean · `1` findings in a selected check · `2` usage / not a git repo.

### Install as a pre-push gate

```bash
docaudit install-hook [path]                    # gate all three checks
docaudit install-hook --checks broken,untracked # nav-driven repos (no orphan gate)
docaudit install-hook --soft                    # fail open if docaudit is absent
```

Writes a tracked `.githooks/pre-push` and sets `core.hooksPath -> .githooks` for
this clone (other clones activate it with `git config core.hooksPath .githooks`).
Refuses to clobber an existing `.githooks/pre-push` (pass `--force`, or integrate
into it — e.g. homelab runs docaudit from `make lint`). Default fails **closed**
(a missing `docaudit` blocks the push); `--soft` fails open, which suits repos
cloned where the tool may be absent (CI, public contributors).

### Doc models and when to drop `--checks orphans`

The orphan check assumes a **prose-linked** doc graph (entry docs link/mention
their way through `docs/`). That fits most repos. Two exceptions:

- **Nav-driven MkDocs sites** (a `docs/` with no `nav:` block — MkDocs
  auto-builds the sidebar from the file tree, pages never cross-link). Every
  page is a prose-orphan by design. Gate these with
  `--checks broken,untracked`.
- Repos with genuinely unreferenced design docs will report real orphans — link
  them from `CLAUDE.md`/`README`, or accept and narrow with `--checks`.

### Entry points (roots)

Reachability starts from whichever of `CLAUDE.md`, `README.md`, `AGENTS.md`
(repo root) and `docs/index.md` are tracked, plus any `--root` you add. This
covers both a whole doc repo and a project whose docs are `CLAUDE.md` + `docs/`.

### Ignoring paths

`**/superpowers/**` is ignored by default (intentionally-untracked scratch). Add
more via a `.docauditignore` file (gitignore syntax, `#` comments) or repeatable
`--ignore` globs. Globs support `**` (any number of path segments), `*`, `?`.

## v1 gaps (known, not silent)

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
just install # go install .
```

Layout: `main.go` is a thin CLI (flags → audit → report → exit code);
`internal/audit/` holds the logic (link parsing, glob-ignore, git wrappers, the
`Audit` orchestrator). See `CLAUDE.md` for design invariants.
