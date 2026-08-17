#!/usr/bin/env bash
set -euo pipefail

# Keep these versions pinned. LaTeX Workshop 10.13+ has a PDF.js regression
# in the web viewer used by this workspace, so do not replace this with the
# latest Marketplace release without a compatibility check.
LATEX_WORKSHOP_VERSION="10.12.0"
LATEX_WORKSHOP_SHA256="3130907e14653f297c0a96888c88a44bf4240df2fad0037d66440ffe3f2928d7"
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
config_home="${XDG_CONFIG_HOME:-${HOME:-}/.config}"
runtime_config="${config_home}/review-hub/runtime-config.json"
node_binary="${REVIEW_HUB_NODE_BIN:-$(command -v node 2>/dev/null || true)}"
data_root="${REVIEW_HUB_RUNTIME_DATA_DIR:-}"
if [[ -z "$data_root" && -f "$runtime_config" ]]; then
  if [[ -z "$node_binary" ]]; then
    data_root="$(sed -nE 's/^[[:space:]]*"dataDirectory"[[:space:]]*:[[:space:]]*"([^"\\]+)"[[:space:]]*,?[[:space:]]*$/\1/p' "$runtime_config" | head -n 1)"
  else
    data_root="$("$node_binary" -e '
    const fs = require("node:fs");
    const path = require("node:path");
    const file = process.argv[1];
    try {
      const config = JSON.parse(fs.readFileSync(file, "utf8"));
      if (typeof config.dataDirectory !== "string" || !path.isAbsolute(config.dataDirectory)) process.exit(1);
      process.stdout.write(config.dataDirectory);
    } catch {
      process.exit(1);
    }
  ' "$runtime_config")" || {
    echo "Runtime configuration is invalid; extension directory cannot be resolved." >&2
    exit 1
    }
  fi
fi
data_root="${data_root:-${repository_root}/data}"
extension_root="${REVIEW_HUB_VSCODE_EXTENSIONS_DIR:-${data_root}/vscode-extensions}"
download_root=""

code_server_directory="${REVIEW_HUB_CODE_SERVER_DIR:-${data_root}/code-server}"
if [[ -z "$node_binary" && -x "${code_server_directory}/lib/node" ]]; then
  node_binary="${code_server_directory}/lib/node"
elif [[ -z "$node_binary" && -x "${code_server_directory}/lib/node/bin/node" ]]; then
  # Keep compatibility with alternate code-server distributions.
  node_binary="${code_server_directory}/lib/node/bin/node"
fi
if [[ -z "$node_binary" ]]; then
  echo "Required command is unavailable: node (install code-server first or set REVIEW_HUB_NODE_BIN)" >&2
  exit 1
fi

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Required command is unavailable: $1" >&2
    exit 1
  }
}

extension_matches() {
  local package_json="$1"
  local expected_id="$2"
  local expected_version="$3"
  "$node_binary" -e '
    const fs = require("node:fs");
    const [file, expectedId, expectedVersion] = process.argv.slice(1);
    try {
      const packageJson = JSON.parse(fs.readFileSync(file, "utf8"));
      const id = `${packageJson.publisher ?? ""}.${packageJson.name ?? ""}`.toLowerCase();
      process.exit(id === expectedId.toLowerCase() && packageJson.version === expectedVersion ? 0 : 1);
    } catch {
      process.exit(1);
    }
  ' "$package_json" "$expected_id" "$expected_version"
}

extension_is_ready() {
  local target_path="$1"
  local extension_id="$2"
  local version="$3"
  [[ -f "${target_path}/package.json" ]] || return 1
  extension_matches "${target_path}/package.json" "$extension_id" "$version"
}

download_pinned_extension() {
  local package_path="$1"
  local extension_id="$2"
  local expected_sha256="$3"
  shift 3
  local url actual_sha256

  for url in "$@"; do
    rm -f -- "$package_path"
    echo "Downloading ${extension_id} from ${url}"
    if curl --compressed --fail --location --silent --show-error \
      --retry 5 --retry-delay 3 --retry-max-time 180 \
      --connect-timeout 20 --max-time 300 \
      --output "$package_path" "$url"; then
      actual_sha256="$(sha256sum "$package_path" | awk '{print $1}')"
      if [[ "$actual_sha256" == "$expected_sha256" ]]; then
        return 0
      fi
      echo "Downloaded package checksum does not match ${extension_id}; trying the next source." >&2
    else
      echo "Download failed for ${extension_id}; trying the next source." >&2
    fi
  done

  rm -f -- "$package_path"
  echo "Unable to download the pinned extension ${extension_id} from Open VSX." >&2
  exit 1
}

