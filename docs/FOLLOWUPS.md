---
type: reference
---

# Follow-ups

Wanted work that is deferred, not out of scope. (Genuinely out-of-scope
non-goals live in the README's *Known gaps* — don't mix them: a gap says "we
won't", this file says "not yet".)

## Generated index as an *embedded section*, not only a standalone page

`docgraph index` emits a whole page (`# Index`, `## <type>` groups) intended to
be redirected into a tracked file. That shape cannot single-source the common
real case: **a hand-written doc whose index is one section among prose.**

The driving case (Locus, 2026-07): its `CLAUDE.md` carries a *Reference
Documentation* section listing all 39 `docs/*.md` with a description each. It is
the same shadow as homelab's `tools.md` *Docs* column — a hand-maintained copy
of a relation the graph already holds, which nothing validates and which rots
the first time a doc is added or renamed. But it can't be fixed the same way
tools.md was:

- The section lives **inside** `CLAUDE.md`, surrounded by prose that must stay.
  `index > docs/index.md` would produce a *second* page, not replace the
  section — so the shadow would remain, now with a rival.
- Its per-doc descriptions are **richer than a one-line `description:`** — some
  are a paragraph carrying real guidance ("Read before touching `sortHierarchy`
  …"). Migrating them into frontmatter YAML makes prose that is edited often
  harder to edit, which is a real cost, not a purity nit.

So the shadow stands in Locus, knowingly, until this is resolved. Two candidate
shapes, neither chosen:

1. **Managed region** — `index --into <file> --marker <name>` rewrites only the
   text between two sentinel comments, leaving the rest of the file untouched.
   Fixes the embedding problem; still needs the descriptions in frontmatter.
2. **Leave the prose in the doc** — generate only the *structure* (which docs
   exist, grouped by type) and let each entry's description stay hand-written.
   Cheaper, but only single-sources half the relation, so the list can still
   drift in exactly the way that matters least (descriptions) and not at all in
   the way that matters most (membership).

The prerequisite for either is that a repo's docs carry `type` (Locus's now do).
Decide the shape before building — a half-generated section that still needs
hand-editing may be worse than an honest hand-written one.
