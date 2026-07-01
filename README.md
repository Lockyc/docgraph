# docaudit

Audits a repo's **agent-facing documentation graph** — the docs an AI agent
navigates by grep + following `[x](y.md)` links, not the rendered site a human
browses. Flags three things and exits non-zero on any finding, so it drops into
a pre-push hook or CI without a wrapper.

## Checks

1. **Orphans** — tracked `.md` not reachable by following links from the entry
   points. The agent can't discover it by pointer-following.
2. **Broken links** — a `[x](y.md)` whose target doesn't exist (renamed/moved/
   deleted, link not updated).
3. **Untracked** — a `.md` on disk but not in git (a forgotten `git add`) —
   absent from clones, the built site, and any mirror.

## Install

```bash
go install git.lsjc.au/lachlan/docaudit@latest   # or: just install
```

Stdlib only — no dependencies. Requires `git` on PATH.

## Usage

```bash
docaudit [path]              # path defaults to the current directory
docaudit --root wiki/Home.md # add an extra entry point (repeatable)
docaudit --ignore 'vendor/**' # exclude a glob from checks (repeatable)
```

Exit codes: `0` clean · `1` findings · `2` usage / not a git repo.

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
out of scope. Links inside fenced/inline code are skipped.

## Development

```bash
just test    # go test ./...
just build   # go build -o docaudit .
just install # go install .
```

Layout: `main.go` is a thin CLI (flags → audit → report → exit code);
`internal/audit/` holds the logic (link parsing, glob-ignore, git wrappers, the
`Audit` orchestrator). See `CLAUDE.md` for design invariants.