ensure_latex_workspace_activation() {
  local target_path="$1"
  local package_json="${target_path}/package.json"
  [[ -f "$package_json" ]] || return 1
  "$node_binary" -e '
    const fs = require("node:fs");
    const file = process.argv[1];
    const event = "workspaceContains:**/*.tex";
    const packageJson = JSON.parse(fs.readFileSync(file, "utf8"));
    const activationEvents = Array.isArray(packageJson.activationEvents)
      ? packageJson.activationEvents
      : [];
    if (!activationEvents.includes(event)) {
      packageJson.activationEvents = [...activationEvents, event];
      fs.writeFileSync(file, `${JSON.stringify(packageJson, null, 2)}\n`, { mode: 0o600 });
      console.log("Enabled TeX workspace activation for LaTeX Workshop");
    }
  ' "$package_json"
}

install_extension() {
  local publisher="$1"
  local open_vsx_namespace="$2"
  local name="$3"
  local version="$4"
  local expected_sha256="$5"
  local extension_id="${publisher}.${name}"
  local package_path="${download_root}/${extension_id}-${version}.vsix"
  local staging_path="${download_root}/${extension_id}-${version}"
  local target_path="${extension_root}/${extension_id}-${version}"
  local file_name="${open_vsx_namespace}.${name}-${version}.vsix"
  local api_url="https://open-vsx.org/api/${open_vsx_namespace}/${name}/${version}/file/${file_name}"
  local cdn_url="https://openvsx.eclipsecontent.org/${open_vsx_namespace}/${name}/${version}/${file_name}"

  if extension_is_ready "$target_path" "$extension_id" "$version"; then
    if [[ "$extension_id" == "james-yu.latex-workshop" ]]; then
      ensure_latex_workspace_activation "$target_path"
    fi
    echo "Extension already installed: ${extension_id}@${version}; skipping download"
    return
  fi

  mkdir -p "$staging_path" "$extension_root"
  download_pinned_extension \
    "$package_path" "$extension_id" "$expected_sha256" \
    "$api_url" "$cdn_url"
  unzip -q "$package_path" -d "$staging_path"
  if [[ ! -f "${staging_path}/extension/package.json" ]]; then
    echo "Extension package is missing package.json: ${extension_id}@${version}" >&2
    exit 1
  fi
  if ! extension_matches "${staging_path}/extension/package.json" "$extension_id" "$version"; then
    echo "Extension package ID or version does not match: ${extension_id}@${version}" >&2
    exit 1
  fi
  if [[ -e "$target_path" ]]; then
    mv "$target_path" "${target_path}.previous.$(date +%s%N)"
  fi
  mv "${staging_path}/extension" "$target_path"
  if [[ "$extension_id" == "james-yu.latex-workshop" ]]; then
    ensure_latex_workspace_activation "$target_path"
  fi
  echo "Installed ${extension_id}@${version} -> ${target_path}"
}

check_extensions() {
  local failed=0
  local extension_id version target_path
  for extension_id in "james-yu.latex-workshop"; do
    version="$LATEX_WORKSHOP_VERSION"
    target_path="${extension_root}/${extension_id}-${version}"
    if extension_is_ready "$target_path" "$extension_id" "$version"; then
      echo "Verified ${extension_id}@${version}"
    else
      echo "Extension is not installed: ${extension_id}@${version} (${target_path})" >&2
      failed=1
    fi
  done
  return "$failed"
}

[[ -x "$node_binary" ]] || {
  echo "Required Node.js binary is unavailable: ${node_binary}" >&2
  exit 1
}
if [[ "${1:-}" == "--check" ]]; then
  check_extensions
  echo "VS Code built-in support: vscode.git, vscode.markdown-language-features, vscode.markdown-basics, vscode.markdown-math"
  exit 0
fi
require_command curl
require_command unzip
require_command sha256sum
download_root="$(mktemp -d "${TMPDIR:-/tmp}/review-hub-vscode-extensions.XXXXXX")"
trap 'rm -rf -- "$download_root"' EXIT

install_extension \
  "james-yu" "James-Yu" "latex-workshop" \
  "$LATEX_WORKSHOP_VERSION" "$LATEX_WORKSHOP_SHA256"
echo "VS Code built-in support: vscode.git, vscode.markdown-language-features, vscode.markdown-basics, vscode.markdown-math"
echo "Start VS Code Server with --extensions-dir set to: ${extension_root}"
