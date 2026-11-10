# docgraph

[![Release](https://img.shields.io/github/v/release/lockyc/docgraph?sort=semver&label=release)](https://github.com/lockyc/docgraph/releases/latest)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-555)
![Go](https://img.shields.io/github/go-mod/go-version/lockyc/docgraph?logo=go&logoColor=white)
[![License](https://img.shields.io/github/license/lockyc/docgraph)](LICENSE)

Catches the docs your coding agent relies on going stale before you push. An agent finds its way
through a repo by reading `CLAUDE.md` and following the Markdown it links to; docgraph checks
those docs for pages nothing reaches, links that point nowhere and docs never added to git. It
also scans tracked files for secrets and for names you've listed. Run as a pre-push hook, any
finding blocks the push.

## Quick start

```bash
go install github.com/lockyc/docgraph/v3@latest   # needs Go + git on PATH
docgraph .                                      # audit the current repo
docgraph install-hook                           # wire it in as a pre-push gate
```

A clean run prints one line: `docgraph: clean ✓ (12 tracked .md, 12 reachable, 0 findings)`. A
run with findings names each one and how to fix it, and exits 1:

```console
$ docgraph .
roots: [README.md]   tracked .md: 3   reachable: 2

ORPHANS (1) — docs unreachable by link/path-following:
  docs/old-notes.md

BROKEN LINKS (1) — .md targets that don't exist:
  docs/setup.md:9 → docs/install.md

──────────────────────────────────────────────────────────────────────────────────
docgraph: 2 finding(s) in gated checks → exiting non-zero.
…
Fix the findings listed above:
  ORPHAN    → link it in from a reachable doc; or `--ignore '<glob>'` (a
              .docgraphignore entry) if it is intentionally standalone; or delete it.
  BROKEN    → repair or remove the dead .md link at the shown file:line.
```

## The four modes

| Mode | Command | Scans | Runs at | Blocks? |
| --- | --- | --- | --- | --- |
| **Whole-state checks** | `docgraph [path]` | the current tree | pre-push / CI | **yes** — exit 1 on any finding |
| **`footgun-drift`** | `docgraph footgun-drift` | what a push *adds* | pre-push (advisory rider) | no — nags, always exit 0 |
| **`covers-drift`** | `docgraph covers-drift` | the code a push *changes* | pre-push (advisory rider) | no — nags, always exit 0 |
| **`doc-drift`** | `docgraph doc-drift` | the branch diff incl. uncommitted changes (on a trunk branch: this agent session's commits + uncommitted changes) | agent Stop hook | **yes** — exit 2, once per finding |

- **Whole-state checks** — seven, all on by default: orphans, disconnected (a doc with
  frontmatter but no place in the doc structure), broken links, untracked docs, leaks,
  frontmatter, and frontmatter edges. `--skip <check>` turns one off. In full:
  [docs/checks.md](docs/checks.md).
- **Leaks** — scans every tracked file against rules in a machine-local
  `~/.config/docgraph/leaks.toml`, never committed; with no rules file it scans nothing.
  [docs/leaks.md](docs/leaks.md).
- **The drift modes** — what a push or an agent's turn just changed: an added footgun
  declaration (a documented trap), code a doc says it `covers`, a removed symbol a doc still
  names, a constant whose old value a doc still shows. [docs/drift.md](docs/drift.md).
- **Read-only views** — `covers <path>`, `index`, `stale` and `graph [--json]` query the doc
  graph and never gate. [docs/views.md](docs/views.md).

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/lockyc/docgraph/main/install.sh | bash
```

This runs `go install` (from the current checkout if you're in one, else `@latest`), seeds
`~/.config/docgraph/`, and prints where the binary landed. Or directly:

```bash
go install github.com/lockyc/docgraph/v3@latest
```

Needs **Go** and **git** on PATH. From a clone of this repo, Claude Code's `/docgraph:install`
also wires `doc-drift` into `~/.claude/settings.json` as a Stop hook.

### As a pre-push gate

```bash
docgraph install-hook [path]                    # gate: enforce ALL checks (default)
docgraph install-hook --skip orphans            # auto-nav MkDocs repos (no orphan gate)
docgraph install-hook --ignore '**/*_test.go'   # bake an --ignore glob into the hook
docgraph install-hook --force                   # regenerate an existing hook
docgraph install-hook --no-footgun-drift        # omit the footgun-drift rider
docgraph install-hook --no-covers-drift         # omit the covers-drift rider
```

The generated hook runs `docgraph .`, then the advisory `footgun-drift` and `covers-drift`;
only the first can block a push. `install-hook` writes a tracked `.githooks/pre-push` and sets
`core.hooksPath` for this clone (others activate it with `git config core.hooksPath .githooks`).
It refuses to clobber an existing hook without `--force`, and fails **closed**: a missing
`docgraph` blocks the push. It finds docgraph on PATH **and** in the Go bin dir, so it works
from GUI clients and sandboxed agents too.

## Usage

```bash
docgraph [path]                     # path defaults to '.'; enforces all checks
docgraph --root wiki/Home.md        # add an extra entry point (repeatable)
docgraph --ignore 'vendor/**'       # exclude a glob from checks (repeatable)
docgraph --skip orphans             # exclude a check (comma-separated)
docgraph --leaks-config <path>      # override the global leak rules file
docgraph --config <path>            # override the global config.toml (usage logging)
docgraph install-hook               # write .githooks/pre-push and set core.hooksPath
docgraph leaks-rules                # print the leak rules as a git-filter-repo replace list
docgraph footgun-drift              # advisory: reads pre-push ref lines from stdin
docgraph covers-drift               # advisory: docs covering the code a push changes
docgraph doc-drift                  # Stop-hook: working-tree-inclusive diff
docgraph schema                     # print the frontmatter JSON Schema (read-only)
docgraph covers <path>              # read-only: docs that cover <path>
docgraph index                      # read-only: generated markdown index
docgraph stale                      # read-only: docs past their freshness threshold
docgraph graph [--json]             # read-only: both doc graphs (markdown or JSON)
docgraph version                    # print version (also --version, -v)
```

Flags may appear before or after the path. Linked worktrees work with nothing to configure.

**Exit codes.** `docgraph [path]`: `0` clean · `1` findings · `2` usage / not a
git repo / malformed leak config. `footgun-drift` and `covers-drift` are
advisory: `0` regardless of findings (nag on stdout), `2` only on a git/usage
error. `doc-drift` blocks: `0` clean (or loop-guard-silenced) · `2` on a
dangling-reference, anchored-value or covers finding (stderr), or on an error the
first time it is seen · `1` on the same error again (bare runs; a clean run re-arms
it — `--range` always exits `2` on an error) or a bad flag. `covers` /
`index` / `stale` / `graph` are read-only: `0` always on success, `2` only on error.

Optional, machine-local usage logging (one JSON line per run): [docs/logging.md](docs/logging.md).

## Limits

- Anchor validity (`y.md#missing`), external-URL liveness, raw `<a href>` in HTML blocks and
  per-section `index.md` implicit nav aren't checked.
- Broken-link detection skips fenced and inline code, so example paths aren't treated as
  links; orphan reachability does read inline-code path mentions.
- `footgun-drift` has no code-fence awareness: an example `Footgun:` line inside a fenced block
  reads as a real declaration.
- `leaks` scans the working tree, never git history; use a history scanner such as gitleaks,
  and `docgraph leaks-rules` to scrub a known leak.

## Status

In use, `v3`. What's next: [docs/FOLLOWUPS.md](docs/FOLLOWUPS.md).

## Development

```bash
git clone https://github.com/lockyc/docgraph && cd docgraph
just test    # go test ./...
just build   # go build -o docgraph .
just install # go install . -> ~/go/bin/docgraph
just gate    # gofmt check + vet + tests
```

Branch model, release process and layout: [CLAUDE.md](CLAUDE.md).

## License

MIT — see [LICENSE](LICENSE).
