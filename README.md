# docgraph

[![Release](https://img.shields.io/github/v/release/lockyc/docgraph?sort=semver&label=release)](https://github.com/lockyc/docgraph/releases/latest)
![Built with Go](https://img.shields.io/badge/built%20with-Go-00ADD8?logo=go&logoColor=white)
[![License](https://img.shields.io/github/license/lockyc/docgraph)](LICENSE)

Audits a repo's **agent-facing documentation graph** — the docs an AI agent
navigates by grep and by following `[x](y.md)` links, not the rendered site a
human browses — and scans tracked file content for leaked secret / owner-specific
strings. It's built to run as a **pre-push gate** or in CI without a wrapper: a
finding exits non-zero and aborts the push.

## Quick start

```bash
go install github.com/lockyc/docgraph/v3@latest   # needs Go + git on PATH
docgraph .                                      # audit the current repo
docgraph install-hook                           # wire it in as a pre-push gate
```

Under Claude Code, `/docgraph:install` does the install, wires the `doc-drift`
Stop hook, and seeds the leaks config for you. See [Install](#install) for the
full menu.

## The four modes

docgraph has four independent modes, each with its own trigger and scope:

| Mode | Command | Scans | Runs at | Blocks? |
| --- | --- | --- | --- | --- |
| **Whole-state checks** | `docgraph [path]` | the current tree | pre-push / CI | **yes** — exit 1 on any finding |
| **`footgun-drift`** | `docgraph footgun-drift` | what a push *adds* | pre-push (advisory rider) | no — nags, always exit 0 |
| **`covers-drift`** | `docgraph covers-drift` | the code a push *changes* | pre-push (advisory rider) | no — nags, always exit 0 |
| **`doc-drift`** | `docgraph doc-drift` | the branch diff incl. uncommitted changes (on a trunk branch: this agent session's commits + uncommitted changes) | agent Stop hook | **yes** — exit 2, once per finding |

Plus **read-only** helpers that never gate: `schema` (emits the frontmatter
vocabulary), and the doc-graph **views** `covers` / `index` / `stale` / `graph`
(`graph` serves both doc graphs — human render or JSON).

**Scope note.** The whole-state doc-graph checks audit *documents* (`docs/`,
`CLAUDE.md`, config-dir READMEs) — not content a site framework routes and
renders (an Astro/MkDocs content collection, a seed-data corpus); exclude those
per-repo via `.docgraphignore`. The `leaks` check is broader — it scans *all*
tracked files (see [`leaks`](#leaks--the-content-scan)).

## Whole-state checks

Seven checks run by default on `docgraph [path]`. **Everything is enforced; you
exclude explicitly** with `--skip <check[,check]>`; a check added in a later
version is enforced automatically. (An auto-nav MkDocs repo, for instance, runs `--skip
orphans`.)

The checks span **two independent doc graphs**:

- **The content graph** — *findability*. Tracked docs joined by prose references:
  markdown `[x](y.md)` links ∪ bare/inline-code path mentions (`` `docs/x.md` ``),
  read over each doc's **body** (frontmatter stripped). `orphans` enforces it.
- **The metadata graph** — *structural placement*. Tracked docs joined by their
  frontmatter `links:` doc→doc edges (`part-of`, `see-also`, `depends-on`, …).
  `disconnected` enforces it.

They answer different questions and are **not** a single union: a doc can be
findable (reachable in the content graph) yet declare no place in the structure,
or vice versa. Both are flagged.

1. **Orphans** — a **content-graph island**: a non-root tracked doc with zero
   *inbound* content edges (nothing links to it or mentions its path). Roots
   (`CLAUDE.md`/`README.md`/`AGENTS.md`/`docs/index.md` + `--root`) are exempt.
   Frontmatter doc→doc edges do **not** count here — they belong to the metadata
   graph. Every real `.md` is audited, including docs outside `docs/` (e.g. a
   config-dir README); only agent tooling under `.claude/` and `.agents/` plus
   untracked scratch are excluded.
2. **Disconnected** — a **metadata-graph island**: a doc that carries frontmatter
   but participates in zero doc→doc edges in *either* direction. `covers` (code
   ownership) and `source` (external provenance) edges don't count, so a doc whose
   only edges are `covers`→code is disconnected. Fix it with a real `part-of`/`see-also`/`depends-on` edge.
3. **Broken links** — a `[x](y.md)` whose target doesn't exist. Checked across
   the same tracked, non-ignored `.md` set as orphans.
4. **Untracked** — a `.md` on disk but not in git (a forgotten `git add`).
5. **Leaks** — tracked file *content* matching a configured leak pattern (see
   [`leaks`](#leaks--the-content-scan)).
6. **Frontmatter** — every doc except `README.md` (matched by basename) must
   carry a leading YAML block (first line exactly `---` to the next `---`) that is
   well-formed YAML and declares a `type`. A missing block, malformed YAML, or a
   missing `type` is a finding; `README.md` is exempt (see *Where to put
   frontmatter* below). `type` is an advisory vocabulary — see
   [`schema`](#docgraph-schema--the-frontmatter-vocabulary).
7. **Edges** — every internal `to` target in a frontmatter `links:` list (a
   repo-root-relative `.md` doc or code path) must exist, and `part-of` /
   `supersedes` edges between docs must not form a cycle. External URLs and
   `owner/repo:...` cross-repo targets are never checked.

### `docgraph schema` — the frontmatter vocabulary

```bash
docgraph schema    # prints the JSON Schema (draft 2020-12) to stdout
```

Emits the [JSON Schema](https://json-schema.org/) describing valid doc
frontmatter — the `type` / `verified` / `review` / `links` shape the
`frontmatter` and `edges` checks enforce, plus the advisory `type` / `rel`
vocabularies (as `x-docgraph-core-types` / `x-docgraph-core-rels`), for
validating frontmatter in another tool (an editor, a catalog builder). **Read-only** — never reads the repo, never part of
the gate.

**Where to put frontmatter.** Frontmatter is **required on every doc except
`README.md`**, and `disconnected` never fires on a README without it. A
frontmattered README still passes, but GitHub renders the block as a table above
the page, so keep it out.

### `leaks` — the content scan

Scans **tracked file content** (working tree only, never git history) for
secret / owner-specific strings, to catch them before a repo goes public. Runs by
default; `--skip leaks` turns it off.

It is the only check with a noticeable cost, roughly proportional to tracked
bytes and rule count: ~6s on a 9.7k-file, ~1GB repo, against ~0.3s for all six
doc-graph checks there. Binary files are skipped.

Scope is governed by **git tracking**, not the doc-graph ignore layers: every
`git ls-files` entry is scanned (so `.gitignore` decides what's excluded), and
`defaultIgnores` / `.docgraphignore` do **not** narrow it — only an explicit
`--ignore` glob does. Tracked `.claude/` files are scanned, even though the
doc-graph checks skip them.

Patterns come from a **global TOML file, never committed to a repo**. Resolution:
`--leaks-config <path>` → `$DOCGRAPH_LEAKS` →
`$XDG_CONFIG_HOME/docgraph/leaks.toml` (default `~/.config/docgraph/leaks.toml`).

| Level | Key | Default | What it does |
|---|---|---|---|
| top | `terms` | `[]` | Literal deny strings (the `default` group) |
| top | `regex` | `[]` | Regexp deny patterns (the `default` group) |
| top | `allow` | `[]` | Literal strings that suppress a deny match, everywhere |
| top | `allow_regex` | `[]` | Regexps that suppress a deny match, everywhere |
| `[[group]]` | `name` | *required* | Named deny list; unique, never `default` |
| `[[group]]` | `terms` / `regex` | `[]` | The group's deny rules (at least one required) |
| `[[dir]]` | `path` | *required* | Absolute directory the exceptions apply under (`~/` expanded) |
| `[[dir]]` | `ignore` | `[]` | Globs (relative to `path`) whose files skip the silenced groups |
| `[[dir]]` | `ignore_groups` | `["default"]` | Which groups `ignore` silences (replaces the default) |
| `[[dir]]` | `allow` / `allow_regex` | `[]` | Allow rules scoped to this subtree |

`terms` match literally and case-insensitively; `regex` / `allow_regex` are Go
regexps, also case-insensitive unless you opt out per-pattern with a leading
`(?-i)`. A non-absolute `[[dir]].path` is a fatal config error.

    terms       = ["acme-host", "you@example.com", "/Users/you"]
    regex       = ['10\.0\.0\.\d+']
    allow       = ["github.com/you"]
    allow_regex = ['com\.example\.[a-z]+']

    [[dir]]
    path   = "/abs/path/to/repo"
    ignore = ["vendor/*.json"]        # skip vendored specs
    [[dir]]
    path  = "/abs/path/to/repo/sub"
    allow = ["some-project"]          # legit in this subtree

Deny rules fall into **groups**. Top-level `terms` / `regex` are the implicit
`default` group; a `[[group]]` block is a named deny list. A `[[dir]].ignore`
glob silences the `default` group unless `ignore_groups` names other groups.
`ignore_groups` *replaces* that default rather than adding to it, so list
`"default"` too if you want it silenced as well. This lets a private repo ignore
its own footprint vocabulary while still being scanned for terms that must not
appear in *any* repo:

    regex = ['(?-i)AKIA[0-9A-Z]{16}']   # default group: secret shapes

    [[group]]
    name  = "footprint"
    terms = ["acme-host", "/Users/you"]

    [[group]]
    name  = "client"
    terms = ["big-client"]

    [[dir]]
    path          = "/abs/path/to/private-repo"
    ignore        = ["**"]
    ignore_groups = ["footprint"]     # footprint is fine here; client + secrets stay live

`default` is reserved for the top-level rules. These are all fatal config errors:
a `[[group]]` named `default`; a group with no name, a duplicate name, or no
terms/regex; an `ignore_groups` naming an undefined group; an `ignore_groups`
with no `ignore` globs.
`allow` / `allow_regex` are not group-scoped: naming the string suppresses it
whatever group it belongs to, which is the per-repo escape hatch for a class the
repo legitimately owns.

**The config is the sole source of rules — no hidden built-ins.** Generic secret
shapes (PEM, AWS, GitHub, Slack) are just `regex` entries you add, with a leading
`(?-i)` to keep them case-sensitive:

    regex = [
      '(?-i)-----BEGIN [A-Z ]*PRIVATE KEY-----',
      '(?-i)AKIA[0-9A-Z]{16}',
      '(?-i)ghp_[A-Za-z0-9]{36}',
      '(?-i)xox[baprs]-[A-Za-z0-9-]{10,}',
    ]

Because the config is machine-local, CI and fresh clones (no file) have no rules
and the scan is a no-op there — it stays a **local** pre-push gate. Handling:

- **No config file** → no rules, scans nothing, prints a warning. **Not** fatal.
- **Malformed config** (bad TOML, an unknown key, bad regex, a non-absolute
  `[[dir]]` path, a `[[group]]` with no terms or regex) → exit 2, fail-closed.
  `--skip leaks`
  bypasses config loading entirely, so it's the escape hatch if a config trips
  this path — fix the config rather than leaning on the skip, since it also
  turns off the check.

**Known gaps:** no git-history *detection* (use a history scanner such as gitleaks);
*scrubbing* a known leak from history is `docgraph leaks-rules` below. No
per-rule messages.

### `docgraph leaks-rules` — export rules for history scrubbing

The `leaks` check scans the current tree. To scrub a leak already in **git
history**, export the vocabulary as a
[`git-filter-repo`](https://github.com/newren/git-filter-repo) rules file and run
the rewrite separately:

```sh
docgraph leaks-rules > rules.txt          # non-destructive: reads only the config
git filter-repo --replace-text rules.txt  # destructive: rewrites history
```

It emits one `regex:` line per deny rule, including every `[[group]]`'s rules
(terms are escaped and case-insensitive; `regex` entries are case-insensitive,
except those starting with `(?-i)`, which are emitted as plain case-sensitive
patterns), using filter-repo's `***REMOVED***` replacement. A
stderr summary reports any `allow` / `allow_regex` / `[[dir]]` rules it
**dropped** (filter-repo can't apply them). Emitted patterns target filter-repo's Python regex
engine, so RE2-only syntax may need manual review.

> The rewrite changes commit SHAs: force-push and have every collaborator
> re-clone.

## `footgun-drift` — the advisory pre-push rider

`footgun-drift` scans only what a push **adds** to tracked markdown, and it is
**advisory**: it prints a nag but exits 0 and never blocks. It flags any *added*
line that declares a footgun (a documented trap): a line starting with
`Footgun:`, or a bolded footgun lead-in mid-line. Mentioning or
cross-referencing a footgun, or a bare `## Footguns` heading, doesn't count.

```bash
docgraph footgun-drift                       # reads git's pre-push ref lines from stdin
docgraph footgun-drift --range base..head    # explicit range, for manual use
```

With no `--range` it reads the pushed refs from the `pre-push` hook's stdin (a new
branch is compared against its merge-base with the nearest integration branch).
Only added lines count, so a declaration already on the remote is never
re-flagged. File scope is every `.md` the diff
touches, including `.claude/` skill files that the doc-graph checks exclude.

It is a **nag, not a judge**: it reports every added declaration. On a finding it asks two questions — (1) is this a real footgun (a trap
you hit, a tempting-but-wrong approach, a re-litigated decision)? (2) is it at
the right doc level (invariant → `CLAUDE.md`, rationale → `docs/`, human prose →
`README`)? It also names the commonest false footgun: a bug you just fixed, whose
story belongs in the commit message while the doc keeps at most the surviving
rule. Fix or leave it in a follow-up; nothing was blocked.

`DOCGRAPH_FOOTGUN_OFF=1` silences it outright (for a repo that doesn't use the
`Footgun:` convention); `docgraph install-hook --no-footgun-drift` generates a
hook that never invokes it. There is no in-file suppression marker.

## `covers-drift` — the advisory pre-push rider

`covers-drift` scans the **code** a push changes and reports any doc that
declares a frontmatter `covers` edge onto it while the doc itself went
untouched. Like `footgun-drift` it is **advisory**: it prints a nag, exits 0, and
never blocks.

```bash
docgraph covers-drift                       # reads git's pre-push ref lines from stdin
docgraph covers-drift --range base..head    # explicit range, for manual use
```

The same check runs at
the end of each agent turn inside
[`doc-drift`](#docgraph-doc-drift--the-stop-hook-staleness-gate), scoped to that
session's changes; this rider is the push-time pass over the whole pushed range.

With no `--range` it reads the pushed refs from the `pre-push` hook's stdin,
deduping findings across refs. Docs are `.md` only (unlike `doc-drift`, which also
reads `.mdx`); "code" is every other tracked path except the prose formats
`.txt`/`.rst`/`.adoc`/`.markdown`. A `covers` edge declared in an `.mdx` file
never fires.

**A still-accurate doc wants no edit, and the nag says so**; a falsified one is
corrected or cut, nothing more. A doc the push also touched never fires. A repo with no `covers` edges never sees it. To turn it off outright: `DOCGRAPH_COVERS_OFF=1`, or
`docgraph install-hook --no-covers-drift` to generate a hook that never invokes
it.

Riders are written into `.githooks/pre-push` at hook-generation time;
regenerate with `docgraph install-hook --force` to pick up new ones.

## `docgraph doc-drift` — the Stop-hook staleness gate

Wire `doc-drift` into your agent harness's `Stop` hook (invoked directly, no
wrapper) so it runs at the end of every turn, catching tracked docs that still
describe code which just changed underneath them — before the agent hands control
back.

It scans a **working-tree-inclusive** range — base→worktree, covering committed
*and* uncommitted changes.
On a feature branch the range starts at the closest integration branch's
merge-base (the whole branch so far). On a trunk branch it covers only this agent
session's changes: commits since the agent's previous turn ended (its last Stop),
plus the working tree; without a session (a manual run), the working tree alone. It flags
two mechanical staleness classes, and both **block** the turn from ending:

1. **Dangling reference** — a symbol whose *definition* was removed in the diff
   and survives nowhere else in tracked code, but a tracked doc still names it.
2. **Anchored value drift** — a constant whose numeric *value* changed while a
   tracked doc still names the constant **and** shows the old literal.

```bash
docgraph doc-drift                        # bare: resolves the base itself, applies the loop-guard
docgraph doc-drift --range base..head     # explicit range — bypasses the loop-guard
```

Bare invocation applies a **once-per-finding loop-guard**: a finding blocks the
first time it appears, then stays silent at that `HEAD` and every later one. Any
finding that wasn't in the previous nag blocks the turn, and that block lists all
current findings. A finding that is fixed and later comes back counts as new.

A finding in either class **blocks** — prints to **stderr**, exits **2**.

It also raises **covers drift**: code this session changed that a doc declares it
`covers`, where the session didn't touch the doc. The change set is the paths
this session edited or committed since its previous Stop (or since it started),
plus the working tree — reading a file, merging a trunk in, or another session's
commits never count. It reads the Stop payload on stdin; without one it
checks the working tree alone, and `--range` checks that spec. A doc touched
anywhere in the set clears the finding. Each (doc, path) pair blocks once per
session; the message asks the agent to read the parts of the doc describing
what it changed and fix only what is now false. `DOCGRAPH_COVERS_OFF=1` turns
off covers drift.
`DOC_DRIFT_OFF=1` disables the whole subcommand outright, for a repo whose docs
don't name code symbols alongside their values. Paths the
doc-graph ignore layers exclude (defaults + `.docgraphignore`) are out of scope on
both sides — neither scanned as code nor grepped as docs. There is no
other suppression surface (no per-finding flag, no inline
marker): a flagged reference is a judgment call — fix the doc, or confirm the
mention deliberately describes past behaviour. The fix the message asks for is the smallest edit
that makes the doc true again (swap the value, rename the symbol, or delete the
sentence that now describes nothing), never an account of what changed.

Scope limits worth knowing:

- **Docs are `.md`/`.mdx`; "code" is everything else** except the prose formats
  `.txt`/`.rst`/`.adoc`/`.markdown`.
- **Anchored value drift is not proximity-checked** — it fires when a
  symbol-naming doc *also* contains the old literal anywhere in the file, so it
  can over-report if that number appears coincidentally.
- **`--range` evaluates against the current working tree**, so a dirty tree can
  change the verdict for the same range. Bare Stop-hook mode diffs base→worktree
  and is self-consistent.
- It only catches those two mechanical classes — a paraphrased value or a
  reversed decision with no anchored symbol needs a semantic doc sweep.

## `covers`, `index`, `stale`, `graph` — read-only views

Four subcommands that query the doc graph for a human or an agent tool. All are
**read-only**: never write, never gate (not `--skip`-able, not run by the
generated hook),
always exit `0` on success — `2` only on a usage/git error.

```bash
docgraph covers <path>               # docs that cover <path> (repo-root-relative)
docgraph index                       # generated markdown index of the doc graph
docgraph stale                       # docs whose verified date is past its threshold
docgraph stale --older-than 90       # override the default 180-day threshold
docgraph graph                       # both doc graphs as human-readable markdown
docgraph graph --json                # ...or as a stable JSON payload (machine seam)
```

- **`covers <path>`** — prints every tracked doc that documents `<path>` via a
  frontmatter `covers` edge, directly or by covering a parent directory
  (`covers: src/auth/` covers `src/auth/login.go`). `<path>` is
  **repo-root-relative**. Prints nothing (exit `0`) if no doc covers it. The same
  `covers` edges feed covers drift (`doc-drift` at Stop,
  [`covers-drift`](#covers-drift--the-advisory-pre-push-rider) at push), so
  declaring one both answers this query and gets the doc surfaced when its code
  changes.
- **`index`** — prints a **generated** markdown index: every doc with
  frontmatter, grouped by `type` (core types in canonical order, then custom
  types alphabetically), each as `- [label](path) — description` (the tail is
  omitted when a doc has no `description`). The label is the doc's `title` if
  set, else its **body H1**, else its path; set `title` only when the index label
  should differ from the H1. It's a view, not a hand-maintained
  page — redirect it into a tracked file (`docgraph index > docs/index.md`) and
  regenerate when the graph changes.
- **`stale [--older-than <days>]`** — prints every doc whose `verified` date is
  past its staleness threshold: `docs/old.md (verified 2026-01-01 — 195d old,
  threshold 180d)`. The threshold is `--older-than` (default **180**) unless the
  doc's own `review:` cadence (e.g. `review: 90d`) overrides it. A doc with no
  `verified` date, or an unparseable value, is silently skipped — malformed
  frontmatter is the `frontmatter` check's concern.
- **`graph [--json]`** — serves **both** doc graphs, exactly as the gate sees them. Default
  output is human-readable markdown: the `part-of` hierarchy, cross-reference
  edges, and both island lists (content-graph and metadata-graph). `--json` emits
  a stable, `schemaVersion`-stamped payload instead — the machine seam a catalog
  builder or an ecosystem-graph tool ingests. The JSON keys are
  camelCase throughout: `schemaVersion`, `repoRoot`, `nodes`, `contentEdges`,
  `metadataEdges`, `islands` (`{content, metadata}`). Edge/island slices are
  always present as arrays (empty, never `null`), so a consumer keys off their
  presence. `schemaVersion` is `1`; a breaking change to the shape bumps it rather
  than silently reshaping the payload. `--root`/`--ignore` apply as elsewhere.

  `graph` reads the working tree by default. Pass `--ref <ref>` (e.g. `--ref HEAD`,
  `--ref dev`) to build the graph from the **committed state at that ref**; this
  also works inside a **bare** repo (no checkout). On a clean checkout `--ref HEAD`
  matches the default output exactly.

## Install

**Guided (Claude Code):** `/docgraph:install` — installs the binary, offers to
wire the `doc-drift` Stop hook into `~/.claude/settings.json`, offers this repo's
pre-push gate, and seeds the leaks config.

**Manual:**

```bash
curl -fsSL https://raw.githubusercontent.com/lockyc/docgraph/main/install.sh | bash
```

Runs `go install` (from the current checkout if you're in one, else `@latest`),
seeds `~/.config/docgraph/`, and prints where the binary landed. Or directly:

```bash
go install github.com/lockyc/docgraph/v3@latest   # or, from a checkout: just install
```

Needs **Go** and **git** on PATH.

### As a pre-push gate

```bash
docgraph install-hook [path]                    # gate: enforce ALL checks (default)
docgraph install-hook --skip orphans            # auto-nav MkDocs repos (no orphan gate)
docgraph install-hook --ignore '**/*_test.go'   # bake an --ignore glob into the hook
docgraph install-hook --force                   # regenerate an existing hook
docgraph install-hook --no-footgun-drift        # omit the footgun-drift rider
docgraph install-hook --no-covers-drift         # omit the covers-drift rider
```

The generated hook runs the whole-state gate `docgraph .`, then the advisory
`docgraph footgun-drift` and `docgraph covers-drift`, each fed git's pre-push
stdin. Only the first can block a push; the riders never do, even on error.
`install-hook` writes a tracked `.githooks/pre-push`
and sets `core.hooksPath` for this clone (others activate with `git config
core.hooksPath .githooks`). It refuses to clobber an existing hook (pass
`--force`, or integrate a docgraph call into your own). It fails **closed** — a
missing `docgraph` blocks the push.

The generated hook finds docgraph via PATH **and** the Go bin dir (`$GOBIN` /
`$GOPATH/bin` / `~/go/bin`), so it works from GUI clients and sandboxed agents too.

## Usage

```bash
docgraph [path]                     # path defaults to '.'; enforces all checks
                                    # flags may appear before or after the path
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

**Exit codes.** `docgraph [path]`: `0` clean · `1` findings · `2` usage / not a
git repo / malformed leak config. `footgun-drift` and `covers-drift` are
advisory: `0` regardless of findings (nag on stdout), `2` only on a git/usage
error. `doc-drift` blocks: `0` clean (or loop-guard-silenced) · `2` on a
dangling-reference, anchored-value or covers finding (stderr), or on an error the
first time it is seen · `1` on the same error again (bare runs; a clean run re-arms
it — `--range` always exits `2` on an error) or a bad flag. `covers` /
`index` / `stale` / `graph` are read-only: `0` always on success, `2` only on error.

**Linked worktrees work**, with nothing to configure. A worktree whose pointer is
*dangling* (its main repo was moved or pruned, or is unreachable from a sandbox)
fails with `not a git repository`, followed by git's own explanation naming the
dangling target.

A clean run prints a single line (`docgraph: clean ✓ (N tracked .md, M reachable,
0 findings)`). A run with findings prints a banner, the sections that have
findings, and a footer explaining what docgraph is, why the push was aborted, and
how to remediate each category.

### Entry points (roots)

Reachability starts from whichever of `CLAUDE.md`, `README.md`, `AGENTS.md` (repo
root) and `docs/index.md` are tracked, plus any `--root` you add. This covers both
a whole doc repo and a project whose docs are `CLAUDE.md` + `docs/`.

### Ignoring paths

`**/superpowers/**` (an agent plugin's scratch files), `.claude/**`, and `.agents/**` are ignored by default for the
doc-graph checks and by `doc-drift`. Add more via `.docgraphignore` (one glob per
line, `#` comments) or repeatable `--ignore` globs. These are repo-root-relative globs, not
gitignore syntax: `*` and `?` match within one path segment, `**` matches any number of
segments, and there is no negation — exclude a directory with `dir/**`, and a file at any
depth with `**/name.md`. The leak scan honors only `--ignore`,
not the default/`.docgraphignore` layers — see [`leaks`](#leaks--the-content-scan).

**No inline markers.** Every suppression lives in config or on the command line —
`.docgraphignore`, `--ignore`, `--skip`, and the leaks config's `allow` /
`allow_regex`, and a `[[dir]]`'s `ignore` + `ignore_groups`. docgraph never reads a
suppression comment inside the audited files. `footgun-drift`, `covers-drift`
and `doc-drift` have no in-file escape at all — they're opted out only
whole-check, via `DOCGRAPH_FOOTGUN_OFF=1` /
`--no-footgun-drift`, `DOCGRAPH_COVERS_OFF=1` / `--no-covers-drift`, and
`DOC_DRIFT_OFF=1` respectively.

### Doc models and when to `--skip orphans`

The default is a **prose-linked, frontmattered** doc graph: entry docs link or
mention their way through `docs/` (satisfying `orphans`), and every doc carries a
`type:` block placed with a doc→doc edge (satisfying `frontmatter` and
`disconnected`). That fits most repos. Two exceptions:

- **Auto-nav MkDocs sites** — a `docs/` with no `nav:` block, where MkDocs
  builds the sidebar itself and pages never cross-link, so every page is a
  content-graph island by design, and such pages usually carry no `type:`
  frontmatter either. Gate with `--skip orphans` and, unless you'll add a `type:`
  block to every page, `--skip frontmatter` (which also moots `disconnected`,
  since a doc with no frontmatter isn't a metadata-graph node).
- Repos with genuinely unreferenced design docs report real orphans — link them
  from `CLAUDE.md`/`README`, or accept and `--skip orphans`.

**A content corpus — a cheatsheet section, a wiki's pages, a seed export,
verbatim clippings — asks one question: is it yours to conform?**

- **Hand-curated → conform it.** Give each page a `type:` (plus a `part-of` edge
  into an index doc so it isn't a metadata island) and the corpus one
  hand-maintained prose-link index page for content reachability. No
  `.docgraphignore`, no `--skip`. A foreign frontmatter vocabulary is no
  obstacle — unknown keys ride along untouched, so an Obsidian clipping keeps
  `created`/`source`/`author` and just gains `type: reference`. The corpus becomes
  a real graph node, and `broken`/`untracked` keep covering it.
- **Derived / never hand-edited → exclude it** with `.docgraphignore`, because a
  regen would drop any `type:` you added, so conforming can't stick.
- **`--skip` is wrong either way** — it's repo-wide, so skipping a check to suit
  a corpus also turns it off for your `CLAUDE.md`/`README.md`, where it still applies.

A flood of orphans or frontmatter findings is a reason to look, not to exclude:
each has a cheap fix (an index page; a `type:` line), and excluding buys a quiet
gate by never checking that content again.

`.docgraphignore` does **not** exempt a corpus from `leaks` — that scan is scoped
by git tracking, not the doc-graph ignore layers. To silence a corpus legitimately
full of the hosts, paths and identifiers your rules match, add a `[[dir]]` `ignore`
for it in the **leaks config**.

A repo that doesn't use the `Footgun:` convention opts out of `footgun-drift`
outright (it's not a check, so no `--skip` name) — `DOCGRAPH_FOOTGUN_OFF=1` or
`install-hook --no-footgun-drift`. `covers-drift` has the same pair
(`DOCGRAPH_COVERS_OFF=1` / `install-hook --no-covers-drift`), though a repo with
no `covers` edges has nothing to turn off.

## Usage logging

docgraph can append one JSON line per run to a local log, for usage and finding
trends over time across all your repos. It is **opt-in** and machine-local: off
unless a global `config.toml` enables it, so CI, fresh clones, and contributors
never log.

Enable it in `~/.config/docgraph/config.toml` (resolved `--config` →
`$DOCGRAPH_CONFIG` → `$XDG_CONFIG_HOME/docgraph/config.toml`):

```toml
[log]
enabled = true
level   = 1                                   # 1 counts · 2 +paths · 3 +findings
# path  = "~/.local/state/docgraph/usage.jsonl"   # optional; this is the default
```

| Key | Default | What it does |
|---|---|---|
| `[log].enabled` | `false` | Turn logging on |
| `[log].level` | `1` | Detail tier, 1–3 (below) |
| `[log].path` | `$XDG_STATE_HOME/docgraph/usage.jsonl` | Where records are appended |

Records land in `$XDG_STATE_HOME/docgraph/usage.jsonl` (default
`~/.local/state/docgraph/usage.jsonl`), overridable via `[log].path` or
`DOCGRAPH_LOG`. A level-1 record:

```json
{"ts":"2026-07-09T21:30:00+10:00","version":"3.0.0","repo":"/abs/git/root",
 "cmd":"run","checks":["broken","disconnected","edges","frontmatter","leaks","orphans","untracked"],
 "exit":1,
 "counts":{"broken":1,"disconnected":0,"edges":0,"frontmatter":0,"leaks":0,"orphans":0,"untracked":0}}
```

**Detail level** trades richness for exposure:

- **1 — counts only.** No paths, no content. Safe default.
- **2 — adds `files`.** Flagged paths (broken/leaks include `file:line`), but
  **never leak match text.**
- **3 — adds `findings`.** Full detail **including leak match strings** — this
  turns the log into exactly the sensitive-string sink `leaks` exists to prevent.
  Use only on a trusted machine.

Notes:

- **Absent config → silently off** (the normal state). A **malformed**
  `config.toml` warns and disables logging but **does not fail the run** (unlike a
  malformed `leaks.toml`, which is fatal).
- `DOCGRAPH_NO_LOG=1` disables logging for one run even when the config enables it.
- Best-effort: an unwritable log file is silently skipped and never changes the
  exit code.

## Known gaps

Anchor validity (`y.md#missing`), external-URL liveness, raw `<a href>` in HTML
blocks, per-section `index.md` implicit-nav, and repo-specific conventions are out
of scope. Markdown-link extraction (broken-link detection, link edges) skips
fenced/inline code so example paths aren't treated as real links; the orphan
*reachability* pass, by contrast, does read inline-code path mentions.
`footgun-drift`'s declaration scan has no code-fence awareness — an example
`Footgun:` line inside a ` ``` ` block reads as a real declaration.

## Development

```bash
just test    # go test ./...
just build   # go build -o docgraph .
just install # go install . -> ~/go/bin/docgraph
just gate    # gofmt check + vet + tests (pre-release gate)
```

Branch model, release process and layout: [`CLAUDE.md`](CLAUDE.md).
