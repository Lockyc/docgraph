# docaudit — notes for the next agent

A Go CLI with two layers. `docaudit .` runs four **whole-state** checks against
the current tree — orphans (tracked `docs/` files unreachable from the roots),
broken internal `.md` links, untracked `.md` files, and a content scan for
configured `leaks` patterns. Reachability follows markdown links **and**
bare/inline-code path mentions. All four are **enforced by default**; you
exclude one explicitly with `--skip` (there is no opt-in — an opt-in check
enforces nothing). Separately, `docaudit footgun-drift` is a **diff-scoped**
pre-push subcommand: it flags only footgun *declarations added in the pushed
range* that lack a rationale or `<!-- footgun-ok -->` marker — it never
re-scans the existing corpus. `footguns` is deliberately **not** one of the
four `checkNames`; it's a separate subcommand with a separate trigger (a git
range, not a repo path). Exits non-zero on any finding. Stdlib +
`github.com/BurntSushi/toml` (config decode); shells out to `git`. Usage and
checks are in `README.md` — this file carries the invariants and footguns.

## Intended use

docaudit is built to run as a **pre-push documentation gate** (and in CI): it
exits non-zero on a finding so a broken doc-graph blocks the push without a
wrapper. `docaudit install-hook` writes a tracked `.githooks/pre-push` for that.

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

## Footguns <!-- footgun-ok: section title, not a claim -->

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
  flood of false positives against the existing corpus — already-accepted
  footgun notes whose rationale sat just outside whatever window the scanner
  used, re-litigated on every unrelated push. Do NOT re-add `footguns` to
  `checkNames` to "fix" this; the flood is exactly why it isn't there. The fix
  was to scope the check to what's *new*: `footgun-drift` shares `doc-drift.sh`'s
  diff-driven model — check what changed, not the whole tree — but runs as a
  **pre-push** command, where git hands it the exact pushed range (ref lines on
  stdin, or `--range base..head` for manual use), so it flags only footgun
  *declarations added in that range*; content already on the remote is never
  re-scanned. That's also why it stays a **separate subcommand** rather than
  merging into `doc-drift.sh`: `doc-drift.sh` is a Stop hook driven by the
  in-session code diff (a changed constant whose old literal lingers in a doc);
  `footgun-drift` is a pre-push gate driven by git's pushed-ref range — different
  trigger, different diff source. The four `docaudit .` checks remain
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
  seen or tuned). History is out of scope (owner's call); that stays with the
  manual `pre-public-leak-audit` skill.
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
- **`footgun-drift` is a heuristic, not a judge.** It catches the bare-assertion
  case — a footgun *declaration* (a line-leading `Footgun:` marker or a bolded
  mid-line footgun lead, not a passing mention — a cross-reference or a bare
  container heading with no delimiter never counts) added with no rationale
  anywhere nearby — but it cannot rank whether a stated rationale is actually
  *good*, because judging rationale quality is a judgment task and docaudit is a
  deterministic pattern scanner. The two-question test it prints on a finding
  (is this a real footgun; is it at the right doc level) is the same test the
  `doc-and-audit-rigor` skill applies — docaudit only mechanizes the
  bare-assertion half of it.
- **A declaration's window is bounded by its next sibling, not by the whole
  paragraph.** `scanDeclarations` (`internal/audit/footguns.go`) starts a
  declaration's window at its own line and stops at the *next* declaration in
  the same paragraph, so a justified bullet can't launder an unjustified
  neighbour in a tight list. Only when a declaration owns the tail of its
  paragraph, and that paragraph is a lone line or a heading, does the window
  extend into the following paragraph — so a heading declaration *does* see an
  explanation written as the next paragraph (it does not need a separate
  `<!-- footgun-ok -->` marker for that shape; add one only when no rationale
  is reachable at all). Narrowing this back to "whole paragraph only" would
  let an unjustified sibling hide behind a justified one; widening it to "next
  N paragraphs" would start pulling in unrelated prose.
- **`footgun-drift`'s file scope is `git diff --name-only <range> -- '*.md'` —
  not the doc-graph ignore layers, and not the leaks git-tracking scope
  either.** Any `.md` file the diff touches is in scope, including a
  `.claude/**` skill file that `orphans`/`broken`/`untracked` exclude as
  non-documentation: a footgun declaration added inside agent tooling is just
  as undocumented as one in `CLAUDE.md`, so narrowing to the doc-graph roots
  would blind the check to exactly the files most likely to accumulate
  unjustified footgun notes over time. Do not apply `defaultIgnores` or
  `.docauditignore` here.
- **The rationale vocabulary is a built-in Go constant
  (`footgunRationaleSignals` in `internal/audit/footguns.go`), not a config
  file.** Unlike `leaks`, these words aren't secret and don't vary per repo,
  because they're a fixed set of English connectives rather than an owner's
  private footprint, so a global TOML file would add indirection with no
  privacy payoff. Anchor to the symbol — don't restate the phrase list here,
  since a restated copy would drift out of sync the next time the constant
  changes.

## Doc models (why `--skip` exists)

Repos fall into models the orphan check treats differently:
- **A — prose-linked**: entry docs link/mention through `docs/`. Orphans are
  real. Enforce every check (the default).
- **B — nav-driven MkDocs**: `docs/` with no `nav:` block; MkDocs auto-builds
  the sidebar, pages never cross-link → every page is a prose-orphan *by design*.
  Run with `--skip orphans`.
- **C — flat reference `docs/`**: design notes referenced by path. `mentionsPath`
  makes these reachable; genuine orphans that remain are real gaps worth linking.

A repo that doesn't use the footgun-with-rationale convention at all skips
`footgun-drift` entirely rather than passing `--skip` (it isn't a `docaudit .`
check to skip): set `DOCAUDIT_FOOTGUN_OFF=1`, or generate the hook with
`install-hook --no-footgun-drift` so it's never invoked in the first place.

## Roots

Auto = tracked ones of `{CLAUDE.md, README.md, AGENTS.md, docs/index.md}` +
`--root` additions. Unifies "whole doc repo" and "project with CLAUDE.md +
docs/" with zero config.

## Layout & commands

- `main.go` — thin CLI: flags, `run(args, stdout, stderr) int` (the four
  whole-state checks), `runFootgunDrift(args, stdout, stderr) int` (the
  diff-scoped subcommand), report format.
- `internal/audit/` — `links.go` (parse/resolve), `ignore.go` (`**` globs),
  `git.go` (`ls-files` wrappers **plus** the diff helpers `changedMarkdown`/
  `addedLines`/`fileAtRev`/`ClosestBase` that `footgun_drift.go` uses to read a
  range instead of a tree snapshot), `leaks.go` (TOML config decode +
  dir-scoped content scan), `footguns.go` (the declaration scanner —
  `scanDeclarations`/`isFootgunDeclaration`/`footgunRationaleSignals`; a
  *declaration* is a footgun being introduced, not a passing mention of one),
  `footgun_drift.go` (`FootgunDrift`: runs `scanDeclarations` per range,
  keeps only declarations whose line is in that range's added-line set,
  dedupes by file:line), `audit.go` (`Audit` → `Report`, the whole-state
  orchestrator; unrelated to `FootgunDrift`).
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

## Footgun — the gate must find its own binary under a minimal PATH <!-- footgun-ok: section title, not a claim -->

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
