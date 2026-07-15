# docgraph — notes for the next agent

A Go CLI (stdlib + `github.com/BurntSushi/toml` for config decode; shells out to
`git`) with three independent audit modes, each with its own trigger, plus
read-only subcommands (`schema`, and the doc-graph views `covers`/`index`/
`stale`):

- **`docgraph .`** — six **whole-state** checks against the current tree:
  orphans (tracked `.md` unreachable from the roots), broken internal `.md`
  links, untracked `.md`, a `leaks` content scan, `frontmatter` (a doc's
  leading YAML frontmatter block, if present, must parse and carry a `type`),
  and `edges` (a frontmatter `links:` list's internal targets must exist, and
  `part-of`/`supersedes` edges must not cycle — see "Frontmatter model"
  below). All six are **enforced by default**; exclude one with `--skip` (no
  opt-in — an opt-in check enforces nothing). A finding exits non-zero —
  that's the pre-push gate.
- **`docgraph footgun-drift`** — a **diff-scoped, advisory** pre-push subcommand:
  flags *every* footgun declaration added in the pushed range (no rationale
  judgment, never re-scans the existing corpus) and **exits 0** — a nag to
  double-check the declaration, never a block.
- **`docgraph doc-drift`** — a **Stop-hook, blocking** subcommand: scans the
  branch's working-tree-inclusive diff (base→worktree, committed + uncommitted)
  for two mechanical staleness classes — a **dangling reference** (a symbol whose
  definition was removed but a tracked doc still names it) and **anchored value
  drift** (a constant whose numeric value changed while a doc still names the
  symbol and shows the old literal) — and **exits 2** to block the Stop.
- **`docgraph schema`** — read-only, no repo state read at all: emits the JSON
  Schema (draft 2020-12) describing the frontmatter vocabulary that
  `frontmatter`/`edges` enforce, so another consumer (an editor, a catalog
  builder) conforms to it instead of re-encoding it. Never part of the gate.
- **`docgraph covers`/`index`/`stale`** — read-only doc-graph **views**: they
  read the graph via `audit.RepoDocs` (the same `parseDocs` path the six
  whole-state checks use, malformed docs simply omitted) and never gate —
  not `checkNames` entries, not `--skip`-able, never invoked by the generated
  pre-push hook, always exit `0` on success regardless of what they print.
  `covers <path>` answers "which doc governs this file" (a `covers`
  frontmatter edge, direct or parent-directory); `stale` reads the
  `verified`/`review` freshness fields the frontmatter model has carried since
  the `schema` vocabulary but that no check has read until now; `index` is a
  **generated** view (`IndexMarkdown`), not a hand-maintained page — redirect
  it into a tracked file rather than editing the output.

`frontmatter` and `edges` are ordinary `checkNames` entries — whole-state,
`--skip`-able exactly like orphans/broken/untracked/leaks. `footgun-drift` and
`doc-drift` are **not** `checkNames` — each is its own subcommand with its own
trigger (a git range / a Stop invocation), not a `docgraph .` check. `schema`
and the `covers`/`index`/`stale` views are a third kind again: not a check and
not diff-scoped, no trigger of their own — `schema` emits a fixed vocabulary,
the views query the current tree read-only. There's also an **opt-in usage
log** (see the logging footgun). Human-facing usage lives in `README.md`; this
file carries the invariants and footguns.

## Intended use

