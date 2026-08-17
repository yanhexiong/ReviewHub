# Review Hub

<p align="center">
  <img src="packaging/linux/icons/review-hub-16.svg" width="16" height="16" alt="Review Hub icon 16px">
  <img src="packaging/linux/icons/review-hub-32.svg" width="32" height="32" alt="Review Hub icon 32px">
  <img src="packaging/linux/icons/review-hub-48.svg" width="48" height="48" alt="Review Hub icon 48px">
  <img src="packaging/linux/icons/review-hub-64.svg" width="64" height="64" alt="Review Hub icon 64px">
</p>

[简体中文](README.md) | [English](README_en.md)

[![Release](https://img.shields.io/github/v/release/yanhexiong/ReviewHub?display_name=tag&sort=semver&logo=github)](https://github.com/yanhexiong/ReviewHub/releases)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-15-000000?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![SQLite](https://img.shields.io/badge/SQLite-WAL-003B57?logo=sqlite&logoColor=white)](https://www.sqlite.org/)
[![code-server](https://img.shields.io/badge/code--server-4.96.4-3C82F6?logo=visualstudiocode&logoColor=white)](https://github.com/coder/code-server)
[![License](https://img.shields.io/badge/License-Apache--2.0-D22128)](LICENSE)
[![Stars](https://img.shields.io/github/stars/yanhexiong/ReviewHub?style=flat&logo=github&label=Stars)](https://github.com/yanhexiong/ReviewHub/stargazers)

Review Hub 是面向研究团队的自托管论文审阅与版本管理系统。它将 PDF 审阅、不可变版本快照、批注协作、Git/TeX 源码关联和受控的在线编辑统一在一个本地优先的工作流中。

## Linux 一键部署

发行版部署不需要在目标服务器安装 Node.js、Go、npm 或 TeX。服务器只需具备 Bash、`curl`、`tar`、`unzip`、`sha256sum`、`runuser` 和 systemd；安装脚本会从 GitHub Release 下载已构建的 AppImage，校验 `SHA256SUMS`，创建独立的 `review-hub` 系统用户和服务，并保留运行数据。AppImage 只携带静态前端、必要的 Monaco/PDF.js 浏览器资源和 Go 二进制；安装阶段会把固定版 code-server 与 LaTeX Workshop 放入持久化数据目录，不会把它们或 `node_modules` 打进镜像。

```bash
# 安装最新稳定版本
curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh | sudo bash

# 安装指定测试版本
curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh \
  | sudo bash -s -- install --version v0.1.0-beta.1

# 首次安装时将运行数据放到指定磁盘目录（必须是绝对路径）
curl -fsSL https://raw.githubusercontent.com/yanhexiong/ReviewHub/main/scripts/install.sh \
  | sudo bash -s -- install --version v0.1.0-beta.1 --data-dir /srv/review-hub-data

# 升级、回退和查看日志
sudo review-hub upgrade
sudo review-hub rollback
sudo review-hub logs
```

首次安装时可用 `--data-dir` 选择运行数据目录；省略时，安装器会从控制终端询问目录（无终端时使用 `/var/lib/review-hub/data`）。安装器在启动 systemd 服务前以 `review-hub` 用户执行 AppImage 的 `--install-runtime` 子命令，下载固定版 code-server 与 LaTeX Workshop；升级时检测到已有匹配版本会复用它们。目录选择会写入运行配置并由 systemd 授权给专用服务用户，后续升级不会覆盖。安装器会把服务程序放在 `/opt/review-hub/`，并在 `/usr/local/sbin/review-hub` 提供升级、回退、重启和日志入口。升级只替换 AppImage 并执行健康检查；失败时自动恢复上一个版本。卸载默认保留数据，明确使用 `--purge` 才会删除数据目录。完整命令见 `scripts/install.sh --help`。

## 功能概览

- 管理多个审阅项目，导入本地目录、Git 仓库 ZIP、GitHub 仓库或独立 PDF。
- 将 PDF 归档为 SHA-256 去重的不可变快照，并记录关联 Git 提交、分支及未提交源码状态。
- 提供页级、文本选区和图像区域批注，支持回复、分类、自定义类别、状态流转、删除权限和实时更新。
- 在审阅页面预览历史版本、比较 PDF 文本与批注变化，并将指定版本的源码打开到独立 Git worktree。
- 通过访问密码和权限范围分享审阅内容；项目所有者可管理协作者和成员权限。
- 为管理员提供账户、配额、注册策略、审计日志、备份恢复、数据迁移和应用更新管理。
- 集成独立安装的 code-server，用真实的 VS Code Web 工作区编辑 TeX；LaTeX Workshop、Git 和 Markdown 预览遵循固定版本策略。

Review Hub 不是 Overleaf 的替代品。它不会自动改写 TeX 源码、创建 Git commit、执行 push/merge/rebase/reset，且不提供匿名访问。TeX 编译器和 Git 仍由部署主机提供。

## 架构

```text
Browser
  |
  `-- Go API :3000 (release mode)
        |- static Next.js export and browser assets
        |- /api authentication, projects and review workflows
        |- /vscode protected editor proxy
        `- SQLite WAL repository, snapshots, imports and exports

Development mode additionally runs:
  |
  +-- Next.js :3000
  |     `- App Router pages and UI proxy
  |
  `-- Go API  127.0.0.1:39100
        |- authentication, authorization, projects and review workflows
        |- SQLite WAL repository, snapshots, imports and exports
        |- Git/TeX orchestration, realtime SSE and administration
        `- code-server lifecycle and proxy integration
```

前端只负责页面与交互；所有 `/api/*` 请求由 Go 服务处理。SQLite、密钥、文件系统、Git 和导入逻辑不在浏览器或 Next.js route handler 中实现。详细边界见 [架构约定](docs/architecture.md)。

## 快速开始

### 前置开发环境

- Linux、macOS 或兼容的开发环境。
- Node.js 24 和 npm 10+。
- Go 1.24.x，用于构建和测试正式 API。
- Git，用于 GitHub 导入、源码版本关联和编辑器源代码管理。
- `curl`、`tar`、`unzip`，仅在安装独立 code-server 与固定扩展时需要。
- TeX Live、MiKTeX 或其他受支持 TeX 工具链，仅在服务器编译论文时需要。

从官方远端克隆并启动开发服务：

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

打开 <http://127.0.0.1:3000/setup> 创建首个管理员账户。页面服务默认监听 `0.0.0.0:3000`，Go API 只监听本机回环地址 `127.0.0.1:39100`。首个管理员可在网站后台保存公开监听地址、端口、安全目录和平台限制；修改监听配置后在下一次启动时生效。

### 安装在线编辑器

仅当需要浏览器内 TeX 编辑时执行：

```bash
npm run vscode:setup
```

该命令用于开发环境，将固定版 `code-server 4.96.4` 安装到 `vendor/code-server/`，并安装固定版 LaTeX Workshop `10.12.0`。Git 与 Markdown 预览使用 VS Code 内置扩展，不会安装 Git Graph。正式 AppImage 不携带这些目录，而是在安装阶段执行 `AppImage --install-runtime`，将它们安装到 `<data-directory>/code-server` 和 `<data-directory>/vscode-extensions`；项目不依赖宿主机的 VS Code Remote Server，也不会在运行时自动从扩展市场升级这些组件。

## 配置与数据

首次运行会自动创建运行配置和随机密钥；正式安装的运行时组件位于所选数据目录：

- 运行配置：`~/.config/review-hub/runtime-config.json`，权限为 `0600`。
- 会话签名密钥与凭据加密密钥：同一受保护配置目录，由 CSPRNG 生成。
- 默认运行数据：安装器使用 `/var/lib/review-hub/data`；手动运行 AppImage 时使用所选数据目录，包括 SQLite、PDF 快照、受管工作区、导出文件和日志。

应用不读取 `.env.local`，也不要求通过环境变量提供账号、密码或密钥。不要提交 `data/`、PDF、SQLite 文件、Token、SSH 私钥、真实项目路径或编译产物。管理员在后台配置可访问目录；所有项目路径在服务端规范化并限制在这些目录内。

## 审阅与版本工作流

1. 创建项目并选择来源：本地目录、GitHub、Git ZIP 或独立 PDF。
2. 选择现有 PDF，或在仓库没有 PDF 时选择 TeX 编译入口并由服务器编译。
3. 归档 PDF 为新的不可变审阅版本。内容未变化时会拒绝创建重复版本。
4. 在 PDF 中添加选区、区域或页级批注；评论始终绑定创建时的版本。
5. 在快照历史中预览、比较或切换到对应源码。预览旧版本不会修改当前项目源码。

Git 元数据不可用不会阻止 PDF 归档，但系统不会伪造提交信息。归档时若仓库有未提交修改，系统会保存必要的 Git diff 和未跟踪文件归档，以便之后恢复到与 PDF 对齐的源码状态。

## 开发与测试

常用检查命令如下：

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

首次运行浏览器端到端测试时，按 Playwright 的提示安装 Chromium 及操作系统依赖。测试使用临时数据目录和夹具，不能使用真实论文仓库、用户数据库或凭据。

## 生产运行与发行

开发机上的生产构建仍可使用 Next.js 自定义服务器；正式 AppImage 使用 Go API 直接托管静态导出：

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

`npm run build` 会生成 `out/` 静态目录。不要在正在运行的 `npm run dev` 服务旁执行构建，两者会共享 `.next/` 输出目录。

AppImage 构建只复制 `out/`、浏览器端 Monaco/PDF.js 资源和静态 Go 二进制；`node_modules`、`.next`、Node.js、code-server 与扩展都不会进入镜像。静态页面本身约占 4.8 MB，发行包的其余空间主要来自 Monaco 编辑器资源和 Go API；固定版 code-server 与 LaTeX Workshop 在安装阶段写入持久化数据目录。

## 项目结构

```text
src/                 Next.js App Router, UI components, features and i18n
backend/             Go API, domain services, repositories and migrations
updater/             Static Go helper for atomic AppImage replacement
e2e/                 Playwright end-to-end tests
scripts/             Verification, editor setup, release, and deployment helpers
packaging/linux/     AppImage launcher and desktop integration assets
docs/                Architecture and engineering documentation
data/                Ignored runtime data; never commit
```

## 贡献

提交前请阅读 [贡献指南](CONTRIBUTING.md)、[架构约定](docs/architecture.md) 和 `AGENTS.md`。使用简短的 Conventional Commit，例如 `feat: add snapshot filters` 或 `fix: validate share permissions`。每个 Pull Request 应说明用户可见行为、迁移影响、已执行测试，以及涉及 UI 时的截图。

## 许可证

Review Hub 的原创代码以 [Apache License 2.0](LICENSE) 发布，版权与归属说明见 [NOTICE](NOTICE)。第三方依赖、code-server 和编辑器扩展仍遵循各自的许可证。

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=yanhexiong/ReviewHub&type=Date)](https://www.star-history.com/#yanhexiong/ReviewHub&Date)
