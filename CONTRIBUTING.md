# 贡献指南

感谢对 Review Hub 的贡献。提交前请先阅读 [架构约定](docs/architecture.md) 和 `AGENTS.md`。

## 开始开发

使用 Node.js 20 至 24 和 npm 10。在仓库根目录执行：

```bash
npm ci
cd backend && go build -o ../review-hub-api ./cmd/review-hub-api && cd ..
REVIEW_HUB_GO_API_BIN="$PWD/review-hub-api" npm run dev
```

请使用临时数据目录和测试夹具。不得提交 `data/`、PDF、数据库、账户信息、Token、SSH 私钥或真实论文路径。

## 提交变更

每个 Pull Request 只解决一个清晰的问题，并包含：用户可见行为说明、测试结果、Go schema 或迁移影响、必要的截图。使用简短的祈使式 Conventional Commit，例如 `feat: add snapshot filters` 或 `fix: validate share permissions`。

新增业务功能应建立 `src/features/<domain>/` 目录；跨领域服务才进入 `src/lib/`。新增界面文案必须先加入 `src/i18n/locales/zh-CN.ts`，再同步到所有其他语言文件。不要绕过翻译类型校验。

## 贡献许可

本项目以 Apache License 2.0 发布。除非在提交前以书面方式另行约定，向本仓库提交
Pull Request、补丁或其他贡献即表示你有权按 Apache License 2.0 的条款提供该贡献。
不得提交从其他项目复制、但许可证不兼容的代码、二进制文件或扩展资源。

## 提交前检查

```bash
npm run typecheck
npm run lint
npm run format:check
npm test
npm run test:e2e
npm run db:check
git diff --check
```

涉及路径、权限、快照或 Git 的改动必须覆盖拒绝不安全输入的测试。涉及界面的改动应附带桌面和移动端截图。所有 Go API 外部输入必须经过长度、类型和枚举校验；路由中不得混合授权、文件系统访问和持久化。