docgraph is built to run as a **pre-push documentation gate** (and in CI): the
six whole-state checks exit non-zero on a finding so a broken doc-graph blocks
the push without a wrapper. `docgraph install-hook` writes a tracked
`.githooks/pre-push` for that; the generated hook also runs `footgun-drift` as an
**advisory** rider (it prints its nag but never blocks — see its footgun below).
Separately, `docgraph doc-drift` is meant to be wired as a **Stop hook** by the
agent harness (e.g. a Claude Code `Stop` hook entry that runs `docgraph
doc-drift`): it fires at the end of a turn, not at push time, so a dangling
reference or stale anchored value is caught and blocked before the agent hands
control back — see [`doc-drift`](README.md#docgraph-doc-drift) in `README.md`.

## What it is (and is not)

- **Agent-facing, not human-facing.** It measures the graph an agent traverses
  (grep + `[x](y.md)`), *not* whether a human can reach a page.
- **Reads doc-graph structure, frontmatter, and, for `leaks`, arbitrary file
  content — all as whole-state.** `orphans`/`broken`/`untracked`/`edges`
  traverse the link graph (`edges` additionally checks internal-target
  existence and `part-of`/`supersedes` cycles among frontmatter edges);
  `frontmatter` parses each doc's leading YAML block for well-formedness;
  `leaks` scans tracked file *content* (code included) for configured leak
  patterns. All read the current tree, never git history.
- **`footgun-drift` and `doc-drift` read a *diff*, not repo state.**
  `footgun-drift` scans added markdown lines (like `leaks`, but diff-scoped);
  `doc-drift` diffs tracked **code** (removed definitions, changed constants) and
  greps the *docs* for stale references to what changed — the one check that
  compares two different file types. The six `docgraph .` checks have no range
  concept; the intro maps each mode's trigger and range.

## Frontmatter model

A doc's optional leading YAML block (`SplitFrontmatter`: present only when the
file's first line is exactly `---`, ending at the next line that's exactly
`---`) is the agent-facing metadata layer `frontmatter`/`edges` and `docgraph
schema` all key off:

- **`type`** — required whenever a block is present (missing it is a
  `frontmatter` finding; malformed YAML is a separate `frontmatter` finding
  regardless of `type`). Core vocabulary (`CoreTypes` in
  `internal/audit/frontmatter.go`): `runbook`, `architecture`, `reference`,
  `decision`, `guide`, `index` — advisory, not enforced; a custom value is
  accepted (tolerate-unknown), never rejected.
- **`verified`/`review`** — freshness metadata: `verified` is the date the doc
  was last checked against reality, `review` is a per-doc staleness-cadence
  override (e.g. `90d`). Vocabulary only today — no check reads either field
  yet.
- **`links`** — typed edges (`Edge{Rel, To, Note}`, the "label the link"
  model). `rel` core vocabulary (`CoreRels`): `covers`, `part-of`,
  `supersedes`, `depends-on`, `runbook-for`, `see-also`, `source` — advisory,
  custom allowed. `to` is **repo-root-relative** (`ResolveEdgeTarget` resolves
  against the repo root, not the source doc's directory — unlike a markdown
  link) with its kind inferred (`ClassifyTarget`), never declared: an internal
  `.md` path is a doc edge (existence-checked, feeds reachability — see the
  reachability footgun below), another internal path is a code edge
  (existence-checked only), a URL/`mailto:` is external (unverifiable, never
  checked), and an `owner/repo:path` form is cross-repo (deferred to
  Mycelium — docgraph sees only one repo — and never a finding here).
- **Cycles**: only `part-of`/`supersedes` edges between tracked docs form the
  acyclic-checked graph (`detectCycles`/`cycleRels`); every other `rel` is a
  cross-reference, not hierarchy/lineage, and is exempt from cycle detection.

`CoreTypes`/`CoreRels` are single-sourced in `internal/audit/frontmatter.go`;
`docgraph schema` reads them into the emitted JSON Schema rather than
restating the vocabulary, so the schema and the checks can't drift apart.

## Footguns

- **Measures prose-link reachability on purpose — NOT MkDocs nav.** A MkDocs
  site with no `nav:` block auto-builds its sidebar from the file tree, so every
  page is trivially reachable *for a human*. That is not what this tool checks:
  an agent doesn't read the sidebar. Do **not** "fix" orphan detection to defer
  to MkDocs nav — it would make the tool always report zero orphans and destroy
  its purpose.
- **`footgun-drift` is diff-scoped ON PURPOSE — do NOT re-add `footguns` to
  `checkNames`.** A whole-state footgun check re-scanning every tracked `.md`
  floods on the existing corpus: every already-accepted footgun note re-flags on
  every unrelated push (worse now that the check flags *every* declaration, not
  just un-rationalized ones). So the check is scoped to what's *new* —
  declarations added in the pushed range; content already on the remote is never
  re-scanned. `footgun-drift` and `doc-drift` share this check-what-changed model
  but stay **separate** subcommands, because their trigger and diff source differ:
  `doc-drift` is a Stop-hook driven by the working-tree-inclusive **code** diff,
  `footgun-drift` a pre-push subcommand driven by git's pushed-ref range diffing
  **markdown**. Do not merge them. The six `docgraph .` checks stay whole-state:
  reachability, link existence, and leak content have no meaningful "diff" version
  — they're properties of the current tree, not of a range.
