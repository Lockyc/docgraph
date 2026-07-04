# default: list recipes
default:
    @just --list

build:
    go build -o docaudit .

test:
    go test ./...

install:
    go install .

fmt:
    gofmt -w .

# non-mutating pre-release gate: gofmt check + vet + tests
gate:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
      echo "✗ gofmt: these files need formatting:" >&2
      echo "$unformatted" >&2
      exit 1
    fi
    go vet ./...
    go test ./...
    echo "✓ gate passed"

# cut the release for the current VERSION: fast-forward main → tag v<VERSION> → GitHub release.
# run on dev with VERSION bumped and committed; the tree must be clean and gate-green.
release:
    #!/usr/bin/env bash
    set -euo pipefail
    version="$(tr -d '[:space:]' < VERSION)"
    tag="v${version}"
    if [ -n "$(git status --porcelain)" ]; then
      echo "✗ working tree is dirty — commit the VERSION bump first" >&2
      exit 1
    fi
    if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
      echo "✗ tag ${tag} already exists — bump VERSION before releasing" >&2
      exit 1
    fi
    just gate
    git push origin dev
    # main only fast-forwards to the release commit; it never diverges from dev.
    git branch -f main dev
    git push origin main
    git tag -a "${tag}" -m "${tag}" main
    git push origin "${tag}"
    gh release create "${tag}" --target main --title "${tag}" --generate-notes
    echo "✓ released ${tag}"
