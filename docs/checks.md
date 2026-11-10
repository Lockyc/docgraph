---
type: reference
description: The seven whole-state checks, the frontmatter schema, entry points, ignoring paths, and which doc models need --skip.
links:
  - rel: part-of
    to: README.md
---

# Whole-state checks

Seven checks run by default on `docgraph [path]`. **Everything is enforced; you
exclude explicitly** with `--skip <check[,check]>`; a check added in a later
version is enforced automatically. (An auto-nav MkDocs repo, for instance, runs `--skip
orphans`.)

**Scope.** The whole-state doc-graph checks audit *documents* (`docs/`,
`CLAUDE.md`, config-dir READMEs) — not content a site framework routes and
renders (an Astro/MkDocs content collection, a seed-data corpus); exclude those
per-repo via `.docgraphignore`. The `leaks` check is broader — it scans *all*
tracked files (see [leaks.md](leaks.md)).

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
   [leaks.md](leaks.md)).
6. **Frontmatter** — every doc except `README.md` (matched by basename) must
   carry a leading YAML block (first line exactly `---` to the next `---`) that is
   well-formed YAML and declares a `type`. A missing block, malformed YAML, or a
   missing `type` is a finding; `README.md` is exempt (see *Where to put
   frontmatter* below). `type` is an advisory vocabulary — see `docgraph schema`
   below.
7. **Edges** — every entry in a frontmatter `links:` list must carry a `rel`
   and a `to`; every internal `to` target (a repo-root-relative `.md` doc or
   code path) must exist; and `part-of` /
   `supersedes` edges between docs must not form a cycle. External URLs and
   `owner/repo:...` cross-repo targets are never checked.

## `docgraph schema` — the frontmatter vocabulary

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

## Entry points (roots)

Reachability starts from whichever of `CLAUDE.md`, `README.md`, `AGENTS.md` (repo
root) and `docs/index.md` are tracked, plus any `--root` you add. This covers both
a whole doc repo and a project whose docs are `CLAUDE.md` + `docs/`.

## Ignoring paths

`**/superpowers/**` (the scratch files of an agent plugin that writes plans into the
repo), `.claude/**`, and `.agents/**` are ignored by default for the
doc-graph checks and by `doc-drift`. Add more via `.docgraphignore` (one glob per
line, `#` comments) or repeatable `--ignore` globs. These are repo-root-relative globs, not
gitignore syntax: `*` and `?` match within one path segment, `**` matches any number of
segments, and there is no negation — exclude a directory with `dir/**`, and a file at any
depth with `**/name.md`. The leak scan honors only `--ignore`,
not the default/`.docgraphignore` layers — see [leaks.md](leaks.md).

**No inline markers.** Every suppression lives in config or on the command line —
`.docgraphignore`, `--ignore`, `--skip`, and the leaks config's `allow` /
`allow_regex`, and a `[[dir]]`'s `ignore` + `ignore_groups`. docgraph never reads a
suppression comment inside the audited files. `footgun-drift`, `covers-drift`
and `doc-drift` have no in-file escape at all — they're opted out only
whole-check, via `DOCGRAPH_FOOTGUN_OFF=1` /
`--no-footgun-drift`, `DOCGRAPH_COVERS_OFF=1` / `--no-covers-drift`, and
`DOC_DRIFT_OFF=1` respectively.

## Doc models and when to `--skip orphans`

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

A repo that doesn't declare footguns in its docs opts out of `footgun-drift`
outright (it's not a check, so no `--skip` name) — `DOCGRAPH_FOOTGUN_OFF=1` or
`install-hook --no-footgun-drift`. `covers-drift` has the same pair
(`DOCGRAPH_COVERS_OFF=1` / `install-hook --no-covers-drift`), though a repo with
no `covers` edges has nothing to turn off.
