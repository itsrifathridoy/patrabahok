#!/usr/bin/env bash
# Lints the Go CLI/API module. Run locally or from CI (.github/workflows/lint.yml).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../cli"

command -v go >/dev/null 2>&1 || {
  echo "go is not installed." >&2
  exit 1
}

echo "== gofmt =="
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "The following files are not gofmt-formatted:" >&2
  echo "$unformatted" >&2
  exit 1
fi

echo "== go vet =="
go vet ./...

echo "== go build =="
CGO_ENABLED=0 go build ./...
