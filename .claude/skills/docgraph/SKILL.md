---
name: docgraph
description: Use when you need to find which documentation governs a file or subsystem before changing it, when asked "where are the docs for X" / "which doc covers this" / "is there a doc for this code", when reconciling docs after a code change, or when auditing a repo's doc graph (orphans, broken links, stale docs, doc index). Wraps the docgraph CLI's read-only views — covers, index, stale — and its audit gate.
---

# docgraph — finding the doc that governs code

`docgraph` answers **"which doc do I read before changing this file?"** from a
declared graph, not from grepping. Reach for it before editing unfamiliar code
and after changing code, to find the docs that need reconciling.

## Why not just grep

Grepping mentions does not answer this question. In one real repo `tools/hl` is
*mentioned* by ~100 docs and *governed* by exactly one; in another,
`app/Enums/Features.php` is mentioned by nine docs and owned only by
`docs/feature-flags.md`. `covers` returns the owner. It also answers for files
**no doc names at all** — a `covers: tools/nextdns-admin/` edge covers every
file under it, so `covers tools/nextdns-admin/main.go` resolves even though no
doc mentions that file.

## The commands

All three are read-only, never gate, and exit 0 even when they print nothing.

```bash
docgraph covers <path>    # which docs govern <path> — REPO-ROOT-RELATIVE
docgraph index            # generated markdown index of the doc graph
docgraph stale            # docs past their freshness threshold
docgraph stale --older-than 90
```

**`covers <path>` takes a repo-root-relative path**, unlike every markdown link
you've seen — frontmatter edges resolve against the repo root, not the source
doc's directory. `covers app/Models/Plant.php`, never `../app/Models/Plant.php`.

Zero, one, or several docs may govern a path. Several is normal and not a bug:
a policy doc and a mechanics runbook can both legitimately own one script.

## Reading an empty result

**Empty output means "no doc declares this", NOT "docgraph is broken".** A repo
only answers `covers` if its docs carry `covers:` frontmatter edges — many don't
yet. Before concluding a path is undocumented, check whether the repo
participates at all:

```bash
docgraph index | head     # empty/bare "# Index" => this repo has no frontmatter
```

If the repo has no frontmatter, `covers` cannot answer and grep is your
fallback. Do not report "there is no doc for X" on the strength of an empty
`covers` in a repo that never declared any edges.

## Declaring an edge

A doc claims what it governs in its own frontmatter:

```yaml
---
type: architecture
links:
  - rel: covers
    to: app/Models/Plant.php
  - rel: covers
    to: app/Enums/PlantCategory.php
---
```

`to:` is repo-root-relative; a trailing `/` covers everything beneath it. The
pre-push gate existence-checks every target, so a moved file fails the push
rather than rotting silently.

**Declare ownership, not mention.** A doc covers a path if it is where you'd go
to understand or safely change that file. It does *not* cover a shared primitive
it merely references — that belongs to the primitive's own register. A wrong
edge misdirects the next agent and is worse than a missing one; when unsure,
leave it out.

## The audit gate

`docgraph .` runs six whole-state checks (orphans, broken links, untracked,
leaks, frontmatter, edges) and exits non-zero on a finding — it's the pre-push
gate, and `docgraph doc-drift` is a Stop hook. Both are wired by
`/docgraph:install`; you rarely invoke them by hand.

`docgraph schema` emits the frontmatter JSON Schema — use it instead of
re-encoding the vocabulary if you're building something that reads the graph.

## If the binary is missing

`go install github.com/lockyc/docgraph@latest` (it lands in `~/go/bin`, which
may not be on a minimal PATH — try `~/go/bin/docgraph` before concluding it
isn't installed). Nothing auto-updates it.
