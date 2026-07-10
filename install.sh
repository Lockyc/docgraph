#!/usr/bin/env bash
# install.sh — installs/updates the docaudit binary via `go install`.
# Usage:  bash install.sh
#    or:  curl -fsSL https://raw.githubusercontent.com/lockyc/docaudit/main/install.sh | bash
#
# The curl URL requires the GitHub repo to be PUBLIC (docaudit is).
#
# docaudit is a Go CLI, not a bundle — this installs a BINARY to the Go bin dir; there
# is no ~/.docaudit clone to manage. It seeds the global config dir but never edits
# ~/.claude/settings.json or any repo's git hook — that guided wiring is /docaudit:install's
# job (a non-interactive curl|bash must not silently edit global config). Never uses `just`.
set -e

MODULE="github.com/lockyc/docaudit"

command -v go >/dev/null 2>&1 || {
  echo "docaudit: Go is required (https://go.dev/dl/). Install it, then re-run." >&2
  exit 1
}
command -v git >/dev/null 2>&1 || \
  echo "docaudit: warning — git not found on PATH; docaudit shells out to git at runtime." >&2

# IN_REPO when run from a checkout: build the current tree so uncommitted work is installed.
# NOT_IN_REPO: install the published module at @latest.
if [ -f go.mod ] && grep -q "^module $MODULE\$" go.mod 2>/dev/null && [ -f main.go ]; then
  echo "Installing docaudit from the current checkout (go install .) ..."
  go install .
else
  echo "Installing docaudit from $MODULE@latest ..."
  go install "$MODULE@latest"
fi

# Resolve the Go bin dir the binary landed in.
BIN_DIR="$(go env GOBIN)"
[ -n "$BIN_DIR" ] || BIN_DIR="$(go env GOPATH)/bin"
[ -n "$BIN_DIR" ] || BIN_DIR="$HOME/go/bin"

# Seed the global config dir (never overwrite existing rules/config).
CFG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/docaudit"
mkdir -p "$CFG_DIR"

# `docaudit version` prints "docaudit <ver>"; keep just the version token.
VERSION="$("$BIN_DIR/docaudit" version 2>/dev/null | awk '{print $NF}' || echo "")"

echo ""
echo "docaudit${VERSION:+ v$VERSION} installed → $BIN_DIR/docaudit"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "  warning — $BIN_DIR is not on your PATH; add it so 'docaudit' resolves." >&2 ;;
esac
echo ""
echo "Next steps:"
echo "  • Guided setup (Claude Code): /docaudit:install"
echo "      wires the doc-drift Stop hook into ~/.claude/settings.json, offers the"
echo "      per-repo pre-push gate, and seeds the leaks config."
echo "  • Pre-push gate for a repo:   run 'docaudit install-hook' inside it."
echo "  • doc-drift Stop hook (manual): add '$BIN_DIR/docaudit doc-drift' to the"
echo "      Stop hooks array in ~/.claude/settings.json."
echo "  • Leak rules are a global file you curate: $CFG_DIR/leaks.toml"
echo "      (schema + 'docaudit leaks-rules' export — see the README leaks section)."
