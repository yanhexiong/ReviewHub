#!/usr/bin/env bash
set -euo pipefail

# Build a compact Linux x86_64 AppImage. The browser is a static Next.js
# export; Node.js, node_modules, code-server, and extensions stay outside the
# immutable image and are installed into the persistent data directory.
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
release_version="${REVIEW_HUB_RELEASE_VERSION:?Set REVIEW_HUB_RELEASE_VERSION to vX.Y.Z}"
appimagetool_bin="${APPIMAGETOOL_BIN:?Set APPIMAGETOOL_BIN to the verified appimagetool executable}"
updater_binary="${REVIEW_HUB_UPDATER_BIN:?Set REVIEW_HUB_UPDATER_BIN to the static review-hub-updater executable}"
api_binary="${REVIEW_HUB_GO_API_BIN:?Set REVIEW_HUB_GO_API_BIN to the static review-hub-api executable}"
output_dir="${APPIMAGE_OUTPUT_DIR:-${repository_root}/dist}"

require_file() {
  [[ -e "$1" ]] || {
    echo "Missing required release input: $1" >&2
    exit 1
  }
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required release command: $1" >&2
    exit 1
  }
}

[[ "$release_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]] || {
  echo "REVIEW_HUB_RELEASE_VERSION must use vX.Y.Z format" >&2
  exit 1
}
require_file "$appimagetool_bin"
require_file "$updater_binary"
require_file "$api_binary"
require_file "$repository_root/out/index.html"
require_file "$repository_root/out/_next"
require_file "$repository_root/node_modules/monaco-editor/min/vs/loader.js"
require_file "$repository_root/node_modules/pdfjs-dist/build/pdf.worker.min.js"
require_file "$repository_root/package.json"
require_file "$repository_root/packaging/linux/AppRun"
require_file "$repository_root/packaging/linux/review-hub.desktop"
require_file "$repository_root/packaging/linux/icons/review-hub-16.svg"
require_file "$repository_root/packaging/linux/icons/review-hub-32.svg"
require_file "$repository_root/packaging/linux/icons/review-hub-48.svg"
require_file "$repository_root/packaging/linux/icons/review-hub-64.svg"
require_file "$repository_root/scripts/install-code-server.sh"
require_file "$repository_root/scripts/install-vscode-extensions.sh"
require_command git
require_command node
require_command cp
require_command install

case "$(uname -m)" in
  x86_64|amd64) ;;
  *)
    echo "AppImage release only supports Linux x86_64, received $(uname -m)" >&2
    exit 1
    ;;
esac

package_version="$(node -p "require('${repository_root}/package.json').version")"
[[ "$release_version" == "v$package_version" ]] || {
  echo "Release tag $release_version does not match package.json $package_version" >&2
  exit 1
}

app_dir="$(mktemp -d "${TMPDIR:-/tmp}/review-hub-appimage.XXXXXX")"
cleanup() {
  rm -rf -- "$app_dir"
}
trap cleanup EXIT

app_root="$app_dir/usr/lib/review-hub"
mkdir -p \
  "$app_root/bin" \
  "$app_root/out" \
  "$app_root/static/editor-assets" \
  "$app_root/install" \
  "$app_dir/usr/share/icons/hicolor/16x16/apps" \
  "$app_dir/usr/share/icons/hicolor/32x32/apps" \
  "$app_dir/usr/share/icons/hicolor/48x48/apps" \
  "$app_dir/usr/share/icons/hicolor/64x64/apps"

cp -a "$repository_root/out/." "$app_root/out/"
cp -a "$repository_root/node_modules/monaco-editor/min/vs/." \
  "$app_root/static/editor-assets/"
install -m 644 "$repository_root/node_modules/pdfjs-dist/build/pdf.worker.min.js" \
  "$app_root/static/pdf-worker.js"
install -m 755 "$updater_binary" "$app_root/bin/review-hub-updater"
install -m 755 "$api_binary" "$app_root/bin/review-hub-api"
install -m 755 "$repository_root/scripts/install-code-server.sh" \
  "$app_root/install/install-code-server.sh"
install -m 755 "$repository_root/scripts/install-vscode-extensions.sh" \
  "$app_root/install/install-vscode-extensions.sh"

install -m 755 "$repository_root/packaging/linux/AppRun" "$app_dir/AppRun"
install -m 644 "$repository_root/packaging/linux/review-hub.desktop" \
  "$app_dir/review-hub.desktop"
install -m 644 "$repository_root/packaging/linux/icons/review-hub-16.svg" \
  "$app_dir/usr/share/icons/hicolor/16x16/apps/review-hub.svg"
install -m 644 "$repository_root/packaging/linux/icons/review-hub-32.svg" \
  "$app_dir/usr/share/icons/hicolor/32x32/apps/review-hub.svg"
install -m 644 "$repository_root/packaging/linux/icons/review-hub-48.svg" \
  "$app_dir/usr/share/icons/hicolor/48x48/apps/review-hub.svg"
install -m 644 "$repository_root/packaging/linux/icons/review-hub-64.svg" \
  "$app_dir/usr/share/icons/hicolor/64x64/apps/review-hub.svg"
install -m 644 "$repository_root/packaging/linux/icons/review-hub-64.svg" \
  "$app_dir/review-hub.svg"
ln -s "review-hub.svg" "$app_dir/.DirIcon"

cat > "$app_root/RELEASE-METADATA.txt" <<EOF
Review Hub release: $release_version
Source commit: $(git -C "$repository_root" rev-parse HEAD)
Build target: Linux x86_64
Node.js build version: $(node --version)
Frontend: static Next.js export
Runtime editor: installed by AppRun --install-runtime
LaTeX Workshop: installed by AppRun --install-runtime
Update helper: static Go binary
API service: static Go binary

This AppImage contains no Node.js runtime, node_modules, code-server,
extensions, user data, database, PDF, project workspace, access token, SSH
key, or environment file. AppRun initializes persistent runtime data outside
the read-only image.
EOF

if find "$app_root" -type f \( -name '*.db' -o -name '*.sqlite' -o -name '*.pdf' -o -name '.env.local' \) -print -quit | grep -q .; then
  echo "Refusing to package user runtime data" >&2
  exit 1
fi
for forbidden in node node_modules vendor/code-server vscode-extensions .next; do
  if [[ -e "$app_root/$forbidden" ]]; then
    echo "Refusing to package forbidden runtime path: $app_root/$forbidden" >&2
    exit 1
  fi
done

mkdir -p "$output_dir"
output_file="$output_dir/ReviewHub-${release_version}-x86_64.AppImage"
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "$appimagetool_bin" "$app_dir" "$output_file"
[[ -x "$output_file" ]] || {
  echo "appimagetool did not create an executable AppImage" >&2
  exit 1
}
printf '%s\n' "$output_file"
