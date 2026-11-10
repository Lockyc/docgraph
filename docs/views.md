---
type: reference
description: The read-only views — covers, index, stale and graph — that query the doc graph without gating.
links:
  - rel: part-of
    to: README.md
---

# Read-only views: `covers`, `index`, `stale`, `graph`

Four subcommands that query the doc graph for a human or an agent tool. All are
**read-only**: never write, never gate (not `--skip`-able, not run by the
generated hook), always exit `0` on success — `2` only on a usage/git error.

```bash
docgraph covers <path>               # docs that cover <path> (repo-root-relative)
docgraph index [--for <file>]        # generated markdown index of the doc graph
docgraph stale                       # docs past their threshold, or verified before their covered code changed
docgraph stale --older-than 90       # override the default 180-day threshold
docgraph graph                       # both doc graphs as human-readable markdown
docgraph graph --json                # ...or as a stable JSON payload (machine seam)
```

- **`covers <path>`** — prints every tracked doc that documents `<path>` via a
  frontmatter `covers` edge, directly or by covering a parent directory
  (`covers: src/auth/` covers `src/auth/login.go`). `<path>` is
  **repo-root-relative**. Prints nothing (exit `0`) if no doc covers it. The same
  `covers` edges feed covers drift (`doc-drift` at Stop, `covers-drift` at push —
  [drift.md](drift.md)), so declaring one both answers this query and gets the doc
  surfaced when its code changes.
- **`index`** — prints a **generated** markdown index: every doc with
  frontmatter, grouped by `type` (core types in canonical order, then custom
  types alphabetically), each as `- [label](path) — description` (the tail is
  omitted when a doc has no `description`). The label is the doc's `title` if
  set, else its **body H1**, else its path; set `title` only when the index label
  should differ from the H1. It's a view, not a hand-maintained
  page — redirect it into a tracked file and regenerate when the graph changes:
  `docgraph index --for docs/index.md > docs/index.md`. `--for <file>` writes
  each link relative to that file's directory, opens the page with a
  `type: index` frontmatter block and leaves the file out of its own listing, so
  the result passes the gate (it still needs a doc→doc edge, e.g. a `part-of`
  pointing at it, or `disconnected` flags it).
- **`stale [--older-than <days>]`** — prints every doc whose `verified` date is
  past its staleness threshold: `docs/old.md (verified 2026-01-01 — 195d old,
  threshold 180d)`. The threshold is `--older-than` (default **180**) unless the
  doc's own `review:` cadence (e.g. `review: 90d`) overrides it. It also prints
  every doc verified before the code it `covers` last changed, whatever its
  age: `docs/auth.md (verified 2026-03-01 — covered code changed 2026-03-10:
  src/auth.go)`. That is a commit on a later day than `verified` touching a
  covered path (prose files under a covered directory don't count). A doc with no
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
