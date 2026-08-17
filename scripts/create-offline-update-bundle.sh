#!/usr/bin/env bash
set -euo pipefail

appimage=""
version=""
output=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --appimage)
      appimage="${2:?missing AppImage path}"
      shift 2
      ;;
    --version)
      version="${2:?missing release version}"
      shift 2
      ;;
    --output)
      output="${2:?missing output path}"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

[[ -f "$appimage" && -x "$appimage" ]] || {
  echo "AppImage input is missing or not executable" >&2
  exit 1
}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]] || {
  echo "Release version must use vX.Y.Z format, optionally with a pre-release suffix" >&2
  exit 1
}
[[ -n "$output" ]] || {
  echo "Output path is required" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Required command is unavailable: $1" >&2
    exit 1
  }
}

require_command sha256sum
require_command stat
require_command tar

normalized_version="${version#v}"
appimage_name="ReviewHub-${version}-x86_64.AppImage"
expected_name="$(basename -- "$appimage")"
[[ "$expected_name" == "$appimage_name" ]] || {
  echo "AppImage name must be $appimage_name" >&2
  exit 1
}

temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/review-hub-update.XXXXXX")"
cleanup() {
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

digest="$(sha256sum "$appimage" | awk '{print $1}')"
size="$(stat --printf='%s' "$appimage")"
[[ "$digest" =~ ^[a-f0-9]{64}$ && "$size" -gt 0 ]] || {
  echo "Could not calculate AppImage metadata" >&2
  exit 1
}

cp -- "$appimage" "$temporary_directory/$appimage_name"
printf '{\n  "format": "review-hub-offline-update/v1",\n  "version": "%s",\n  "platform": "linux",\n  "architecture": "x86_64",\n  "appImage": "%s",\n  "sha256": "%s",\n  "size": %s\n}\n' \
  "$normalized_version" "$appimage_name" "$digest" "$size" \
  > "$temporary_directory/manifest.json"

mkdir -p "$(dirname -- "$output")"
tar -C "$temporary_directory" -czf "$output" manifest.json "$appimage_name"
[[ -s "$output" ]] || {
  echo "Offline update bundle was not created" >&2
  exit 1
}
printf '%s\n' "$output"
