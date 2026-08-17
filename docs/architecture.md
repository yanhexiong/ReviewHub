# 架构约定

Review Hub 按路由、领域和基础设施分层。目录归属比文件大小更重要：一个变更应优先落在拥有该业务概念的模块中，而不是继续向通用目录堆叠。

## 目录边界

- `src/app/`：Next.js 页面组合和页面级数据装配。这里不实现 API route handler；浏览器请求由
  自定义服务器透明代理到 Go API。
- `src/features/<domain>/`：一个用户可识别的业务领域，包括状态、纯工具、领域组件和对外导出。例如 `features/preferences/` 管理语言、主题和浏览器存储。
- `src/components/`：跨领域复用的展示组件。组件不得反向依赖具体路由，也不应直接访问数据库或文件系统。
- `src/lib/`：跨领域浏览器工具和 UI 适配，例如 PDF.js 文字几何比对与运行配置读取。数据库、密钥、Git 和文件系统访问不属于此层；这些能力由 Go 提供。
- `src/i18n/`：语言目录、目录注册和翻译工具。每种语言只在 `src/i18n/locales/<locale>.ts` 中维护。
- `backend/cmd/`：静态 Go 服务的进程装配和生命周期。不得放入业务 SQL 或 HTTP 细节。
- `backend/internal/config/`：受权限保护的运行配置、独立随机密钥文件与启动期校验；除标准 `XDG_CONFIG_HOME`
  定位目录外，不从应用环境变量或 `.env.local` 导入配置。
- `backend/internal/httpapi/`：HTTP 契约、错误映射、SSE 与中间件；handler 只调用 service。
- `backend/internal/service/`：认证、项目、审阅等领域用例及授权规则。
- `backend/internal/repository/`：SQLite 查询、schema bootstrap 和版本化迁移；它是唯一的数据库权威，不得泄露凭据或部署路径。

## Go API

正式 AppImage 由 Go API 同时托管 Next.js `output: export` 静态页面、浏览器资源和所有
`/api/*` 请求；`/vscode/*` 由同一 Go 进程代理到安装阶段写入持久化目录的 code-server。
开发模式仍由 Node 入口提供热更新页面，并把 API/VS Code 请求代理到 Go。Go 负责健康检查、
账户、项目、批注、快照、导入、Git/TeX、导出、分享、管理员运维、实时事件和 VS Code 配置。Go 使用 SQLite WAL 数据库，并复用 `review_hub_session` 的
HMAC-SHA256 Cookie 格式，从而可以在升级时保留会话。没有未迁移的 Node API 或回环兼容层。

开发 Go API 时使用 Go 1.24：

```bash
cd backend
go test ./...
gofmt -d .
```

开发机应先启动 `review-hub-api`，或设置 `REVIEW_HUB_GO_API_BIN` 让页面服务负责启动它；页面服务
启动前会检查 Go 健康端点。AppImage 运行时强制要求内置 Go 二进制可用，启动健康检查失败会拒绝
启动，不会回退到旧 API。

## 应用更新

应用更新是 Go API 的受限管理功能，更新源固定为
`yanhexiong/ReviewHub`，管理员界面不能配置其他仓库、URL 或 Token。服务使用
`github.com/google/go-github` 读取 GitHub Release，使用
`github.com/Masterminds/semver/v3` 比较严格的稳定版本；只接受精确命名的
Linux x86_64 AppImage 与同一 Release 内的 `SHA256SUMS`。

GitHub Actions 同时发布 `ReviewHub-vX.Y.Z-x86_64.update.tar.gz`。离线包按顺序只包含
`manifest.json` 与精确匹配版本的 AppImage；manifest 声明版本、平台、架构、文件名、
字节数和 SHA-256。上传路径只解压这两个普通文件，拒绝符号链接、额外条目、错误平台、
降级/重放版本或哈希不匹配。通过校验的文件仅暂存在私有数据目录中。

独立静态更新助手在与目标 AppImage 相同的文件系统上执行替换，保留上一版，重启应用并
轮询本机健康检查。启动或健康检查失败时，它会恢复旧版本并写入英文运行日志。更新的
授权、资产选择、校验和审计全部在 Go 中完成；浏览器只显示进度和调度结果。

## Feature 模板

新领域使用下列结构，并通过 `index.ts` 暴露稳定 API：

```text
src/features/<domain>/
  components/             # 仅该领域使用的界面
  <domain>-service.ts     # 领域逻辑或浏览器适配
  types.ts                # 领域类型
  index.ts                # 公共导出
```

其他 feature 只能从其 `index.ts` 导入，避免跨目录依赖实现细节。共享的 Zod schema、服务端 I/O 和持久化应通过 `src/lib/` 暴露。

## 多语言与主题

以 `zh-CN.ts` 作为完整键集合的基准。其他语言通过 `satisfies MessageCatalog` 校验；缺少或拼错翻译键会导致 TypeScript 检查失败。界面组件只能调用 `t("namespace.key")`，不得复制文案或直接读取某个语言文件。

主题属于 `features/preferences`，通过 CSS 自定义属性和 `html[data-theme]` 应用。业务组件不得为深色模式写行内颜色；新增颜色先补充主题变量或相应组件样式。

## 验证

每次结构调整至少执行：

```bash
npm run typecheck
npm run lint
npm test
git diff --check
```
