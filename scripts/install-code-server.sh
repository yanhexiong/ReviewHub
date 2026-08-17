#!/usr/bin/env bash
set -euo pipefail

# Install the pinned, independent code-server release into this repository.
# The application never falls back to a host VS Code, VS Code Remote Server,
# ~/.vscode-server, or /tmp binaries: it only uses this vendored instance or
# an explicit PAPER_REVIEW_VSCODE_SERVER_BIN override.
CODE_SERVER_VERSION="4.96.4"
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
install_root="${REVIEW_HUB_CODE_SERVER_DIR:-${repository_root}/vendor/code-server}"
archive_root="$(mktemp -d "${TMPDIR:-/tmp}/review-hub-code-server.XXXXXX")"
trap 'rm -rf -- "$archive_root"' EXIT

if [[ "$(basename "$install_root")" != code-server* ]]; then
  echo "REVIEW_HUB_CODE_SERVER_DIR must point to a code-server installation directory; received: ${install_root}" >&2
  exit 1
fi

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Required command is unavailable: $1" >&2
    exit 1
  }
}

code_server_version() {
  local binary="$1"
  # code-server may write a first-run config message before its version.
  # Select the standalone semantic-version line instead of assuming ordering.
  "$binary" --version 2>/dev/null \
    | sed -nE 's/^([0-9]+\.[0-9]+\.[0-9]+)([[:space:]].*)?$/\1/p' \
    | sed -n '1p' \
    || true
}

if [[ -x "${install_root}/bin/code-server" ]]; then
  installed="$(code_server_version "${install_root}/bin/code-server")"
  if [[ "$installed" == *"${CODE_SERVER_VERSION}"* ]]; then
    echo "Pinned code-server ${CODE_SERVER_VERSION} is already installed: ${install_root}"
    exit 0
  fi
  echo "Installed code-server version (${installed:-unknown}) does not match; reinstalling ${CODE_SERVER_VERSION}" >&2
  rm -rf -- "$install_root"
fi

require_command curl
require_command tar
case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "Unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

package="${archive_root}/code-server-${CODE_SERVER_VERSION}-linux-${arch}.tar.gz"
url="https://github.com/coder/code-server/releases/download/v${CODE_SERVER_VERSION}/code-server-${CODE_SERVER_VERSION}-linux-${arch}.tar.gz"
echo "Downloading pinned code-server ${CODE_SERVER_VERSION} -> ${install_root}"
curl --compressed --fail --location --silent --show-error \
  --retry 5 --retry-delay 3 --retry-max-time 180 \
  --connect-timeout 20 --max-time 300 \
  --output "$package" "$url"
tar -xzf "$package" -C "$archive_root"
mkdir -p "$(dirname "$install_root")"
mv "${archive_root}/code-server-${CODE_SERVER_VERSION}-linux-${arch}" "$install_root"
echo "Installed pinned code-server ${CODE_SERVER_VERSION}: ${install_root}"
