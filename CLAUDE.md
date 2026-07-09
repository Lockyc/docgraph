# docaudit — notes for the next agent

A Go CLI that audits a repo's **agent-facing documentation graph**: orphans
(tracked `docs/` files unreachable from the roots), broken internal `.md` links,
and untracked `.md` files. Reachability follows markdown links **and** bare/
inline-code path mentions. Exits non-zero on a finding in a selected check
(`--checks`, default all three). Stdlib only; shells out to `git`. Usage and
checks are in `README.md` — this file carries the invariants and footguns.

## Intended use

docaudit is built to run as a **pre-push documentation gate** (and in CI): it
exits non-zero on a finding so a broken doc-graph blocks the push without a
wrapper. `docaudit install-hook` writes a tracked `.githooks/pre-push` for that.

## What it is (and is not)

- **Agent-facing, not human-facing.** It measures the graph an agent traverses
  (grep + `[x](y.md)`), *not* whether a human can reach a page.
- **Reads doc-graph structure and, for the `leaks` check, file content.** The
  three doc-graph checks (orphans/broken/untracked) traverse only the link graph.
  The opt-in `leaks` check additionally scans tracked file *content* (code
  included) for configured leak patterns. It never reads git history.

## Footguns

- **Measures prose-link reachability on purpose — NOT MkDocs nav.** A MkDocs
  site with no `nav:` block auto-builds its sidebar from the file tree, so every
  page is trivially reachable *for a human*. That is not what this tool checks:
  an agent doesn't read the sidebar. Do **not** "fix" orphan detection to defer
  to MkDocs nav — it would make the tool always report zero orphans and destroy
  its purpose.
- **Do NOT merge this with `doc-drift.sh`.** That Stop hook is a *content-vs-code*
  drift check driven by the code diff (a changed constant whose old literal lingers
  in a doc). `docaudit` audits *repo state* — doc-graph integrity plus, opt-in, a
  content leak scan — not diffs. Different inputs and cadence; keep them separate.
- **`leaks` rules live in a GLOBAL file, never in the repo — on purpose.** A
  per-repo deny list committed to a public repo *is itself the leak* (it
  enumerates every sensitive term the owner has). The footprint vocabulary is
  also identical across repos. So `leaks` reads `--leaks-config` →
  `$DOCAUDIT_LEAKS` → `os.UserConfigDir()/docaudit/leaks`, and selecting `leaks`
  with no resolvable file is **fail-closed (exit 2)** — a silent skip would be a
  false green. Built-in secret patterns (PEM/AWS/GitHub/Slack shapes) always run
  and are suppressible by `!` allow lines. History is out of scope (owner's call);
  that stays with the manual `pre-public-leak-audit` skill.
- **`leaks` scope is git tracking, not the doc-graph ignore layers.** `LeakScan`
  scans every file `git ls-files` returns — so `.gitignore` governs what's
  in-scope — and honors only the explicit `--ignore` CLI globs as a per-run
  escape hatch. It does **not** apply `defaultIgnores` or `.docauditignore`: a
  tracked file ships publicly regardless of the doc-graph scope, so a tracked
  `.claude/` config (excluded from orphans/broken/untracked because it isn't
  documentation) is exactly where owner-specific strings hide and must stay
  in-scope for the leak pass.
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

## Doc models (why `--checks` exists)

Repos fall into models the orphan check treats differently:
- **A — prose-linked**: entry docs link/mention through `docs/`. Orphans are
  real. Gate all three checks.
- **B — nav-driven MkDocs**: `docs/` with no `nav:` block; MkDocs auto-builds
  the sidebar, pages never cross-link → every page is a prose-orphan *by design*.
  Gate `--checks broken,untracked` only.
- **C — flat reference `docs/`**: design notes referenced by path. `mentionsPath`
  makes these reachable; genuine orphans that remain are real gaps worth linking.

## Roots

Auto = tracked ones of `{CLAUDE.md, README.md, AGENTS.md, docs/index.md}` +
`--root` additions. Unifies "whole doc repo" and "project with CLAUDE.md +
docs/" with zero config.

## Layout & commands

- `main.go` — thin CLI: flags, `run(args, stdout, stderr) int`, report format.
- `internal/audit/` — `links.go` (parse/resolve), `ignore.go` (`**` globs),
  `git.go` (`ls-files` wrappers), `leaks.go` (leak rules parse + content scan),
  `audit.go` (`Audit` → `Report`).
- `just test` / `just build` / `just install`. Tests build throwaway git repos
  in temp dirs, so `git` must be on PATH.
- **Self-deploy hooks**: tracked `.githooks/post-commit` + `post-merge` both run
  `go install .` (activate with `git config core.hooksPath .githooks`), so a
  local checkout keeps `~/go/bin/docaudit` current without remembering
  `just install`. A stale binary means the gate runs old logic, so the auto-install
  matters when docaudit gates its own pushes.

## Branching & releases

- **`main` + `dev`.** `dev` is the integration trunk; `main` is the release branch —
  it only fast-forwards to a tagged release commit and stays a clean ancestor of `dev`.
  Never commit directly to `main` (drift breaks the fast-forward; fix by back-merging
  `main` into `dev`, never force-push). Feature/fix branches off `dev`.
- **Semver, `v`-prefixed tags.** The tracked root **`VERSION`** file is the single source
  of truth, `go:embed`-ed via `version.go` so `docaudit version` (also `--version`, `-v`)
  self-reports — never restate the version elsewhere. Consumers `go install …@latest`, so
  a release moves everyone's pinned tool: keep `main` releasable and bump minor for a new
  feature, patch for a fix.
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
