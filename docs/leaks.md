---
type: reference
description: The leaks content scan — its scope, the leaks.toml rules and groups, failure handling, and exporting rules for history scrubbing.
links:
  - rel: part-of
    to: README.md
  - rel: see-also
    to: docs/checks.md
---

# `leaks` — the content scan

Scans **tracked file content** (working tree only, never git history; for a
tracked symlink, the link's target path rather than the file it points to) for
secret / owner-specific strings, to catch them before a repo goes public. Runs by
default; `--skip leaks` turns it off.

It is the only check with a noticeable cost, roughly proportional to tracked
text: ~1s on a 15k-file repo holding ~200 MB of text, scanned across every core.
Binary files are skipped. A file that scanned clean is cached in
`$XDG_CACHE_HOME/docgraph/leaks/` (default `~/.cache`) and skipped next run
while its content, its path, the leaks config and the docgraph binary are all
unchanged, so a repeat push scans only what changed. `DOCGRAPH_NO_CACHE=1`
scans everything.

Scope is governed by **git tracking**, not the doc-graph ignore layers: every
`git ls-files` entry is scanned (so `.gitignore` decides what's excluded), and
`defaultIgnores` / `.docgraphignore` do **not** narrow it — only an explicit
`--ignore` glob does. Tracked `.claude/` files are scanned, even though the
doc-graph checks skip them.

## The rules file

Patterns come from a **global TOML file, never committed to a repo**. Resolution:
`--leaks-config <path>` → `$DOCGRAPH_LEAKS` →
`$XDG_CONFIG_HOME/docgraph/leaks.toml` (default `~/.config/docgraph/leaks.toml`).

| Level | Key | Default | What it does |
|---|---|---|---|
| top | `terms` | `[]` | Literal deny strings (the `default` group) |
| top | `regex` | `[]` | Regexp deny patterns (the `default` group) |
| top | `allow` | `[]` | Literal strings that suppress a deny match, everywhere |
| top | `allow_regex` | `[]` | Regexps that suppress a deny match, everywhere |
| `[[group]]` | `name` | *required* | Named deny list; unique, never `default` |
| `[[group]]` | `terms` / `regex` | `[]` | The group's deny rules (at least one required) |
| `[[dir]]` | `path` | *required* | Absolute directory the exceptions apply under (`~/` expanded) |
| `[[dir]]` | `ignore` | `[]` | Globs (relative to `path`) whose files skip the silenced groups |
| `[[dir]]` | `ignore_groups` | `["default"]` | Which groups `ignore` silences (replaces the default) |
| `[[dir]]` | `allow` / `allow_regex` | `[]` | Allow rules scoped to this subtree |

`terms` match literally and case-insensitively; `regex` / `allow_regex` are Go
regexps, also case-insensitive unless you opt out per-pattern with a leading
`(?-i)`. A non-absolute `[[dir]].path` is a fatal config error.

    terms       = ["acme-host", "you@example.com", "/Users/you"]
    regex       = ['10\.0\.0\.\d+']
    allow       = ["github.com/you"]
    allow_regex = ['com\.example\.[a-z]+']

    [[dir]]
    path   = "/abs/path/to/repo"
    ignore = ["vendor/*.json"]        # skip vendored specs
    [[dir]]
    path  = "/abs/path/to/repo/sub"
    allow = ["some-project"]          # legit in this subtree

Deny rules fall into **groups**. Top-level `terms` / `regex` are the implicit
`default` group; a `[[group]]` block is a named deny list. A `[[dir]].ignore`
glob silences the `default` group unless `ignore_groups` names other groups.
`ignore_groups` *replaces* that default rather than adding to it, so list
`"default"` too if you want it silenced as well. This lets a private repo ignore
its own footprint vocabulary while still being scanned for terms that must not
appear in *any* repo:

    regex = ['(?-i)AKIA[0-9A-Z]{16}']   # default group: secret shapes

    [[group]]
    name  = "footprint"
    terms = ["acme-host", "/Users/you"]

    [[group]]
    name  = "client"
    terms = ["big-client"]

    [[dir]]
    path          = "/abs/path/to/private-repo"
    ignore        = ["**"]
    ignore_groups = ["footprint"]     # footprint is fine here; client + secrets stay live

`default` is reserved for the top-level rules. These are all fatal config errors:
a `[[group]]` named `default`; a group with no name, a duplicate name, or no
terms/regex; an `ignore_groups` naming an undefined group; an `ignore_groups`
with no `ignore` globs.
`allow` / `allow_regex` are not group-scoped: naming the string suppresses it
whatever group it belongs to, which is the per-repo escape hatch for a class the
repo legitimately owns.

**The config is the sole source of rules — no hidden built-ins.** Generic secret
shapes (PEM, AWS, GitHub, Slack) are just `regex` entries you add, with a leading
`(?-i)` to keep them case-sensitive:

    regex = [
      '(?-i)-----BEGIN [A-Z ]*PRIVATE KEY-----',
      '(?-i)AKIA[0-9A-Z]{16}',
      '(?-i)ghp_[A-Za-z0-9]{36}',
      '(?-i)xox[baprs]-[A-Za-z0-9-]{10,}',
    ]

Because the config is machine-local, CI and fresh clones (no file) have no rules
and the scan is a no-op there — it stays a **local** pre-push gate. Handling:

- **No config file** → no rules, scans nothing, prints a warning. **Not** fatal.
- **Malformed config** (bad TOML, an unknown key, bad regex, a non-absolute
  `[[dir]]` path, a `[[group]]` with no terms or regex) → exit 2, fail-closed.
  `--skip leaks`
  bypasses config loading entirely, so it's the escape hatch if a config trips
  this path — fix the config rather than leaning on the skip, since it also
  turns off the check.

**Known gaps:** no git-history *detection* (use a history scanner such as gitleaks);
*scrubbing* a known leak from history is `docgraph leaks-rules` below. No
per-rule messages.

## `docgraph leaks-rules` — export rules for history scrubbing

The `leaks` check scans the current tree. To scrub a leak already in **git
history**, export the vocabulary as a
[`git-filter-repo`](https://github.com/newren/git-filter-repo) rules file and run
the rewrite separately:

```sh
docgraph leaks-rules > rules.txt          # non-destructive: reads only the config
git filter-repo --replace-text rules.txt  # destructive: rewrites history
```

It emits one `regex:` line per deny rule, including every `[[group]]`'s rules
(terms are escaped and case-insensitive; `regex` entries are case-insensitive,
except those starting with `(?-i)`, which are emitted as plain case-sensitive
patterns), using filter-repo's `***REMOVED***` replacement. A
stderr summary reports any `allow` / `allow_regex` / `[[dir]]` rules it
**dropped** (filter-repo can't apply them). Emitted patterns target filter-repo's Python regex
engine, so RE2-only syntax may need manual review.

> The rewrite changes commit SHAs: force-push and have every collaborator
> re-clone.
