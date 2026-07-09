# docaudit

[![Release](https://img.shields.io/github/v/release/Lockyc/docaudit?sort=semver&label=release)](https://github.com/Lockyc/docaudit/releases/latest)
![Built with Go](https://img.shields.io/badge/built%20with-Go-00ADD8?logo=go&logoColor=white)
[![License](https://img.shields.io/github/license/Lockyc/docaudit)](LICENSE)

Audits a repo's **agent-facing documentation graph** — the docs an AI agent
navigates by grep + following `[x](y.md)` links, not the rendered site a human
browses — **and** scans tracked file content for leaked owner-specific/secret
strings. `docaudit [path]` **enforces every one of these checks by default**
and exits non-zero on any finding, so it drops into a pre-push hook or CI
without a wrapper. You *exclude* a check explicitly (`--skip`); nothing is
opt-in, because an opt-in check enforces nothing.

Separately, `docaudit footgun-drift` is a **diff-scoped** subcommand: at
pre-push it checks only what a push *adds* — new "footgun" declarations in
tracked markdown that lack a rationale — never the existing corpus. It isn't
one of the checks below and isn't exclude-able with `--skip`; see
[`footgun-drift`](#footgun-drift--the-diff-scoped-pre-push-check).

**Scope: project *documents*, not project *content*** (for the doc-graph
checks). Those checks audit the docs that explain the project (`docs/`,
`CLAUDE.md`, config-dir READMEs), not content a site framework routes and renders
(an Astro/MkDocs content collection, a seed-data corpus) — exclude those per-repo
via `.docauditignore`. The `leaks` check is broader: it scans *all* tracked files
(see below).

## Checks

All four run by default when you run `docaudit [path]`. Exclude one with
`--skip <check[,check]>` (e.g. a nav-driven MkDocs repo runs `--skip orphans`).
A newly-added check is enforced everywhere automatically — there is no
run-list to update.

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

Unlike the four checks above, `footgun-drift` never scans the whole repo — only
what a push *adds*. It flags a footgun **declaration** (a line-leading
`Footgun:` marker or a bolded mid-line footgun lead — introducing one, not just
mentioning it; a cross-reference or a bare `## Footguns` container heading
never counts) whose *added lines* have no rationale signal nearby (docaudit
honors no inline suppression marker — see below). The rationale
vocabulary is a fixed internal word list (`footgunRationaleSignals` in
`internal/audit/footguns.go`) — not configurable, since it's a small set of
English connectives, not an owner-specific footprint like `leaks`.

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
file added in the same push is checked exactly like `CLAUDE.md`, because an
unjustified footgun there is just as undocumented.

Exit codes match the other checks: `1` on a finding, `0` clean, `2` on a git/
usage error. `DOCAUDIT_FOOTGUN_OFF=1` disables it outright (for a repo that
doesn't use the footgun-with-rationale convention); `docaudit install-hook
--no-footgun-drift` generates a hook that never invokes it.

It is a **heuristic, not a judge**: it catches the bare-assertion case (a
footgun declaration added with no "why" anywhere nearby) but cannot rank
whether a given rationale is actually *good* — that's a judgment call docaudit,
being deterministic, doesn't make. On a finding, the printed message asks two
questions: (1) is this a real footgun — a trap you hit, a tempting-but-wrong
approach, a re-litigated decision? (2) is it at the right doc level — an
invariant belongs in `CLAUDE.md`, in-depth rationale in `docs/`, human-facing
prose in `README`? If yes to both, state the "why" inline; if not, reword it
as a plain note or move it. (There is no suppression marker — see "No inline
markers" below; a footgun is silenced only by a nearby rationale.)

## Install

```bash
go install github.com/lockyc/docaudit@latest   # or: just install
```

Depends only on `github.com/BurntSushi/toml` (config decode) plus the Go stdlib. Requires `git` on PATH.

## Usage

```bash
docaudit [path]                     # path defaults to the current directory; enforces all checks
docaudit --root wiki/Home.md        # add an extra entry point (repeatable)
docaudit --ignore 'vendor/**'       # exclude a glob from checks (repeatable)
docaudit --skip orphans             # exclude a check (comma-separated; e.g. nav-driven MkDocs)
docaudit --skip leaks               # exclude the content leak scan
docaudit --leaks-config <path>      # override the global leak rules file
docaudit --config <path>            # override the global config.toml (usage logging)
docaudit footgun-drift              # diff-scoped: reads pre-push ref lines from stdin
docaudit footgun-drift --range base..head  # diff-scoped: explicit range
docaudit version                    # print version (also --version, -v)
```

Exit codes (`docaudit [path]` and `footgun-drift` alike): `0` clean · `1`
findings in an enforced check · `2` usage / not a git repo / malformed leak
config.

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

The generated hook runs two checks: the whole-state `docaudit .` (a bare
invocation, so a check added in a later version is enforced without
regenerating the hook), then the diff-scoped `docaudit footgun-drift`, fed
git's pre-push stdin so it can scope itself to only the commit range being
pushed. Pass `--no-footgun-drift` to omit the second check. It writes a tracked
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

Repos that don't use the footgun-with-rationale convention at all skip
`footgun-drift` outright, since there's no rationale-labeling scheme to check
against — it isn't a `docaudit [path]` check, so there's no `--skip` name for
it; use `DOCAUDIT_FOOTGUN_OFF=1` or `install-hook --no-footgun-drift` instead
(see [`footgun-drift`](#footgun-drift--the-diff-scoped-pre-push-check)).

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

**No inline markers.** Every suppression lives in config or on the command line —
`.docauditignore`, `--ignore`, `--skip`, and the leaks config's `allow`/`allow_regex`
and `[[dir]]` sections. docaudit **never** reads a suppression comment or pragma inside
the audited files (no `<!-- docaudit-ignore -->`, no `# docaudit:allow`, no `# nosec`
equivalent, and no `<!-- footgun-ok -->` for `footgun-drift`). Such a marker would be
silently ignored, not honored — so an unwanted finding is silenced by tuning the
config/flags, or (for `footgun-drift`) by stating the rationale, never by annotating
the file.

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
`Audit` orchestrator, the leak scanner, and the diff-scoped `FootgunDrift`
orchestrator built on the declaration scanner). See `CLAUDE.md` for design
invariants.
