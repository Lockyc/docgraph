---
type: reference
description: Opt-in, machine-local usage logging — the config, the record shape and the three detail levels.
links:
  - rel: part-of
    to: README.md
---

# Usage logging

docgraph can append one JSON line per run to a local log, for usage and finding
trends over time across all your repos. It is **opt-in** and machine-local: off
unless a global `config.toml` enables it, so CI, fresh clones, and contributors
never log.

`doc-drift` logs too (`"cmd":"doc-drift"`), on a Stop that blocks and on one that
sees a doc an earlier covers nag in the session named since changed — never on a
quiet Stop. Its counts are `dangling`, `value`, `covers` (docs nagged),
`covers_pointed` (of those, docs the nag pointed at lines in) and `covers_acted`
(nagged docs since changed); level 2 adds the doc paths. `covers_acted` against
`covers` is how often a covers nag leads to a doc edit.

Enable it in `~/.config/docgraph/config.toml` (resolved `--config` →
`$DOCGRAPH_CONFIG` → `$XDG_CONFIG_HOME/docgraph/config.toml`):

```toml
[log]
enabled = true
level   = 1                                   # 1 counts · 2 +paths · 3 +findings
# path  = "~/.local/state/docgraph/usage.jsonl"   # optional; this is the default
```

| Key | Default | What it does |
|---|---|---|
| `[log].enabled` | `false` | Turn logging on |
| `[log].level` | `1` | Detail tier, 1–3 (below) |
| `[log].path` | `$XDG_STATE_HOME/docgraph/usage.jsonl` | Where records are appended |

Records land in `$XDG_STATE_HOME/docgraph/usage.jsonl` (default
`~/.local/state/docgraph/usage.jsonl`), overridable via `[log].path` or
`DOCGRAPH_LOG`. A level-1 record:

```json
{"ts":"2026-07-09T21:30:00+10:00","version":"3.0.0","repo":"/abs/git/root",
 "cmd":"run","checks":["broken","disconnected","edges","frontmatter","leaks","orphans","untracked"],
 "exit":1,
 "counts":{"broken":1,"disconnected":0,"edges":0,"frontmatter":0,"leaks":0,"orphans":0,"untracked":0}}
```

**Detail level** trades richness for exposure:

- **1 — counts only.** No paths, no content. Safe default.
- **2 — adds `files`.** Flagged paths (broken/leaks include `file:line`), but
  **never leak match text.**
- **3 — adds `findings`.** Full detail **including leak match strings** — this
  turns the log into exactly the sensitive-string sink `leaks` exists to prevent.
  Use only on a trusted machine.

Notes:

- **Absent config → silently off** (the normal state). A **malformed**
  `config.toml` warns and disables logging but **does not fail the run** (unlike a
  malformed `leaks.toml`, which is fatal).
- `DOCGRAPH_NO_LOG=1` disables logging for one run even when the config enables it.
- Best-effort: an unwritable log file is silently skipped and never changes the
  exit code.
