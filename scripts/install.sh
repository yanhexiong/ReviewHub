#!/usr/bin/env bash
# Review Hub one-click AppImage installer.
#
# Usage examples:
#   curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh | sudo bash -s -- install --version v0.1.0-beta.1
#
# The installer downloads a verified AppImage from GitHub Releases. It does
# not install Node.js, Go, npm, TeX, or a host VS Code instance.

set -Eeuo pipefail

readonly GITHUB_REPOSITORY="yanhexiong/ReviewHub"
readonly INSTALL_ROOT="/opt/review-hub"
readonly APPIMAGE_PATH="${INSTALL_ROOT}/review-hub.AppImage"
readonly VERSION_PATH="${INSTALL_ROOT}/VERSION"
readonly APPIMAGE_BACKUP="${APPIMAGE_PATH}.backup"
readonly VERSION_BACKUP="${VERSION_PATH}.backup"
readonly STATE_ROOT="/var/lib/review-hub"
readonly CONFIG_HOME="${STATE_ROOT}/config"
readonly DEFAULT_DATA_HOME="${STATE_ROOT}/data"
readonly CONFIG_ROOT="/etc/review-hub"
readonly SERVICE_NAME="review-hub"
readonly SERVICE_USER="review-hub"
readonly SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
readonly MANAGEMENT_COMMAND="/usr/local/sbin/review-hub"
readonly DATA_DIRECTORY_STATE="${CONFIG_ROOT}/data-directory"
readonly HEALTH_URL="http://127.0.0.1:3000/api/system/health"
readonly VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$'

force_yes=false
purge=false
requested_version=""
requested_data_directory=""
data_directory=""
download_directory=""
downloaded_appimage=""

cleanup() {
  if [[ -n "${download_directory}" && -d "${download_directory}" ]]; then
    rm -rf -- "${download_directory}"
  fi
}
trap cleanup EXIT

info() {
  printf '[INFO] %s\n' "$*"
}

success() {
  printf '[OK] %s\n' "$*"
}

warning() {
  printf '[WARNING] %s\n' "$*" >&2
}

error() {
  printf '[ERROR] %s\n' "$*" >&2
}

usage() {
  cat <<'EOF'
Review Hub AppImage installer

Usage:
  install.sh [install] [--version VERSION]
  install.sh upgrade [--version VERSION]
  install.sh rollback
  install.sh list-versions
  install.sh status
  install.sh logs
  install.sh restart
  install.sh uninstall [--yes] [--purge]

Commands:
  install          Install the latest stable release, or a specified version.
  upgrade          Upgrade to the latest stable release, or a specified version.
  rollback         Restore the previous AppImage kept by the last upgrade.
  list-versions    List recent GitHub Release tags, including pre-releases.
  status           Show systemd service status.
  logs             Follow the service journal.
  restart          Restart the service and verify its health endpoint.
  uninstall        Remove the service and application; keep data by default.

Options:
  -v, --version VERSION  Install a tag such as v0.1.0-beta.1.
  --data-dir PATH         Choose the persistent data directory on first install.
  -y, --yes              Skip the uninstall confirmation prompt.
  --purge               Also remove /var/lib/review-hub and /etc/review-hub.
  -h, --help             Show this help.

Examples:
  install.sh install --version v0.1.0-beta.1
  install.sh upgrade
  install.sh rollback
  install.sh uninstall --purge --yes
EOF
}

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    error "This command must run as root. Use sudo."
    exit 1
  fi
}

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    error "Required command is unavailable: ${command_name}"
    exit 1
  fi
}

check_platform() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    error "Only Linux deployments are supported by this installer."
    exit 1
  fi
  case "$(uname -m)" in
    x86_64|amd64) ;;
    *)
      error "Only Linux x86_64 releases are currently published; detected $(uname -m)."
      exit 1
      ;;
  esac
}

check_dependencies() {
  local command_name
  for command_name in curl sha256sum systemctl install useradd runuser awk mktemp realpath tar unzip; do
    require_command "$command_name"
  done
}

