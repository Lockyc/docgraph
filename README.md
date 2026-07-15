# docaudit

[![Release](https://img.shields.io/github/v/release/Lockyc/docaudit?sort=semver&label=release)](https://github.com/Lockyc/docaudit/releases/latest)
[![CI](https://github.com/Lockyc/docaudit/actions/workflows/ci.yml/badge.svg)](https://github.com/Lockyc/docaudit/actions/workflows/ci.yml)
![Built with Go](https://img.shields.io/badge/built%20with-Go-00ADD8?logo=go&logoColor=white)
[![License](https://img.shields.io/github/license/Lockyc/docaudit)](LICENSE)

Audits a repo's **agent-facing documentation graph** — the docs an AI agent
navigates by grep + following `[x](y.md)` links, not the rendered site a human
browses — **and** scans tracked file content for leaked owner-specific/secret
strings. `docaudit [path]` **enforces every one of these checks by default**
and exits non-zero on any finding, so it drops into a pre-push hook or CI
without a wrapper. You *exclude* a check explicitly (`--skip`); nothing is
opt-in, because an opt-in check enforces nothing.

Separately, `docaudit footgun-drift` is a **diff-scoped, advisory** subcommand:
at pre-push it flags every new "footgun" declaration a push *adds* to tracked
markdown — never the existing corpus — and prints a nag to go verify each is a
real footgun, but **exits 0 and never blocks the push**. It isn't one of the
checks below and isn't exclude-able with `--skip`; see
[`footgun-drift`](#footgun-drift--the-diff-scoped-pre-push-check).

`docaudit doc-drift` is a third mode: a **diff-scoped, blocking** subcommand
meant to run as a **Stop hook** (invoked directly, no wrapper) at the end of an
agent turn. It scans the branch's working-tree-inclusive diff for code that
drifted away from what a tracked doc still says, and **exits 2** (findings on
stderr) to block the turn from ending on stale docs; see
[`doc-drift`](#docaudit-doc-drift).

**Scope: project *documents*, not project *content*** (for the doc-graph
checks). Those checks audit the docs that explain the project (`docs/`,
`CLAUDE.md`, config-dir READMEs), not content a site framework routes and renders
(an Astro/MkDocs content collection, a seed-data corpus) — exclude those per-repo
via `.docauditignore`. The `leaks` check is broader: it scans *all* tracked files
(see below).

## Checks

All six run by default when you run `docaudit [path]`. Exclude one with
`--skip <check[,check]>` (e.g. a nav-driven MkDocs repo runs `--skip orphans`).
A newly-added check is enforced everywhere automatically — there is no
run-list to update.

1. **Orphans** — a tracked doc not reachable from the entry points.
   Reachability follows markdown links, bare/inline-code path mentions
   (`` `docs/x.md` ``), *and* frontmatter typed edges (see `edges` below) —
   because an agent follows any of the three. A doc reached only via a typed
   edge (e.g. a `part-of` pointing at it) is not an orphan. Every real `.md` is
   audited — including docs outside `docs/` (e.g. a config-dir README); only
   agent tooling under `.claude/` and `.agents/` plus untracked scratch are excluded, as
   those aren't documentation.
2. **Broken links** — a `[x](y.md)` whose target doesn't exist (renamed/moved/
   deleted, link not updated). Checked across all tracked `.md`.
3. **Untracked** — a `.md` on disk but not in git (a forgotten `git add`) —
   absent from clones, the built site, and any mirror.
4. **Leaks** — tracked file *content* matching a configured leak pattern (see
   below). Scans file content rather than the doc graph.
5. **Frontmatter** — a doc's leading YAML frontmatter block (first line
   exactly `---` to the next `---`), if present, must be well-formed YAML and
   carry a `type` field; a doc with no frontmatter block at all is fine.
   Malformed YAML and a block missing `type` are each a finding. `type` is an
   advisory vocabulary, not a closed enum — see
   [`docaudit schema`](#docaudit-schema--the-frontmatter-vocabulary) below.
6. **Edges** — a frontmatter `links:` list of typed edges (`rel`/`to`/`note`).
   Every internal `to` target — a repo-root-relative `.md` doc or code path —
   must exist on disk, and `part-of`/`supersedes` edges between tracked docs
   must not form a cycle. External URLs and `owner/repo:...` cross-repo
   targets are never checked (unverifiable and deferred, respectively).

### `docaudit schema` — the frontmatter vocabulary

```bash
docaudit schema    # prints the JSON Schema to stdout
```

Emits the [JSON Schema](https://json-schema.org/) (draft 2020-12) describing
valid doc frontmatter — the `type`/`verified`/`review`/`links` shape the
`frontmatter` and `edges` checks enforce, plus the advisory `type`/`rel`
vocabularies (as `x-docgraph-core-types`/`x-docgraph-core-rels`), so another
tool (an editor, a linter, a catalog builder) can validate or generate
frontmatter against the same rules docaudit uses, instead of re-encoding the
vocabulary by hand. It's **read-only** — it never reads the repo it's run in
and is never part of the gate (not in `checkNames`, not `--skip`-able,
nothing to enable or disable).

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

Patterns come from a **global TOML file** — never committed to a repo, because a
per-repo deny (or allow) list would itself enumerate your sensitive terms.
Resolution order: `--leaks-config <path>` → `$DOCAUDIT_LEAKS` →
`$XDG_CONFIG_HOME/docaudit/leaks.toml` (default `~/.config/docaudit/leaks.toml`).

Top-level `terms` match literally and case-insensitively; `regex`/`allow_regex`
are Go regexps, **also case-insensitive by default** (a leak must be caught in any
casing — opt out per-pattern with `(?-i)`). `allow`/`allow_regex` suppress a deny
match they cover. `[[dir]]` sections scope exceptions to files under an absolute
`path` (a whole repo or a subdir; a leading `~/` is expanded, and a non-absolute
`path` is a fatal config error rather than a silent no-op): `ignore` globs
(relative to `path`) drop files from the scan, and `allow`/`allow_regex` suppress
terms within that subtree.

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

**The config is the sole source of rules** — there are no hidden built-in patterns.
Generic secret shapes (PEM headers, AWS `AKIA…`, GitHub `ghp_…`, Slack `xox…`) are
just `regex` entries you add; use a leading `(?-i)` to keep them case-sensitive:

    regex = [
      '(?-i)-----BEGIN [A-Z ]*PRIVATE KEY-----',
      '(?-i)AKIA[0-9A-Z]{16}',
      '(?-i)ghp_[A-Za-z0-9]{36}',
      '(?-i)xox[baprs]-[A-Za-z0-9-]{10,}',
    ]

The config is global (machine-local), so in CI / a fresh clone (no file) there are
no rules and the scan is a no-op there (it stays a local pre-push gate).

**Config handling** (leaks runs by default, incl. in CI, which has no global file):
- **No config file** → the leaks check has no rules, so it scans nothing and
  prints a warning; **not** fatal (a hard-fail would brick every CI push). Define
  your global file (or pass `--leaks-config`) to give the check its rules — they
  take effect wherever that file exists (your local pre-push).
- **Malformed config** (bad TOML, or a bad regex in `regex`/`allow_regex`) → exit 2 (fail-closed). A broken config is a
  real bug, not the common "not set up yet" case, so it fails loudly rather than
  silently degrading.

**Known gaps:** no git-history *detection* (finding leaks already committed to
history is the owner's call — use the manual leak-audit skill); *scrubbing* a known
leak from history is supported via `docaudit leaks-rules` below. No per-rule
messages.

### `docaudit leaks-rules` — export rules for history scrubbing

The `leaks` check scans the current tracked tree. To scrub a leak that already
landed in **git history**, export the leak vocabulary as a
[`git-filter-repo`](https://github.com/newren/git-filter-repo) rules file and run the
rewrite separately:

```sh
docaudit leaks-rules > rules.txt          # non-destructive: reads only the config
git filter-repo --replace-text rules.txt  # destructive: rewrites history
```

`leaks-rules` reads the same global config as the `leaks` check and emits one
`regex:` line per deny rule (terms are escaped and case-insensitive; `regex`
entries stay case-insensitive unless they carry a leading `(?-i)`, which is
normalized to a plain case-sensitive pattern — see the caveat below), using
filter-repo's default `***REMOVED***` replacement. Emitted patterns target
git-filter-repo's Python regex engine: a leading `(?-i)` opt-out is normalized to a
case-sensitive rule, but other Go/RE2-only regex syntax may need manual review
before you run the rewrite. stdout is rules only; a stderr summary reports
any `allow` / `allow_regex` / `[[dir]]` rules it **dropped** — filter-repo rewrites
by content across all paths and history, so those exceptions cannot apply and the
rewrite is broader than the audit's scope. Review the result.

> The rewrite changes commit SHAs: force-push and have every collaborator re-clone.
> docaudit itself never reads or rewrites history — it only exports the rules.

### `footgun-drift` — the diff-scoped pre-push check

Unlike the six checks above, `footgun-drift` never scans the whole repo — only
what a push *adds* — and it is **advisory**: it prints a nag but exits 0 and
never blocks the push. It flags a footgun **declaration** (a line-leading
`Footgun:` marker or a bolded mid-line footgun lead — introducing one, not just
mentioning it; a cross-reference or a bare `## Footguns` container heading
never counts) on any *added line*. It makes **no** attempt to detect a rationale
and has no in-file escape: every added declaration is reported, and you go
double-check it yourself.

```bash
docaudit footgun-drift                       # reads git's pre-push ref lines from stdin
docaudit footgun-drift --range base..head    # explicit range, for manual use
```

With no `--range`, it reads the ref lines git feeds a `pre-push` hook on stdin
(`<localref> <localsha> <remoteref> <remotesha>`) and derives `remotesha..localsha`
per ref — a new branch (zero remote sha) falls back to the merge-base with the
nearest integration branch. A declaration counts only if its line is in the
*added*-line set for that range (via `git diff`), read at the *head* revision —
a declaration already on the remote, even if the file around it also changed,
is never re-flagged. File scope is every `.md` the diff touches, **not** the
doc-graph ignore layers or the `leaks` git-tracking scope — a `.claude/` skill
file added in the same push is checked exactly like `CLAUDE.md`, because a
footgun there is just as undocumented.

Because it's advisory, its exit code is always `0` on findings (only `2` on a
git/usage error), so it can ride in the pre-push hook without ever aborting a
push. `DOCAUDIT_FOOTGUN_OFF=1` silences it outright (for a repo that doesn't use
the `Footgun:` note convention); `docaudit install-hook --no-footgun-drift`
generates a hook that never invokes it.

It is a **nag, not a judge**: it reports every added declaration precisely
because it *cannot* rank whether a stated "why" is actually good — that's a
judgment call docaudit, being deterministic, doesn't make, so it doesn't pretend
to. On a finding, the printed message asks two questions: (1) is this a real
footgun — a trap you hit, a tempting-but-wrong approach, a re-litigated
decision? (2) is it at the right doc level — an invariant belongs in
`CLAUDE.md`, in-depth rationale in `docs/`, human-facing prose in `README`? If
it's a real footgun, leave it (ideally with its "why"); if it's a
note-just-in-case, reword it as a plain note or remove it — a follow-up commit is
fine, since nothing was blocked. (There is no suppression marker — see "No
inline markers" below.)

### `docaudit doc-drift`

A **Stop-hook** doc-staleness gate: wire it into your agent harness's `Stop`
hook (invoked directly — `docaudit doc-drift`, no wrapper script) so it runs at
the end of every turn and **blocks** the turn from ending while a tracked doc
still describes code that just changed underneath it.

It scans a **working-tree-inclusive** range — base→worktree, covering both
committed and uncommitted changes — because it fires before a commit
necessarily exists to diff against. On a trunk branch (no integration branch
ahead of it) the base is `HEAD` itself, so the range is uncommitted-only; on a
feature branch it's the closest integration branch's merge-base, so the range
covers everything done on the branch so far. Contrast `footgun-drift`, whose
range is always a **committed** `base..head`.

It flags two mechanical staleness classes:

1. **Dangling reference** — a symbol whose *definition* was removed in the
   diff and doesn't survive anywhere else in tracked code, but a tracked doc
   still names it.
2. **Anchored value drift** — a constant whose numeric *value* changed in the
   diff while a tracked doc still names the constant **and** still shows the
   old literal.

Scope and known limits:

- **Docs are `.md`/`.mdx`; the "code" side is everything else** except the prose
  formats `.txt`/`.rst`/`.adoc`/`.markdown`, which are excluded from the code
  scan so def-shaped prose (a `class …`/`type …` sentence in a `CHANGELOG.txt`)
  isn't read as a removed definition. Put prose in one of those formats, not a
  bespoke extension the code scan would parse.
- **Anchored value drift is not proximity-checked** — it fires when a
  symbol-naming doc *also* contains the old literal anywhere in the file, so it
  can over-report (and cite an unrelated line) if that number appears
  coincidentally in the same doc.
- **`--range` evaluates references against the current working tree.** The
  still-defined check and the doc grep run against the working tree, not the
  named committed head, so a dirty tree changes the verdict for the same range.
  Bare Stop-hook mode diffs base→worktree and is self-consistent; `--range` is a
  manual convenience.

```bash
docaudit doc-drift                        # bare: resolves the diff base itself, applies the loop-guard
docaudit doc-drift --range base..head     # explicit range, for manual use — bypasses the loop-guard
```

Bare invocation applies a **once-per-HEAD loop-guard**: after it nags for a
given `HEAD`, a repeat bare invocation at the same `HEAD` is silent, so an
agent that keeps ending its turn without acting on the finding isn't nagged on
every single Stop. This is a de-dupe on the *nag*, not a suppressor of the
*finding* — the next commit moves `HEAD` and re-arms it. `--range` bypasses the
guard entirely (deterministic, for manual runs).

Unlike every other check in this tool, a `doc-drift` finding **blocks**: it
prints to **stderr** and exits **2**, versus `footgun-drift`'s advisory exit
`0`. `DOC_DRIFT_OFF=1` disables it outright — for a repo that doesn't use the
anchored-symbol-and-value convention it relies on (a doc naming a code symbol,
and for value drift, also showing the literal it's currently set to).

It has **no suppression surface** beyond that whole-check kill switch: no
`.docauditignore`, no per-finding CLI flag, no inline marker. A flagged
reference is a situation-based judgment call — reconcile the doc, or confirm
it's intentional framed history and move on.

It only catches the two mechanical classes above — a paraphrased value or a
reversed decision with no anchored symbol is out of scope; run a semantic doc
sweep for those.

### `covers`, `index`, `stale` — read-only doc-graph views

Three subcommands that query the doc graph for a human or an agent tool, built
on the same `RepoDocs` parse the six checks use. All three are **read-only**:
they never write to the repo, are never part of the gate (not in `checkNames`,
not `--skip`-able, not run by the generated pre-push hook), and always exit
`0` on success regardless of findings — `2` only on a usage or git error.

```bash
docaudit covers <path>               # docs that cover <path> (repo-root-relative)
docaudit index                       # generated markdown index of the doc graph
docaudit stale                       # docs whose verified date is past its threshold
docaudit stale --older-than 90       # override the default 180-day threshold
```

- **`covers <path>`** — prints, one per line, every tracked doc that documents
  `<path>` via a frontmatter `covers` edge, either directly or by covering a
  parent directory (`covers: src/auth/` covers `src/auth/login.go`). `<path>`
  is **repo-root-relative** — frontmatter edges resolve against the repo root,
  unlike an inline markdown link, which resolves relative to the doc it's
  in. Answers "which doc governs this file" for an agent about to touch it.
  Prints nothing (still exit `0`) if no doc covers the path.
- **`index`** — prints a **generated** markdown index of the doc graph to
  stdout: every doc that carries frontmatter, grouped by `type` (core types in
  their canonical order, then custom types alphabetically), each listed as
  `- [title](path) — description` (the ` — description` tail is omitted when a doc has no `description` field). It's a view, not a hand-maintained page —
  redirect it into a tracked file (`docaudit index > docs/index.md`) rather
  than editing the output by hand, and regenerate after the doc graph changes.
- **`stale [--older-than <days>]`** — prints every doc whose `verified` date is
  older than its staleness threshold, one per line:
  `docs/old.md (verified 2026-01-01 — 195d old, threshold 180d)`. The
  threshold is `--older-than` (default **180** days) unless the doc's own
  `review:` cadence (e.g. `review: 90d`, `review: 2w`) overrides it. A doc with
  no `verified` date, or an unparseable `verified`/`review` value, is silently
  skipped — not flagged; malformed frontmatter is the `frontmatter` check's
  concern, not this view's.

## Install

**Guided (Claude Code):** run `/docaudit:install` — it installs the binary, offers to wire
the `doc-drift` Stop hook into `~/.claude/settings.json`, offers this repo's pre-push gate,
and seeds the leaks config.

**Manual:**

```bash
curl -fsSL https://raw.githubusercontent.com/lockyc/docaudit/main/install.sh | bash
```

This runs `go install` (from the current checkout if you're in one, else `@latest`), seeds
`~/.config/docaudit/`, and prints where the binary landed. Or install directly:

```bash
go install github.com/lockyc/docaudit@latest   # or, from a checkout: just install
```

Needs **Go** (the install is `go install`) and **git** on PATH (docaudit shells out to it at
runtime). The only module dependency is `github.com/BurntSushi/toml` (config decode); the rest
is the Go stdlib.

## Usage

```bash
docaudit [path]                     # path defaults to the current directory; enforces all checks
docaudit --root wiki/Home.md        # add an extra entry point (repeatable)
docaudit --ignore 'vendor/**'       # exclude a glob from checks (repeatable)
docaudit --skip orphans             # exclude a check (comma-separated; e.g. nav-driven MkDocs)
docaudit --skip leaks               # exclude the content leak scan
docaudit --skip frontmatter         # exclude the frontmatter well-formedness check
docaudit --skip edges               # exclude the typed-edge integrity check
docaudit --leaks-config <path>      # override the global leak rules file
docaudit --config <path>            # override the global config.toml (usage logging)
docaudit footgun-drift              # diff-scoped: reads pre-push ref lines from stdin
docaudit footgun-drift --range base..head  # diff-scoped: explicit range
docaudit doc-drift                  # Stop-hook: working-tree-inclusive diff, once-per-HEAD loop-guard
docaudit doc-drift --range base..head  # Stop-hook: explicit range, bypasses the loop-guard
docaudit schema                     # print the JSON Schema for the frontmatter vocabulary (read-only)
docaudit covers <path>              # read-only: docs that cover <path> (repo-root-relative)
docaudit index                      # read-only: generated markdown index of the doc graph
docaudit stale                      # read-only: docs whose verified date is past its threshold
docaudit stale --older-than 90      # read-only: override the default 180-day threshold
docaudit version                    # print version (also --version, -v)
```

Exit codes for `docaudit [path]`: `0` clean · `1` findings in an enforced
check · `2` usage / not a git repo / malformed leak config. `footgun-drift` is
advisory — `0` whether or not it prints findings, `2` only on a git/usage error.
`doc-drift` **blocks**: `0` clean (or silenced by the loop-guard) · `2` on a
finding (printed to stderr) or a git/usage error. `covers`/`index`/`stale` are
**read-only views**, not checks: `0` always on success, whatever they print —
`2` only on a usage or git error.

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
docaudit install-hook --ignore '**/*_test.go'  # bake an --ignore glob into the gated hook
docaudit install-hook --force               # regenerate an existing hook (e.g. after upgrading)
docaudit install-hook --no-footgun-drift    # omit the diff-scoped footgun-drift check
```

The generated hook runs the whole-state gate `docaudit .` (a bare invocation, so
a check added in a later version is enforced without regenerating the hook), then
the diff-scoped `docaudit footgun-drift`, fed git's pre-push stdin so it can scope
itself to only the commit range being pushed. Only the first can block a push —
`footgun-drift` is advisory (`|| true` in the hook, and it exits 0 on findings),
so it only ever prints its nag. Pass `--no-footgun-drift` to omit it. It writes a tracked
`.githooks/pre-push` and sets `core.hooksPath -> .githooks` for this clone
(other clones activate it with `git config core.hooksPath .githooks`). Refuses
to clobber an existing `.githooks/pre-push` (pass `--force`, or integrate into
it — e.g. call docaudit from an existing `make lint`). Fails **closed**: a
missing `docaudit` blocks the push, because a gate that skips when its tool is
absent is a false green, not a gate.

### Doc models and when to `--skip orphans`

The orphan check assumes a **prose-linked** doc graph (entry docs link/mention
their way through `docs/`). That fits most repos. Two exceptions:

- **Nav-driven MkDocs sites** (a `docs/` with no `nav:` block — MkDocs
  auto-builds the sidebar from the file tree, pages never cross-link). Every
  page is a prose-orphan by design. Gate these with `--skip orphans`.
- Repos with genuinely unreferenced design docs will report real orphans — link
  them from `CLAUDE.md`/`README`, or accept and exclude with `--skip orphans`.

Repos that don't use the `Footgun:` note convention at all opt out of
`footgun-drift` outright — it isn't a `docaudit [path]` check, so there's no
`--skip` name for it; use `DOCAUDIT_FOOTGUN_OFF=1` or `install-hook
--no-footgun-drift` instead
(see [`footgun-drift`](#footgun-drift--the-diff-scoped-pre-push-check)).

### Entry points (roots)

Reachability starts from whichever of `CLAUDE.md`, `README.md`, `AGENTS.md`
(repo root) and `docs/index.md` are tracked, plus any `--root` you add. This
covers both a whole doc repo and a project whose docs are `CLAUDE.md` + `docs/`.

### Ignoring paths

`**/superpowers/**`, `.claude/**`, and `.agents/**` are ignored by default for
the doc-graph checks (intentionally-untracked scratch, and agent skill/config
tooling that is never part of the doc graph). Add more via a `.docauditignore` file
(gitignore syntax, `#` comments) or repeatable `--ignore` globs. Globs support
`**` (any number of path segments), `*`, `?`. Note the leak scan honors only
`--ignore` (not the default/`.docauditignore` layers) — see the leaks section.

**No inline markers.** Every suppression lives in config or on the command line —
`.docauditignore`, `--ignore`, `--skip`, and the leaks config's `allow`/`allow_regex`
and `[[dir]]` sections. docaudit **never** reads a suppression comment or pragma inside
the audited files; such a marker would be silently ignored, not honored — so an
unwanted doc-graph/leak finding is silenced by tuning the config/flags, never by
annotating the file. `footgun-drift` has no in-file escape at all: it's advisory,
flags every added declaration, and is opted out only whole-check via
`DOCAUDIT_FOOTGUN_OFF=1` / `--no-footgun-drift`. `doc-drift` likewise has no in-file
escape and no per-finding CLI flag: the sole opt-out is the whole-check
`DOC_DRIFT_OFF=1`; a flagged reference is otherwise a situation-based judgment call,
de-duped as a *nag* (not silenced as a finding) by its once-per-HEAD loop-guard.

## Usage logging

docaudit can append one JSON line per run to a local log, so you can see usage and
finding trends over time (across all your repos). It is **opt-in** and machine-local:
off unless a global `config.toml` enables it, so CI, fresh clones, and contributors
never log.

Enable it in `~/.config/docaudit/config.toml` (resolved `--config` →
`$DOCAUDIT_CONFIG` → `$XDG_CONFIG_HOME/docaudit/config.toml`):

```toml
[log]
enabled = true
level   = 1                                   # 1 counts · 2 +paths · 3 +findings
# path  = "~/.local/state/docaudit/usage.jsonl"   # optional; this is the default
```

Records land in `$XDG_STATE_HOME/docaudit/usage.jsonl` (default
`~/.local/state/docaudit/usage.jsonl`), overridable via `[log].path` or the
`DOCAUDIT_LOG` env var. A level-1 record:

```json
{"ts":"2026-07-09T21:30:00+10:00","version":"2.0.0","repo":"/abs/git/root",
 "cmd":"run","checks":["broken","leaks","orphans","untracked"],"exit":1,
 "counts":{"orphans":0,"broken":1,"untracked":0,"leaks":0}}
```

**Detail level** trades richness for exposure:

- **1 — counts only.** No paths, no content. Safe default.
- **2 — adds `files`.** The flagged paths (broken/leaks include `file:line`), but
  **never leak match text.**
- **3 — adds `findings`.** Full detail **including leak match strings.** This turns
  the log into exactly the sensitive-string sink the `leaks` check exists to
  prevent — a deliberate opt-in; use it only on a trusted machine.

Notes:

- **Absent config → silently off** (no warning — an unconfigured log is the normal
  state). A **malformed** `config.toml` prints a warning and disables logging but
  **does not fail the run** — logging is auxiliary, so a log-config typo must never
  block a push (unlike a malformed `leaks.toml`, which is fatal).
- `DOCAUDIT_NO_LOG=1` disables logging for one run even when the config enables it.
- Logging is best-effort: an unwritable log file is silently skipped and never
  changes the exit code.

## Known gaps

Anchor validity (`y.md#missing`), external-URL liveness, raw `<a href>` in HTML
blocks, per-section `index.md` implicit-nav, and repo-specific conventions are
out of scope. Markdown-link extraction (used for broken-link detection and link
edges) skips fenced/inline code so example paths aren't treated as real links;
the orphan *reachability* pass, by contrast, does read inline-code path mentions
(that's how it follows `` `docs/x.md` ``). `footgun-drift`'s declaration scan
has no code-fence awareness at all — an example `Footgun:` line inside a
` ``` ` block reads as a real declaration.

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

**Install with `just install`** (or `go install .`) → `~/go/bin/docaudit`. The binary
is not rebuilt automatically, so reinstall after changing the CLI. Consumers track
releases via `go install …@latest`.

The installed binary must be reachable when a hook fires: the pre-push hook
`install-hook` generates resolves docaudit via PATH **and** the Go bin dir
(`$GOBIN` / `$GOPATH/bin` / `~/go/bin`), because git runs hooks with the caller's
PATH — GUI clients and sandboxed agents often push with a bare PATH that omits
`~/go/bin`, and a `command -v`-only hook would then fail-closed for the wrong
reason (tool present but unseen).

Layout: `main.go` is a thin CLI (flags → audit → report → exit code), because
keeping flag-parsing and reporting separate from the audit logic lets the
checks be tested without a CLI in the loop; `internal/audit/` holds that logic
(link parsing, glob-ignore, git wrappers plus diff helpers, the whole-state
`Audit` orchestrator, the leak scanner, the diff-scoped `FootgunDrift`
orchestrator built on the declaration scanner, and the diff-scoped `DocDrift`
orchestrator that diffs code and greps docs for the two staleness classes). See
`CLAUDE.md` for design invariants.
