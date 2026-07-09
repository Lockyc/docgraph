# docaudit — notes for the next agent

A Go CLI that audits a repo's **agent-facing documentation graph** — orphans
(tracked `docs/` files unreachable from the roots), broken internal `.md` links,
untracked `.md` files — **and** scans tracked file content for configured `leaks`
patterns. Reachability follows markdown links **and** bare/inline-code path
mentions. All four checks are **enforced by default**; you exclude one explicitly
with `--skip` (there is no opt-in — an opt-in check enforces nothing). Exits
non-zero on any finding in an enforced check. Stdlib + `github.com/BurntSushi/toml`
(config decode); shells out to `git`. It also has an **opt-in usage log** (one JSONL
record per run, for trend-watching — see the logging footgun). Usage and checks are
in `README.md` — this file carries the invariants and footguns.

## Intended use

docaudit is built to run as a **pre-push documentation gate** (and in CI): it
exits non-zero on a finding so a broken doc-graph blocks the push without a
wrapper. `docaudit install-hook` writes a tracked `.githooks/pre-push` for that.

## What it is (and is not)

- **Agent-facing, not human-facing.** It measures the graph an agent traverses
  (grep + `[x](y.md)`), *not* whether a human can reach a page.
- **Reads doc-graph structure and, for the `leaks` check, file content.** The
  three doc-graph checks (orphans/broken/untracked) traverse only the link graph.
  The `leaks` check additionally scans tracked file *content* (code included) for
  configured leak patterns. It never reads git history.

## Footguns

- **Measures prose-link reachability on purpose — NOT MkDocs nav.** A MkDocs
  site with no `nav:` block auto-builds its sidebar from the file tree, so every
  page is trivially reachable *for a human*. That is not what this tool checks:
  an agent doesn't read the sidebar. Do **not** "fix" orphan detection to defer
  to MkDocs nav — it would make the tool always report zero orphans and destroy
  its purpose.
- **Do NOT merge this with `doc-drift.sh`.** That Stop hook is a *content-vs-code*
  drift check driven by the code diff (a changed constant whose old literal lingers
  in a doc). `docaudit` audits *repo state* — doc-graph integrity plus a content
  leak scan — not diffs. Different inputs and cadence; keep them separate.
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
  `--skip`/`--ignore` or an inline comment.
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
  Claude Code skill/config files, which aren't documentation, and untracked
  scratch). A real doc outside `docs/` (a config-dir README, e.g. a
  `monitoring/README.md`) **is** a document and must be audited — an earlier
  `docs/`-only scope wrongly made such docs invisible (neither flagged nor
  checked). `.claude/**` files are runtime tooling; a config-dir README is not.
  Keep that distinction.
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

## Doc models (why `--skip` exists)

Repos fall into models the orphan check treats differently:
- **A — prose-linked**: entry docs link/mention through `docs/`. Orphans are
  real. Enforce every check (the default).
- **B — nav-driven MkDocs**: `docs/` with no `nav:` block; MkDocs auto-builds
  the sidebar, pages never cross-link → every page is a prose-orphan *by design*.
  Run with `--skip orphans`.
- **C — flat reference `docs/`**: design notes referenced by path. `mentionsPath`
  makes these reachable; genuine orphans that remain are real gaps worth linking.

## Roots

Auto = tracked ones of `{CLAUDE.md, README.md, AGENTS.md, docs/index.md}` +
`--root` additions. Unifies "whole doc repo" and "project with CLAUDE.md +
docs/" with zero config.

## Layout & commands

- `main.go` — thin CLI: flags, `run(args, stdout, stderr) int`, report format,
  `maybeLog` (opt-in usage logging side-channel).
- `internal/audit/` — `links.go` (parse/resolve), `ignore.go` (`**` globs),
  `git.go` (`ls-files` wrappers), `leaks.go` (TOML config decode + dir-scoped
  content scan), `audit.go` (`Audit` → `Report`), `usage.go` (usage-log config +
  tiered `BuildRecord` + best-effort `LogRun`).
- `just test` / `just build` / `just install`. Tests build throwaway git repos
  in temp dirs, so `git` must be on PATH.
- **Install with `just install`** (or `go install .`) → `~/go/bin`. The binary is not
  reinstalled automatically, so reinstall after changing the CLI or the local binary
  runs stale logic.

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