normalize_data_directory() {
  local candidate="$1"
  if [[ -z "$candidate" || "$candidate" != /* ]]; then
    error "The data directory must be an absolute path."
    exit 2
  fi
  if [[ "$candidate" == *$'\n'* || "$candidate" == *$'\r'* || "$candidate" == *[[:space:]]* || "$candidate" == *'"'* || "$candidate" == *'\\'* ]]; then
    error "The data directory cannot contain whitespace, quotes, or backslashes."
    exit 2
  fi
  candidate="$(realpath -m -- "$candidate")"
  # The installer-owned default is a valid target even though it lives below
  # the service state directory. Other system paths remain prohibited.
  if [[ "$candidate" == "$DEFAULT_DATA_HOME" ]]; then
    printf '%s' "$candidate"
    return
  fi
  case "$candidate" in
    /|/etc|/etc/*|/usr|/usr/*|/bin|/bin/*|/sbin|/sbin/*|/opt|/opt/review-hub|/opt/review-hub/*|/var/lib/review-hub|/var/lib/review-hub/*)
      error "The data directory must be an application-owned or user-selected storage path."
      exit 2
      ;;
  esac
  printf '%s' "$candidate"
}

read_data_directory_from_terminal() {
  local prompt_fd=""
  local value

  # A curl pipe consumes stdin, but an administrator still has a controlling
  # terminal. Prefer that terminal so the first-install choice is not skipped.
  if [[ -t 0 ]]; then
    printf 'Persistent data directory [%s]: ' "$DEFAULT_DATA_HOME" >&2
    IFS= read -r value || return 1
  elif [[ -r /dev/tty && -w /dev/tty ]]; then
    exec {prompt_fd}<>/dev/tty
    printf 'Persistent data directory [%s]: ' "$DEFAULT_DATA_HOME" >&"$prompt_fd"
    IFS= read -r value <&"$prompt_fd" || {
      exec {prompt_fd}>&-
      return 1
    }
    exec {prompt_fd}>&-
  else
    return 1
  fi

  if [[ -z "$value" ]]; then
    value="$DEFAULT_DATA_HOME"
  fi
  printf '%s' "$value"
}

choose_interactive_data_directory() {
  local entered=""
  local normalized=""
  while :; do
    entered="$(read_data_directory_from_terminal || true)"
    [[ -n "$entered" ]] || return 1
    if normalized="$(normalize_data_directory "$entered" 2>/dev/null)"; then
      data_directory="$normalized"
      return 0
    fi
    warning "Invalid data directory. Enter an absolute path outside system directories."
  done
}

read_configured_data_directory() {
  local runtime_file="${CONFIG_HOME}/review-hub/runtime-config.json"
  local configured=""
  if [[ -f "$DATA_DIRECTORY_STATE" ]]; then
    configured="$(sed -n '1p' "$DATA_DIRECTORY_STATE")"
  elif [[ -f "$runtime_file" ]]; then
    configured="$(sed -nE 's/^[[:space:]]*"dataDirectory"[[:space:]]*:[[:space:]]*"([^"]+)"[[:space:]]*,?[[:space:]]*$/\1/p' "$runtime_file" | head -n 1)"
  fi
  if [[ -n "$configured" ]]; then
    normalize_data_directory "$configured"
  fi
}

choose_data_directory() {
  local configured=""
  configured="$(read_configured_data_directory || true)"
  if [[ -n "$configured" ]]; then
    if [[ -n "$requested_data_directory" ]]; then
      local requested
      requested="$(normalize_data_directory "$requested_data_directory")"
      if [[ "$requested" != "$configured" ]]; then
        error "The data directory is already configured as ${configured}; changing it during upgrade is not supported."
        info "Back up the data, stop Review Hub, move it, and update runtime-config.json before restarting."
        exit 1
      fi
    fi
    data_directory="$configured"
    return
  fi

  if [[ -n "$requested_data_directory" ]]; then
    data_directory="$(normalize_data_directory "$requested_data_directory")"
  else
    data_directory="$DEFAULT_DATA_HOME"
    if [[ ! -e "$APPIMAGE_PATH" ]]; then
      choose_interactive_data_directory || true
    fi
  fi
}

json_escape() {
  local value="$1"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/}
  value=${value//$'\r'/}
  printf '%s' "$value"
}

write_initial_runtime_config() {
  local runtime_directory="${CONFIG_HOME}/review-hub"
  local runtime_file="${runtime_directory}/runtime-config.json"
  local escaped_data
  [[ -e "$runtime_file" ]] && return
  escaped_data="$(json_escape "$data_directory")"
  install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0700 "$runtime_directory"
  umask 077
  cat > "${runtime_file}.new" <<EOF
{
  "version": 1,
  "dataDirectory": "${escaped_data}",
  "goApiListen": "127.0.0.1:39100",
  "listenHost": "0.0.0.0",
  "listenPort": 3000,
  "defaults": {
    "allowedRoots": ["${escaped_data}"],
    "maxPdfBytes": 104857600,
    "maxImportBytes": 1073741824,
    "maxProjectsPerUser": 20,
    "maxUsers": 1000
  }
}
EOF
  chown "$SERVICE_USER:$SERVICE_USER" "${runtime_file}.new"
  chmod 0600 "${runtime_file}.new"
  mv -f -- "${runtime_file}.new" "$runtime_file"
}

normalize_version() {
  local version="$1"
  [[ "$version" == v* ]] || version="v${version}"
  if [[ ! "$version" =~ $VERSION_PATTERN ]]; then
    error "Invalid version '${version}'. Expected vX.Y.Z, optionally with -alpha.N, -beta.N, or -rc.N."
    exit 1
  fi
  printf '%s' "$version"
}

github_api() {
  local endpoint="$1"
  curl --fail --silent --show-error --location --connect-timeout 10 --max-time 30 \
    "https://api.github.com/repos/${GITHUB_REPOSITORY}${endpoint}"
}

latest_version() {
  local tag
  tag="$(github_api '/releases/latest' | sed -nE 's/^[[:space:]]*"tag_name":[[:space:]]*"([^"]+)".*/\1/p' | head -n 1)"
  if [[ -z "$tag" ]]; then
    error "Could not determine the latest stable release from GitHub."
    exit 1
  fi
  normalize_version "$tag"
}

list_versions() {
  info "Fetching recent releases from GitHub."
  github_api '/releases?per_page=30' \
    | sed -nE 's/^[[:space:]]*"tag_name":[[:space:]]*"([^"]+)".*/\1/p' \
    | while IFS= read -r tag; do
        [[ "$tag" =~ $VERSION_PATTERN ]] && printf '%s\n' "$tag"
      done
}

release_exists() {
  local version="$1"
  local status
  status="$(curl --silent --show-error --location --connect-timeout 10 --max-time 30 \
    --output /dev/null --write-out '%{http_code}' \
    "https://api.github.com/repos/${GITHUB_REPOSITORY}/releases/tags/${version}")"
  [[ "$status" == "200" ]]
}

resolve_version() {
  if [[ -n "$requested_version" ]]; then
    requested_version="$(normalize_version "$requested_version")"
    if ! release_exists "$requested_version"; then
      error "GitHub Release does not exist: ${requested_version}"
      info "Use 'list-versions' to inspect available releases."
      exit 1
    fi
    printf '%s' "$requested_version"
    return
  fi
  latest_version
}

download_release() {
  local version="$1"
  local asset_name="ReviewHub-${version}-x86_64.AppImage"
  local release_url="https://github.com/${GITHUB_REPOSITORY}/releases/download/${version}"
  local checksum_file
  local expected_checksum

  download_directory="$(mktemp -d "${TMPDIR:-/tmp}/review-hub-install.XXXXXX")"
  downloaded_appimage="${download_directory}/${asset_name}"
  checksum_file="${download_directory}/SHA256SUMS"

  info "Downloading ${asset_name}."
  curl --fail --silent --show-error --location --retry 3 \
    --output "$downloaded_appimage" "${release_url}/${asset_name}"
  curl --fail --silent --show-error --location --retry 3 \
    --output "$checksum_file" "${release_url}/SHA256SUMS"

  expected_checksum="$(awk -v file="$asset_name" '$2 == file || $2 == "*" file { print $1; exit }' "$checksum_file")"
  if [[ ! "$expected_checksum" =~ ^[[:xdigit:]]{64}$ ]]; then
    error "SHA256SUMS does not contain a valid entry for ${asset_name}."
    exit 1
  fi

  info "Verifying SHA-256 checksum."
  (
    cd "$download_directory"
    printf '%s  %s\n' "$expected_checksum" "$asset_name" | sha256sum --check --status -
  ) || {
    error "Checksum verification failed for ${asset_name}."
    exit 1
  }
  chmod 0755 "$downloaded_appimage"
  success "Downloaded and verified ${version}."
}

ensure_service_user() {
  if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    useradd --system --home-dir "$STATE_ROOT" --shell /usr/sbin/nologin "$SERVICE_USER"
    info "Created system user ${SERVICE_USER}."
  fi
  install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0700 "$STATE_ROOT" "$CONFIG_HOME" "$data_directory"
  write_initial_runtime_config
  install -d -m 0755 "$CONFIG_ROOT"
  printf '%s\n' "$data_directory" > "${DATA_DIRECTORY_STATE}.new"
  chmod 0644 "${DATA_DIRECTORY_STATE}.new"
  mv -f -- "${DATA_DIRECTORY_STATE}.new" "$DATA_DIRECTORY_STATE"
}

write_service() {
  install -d -m 0755 "$CONFIG_ROOT" "$INSTALL_ROOT"
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Review Hub paper review service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${STATE_ROOT}
Environment=HOME=${STATE_ROOT}
Environment=XDG_CONFIG_HOME=${CONFIG_HOME}
Environment=XDG_DATA_HOME=${STATE_ROOT}/data
Environment=NODE_ENV=production
Environment=APPIMAGE_EXTRACT_AND_RUN=1
ExecStart=${APPIMAGE_PATH}
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${STATE_ROOT} ${data_directory}
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "$SERVICE_FILE"
  systemctl daemon-reload
}

install_management_command() {
  local command_directory
  command_directory="$(dirname -- "$MANAGEMENT_COMMAND")"
  install -d -m 0755 "$command_directory"
  cat > "${MANAGEMENT_COMMAND}.new" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

installer_url="https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh"
temporary_file="$(mktemp "${TMPDIR:-/tmp}/review-hub-installer.XXXXXX")"
cleanup() {
  rm -f -- "$temporary_file"
}
trap cleanup EXIT
curl --fail --silent --show-error --location --retry 3 --output "$temporary_file" "$installer_url"
bash "$temporary_file" "$@"
EOF
  chmod 0755 "${MANAGEMENT_COMMAND}.new"
  mv -f -- "${MANAGEMENT_COMMAND}.new" "$MANAGEMENT_COMMAND"
}

service_active() {
  systemctl is-active --quiet "$SERVICE_NAME"
}

stop_service() {
  if service_active; then
    info "Stopping ${SERVICE_NAME}."
    systemctl stop "$SERVICE_NAME"
  fi
}

wait_for_health() {
  local attempt
  for attempt in $(seq 1 60); do
    if curl --fail --silent --show-error --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

start_service() {
  info "Starting ${SERVICE_NAME}."
  systemctl enable "$SERVICE_NAME" >/dev/null
  systemctl restart "$SERVICE_NAME"
  if ! wait_for_health; then
    error "Review Hub did not pass its health check."
    systemctl --no-pager --full status "$SERVICE_NAME" >&2 || true
    return 1
  fi
  success "Review Hub is healthy at ${HEALTH_URL}."
}

install_runtime_dependencies() {
  info "Installing pinned code-server and LaTeX Workshop into ${data_directory}."
  runuser --user "$SERVICE_USER" -- env \
    HOME="$STATE_ROOT" \
    XDG_CONFIG_HOME="$CONFIG_HOME" \
    XDG_DATA_HOME="${STATE_ROOT}/data" \
    APPIMAGE="$APPIMAGE_PATH" \
    APPIMAGE_EXTRACT_AND_RUN=1 \
    "$APPIMAGE_PATH" --install-runtime
}

current_version() {
  if [[ -f "$VERSION_PATH" ]]; then
    sed -n '1p' "$VERSION_PATH"
  else
    printf 'not-installed'
  fi
}

restore_previous_release() {
  if [[ ! -f "$APPIMAGE_BACKUP" ]]; then
    return 1
  fi
  install -m 0755 "$APPIMAGE_BACKUP" "$APPIMAGE_PATH"
  if [[ -f "$VERSION_BACKUP" ]]; then
    install -m 0644 "$VERSION_BACKUP" "$VERSION_PATH"
  fi
  return 0
}

install_release() {
  local version="$1"
  local had_current=false

  choose_data_directory
  download_release "$version"
  ensure_service_user
  write_service
  [[ -f "$APPIMAGE_PATH" ]] && had_current=true
  stop_service

  if [[ "$had_current" == true ]]; then
    install -m 0755 "$APPIMAGE_PATH" "$APPIMAGE_BACKUP"
    [[ -f "$VERSION_PATH" ]] && install -m 0644 "$VERSION_PATH" "$VERSION_BACKUP"
  fi
  install -m 0755 "$downloaded_appimage" "${APPIMAGE_PATH}.new"
  mv -f -- "${APPIMAGE_PATH}.new" "$APPIMAGE_PATH"
  printf '%s\n' "$version" > "$VERSION_PATH"
  chmod 0644 "$VERSION_PATH"
  chown "$SERVICE_USER:$SERVICE_USER" "$STATE_ROOT" "$CONFIG_HOME" "$data_directory"

  if ! install_runtime_dependencies; then
    error "Runtime dependency installation failed; restoring the previous release."
    if [[ "$had_current" == true ]] && restore_previous_release; then
      start_service || true
      error "Previous release was restored."
    else
      error "No previous release was available to restore."
    fi
    exit 1
  fi

  if ! start_service; then
    error "The new release failed its health check; restoring the previous release."
    stop_service || true
    if [[ "$had_current" == true ]] && restore_previous_release; then
      start_service || true
      error "Previous release was restored."
    else
      error "No previous release was available to restore."
    fi
    exit 1
  fi
  install_management_command
  success "Installed Review Hub ${version}."
}

rollback_release() {
  require_root
  check_platform
  check_dependencies
  if [[ ! -f "$APPIMAGE_PATH" || ! -f "$APPIMAGE_BACKUP" ]]; then
    error "No local rollback release is available."
    exit 1
  fi

  local current_image_backup="${APPIMAGE_PATH}.rollback-current"
  local current_version_backup="${VERSION_PATH}.rollback-current"
  info "Rolling back from $(current_version)."
  stop_service
  install -m 0755 "$APPIMAGE_PATH" "$current_image_backup"
  install -m 0755 "$APPIMAGE_BACKUP" "$APPIMAGE_PATH"
  install -m 0755 "$current_image_backup" "$APPIMAGE_BACKUP"
  rm -f -- "$current_image_backup"
  if [[ -f "$VERSION_PATH" && -f "$VERSION_BACKUP" ]]; then
    install -m 0644 "$VERSION_PATH" "$current_version_backup"
    install -m 0644 "$VERSION_BACKUP" "$VERSION_PATH"
    install -m 0644 "$current_version_backup" "$VERSION_BACKUP"
    rm -f -- "$current_version_backup"
  fi

  if ! start_service; then
    error "Rollback failed its health check; restoring the previous active release."
    stop_service || true
    install -m 0755 "$APPIMAGE_PATH" "$current_image_backup"
    install -m 0755 "$APPIMAGE_BACKUP" "$APPIMAGE_PATH"
    install -m 0755 "$current_image_backup" "$APPIMAGE_BACKUP"
    rm -f -- "$current_image_backup"
    if [[ -f "$VERSION_PATH" && -f "$VERSION_BACKUP" ]]; then
      install -m 0644 "$VERSION_PATH" "$current_version_backup"
      install -m 0644 "$VERSION_BACKUP" "$VERSION_PATH"
      install -m 0644 "$current_version_backup" "$VERSION_BACKUP"
      rm -f -- "$current_version_backup"
    fi
    start_service || true
    exit 1
  fi
  success "Rollback completed; active version is $(current_version)."
}

confirm_uninstall() {
  if [[ "$force_yes" == true ]]; then
    return
  fi
  if [[ ! -t 0 ]]; then
    error "Non-interactive uninstall requires --yes."
    exit 1
  fi
  printf 'Remove Review Hub but keep application data? [y/N] '
  read -r answer
  [[ "$answer" =~ ^[Yy]$ ]] || {
    info "Uninstall cancelled."
    exit 0
  }
}

uninstall() {
  require_root
  check_dependencies
  confirm_uninstall
  systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
  rm -f -- "$SERVICE_FILE"
  systemctl daemon-reload
  rm -f -- "$MANAGEMENT_COMMAND"
  rm -rf -- "$INSTALL_ROOT" "$CONFIG_ROOT"
  userdel "$SERVICE_USER" >/dev/null 2>&1 || true
  if [[ "$purge" == true ]]; then
    rm -rf -- "$STATE_ROOT"
    success "Review Hub and all application data were removed."
  else
    success "Review Hub was removed; application data remains in ${STATE_ROOT}."
  fi
}

parse_arguments() {
  local positional=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -v|--version)
        [[ $# -ge 2 ]] || { error "${1} requires a version."; exit 2; }
        requested_version="$2"
        shift 2
        ;;
      --version=*)
        requested_version="${1#*=}"
        [[ -n "$requested_version" ]] || { error "--version requires a version."; exit 2; }
        shift
        ;;
      --data-dir)
        [[ $# -ge 2 ]] || { error "--data-dir requires an absolute path."; exit 2; }
        requested_data_directory="$2"
        shift 2
        ;;
      --data-dir=*)
        requested_data_directory="${1#*=}"
        [[ -n "$requested_data_directory" ]] || { error "--data-dir requires an absolute path."; exit 2; }
        shift
        ;;
      -y|--yes)
        force_yes=true
        shift
        ;;
      --purge)
        purge=true
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        positional+=("$1")
        shift
        ;;
    esac
  done
  set -- "${positional[@]}"
  command_name="${1:-install}"
}

main() {
  local command_name
  local version
  parse_arguments "$@"
  command_name="${command_name:-install}"

  case "$command_name" in
    list-versions)
      check_platform
      check_dependencies
      list_versions
      ;;
    status)
      require_command systemctl
      systemctl --no-pager --full status "$SERVICE_NAME"
      ;;
    logs)
      require_command journalctl
      journalctl -u "$SERVICE_NAME" -f
      ;;
    install|upgrade)
      require_root
      check_platform
      check_dependencies
      version="$(resolve_version)"
      if [[ "$command_name" == install && -f "$APPIMAGE_PATH" && -z "$requested_version" ]]; then
        warning "Review Hub is already installed at $(current_version); use upgrade to update it."
        exit 0
      fi
      install_release "$version"
      ;;
    rollback)
      rollback_release
      ;;
    restart)
      require_root
      require_command systemctl
      start_service
      ;;
    uninstall|remove)
      uninstall
      ;;
    *)
      error "Unknown command: ${command_name}"
      usage
      exit 2
      ;;
  esac
}

main "$@"
