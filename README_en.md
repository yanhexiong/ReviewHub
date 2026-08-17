# Review Hub

<p align="center">
  <img src="packaging/linux/icons/review-hub-64.svg" width="64" height="64" alt="Review Hub">
</p>

[简体中文](README.md) | [English](README_en.md)

[![Release](https://img.shields.io/github/v/release/yanhexiong/ReviewHub?display_name=tag&sort=semver&logo=github)](https://github.com/yanhexiong/ReviewHub/releases)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-15-000000?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![SQLite](https://img.shields.io/badge/SQLite-WAL-003B57?logo=sqlite&logoColor=white)](https://www.sqlite.org/)
[![code-server](https://img.shields.io/badge/code--server-4.96.4-3C82F6?logo=visualstudiocode&logoColor=white)](https://github.com/coder/code-server)
[![License](https://img.shields.io/badge/License-Apache--2.0-D22128)](LICENSE)
[![Stars](https://img.shields.io/github/stars/yanhexiong/ReviewHub?style=flat&logo=github&label=Stars)](https://github.com/yanhexiong/ReviewHub/stargazers)

Review Hub is a self-hosted paper review and version management system for research teams. It brings PDF review, immutable version snapshots, collaborative annotations, Git/TeX source association, and controlled online editing into one local-first workflow.

## One-Command Linux Deployment

Deployments do not require Node.js, Go, npm, or TeX on the target server. The host only needs Bash, `curl`, `tar`, `unzip`, `sha256sum`, `runuser`, and systemd. The installer downloads a built AppImage from GitHub Releases, verifies `SHA256SUMS`, creates a dedicated `review-hub` system user and service, and keeps runtime data outside the application image.

```bash
# Install the latest stable release
curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh | sudo bash

# Install a specified test release
curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh \
  | sudo bash -s -- install --version v0.1.0-beta.1

# Choose a persistent data directory on the first install (absolute path)
curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh \
  | sudo bash -s -- install --version v0.1.0-beta.1 --data-dir /srv/review-hub-data

# Upgrade, roll back, and view logs
sudo review-hub upgrade
sudo review-hub rollback
sudo review-hub logs
```

On the first install, `--data-dir` lets you choose the persistent data directory. When omitted, the installer asks through the controlling terminal (including when invoked through `curl | sudo bash`); a terminal-less install uses `/var/lib/review-hub/data`. Before starting the service, the installer runs `AppImage --install-runtime` as the dedicated user to download the pinned code-server and LaTeX Workshop into that data directory. The choice is persisted in the runtime configuration and granted to the dedicated system user; upgrades do not overwrite it. The installer stores the application in `/opt/review-hub/` and installs the upgrade, rollback, restart, and log entry point at `/usr/local/sbin/review-hub`. Upgrades replace only the AppImage and run a health check; a failed check restores the previous version. Uninstall keeps data by default; use `--purge` explicitly to remove it. See `scripts/install.sh --help` for all commands.

## Features

- Manage multiple review projects and import a local directory, Git repository ZIP, GitHub repository, or standalone PDF.
- Archive PDFs as immutable, SHA-256-deduplicated snapshots while recording the associated Git commit, branch, and uncommitted source state.
- Add page-level, text-selection, and image-region annotations with replies, categories, custom categories, status transitions, deletion permissions, and realtime updates.
- Preview historical versions, compare PDF text and annotation changes, and open the matching source revision in an isolated Git worktree.
- Share review content with an access password and explicit permission scope; project owners can manage collaborators and member permissions.
- Provide administrators with account, quota, registration policy, audit log, backup and restore, data migration, and application update controls.
- Integrate an independently installed code-server for browser-based TeX editing; LaTeX Workshop, Git, and Markdown preview follow a fixed-version policy.

Review Hub is not an Overleaf replacement. It never rewrites TeX source, creates Git commits, runs push/merge/rebase/reset, or enables anonymous access. TeX compilers and Git remain provided by the deployment host.

## Architecture

```text
Browser
  |
  `-- Go API :3000 (release mode)
        |- static Next.js export and browser assets
        |- /api authentication, projects, and review workflows
        |- /vscode protected editor proxy
        `- SQLite WAL repository, snapshots, imports, and exports

Development mode additionally runs:
  |
  +-- Next.js :3000
  |     `- App Router pages and UI proxy
  |
  `-- Go API  127.0.0.1:39100
        |- authentication, authorization, projects, and review workflows
        |- SQLite WAL repository, snapshots, imports, and exports
        |- Git/TeX orchestration, realtime SSE, and administration
        `- code-server lifecycle and proxy integration
```

The frontend is responsible only for pages and interaction; all `/api/*` requests are handled by the Go service. SQLite, keys, filesystem access, Git, and import logic are not implemented in the browser or Next.js route handlers. See [architecture conventions](docs/architecture.md) for the detailed boundaries.

## Quick Start

### Prerequisites

- Linux, macOS, or a compatible development environment.
- Node.js 24 and npm 10+.
- Go 1.24.x for building and testing the production API.
- Git for GitHub imports, source version association, and editor source control.
- `curl`, `tar`, and `unzip`, only when installing the standalone code-server and fixed extensions.
- TeX Live, MiKTeX, or another supported TeX toolchain when compiling papers on the server.

Clone the official remote and start the development service:

```bash
git clone https://github.com/yanhexiong/ReviewHub.git
cd ReviewHub

# If Node.js is managed by nvm, activate the repository's Node 24 runtime.
source ~/.nvm/nvm.sh
nvm use 24

npm ci
mkdir -p backend/bin
(
  cd backend
  go build -trimpath -o bin/review-hub-api ./cmd/review-hub-api
)

REVIEW_HUB_GO_API_BIN="$PWD/backend/bin/review-hub-api" npm run dev
```

Open <http://127.0.0.1:3000/setup> to create the first administrator account. The web service listens on `0.0.0.0:3000` by default, while the Go API listens only on the loopback address `127.0.0.1:39100`. The first administrator can save the public listen address, port, allowed directories, and platform limits in the administration console; listener changes take effect on the next restart.

### Install the Online Editor

Run this only when browser-based TeX editing is needed:

```bash
npm run vscode:setup
```

The command installs the pinned `code-server 4.96.4` release into `vendor/code-server/` and the pinned LaTeX Workshop `10.12.0` extension for development. The release AppImage does not contain these directories; its installer runs `AppImage --install-runtime` and writes them to `<data-directory>/code-server` and `<data-directory>/vscode-extensions`. Git and Markdown preview use VS Code built-ins; Git Graph is not installed. The project does not depend on the host's VS Code Remote Server and does not upgrade these components from the marketplace at runtime.

## Configuration and Data

The first run creates the runtime configuration and random keys automatically:

- Runtime configuration: `~/.config/review-hub/runtime-config.json`, mode `0600`.
- Session signing and credential-encryption keys: the same protected configuration directory, generated with a CSPRNG.
- Default runtime data: `/var/lib/review-hub/data` for the service installer, or the selected XDG data directory for a manually launched AppImage, including SQLite, PDF snapshots, managed workspaces, exports, and logs.

The application does not read `.env.local` and does not require account passwords, tokens, or keys through environment variables. Never commit `data/`, PDFs, SQLite files, tokens, SSH private keys, real project paths, or build artifacts. Administrators configure accessible directories in the console; the server normalizes every project path and enforces those directory boundaries.

## Review and Version Workflow

1. Create a project and choose a source: local directory, GitHub, Git ZIP, or standalone PDF.
2. Select an existing PDF, or choose a TeX entry file for server-side compilation when the repository has no PDF.
3. Archive the PDF as a new immutable review version. Duplicate content is rejected.
4. Add selection, region, or page annotations in the PDF; comments remain bound to the version on which they were created.
5. Preview, compare, or switch to the matching source revision from snapshot history. Previewing an older version never changes the current project source.

Unavailable Git metadata does not prevent PDF archiving, but the system never invents commit information. When a repository contains uncommitted changes, the archive stores the required Git diff and untracked-file snapshot so that the source state aligned with the PDF can be restored later.

## Development and Testing

Common checks from the repository root:

```bash
npm run typecheck       # TypeScript strict type checking
npm run lint            # ESLint
npm run format:check    # Prettier verification
npm test                # Vitest unit tests
npm run db:check        # Go schema and repository checks
npm run test:e2e        # Playwright login and review workflows

(
  cd backend
  go test ./...
  test -z "$(gofmt -d .)"
)

git diff --check
```

On the first browser end-to-end test run, follow Playwright's prompt to install Chromium and its operating-system dependencies. Tests use temporary data directories and fixtures; they must not use real paper repositories, user databases, or credentials.

## Production and Releases

The development/host production command can still run the Next.js custom server:

```bash
npm ci
npm run build
mkdir -p backend/bin
(
  cd backend
  go build -trimpath -o bin/review-hub-api ./cmd/review-hub-api
)

NODE_ENV=production \
  REVIEW_HUB_GO_API_BIN="$PWD/backend/bin/review-hub-api" \
  npm run start
```

`npm run build` emits the `out/` static export. The release AppImage serves that directory directly from the Go API and contains no Node.js runtime, `node_modules`, `.next`, code-server, or extensions. The static export is about 4.8 MB; most of the approximately 21.5 MB AppImage is Monaco browser workers and the Go services. The pinned editor runtime is installed into the persistent data directory during deployment.

Do not run `npm run build` beside an active `npm run dev` process because both share the `.next/` output directory.

GitHub Actions validates the tag, runs the test suite, builds the static Go services and self-contained AppImage, smoke-tests it without FUSE, and publishes the AppImage, offline update bundle, and `SHA256SUMS` to the matching GitHub Release.

The restored workflow produces:

- `ReviewHub-vX.Y.Z-x86_64.AppImage`: a single-file Linux x86_64 distribution.
- `ReviewHub-vX.Y.Z-x86_64.update.tar.gz`: an offline update bundle importable from the administration console.
- `SHA256SUMS`: checksums for the release files.

## Project Structure

```text
src/                 Next.js App Router, UI components, features, and i18n
backend/             Go API, domain services, repositories, and migrations
updater/             Static Go helper for atomic AppImage replacement
e2e/                 Playwright end-to-end tests
scripts/             Verification, editor setup, release, and deployment helpers
packaging/linux/     AppImage launcher and desktop integration assets
docs/                Architecture and engineering documentation
data/                Ignored runtime data; never commit
```

## Contributing

Before submitting changes, read [CONTRIBUTING.md](CONTRIBUTING.md), [architecture conventions](docs/architecture.md), and `AGENTS.md`. Use concise Conventional Commits such as `feat: add snapshot filters` or `fix: validate share permissions`. Each pull request should describe user-visible behavior, migration impact, executed tests, and screenshots for UI changes.

## License

Original Review Hub code is released under the [Apache License 2.0](LICENSE); attribution details are in [NOTICE](NOTICE). Third-party dependencies, code-server, and editor extensions remain under their respective licenses.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=yanhexiong/ReviewHub&type=Date)](https://www.star-history.com/#yanhexiong/ReviewHub&Date)
