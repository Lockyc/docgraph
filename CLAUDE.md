# docaudit — notes for the next agent

A Go CLI with three modes. `docaudit .` runs four **whole-state** checks against
the current tree — orphans (tracked `docs/` files unreachable from the roots),
broken internal `.md` links, untracked `.md` files, and a content scan for
configured `leaks` patterns. Reachability follows markdown links **and**
bare/inline-code path mentions. All four are **enforced by default**; you
exclude one explicitly with `--skip` (there is no opt-in — an opt-in check
enforces nothing). `docaudit footgun-drift` is a **diff-scoped** pre-push
subcommand: it flags *every* footgun declaration added in the pushed range — it
makes no attempt to judge rationale, never re-scans the existing corpus, and
honors no inline suppression marker (see the no-inline-markers footgun). It is
**advisory** (exits 0, never blocks the push): the finding is a nag to go
double-check the declaration is a real footgun, not a gate. `docaudit doc-drift`
is a **Stop-hook** subcommand: invoked directly (no wrapper) at the end of an
agent turn, it scans the branch's working-tree-inclusive diff (base→worktree,
committed + uncommitted) for two mechanical staleness classes — a **dangling
reference** (a symbol whose definition was removed on this branch and survives
nowhere in tracked code, but a tracked doc still names it) and **anchored value
drift** (a constant whose numeric value changed while a doc still names the
symbol and shows the old literal) — and **blocks the Stop** (findings on
stderr, exit 2) on either. Neither `footguns` nor `doc-drift` is one of the four
`checkNames`; each is a separate subcommand with its own trigger (a git range or
a Stop invocation, not a repo path). The four whole-state checks exit non-zero
on any finding (that's the gate); footgun-drift never blocks; doc-drift always
blocks on a finding. Stdlib +
`github.com/BurntSushi/toml` (config decode); shells out to `git`. It also has
an **opt-in usage log** (one JSONL record per run, for trend-watching — see the
logging footgun). Usage and checks are in `README.md` — this file carries the
invariants and footguns.

## Intended use

docaudit is built to run as a **pre-push documentation gate** (and in CI): the
four whole-state checks exit non-zero on a finding so a broken doc-graph blocks
the push without a wrapper. `docaudit install-hook` writes a tracked
`.githooks/pre-push` for that; the generated hook also runs `footgun-drift` as an
**advisory** rider (it prints its nag but never blocks — see its footgun below).
Separately, `docaudit doc-drift` is meant to be wired as a **Stop hook** by the
agent harness (e.g. a Claude Code `Stop` hook entry that runs `docaudit
doc-drift`): it fires at the end of a turn, not at push time, so a dangling
reference or stale anchored value is caught and blocked before the agent hands
control back — see [`doc-drift`](README.md#docaudit-doc-drift) in `README.md`.

## What it is (and is not)

- **Agent-facing, not human-facing.** It measures the graph an agent traverses
  (grep + `[x](y.md)`), *not* whether a human can reach a page.
- **Reads doc-graph structure and, for `leaks`, file content — both as
  whole-state.** The three doc-graph checks (orphans/broken/untracked) traverse
  only the link graph; `leaks` additionally scans tracked file *content* (code
  included) for configured leak patterns. Both read the current tree, never git
  history.
- **`footgun-drift` reads a git diff, not repo state.** It scans tracked
  markdown *content* like `leaks` does, but only the lines a `git diff` reports
  as added in the given range — the four `docaudit .` checks above have no
  range concept at all.
- **`doc-drift` reads a *code* diff and greps *docs*, over a working-tree-
  inclusive range.** Unlike `footgun-drift` (which diffs and scans the same
  file type — markdown against markdown), `doc-drift` diffs tracked **code**
  (definitions removed, constants whose value changed) and greps the *docs* for
  stale references to what it found. Its range is base→worktree — committed
  **and** uncommitted changes — because it fires as a Stop hook, before a
  commit exists to diff against; `footgun-drift`'s range is always committed
  `base..head`, because it fires at push time.

## Footguns

- **Measures prose-link reachability on purpose — NOT MkDocs nav.** A MkDocs
  site with no `nav:` block auto-builds its sidebar from the file tree, so every
  page is trivially reachable *for a human*. That is not what this tool checks:
  an agent doesn't read the sidebar. Do **not** "fix" orphan detection to defer
  to MkDocs nav — it would make the tool always report zero orphans and destroy
  its purpose.
- **`footgun-drift` is diff-scoped ON PURPOSE — a whole-state footgun check was
  tried first and abandoned.** An earlier attempt made `footguns` a fifth
  *whole-state* check re-scanning every tracked `.md` on every `docaudit .` run.
  It was dropped before shipping because that re-scan produced a validated
  flood of false positives against the existing corpus — every already-accepted
  footgun note in the tree re-flagged on every unrelated push (and now that the
  check flags *every* declaration rather than trying to detect rationale, a
  whole-state re-scan would flood even harder). Do NOT re-add `footguns` to
  `checkNames` to "fix" this; the flood is exactly why it isn't there. The fix
  was to scope the check to what's *new*: it flags only footgun *declarations
  added in that range*; content already on the remote is never re-scanned.
  `footgun-drift` and `doc-drift` both share this check-what-changed-not-
  the-whole-tree model, and both live in docaudit as subcommands, but stay
  **separate** subcommands because their trigger and diff source differ:
  `doc-drift` is a **Stop-hook** subcommand driven by the branch's
  working-tree-inclusive code diff (a definition removed or a constant's value
  changed, with a doc still referencing the old state); `footgun-drift` is a
  **pre-push** subcommand driven by git's pushed-ref range (ref lines on stdin,
  or `--range base..head` for manual use) diffing markdown against markdown.
  Different trigger, different diff source, different file types compared — do
  not merge them into one subcommand. The four `docaudit .` checks remain
  whole-state, unchanged: reachability, link existence, and leak content have
  no meaningful "diff" version — they're properties of the current tree, not of
  a range.
- **`leaks` rules live in a GLOBAL file, never in the repo — on purpose.** A
  per-repo deny list committed to a public repo *is itself the leak* (it
  enumerates every sensitive term the owner has). The footprint vocabulary is
  also identical across repos. So `leaks` reads a **TOML** file at
  `--leaks-config` → `$DOCAUDIT_LEAKS` → `$XDG_CONFIG_HOME/docaudit/leaks.toml`
  (default `~/.config/docaudit/leaks.toml` — XDG, not `os.UserConfigDir()`, which
  on macOS is the wrong `~/Library/Application Support` GUI-app home for a CLI tool),
  with top-level `terms` (literal, case-insensitive) / `regex` (also
  case-insensitive by default — opt out per-pattern with `(?-i)`; a leak must be
  caught in any casing) / `allow` / `allow_regex` deny-and-exception arrays, plus
  `[[dir]]` sections that scope an `ignore`/`allow`/`allow_regex` set to files
  under an absolute `path` (a leading `~/` expands). **The config is the SOLE
  source of rules — there are NO hardcoded built-in patterns.** Generic secret
  shapes (PEM/AWS/GitHub/Slack) are just `regex` entries the owner adds (with a
  leading `(?-i)` to keep them case-sensitive); the binary ships none. Because
  leaks runs by default (incl. in CI, which has no machine-local file), an
  **absent** config is NOT fatal — with no rules the scan is a no-op plus a
  warning; a **malformed** config (bad TOML, a bad regexp in any
  `regex`/`allow_regex` field, or a non-absolute `[[dir]]` `path`) IS fatal
  (exit 2), since that's a real bug, not the common "not set up yet" case. Do NOT
  restore hard-fail-on-absent: leaks being default-on means a missing global file
  is the normal CI/fresh-clone state, and failing there would brick every push.
  Do NOT reintroduce hardcoded built-in patterns either — the config being the
  single visible source of truth is the point (rules hidden in the binary can't be
  seen or tuned). History scrubbing is now supported by **exporting** rules for an
  external rewriter — `docaudit leaks-rules` emits a `git-filter-repo --replace-text`
  file from the config (terms → `regex:(?i)…` escaped; `regex` kept case-insensitive
  unless it has a leading `(?-i)`; `allow`/`allow_regex`/`[[dir]]` **dropped with a
  stderr warning**, since filter-repo rewrites by content across all paths/history and
  can't honor a span or path exception). Emitted rules target filter-repo's **Python
  `re` engine**, not Go/RE2: a leading `(?-i)` is normalized to a plain
  case-sensitive rule rather than emitted verbatim, because Python `re` rejects a
  bare `(?-i)` flag-clear that Go/RE2 accepts and would otherwise abort the whole
  rewrite — a `(?-i)` anywhere else in a pattern, or other RE2-only syntax, has no
  such normalization and needs manual review before running the rewrite. docaudit
  itself still **never reads or rewrites history** — `leaks-rules` reads only the
  TOML; the destructive rewrite is a separate external step. History *detection*
  remains out of scope (owner's call); that stays with the manual
  `pre-public-leak-audit` skill.
- **Enforce-by-default, exclude explicitly — never an opt-in/include model.**
  Every check runs by default; `--skip <check[,check]>` is the only way to not run
  one. The removed `--checks` (include-list) flag could not enforce: a check added
  later is silently absent from every existing `--checks` list, so it enforces
  nowhere until each repo edits its list — exactly how `leaks` first shipped,
  invisible, under that model. With the exclude model a new check is enforced
  everywhere the day it lands, and the generated hook runs a bare `docaudit .` for
  the same reason. `run` and `install-hook` reject a stray `--checks` with a
  migration message (exit 2). Do NOT reintroduce an include-list default.
- **`leaks` scope is git tracking, not the doc-graph ignore layers.** `LeakScan`
  scans every file `git ls-files` returns — so `.gitignore` governs what's
  in-scope — and honors only the explicit `--ignore` CLI globs as a per-run
  escape hatch. It does **not** apply `defaultIgnores` or `.docauditignore`: a
  tracked file ships publicly regardless of the doc-graph scope, so a tracked
  `.claude/` config (excluded from orphans/broken/untracked because it isn't
  documentation) is exactly where owner-specific strings hide and must stay
  in-scope for the leak pass.
- **Dir-scoped exclusions are keyed by ABSOLUTE path and are local-only.**
  `[[dir]]` sections match a scanned file by absolute-path containment, so they
  only take effect where the global config lives (your machine). CI / fresh
  clones have no config → no rules → the scan is a no-op there. So a repo whose
  own tracked fixtures would trip its owner's rules is silenced *in the config*
  with a `[[dir]] ignore` for that repo (e.g. docaudit's own config entry ignores
  `**/*_test.go`) — the config is the single control surface, not a per-repo
  `--skip`/`--ignore` or an inline marker (see next).
- **No inline suppression markers — every control is config or CLI.** docaudit
  never parses a suppression comment/pragma out of the files it audits.
  Suppression is *only* `.docauditignore`/`--ignore`/`--skip` (doc-graph scope)
  and the leaks config's `allow`/`allow_regex`/`[[dir]]` (leak scope);
  `footgun-drift` and `doc-drift` have no in-file escape at all, opted out only
  whole-check via `DOCAUDIT_FOOTGUN_OFF=1` / `--no-footgun-drift` and
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
  in docs don't register as real *links*. Removing this resurrects false-positive
  broken links (e.g. a `[docs](services/name.md)` template row). This was a real
  false positive caught on an early real-world run — the skip is load-bearing.
  (Note the asymmetry: the orphan **reachability** pass, `mentionsPath`, *does*
  read inline-code path mentions on purpose — that's how an agent follows a
  bare `` `docs/x.md` `` reference. Link-extraction and reachability answer
  different questions; don't unify them.)
- **Reachability = markdown links OR path mentions — don't narrow to links.**
  Model-C repos (design docs referenced by path, not clickable link) would show
  a flood of false orphans under link-only reachability. Validated on real
  flat-reference repos: link-only reachability reported dozens of false orphans
  that `mentionsPath` collapsed to the genuine few. Removing `mentionsPath`
  reintroduces the flood.
- **Exclude tooling, not real docs — don't re-narrow to `docs/`.** Orphan
  candidates are *all* tracked `.md` except the `defaultIgnores` (`.claude/**`
  and `.agents/**` agent skill/config files, which aren't documentation, and
  untracked scratch). A real doc outside `docs/` (a config-dir README, e.g. a
  `monitoring/README.md`) **is** a document and must be audited — an earlier
  `docs/`-only scope wrongly made such docs invisible (neither flagged nor
  checked). `.claude/**` and `.agents/**` files are runtime tooling; a config-dir
  README is not. Keep that distinction.
- **Usage logging is OPT-IN, side-channel, and MUST NOT alter the gate.** One JSONL
  record per run appends to `$XDG_STATE_HOME/docaudit/usage.jsonl` (XDG *state*, not
  config) **only** when a global `config.toml` `[log]` table opts in — resolved
  `--config` → `$DOCAUDIT_CONFIG` → `$XDG_CONFIG_HOME/docaudit/config.toml` (same XDG
  discipline as leaks, never `os.UserConfigDir()`). Invariants that are load-bearing:
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
    code is decided by findings alone. `DOCAUDIT_NO_LOG=1` is the one-off kill switch
    (mirrors `DOC_DRIFT_OFF`).
  - **`cmd` is a seam, not decoration.** Each record carries `"cmd":"run"`. It exists
    so a future `docaudit drift` subcommand logs through the *same* file with the
    *same* record shape — trends span both. Keep the field when adding a subcommand.
- **`footgun-drift` is a nag, not a judge — so it flags EVERY added
  declaration.** It detects a footgun *declaration* (a line-leading `Footgun:`
  marker or a bolded mid-line footgun lead, not a passing mention — a
  cross-reference or a bare container heading with no delimiter never counts) and
  reports it, full stop. It deliberately does **not** look for a rationale: it
  cannot rank whether a stated "why" is actually *good* (that's a judgment task,
  and docaudit is a deterministic pattern scanner), and an earlier cut that
  suppressed a declaration when a rationale *word* sat nearby just rewarded typing
  "because" — a gameable in-file escape that judged nothing. So it nags on the
  declaration itself and prints the two-question test (is this a real footgun; is
  it at the right doc level — the same test the `doc-and-audit-rigor` skill
  applies), leaving that judgment to the pusher. Because it judges nothing, it
  does not block — see the advisory note in the intro. Do NOT reintroduce
  rationale detection to "reduce noise": it can't tell a real rationale from a
  plausible-sounding one, and pretending to is worse than an honest nag.
- **`footgun-drift`'s file scope is `git diff --name-only <range> -- '*.md'` —
  not the doc-graph ignore layers, and not the leaks git-tracking scope
  either.** Any `.md` file the diff touches is in scope, including a
  `.claude/**` skill file that `orphans`/`broken`/`untracked` exclude as
  non-documentation: a footgun declaration added inside agent tooling is just
  as undocumented as one in `CLAUDE.md`, so narrowing to the doc-graph roots
  would blind the check to exactly the files most likely to accumulate
  footgun notes over time. Do not apply `defaultIgnores` or `.docauditignore` here.

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
`footgun-drift` entirely rather than passing `--skip` (it isn't a `docaudit .`
check to skip): set `DOCAUDIT_FOOTGUN_OFF=1`, or generate the hook with
`install-hook --no-footgun-drift` so it's never invoked in the first place.
Likewise, a repo that doesn't use the anchored-symbol-and-value convention
`doc-drift` relies on (a doc naming a code symbol, a constant it also shows the
literal value of) disables `doc-drift` outright with `DOC_DRIFT_OFF=1` — it
isn't a `docaudit .` check either, so there's no `--skip` name for it.

## Roots

Auto = tracked ones of `{CLAUDE.md, README.md, AGENTS.md, docs/index.md}` +
`--root` additions. Unifies "whole doc repo" and "project with CLAUDE.md +
docs/" with zero config.

## Layout & commands

- `main.go` — thin CLI: flags, `run(args, stdout, stderr) int` (the four
  whole-state checks), `runFootgunDrift(args, stdout, stderr) int` (the
  diff-scoped pre-push subcommand), `runDocDrift(args, stdin, stdout, stderr)
  int` (the Stop-hook subcommand — checks `DOC_DRIFT_OFF`, resolves the diff
  spec via `docDriftDiffBase`, calls `audit.DocDrift`, and on a finding prints
  via `printDocDrift` and returns 2, gated on bare invocation by
  `docDriftGuardOK`'s once-per-HEAD marker under `docDriftStateDir()`), report
  format, `maybeLog` (opt-in usage logging side-channel).
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

## Branching & releases

- **`main` + `dev`.** `dev` is the integration trunk; `main` is the release branch —
  it only fast-forwards to a tagged release commit and stays a clean ancestor of `dev`.
  Never commit directly to `main` (drift breaks the fast-forward; fix by back-merging
  `main` into `dev`, never force-push). Feature/fix branches off `dev`.
- **Semver, `v`-prefixed tags.** The tracked root **`VERSION`** file is the single source
  of truth, `go:embed`-ed via `version.go` so `docaudit version` (also `--version`, `-v`)
  self-reports — never restate the version elsewhere. Consumers `go install …@latest`, so
  a release moves everyone's pinned tool: keep `main` releasable and bump major for a
  breaking CLI change, minor for a new feature, patch for a fix.
- **Cut a release** from `dev` with `VERSION` bumped + committed: `just release` runs
  `gate`, fast-forwards `main`, tags `v<VERSION>`, and publishes the GitHub release.

## Footgun — the gate must find its own binary under a minimal PATH

The pre-push hook `hookScript` generates must resolve docaudit via PATH **and**
the Go bin dir (`$GOBIN`/`$GOPATH/bin`/`~/go/bin`), not `command -v` alone. Git
runs hooks with the caller's PATH; GUI clients and sandboxed agent harnesses push
with a bare PATH that omits `~/go/bin`. With a `command -v`-only lookup the
fail-closed gate then *blocks the push because it can't see an installed binary*
— tool present, but invisible — which reads as "docaudit is broken" and trains
agents to reach for `--no-verify`. The Go-bin fallback (guarded by a test in
`main_test.go`) is load-bearing; do not narrow it back to `command -v`.

## v1 gaps (documented, not silent)

Anchor validity, external-URL liveness, raw `<a href>`, per-section `index.md`
implicit-nav, repo-specific conventions. Add only with a test and a README note.
