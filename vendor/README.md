# Vendored runtime

`code-server/` 是本仓库独立管理的固定版 code-server，由
`npm run vscode:install-server` 安装到 `code-server/`。它不依赖宿主机是否
安装 VS Code、VS Code Remote Server 或 `~/.vscode-server`，也不会回退到这些
环境。

目录默认不进入 Git 版本库；新环境克隆仓库后先执行：

```bash
npm run vscode:setup
```
