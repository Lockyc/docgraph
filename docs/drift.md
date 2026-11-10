---
type: reference
description: The three drift modes — footgun-drift and covers-drift (advisory pre-push riders) and doc-drift (the blocking Stop-hook gate).
links:
  - rel: part-of
    to: README.md
  - rel: see-also
    to: docs/views.md
---

# Drift checks

The whole-state checks look at the tree as it is. These three look at what just changed.

## `footgun-drift` — the advisory pre-push rider

A *footgun* is a documented trap: a wrong move that looks right. `footgun-drift` scans only what
a push **adds** to tracked markdown, and it is **advisory**: it prints a nag but exits 0 and
never blocks. It flags any *added* line that declares a footgun: a line starting with
`Footgun:`, or a bolded footgun lead-in mid-line. Mentioning or
cross-referencing a footgun, or a bare `## Footguns` heading, doesn't count.

```bash
docgraph footgun-drift                       # reads git's pre-push ref lines from stdin
docgraph footgun-drift --range base..head    # explicit range, for manual use
```

With no `--range` it reads the pushed refs from the `pre-push` hook's stdin (a new
branch is compared against its merge-base with the nearest integration branch).
Only added lines count, so a declaration already on the remote is never
re-flagged. File scope is every `.md` the diff
touches, including `.claude/` skill files that the doc-graph checks exclude.

It is a **nag, not a judge**: it reports every added declaration. On a finding it asks two
questions — (1) is this a real footgun (a trap you hit, a tempting-but-wrong approach, a
re-litigated decision)? (2) is it at the right doc level (invariant → `CLAUDE.md`, rationale →
`docs/`, human prose → `README`)? It also names the commonest false footgun: a bug you just
fixed, whose story belongs in the commit message while the doc keeps at most the surviving
rule. Fix or leave it in a follow-up; nothing was blocked.

`DOCGRAPH_FOOTGUN_OFF=1` silences it outright (for a repo that doesn't declare footguns);
`docgraph install-hook --no-footgun-drift` generates a hook that never invokes it. There is no
in-file suppression marker.

## `covers-drift` — the advisory pre-push rider

`covers-drift` scans the **code** a push changes and reports any doc that
declares a frontmatter `covers` edge onto it while the doc itself went
untouched. Like `footgun-drift` it is **advisory**: it prints a nag, exits 0, and
never blocks.

```bash
docgraph covers-drift                       # reads git's pre-push ref lines from stdin
docgraph covers-drift --range base..head    # explicit range, for manual use
```

The same check runs at the end of each agent turn inside `doc-drift` (below), scoped to that
session's changes; this rider is the push-time pass over the whole pushed range.

With no `--range` it reads the pushed refs from the `pre-push` hook's stdin,
deduping findings across refs. Docs are `.md` only (unlike `doc-drift`, which also
reads `.mdx`); "code" is every other tracked path except the prose formats
`.txt`/`.rst`/`.adoc`/`.markdown`. A `covers` edge declared in an `.mdx` file
never fires.

Each finding lists the doc's lines whose code (an inline code span, or a fenced
block) names an identifier the change added or removed, a definition it changed,
or the function a change sits in — up to 8 (`maxPointedLines`), leaving out an
identifier on more than 3 of the doc's lines (`maxIdentLines`), which is its
subject rather than one claim. A doc with none says so.

**A still-accurate doc wants no edit, and the nag says so**; a falsified one is
corrected or cut, nothing more. A doc the push also touched never fires. A repo with no
`covers` edges never sees it. To turn it off outright: `DOCGRAPH_COVERS_OFF=1`, or
`docgraph install-hook --no-covers-drift` to generate a hook that never invokes it.

Riders are written into `.githooks/pre-push` at hook-generation time;
regenerate with `docgraph install-hook --force` to pick up new ones.

## `doc-drift` — the Stop-hook staleness gate

Wire `doc-drift` into your agent harness's `Stop` hook (invoked directly, no
wrapper) so it runs at the end of every turn, catching tracked docs that still
describe code which just changed underneath them — before the agent hands control
back.

It scans a **working-tree-inclusive** range — base→worktree, covering committed
*and* uncommitted changes.
On a feature branch the range starts at the closest integration branch's
merge-base (the whole branch so far). On a trunk branch it covers only this agent
session's changes: commits since the agent's previous turn ended (its last Stop),
plus the working tree; without a session (a manual run), the working tree alone. It flags
two mechanical staleness classes, and both **block** the turn from ending:

1. **Dangling reference** — a symbol whose *definition* was removed in the diff
   and survives nowhere else in tracked code, but a tracked doc still names it.
2. **Anchored value drift** — a constant whose numeric *value* changed while a
   tracked doc still names the constant **and** shows the old literal.

```bash
docgraph doc-drift                        # bare: resolves the base itself, applies the loop-guard
docgraph doc-drift --range base..head     # explicit range — bypasses the loop-guard
```

Bare invocation applies a **once-per-finding loop-guard**: a finding blocks the
first time it appears, then stays silent at that `HEAD` and every later one. Any
finding that wasn't in the previous nag blocks the turn, and that block lists all
current findings. A finding that is fixed and later comes back counts as new.

A finding in either class **blocks** — prints to **stderr**, exits **2**.

It also raises **covers drift**: code this session changed that a doc declares it
`covers`, where the session didn't touch the doc. The change set is the paths
this session edited or committed since its previous Stop (or since it started),
plus the working tree — reading a file, merging a trunk in, or another session's
commits never count. It reads the Stop payload on stdin; without one it
checks the working tree alone, and `--range` checks that spec. A doc touched
anywhere in the set clears the finding. Each (doc, path) pair blocks once per
session; the message points at the doc lines to check first, as `covers-drift`
does, and asks the agent to fix only what is now false. `DOCGRAPH_COVERS_OFF=1` turns
off covers drift.
`DOC_DRIFT_OFF=1` disables the whole subcommand outright, for a repo whose docs
don't name code symbols alongside their values. Paths the
doc-graph ignore layers exclude (defaults + `.docgraphignore`) are out of scope on
both sides — neither scanned as code nor grepped as docs. There is no
other suppression surface (no per-finding flag, no inline
marker): a flagged reference is a judgment call — fix the doc, or confirm the
mention deliberately describes past behaviour. The fix the message asks for is the smallest edit
that makes the doc true again (swap the value, rename the symbol, or delete the
sentence that now describes nothing), never an account of what changed.

Scope limits worth knowing:

- **Docs are `.md`/`.mdx`; "code" is everything else** except the prose formats
  `.txt`/`.rst`/`.adoc`/`.markdown`.
- **Anchored value drift is not proximity-checked** — it fires when a
  symbol-naming doc *also* contains the old literal anywhere in the file, so it
  can over-report if that number appears coincidentally.
- **`--range` evaluates against the current working tree**, so a dirty tree can
  change the verdict for the same range. Bare Stop-hook mode diffs base→worktree
  and is self-consistent.
- It only catches those two mechanical classes — a paraphrased value or a
  reversed decision with no anchored symbol needs a semantic doc sweep.
