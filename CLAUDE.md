# docaudit — notes for the next agent

A Go CLI that audits a repo's **agent-facing documentation graph**: orphans
(tracked `.md` unreachable by link-following from the roots), broken internal
`.md` links, and untracked `.md` files. Exits non-zero on any finding. Stdlib
only; shells out to `git`. Usage and checks are in `README.md` — this file
carries the invariants and footguns.

## Homelab back-link

This tool guards documentation for the LSJC homelab and other repos. Its
intended production use is homelab's **pre-push** hook — see the homelab docs
repo (`lachlan/homelab`, `docs/runbooks/script-linting.md`) for the wiring.

## What it is (and is not)

- **Agent-facing, not human-facing.** It measures the graph an agent traverses
  (grep + `[x](y.md)`), *not* whether a human can reach a page.
- **It never reads code.** Input is the doc link graph only.

## Footguns

- **Measures prose-link reachability on purpose — NOT MkDocs nav.** A MkDocs
  site with no `nav:` block auto-builds its sidebar from the file tree, so every
  page is trivially reachable *for a human*. That is not what this tool checks:
  an agent doesn't read the sidebar. Do **not** "fix" orphan detection to defer
  to MkDocs nav — it would make the tool always report zero orphans and destroy
  its purpose.
- **Do NOT merge this with `doc-drift.sh`.** That Stop hook is a *content-vs-code*
  check (a changed constant whose old literal lingers in a doc; a deleted symbol
  still referenced) — its input is the code diff. `docaudit` is *graph
  integrity* — its input is the doc link graph, and it never touches code.
  Different inputs, different concerns; keep them as two tools.
- **Code-block links are skipped deliberately.** `extractLinks` ignores fenced
  (```` ``` ````/`~~~`) and inline (`` `...` ``) code so template/example paths
  in docs don't register as real links. Removing this resurrects false-positive
  broken links (e.g. a `[docs](services/name.md)` template row). This was a real
  false positive caught on the first homelab run — the skip is load-bearing.

## Roots

Auto = tracked ones of `{CLAUDE.md, README.md, AGENTS.md, docs/index.md}` +
`--root` additions. Unifies "whole doc repo" and "project with CLAUDE.md +
docs/" with zero config.

## Layout & commands

- `main.go` — thin CLI: flags, `run(args, stdout, stderr) int`, report format.
- `internal/audit/` — `links.go` (parse/resolve), `ignore.go` (`**` globs),
  `git.go` (`ls-files` wrappers), `audit.go` (`Audit` → `Report`).
- `just test` / `just build` / `just install`. Tests build throwaway git repos
  in temp dirs, so `git` must be on PATH.

## v1 gaps (documented, not silent)

Anchor validity, external-URL liveness, raw `<a href>`, per-section `index.md`
implicit-nav, repo-specific conventions. Add only with a test and a README note.