- **`leaks` rules live in a GLOBAL file, never in the repo — on purpose.** A
  per-repo deny list committed to a public repo *is itself the leak* (it
  enumerates the owner's sensitive terms), and the footprint vocabulary is
  identical across repos. The README's `leaks` section documents the TOML schema,
  the `--leaks-config`→`$DOCGRAPH_LEAKS`→`$XDG_CONFIG_HOME` resolution (XDG, never
  `os.UserConfigDir()` — wrong on macOS for a CLI), and the `leaks-rules` export.
  The load-bearing invariants:
  - **The config is the SOLE source of rules — NO hardcoded built-ins.** Generic
    secret shapes (PEM/AWS/GitHub/Slack) are `regex` entries the owner adds; the
    binary ships none. Rules hidden in a binary can't be seen or tuned — the config
    being the single visible source is the point. Do NOT reintroduce built-ins.
  - **Absent config is NOT fatal; malformed IS.** leaks runs by default (incl. CI,
    which has no machine-local file), so an absent file is the normal state → no
    rules, no-op, warn. A malformed config (bad TOML, a bad regexp, or a
    non-absolute `[[dir]]` `path`) is a real bug → fatal (exit 2). Do NOT restore
    hard-fail-on-absent — it would brick every CI/fresh-clone push.
  - **`leaks-rules` targets filter-repo's Python `re`, not Go/RE2.** A leading
    `(?-i)` is normalized to a plain case-sensitive rule (Python `re` rejects the
    bare flag-clear Go/RE2 accepts, which would abort the rewrite); other RE2-only
    syntax needs manual review first. `allow`/`allow_regex`/`[[dir]]` are dropped
    with a warning (filter-repo rewrites by content across all history, so span/path
    exceptions can't apply). docgraph never reads or rewrites history itself — the
    rewrite is a separate external `git filter-repo` step; history *detection* stays
    out of scope (the `pre-public-leak-audit` skill).
- **Enforce-by-default, exclude explicitly — never an opt-in/include model.**
  Every check runs by default; `--skip <check[,check]>` is the only way to not run
  one. The removed `--checks` (include-list) flag could not enforce: a check added
  later is silently absent from every existing `--checks` list, so it enforces
  nowhere until each repo edits its list — exactly how `leaks` first shipped,
  invisible, under that model. With the exclude model a new check is enforced
  everywhere the day it lands, and the generated hook runs a bare `docgraph .` for
  the same reason. `run` and `install-hook` reject a stray `--checks` with a
  migration message (exit 2). Do NOT reintroduce an include-list default.
- **`leaks` scope is git tracking, not the doc-graph ignore layers.** `LeakScan`
  scans every file `git ls-files` returns — so `.gitignore` governs what's
  in-scope — and honors only the explicit `--ignore` CLI globs as a per-run
  escape hatch. It does **not** apply `defaultIgnores` or `.docgraphignore`: a
  tracked file ships publicly regardless of the doc-graph scope, so a tracked
  `.claude/` config (excluded from orphans/broken/untracked because it isn't
  documentation) is exactly where owner-specific strings hide and must stay
  in-scope for the leak pass.
- **Dir-scoped exclusions are keyed by ABSOLUTE path and are local-only.**
  `[[dir]]` sections match a scanned file by absolute-path containment, so they
  only take effect where the global config lives (your machine). CI / fresh
  clones have no config → no rules → the scan is a no-op there. So a repo whose
  own tracked fixtures would trip its owner's rules is silenced *in the config*
  with a `[[dir]] ignore` for that repo (e.g. docgraph's own config entry ignores
  `**/*_test.go`) — the config is the single control surface, not a per-repo
  `--skip`/`--ignore` or an inline marker (see next).
- **No inline suppression markers — every control is config or CLI.** docgraph
  never parses a suppression comment/pragma out of the files it audits.
  Suppression is *only* `.docgraphignore`/`--ignore`/`--skip` (doc-graph scope)
  and the leaks config's `allow`/`allow_regex`/`[[dir]]` (leak scope);
  `footgun-drift` and `doc-drift` have no in-file escape at all, opted out only
  whole-check via `DOCGRAPH_FOOTGUN_OFF=1` / `--no-footgun-drift` and
  `DOC_DRIFT_OFF=1` respectively. This is deliberate: an inline marker committed
  to a public repo would be a visible "here be a secret" annotation (same reason
  the leaks deny-list stays out of the repo), and a per-file override is exactly
  what the config-as-single-source-of-truth model exists to avoid — a line-level
  comment scanner would also have to read file content just to honor
  self-referential annotations, silently un-nagging whatever it's placed on. A
  flagged `footgun-drift`/`doc-drift` reference is a situation-based judgment call
  — reconcile the doc, or confirm it's intentional framed history and move on —
  de-duped only by doc-drift's once-per-HEAD loop-guard.
- **Code-block links are skipped deliberately.** `extractLinks` ignores fenced
  (```` ``` ````/`~~~`) and inline (`` `...` ``) code so template/example paths
  in docs (e.g. a `[docs](services/name.md)` template row) don't register as real
  *links*. Asymmetry: the orphan **reachability** pass, `mentionsPath`, *does*
  read inline-code path mentions — that's how an agent follows a bare
  `` `docs/x.md` `` reference. Link-extraction and reachability answer different
  questions; don't unify them.
- **Reachability = markdown links, path mentions, OR frontmatter doc-edges —
  don't narrow to any one.** Model-C repos (design docs referenced by path,
  not clickable link) would show a flood of false orphans under link-only
  reachability; removing `mentionsPath` reintroduces it. A frontmatter
  `links:` edge to a tracked doc (`rel` doesn't matter — `part-of`,
  `see-also`, anything) is the **third** reachability source: a doc reached
  only via a typed edge is not an orphan. Only `EdgeDoc`-classified targets
  (internal `.md`) count as reachability edges — code/external/cross-repo
  edges never make a doc reachable.
- **Exclude tooling, not real docs — don't re-narrow to `docs/`.** Orphan
  candidates are *all* tracked `.md` except the `defaultIgnores` (`.claude/**`
  and `.agents/**` agent skill/config files, which aren't documentation, and
  untracked scratch). A real doc outside `docs/` (a config-dir README, e.g. a
  `monitoring/README.md`) **is** a document and must be audited — an earlier
  `docs/`-only scope wrongly made such docs invisible (neither flagged nor
  checked). `.claude/**` and `.agents/**` files are runtime tooling; a config-dir
  README is not. Keep that distinction.
- **Usage logging is OPT-IN, side-channel, and MUST NOT alter the gate.** One JSONL
  record per run under `$XDG_STATE_HOME/docgraph/usage.jsonl` (XDG *state*, not
  config), only when a global `config.toml` `[log]` table opts in (the README's
  Usage-logging section has the config + level table). Load-bearing invariants:
  - **Separate file from `leaks.toml`.** `leaks.toml` is a dedicated rules file that
    may be synced on its own; `config.toml` holds `[log]`. Don't merge them.
  - **Malformed `config.toml` is NON-fatal here** — warn, disable logging, run
    continues. This deliberately DIVERGES from malformed-`leaks.toml`-is-fatal:
    leaks is an enforced protection, logging is auxiliary, so a log-config typo must
    never block a push. Absent config → silently off (no warning; the normal
    CI/clone/fresh state). Do NOT make either fatal.
  - **Level gates leak exposure.** L1 counts only, L2 adds paths (`file:line`), L3
    adds full findings **including leak match text**. Levels 1–2 must NEVER write a
    leak `Match` — the log must not become the sensitive-string sink the `leaks`
    check exists to prevent. Only L3 (a documented, trusted-machine opt-in) does.
  - **Best-effort, never fails the run.** `maybeLog` swallows every error; the exit
    code is decided by findings alone. `DOCGRAPH_NO_LOG=1` is the one-off kill switch
    (mirrors `DOC_DRIFT_OFF`).
  - **`cmd` is a seam, not decoration.** Each record carries `"cmd":"run"`. It exists
    so a future `docgraph drift` subcommand logs through the *same* file with the
    *same* record shape — trends span both. Keep the field when adding a subcommand.
- **`footgun-drift` is a nag, not a judge — so it flags EVERY added
  declaration.** It detects a footgun *declaration* (a line-leading `Footgun:` or a
  bolded mid-line footgun lead — introducing one, not a cross-reference or a bare
  container heading with no delimiter) and reports it, full stop. It deliberately
  does **not** detect a rationale: docgraph is a deterministic scanner and can't
  rank whether a stated "why" is real — rationale detection would just reward
  typing "because". So it nags and prints the two-question test (is this a real
  footgun; is it at the right doc level — the `doc-and-audit-rigor` skill's test),
  leaving that judgment to the pusher. Because it judges nothing, it does not
  block. Do NOT reintroduce rationale detection to "reduce noise" — an honest nag
  beats a fake judge.
- **`footgun-drift`'s file scope is `git diff --name-only <range> -- '*.md'` —
  not the doc-graph ignore layers, and not the leaks git-tracking scope
  either.** Any `.md` file the diff touches is in scope, including a
  `.claude/**` skill file that `orphans`/`broken`/`untracked` exclude as
  non-documentation: a footgun declaration added inside agent tooling is just
  as undocumented as one in `CLAUDE.md`, so narrowing to the doc-graph roots
  would blind the check to exactly the files most likely to accumulate
  footgun notes over time. Do not apply `defaultIgnores` or `.docgraphignore` here.

## Doc models (why `--skip` exists)

Repos fall into models the orphan check treats differently:
- **A — prose-linked**: entry docs link/mention through `docs/`. Orphans are
  real. Enforce every check (the default).
- **B — nav-driven MkDocs**: `docs/` with no `nav:` block; MkDocs auto-builds
  the sidebar, pages never cross-link → every page is a prose-orphan *by design*.
  Run with `--skip orphans`.
- **C — flat reference `docs/`**: design notes referenced by path. `mentionsPath`
  makes these reachable; genuine orphans that remain are real gaps worth linking.

A repo that doesn't use the `Footgun:` note convention at all opts out of
`footgun-drift` entirely rather than passing `--skip` (it isn't a `docgraph .`
check to skip): set `DOCGRAPH_FOOTGUN_OFF=1`, or generate the hook with
`install-hook --no-footgun-drift` so it's never invoked in the first place.
Likewise, a repo that doesn't use the anchored-symbol-and-value convention
`doc-drift` relies on (a doc naming a code symbol, a constant it also shows the
literal value of) disables `doc-drift` outright with `DOC_DRIFT_OFF=1` — it
isn't a `docgraph .` check either, so there's no `--skip` name for it.

## Roots

Auto = tracked ones of `{CLAUDE.md, README.md, AGENTS.md, docs/index.md}` +
`--root` additions. Unifies "whole doc repo" and "project with CLAUDE.md +
docs/" with zero config.

## Layout & commands

- `main.go` — thin CLI: flags, `run(args, stdout, stderr) int` (the six
  whole-state checks), `runFootgunDrift(args, stdout, stderr) int` (the
  diff-scoped pre-push subcommand), `runDocDrift(args, stdin, stdout, stderr)
  int` (the Stop-hook subcommand — checks `DOC_DRIFT_OFF`, resolves the diff
  spec via `docDriftDiffBase`, calls `audit.DocDrift`, and on a finding prints
  via `printDocDrift` and returns 2, gated on bare invocation by
  `docDriftGuardOK`'s once-per-HEAD marker under `docDriftStateDir()`),
  `runCovers`/`runIndex`/`runStale` (the read-only views — each resolves the
  repo root, calls `audit.RepoDocs`, and prints; always `return 0`), report
  format, `maybeLog` (opt-in usage logging side-channel).
- `internal/audit/views.go` — `RepoDocs` (the shared parse path behind the
  three views), `CoversOf`, `IndexMarkdown`, `StaleDocs` + `parseReviewDays`.
- `internal/audit/` — `links.go` (parse/resolve), `ignore.go` (`**` globs),
  `git.go` (`ls-files` wrappers **plus** the diff helpers `changedMarkdown`/
  `addedLines`/`fileAtRev`/`ClosestBase` that `footgun_drift.go` and
  `doc_drift.go` use to read a range instead of a tree snapshot), `leaks.go`
  (TOML config decode + dir-scoped content scan), `footguns.go` (the
  declaration scanner — `scanDeclarations`/`isFootgunDeclaration`; a
  *declaration* is a footgun being introduced, not a passing mention of one,
  and every one is reported — `scanDeclarations` does no rationale filtering),
  `footgun_drift.go` (`FootgunDrift`: runs `scanDeclarations` per range,
  keeps only declarations whose line is in that range's added-line set,
  dedupes by file:line), `doc_drift.go` (`DocDrift` + helpers
  `looksLikeSymbol`, `removedNotReadded`, `changedConstants`, `gitDiff`,
  `stillDefinedInCode`, `docGrepSymbol`, `docGrepValue`: diffs `gitDiff(root,
  spec)`, finds removed-and-not-readded definitions and changed numeric
  constants, then greps tracked docs for a lingering reference to either),
  `audit.go` (`Audit` → `Report`, the whole-state orchestrator; unrelated to
  `FootgunDrift`/`DocDrift`), `usage.go` (usage-log config + tiered
  `BuildRecord` + best-effort `LogRun`).
- `just test` / `just build` / `just install`. Tests build throwaway git repos
  in temp dirs, so `git` must be on PATH.
- **Install with `just install`** (or `go install .`) → `~/go/bin`. The binary is not
  reinstalled automatically, because `go install` only runs when invoked — so
  reinstall after changing the CLI or the local binary runs stale logic.
- `install.sh` + `.claude/commands/docgraph/install.md` (`/docgraph:install`) — the
  guided installer (`project-standards` item 13). `install.sh` is the curl-pipeable
  mechanism (`go install`, dual-mode IN_REPO/`@latest`, seeds `~/.config/docgraph/`,
  edits no global config); `/docgraph:install` is the guided layer that wires the
  `doc-drift` Stop hook into `~/.claude/settings.json`, offers the per-repo pre-push
  gate, and seeds the leaks config. Both single-source the module path and never
  depend on `just`.

## Branching & releases

- **`main` + `dev`.** `dev` is the integration trunk; `main` is the release branch —
  it only fast-forwards to a tagged release commit and stays a clean ancestor of `dev`.
  Never commit directly to `main` (drift breaks the fast-forward; fix by back-merging
  `main` into `dev`, never force-push). Feature/fix branches off `dev`.
- **Semver, `v`-prefixed tags.** The tracked root **`VERSION`** file is the single source
  of truth, `go:embed`-ed via `version.go` so `docgraph version` (also `--version`, `-v`)
  self-reports — never restate the version elsewhere. Consumers `go install …@latest`, so
  a release moves everyone's pinned tool: keep `main` releasable and bump major for a
  breaking CLI change, minor for a new feature, patch for a fix.
- **Cut a release** from `dev` with `VERSION` bumped + committed: `just release` runs
  `gate`, fast-forwards `main`, tags `v<VERSION>`, and publishes the GitHub release.

## Footgun — the gate must find its own binary under a minimal PATH

The pre-push hook `hookScript` generates must resolve docgraph via PATH **and**
the Go bin dir (`$GOBIN`/`$GOPATH/bin`/`~/go/bin`), not `command -v` alone. Git
runs hooks with the caller's PATH; GUI clients and sandboxed agent harnesses push
with a bare PATH that omits `~/go/bin`. With a `command -v`-only lookup the
fail-closed gate then *blocks the push because it can't see an installed binary*
— tool present, but invisible — which reads as "docgraph is broken" and trains
agents to reach for `--no-verify`. The Go-bin fallback (guarded by a test in
`main_test.go`) is load-bearing; do not narrow it back to `command -v`.

## v1 gaps (documented, not silent)

Anchor validity, external-URL liveness, raw `<a href>`, per-section `index.md`
implicit-nav, repo-specific conventions. Add only with a test and a README note.
