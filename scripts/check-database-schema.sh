#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
go_bin="$(command -v go || true)"
if [[ -z "$go_bin" && -x /usr/local/go/bin/go ]]; then
  go_bin=/usr/local/go/bin/go
fi
if [[ -z "$go_bin" ]]; then
  echo "Go 1.24 is required for the database schema check" >&2
  exit 1
fi
export PATH="$(dirname -- "$go_bin"):$PATH"

cd "$repository_root/backend"
test -z "$(gofmt -d internal/repository internal/httpapi)"
go test ./internal/repository ./internal/httpapi
