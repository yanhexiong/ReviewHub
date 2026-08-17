#!/usr/bin/env bash
set -euo pipefail

# Keep browser tests isolated from a developer's real Review Hub configuration.
test_root="$(mktemp -d "${TMPDIR:-/tmp}/review-hub-e2e.XXXXXX")"
export XDG_CONFIG_HOME="$test_root/config"
mkdir -p "$XDG_CONFIG_HOME/review-hub"
printf '{\n  "version": 1,\n  "dataDirectory": "%s",\n  "goApiListen": "127.0.0.1:39100",\n  "listenHost": "0.0.0.0",\n  "listenPort": 3000,\n  "defaults": {\n    "allowedRoots": ["%s"],\n    "maxPdfBytes": 104857600,\n    "maxImportBytes": 1073741824,\n    "maxProjectsPerUser": 20,\n    "maxUsers": 1000\n  }\n}\n' \
  "$test_root/data" "$PWD" > "$XDG_CONFIG_HOME/review-hub/runtime-config.json"

api_pid=""
api_binary=""
cleanup() {
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT INT TERM

if [[ -n "${REVIEW_HUB_GO_API_BIN:-}" ]]; then
  npm run dev
  exit $?
fi

go_binary="$(command -v go 2>/dev/null || true)"
if [[ -z "$go_binary" && -x "/usr/local/go/bin/go" ]]; then
  go_binary="/usr/local/go/bin/go"
fi
if [[ -z "$go_binary" ]]; then
  echo "Go 1.24 is required to run the end-to-end test API." >&2
  exit 1
fi

api_binary="$test_root/review-hub-api"
(
  cd backend
  "$go_binary" build -trimpath -o "$api_binary" ./cmd/review-hub-api
)
"$api_binary" &
api_pid=$!
npm run dev
