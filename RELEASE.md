# Release Manifest

## v0.1.0-beta.1

This is the first `0.1.x` test release from the `main` branch. The `dev` branch
continues to receive daily development changes; `main` is used for test and
stable release candidates.

### Runtime

- Node.js `20.9.0` through `24.x`; npm `10.x` or newer are build-time requirements only. They are not shipped in the AppImage.
- Next.js `15.5.22`, React `19.2.8`, and React DOM `19.2.8`.
- SQLite is owned by the Go API and accessed through `modernc.org/sqlite`; schema bootstrap and migrations are versioned in `backend/internal/repository/`.
- PDF.js runtime `3.11.174`; keep this version while the web PDF viewer is
  validated against the application.
- The release frontend is a static Next.js export served directly by the Go
  API. `node_modules/`, `.next/`, the Node.js runtime, code-server, and editor
  extensions are excluded from the AppImage.

### Independent editor components

- code-server `4.96.4`, installed during deployment by `AppImage --install-runtime`
  into the persistent data directory (or by `npm run vscode:install-server` for
  development).
- LaTeX Workshop `10.12.0`, installed during deployment by
  `AppImage --install-runtime` (or by `npm run vscode:install-extensions` for
  development). This is intentionally not the latest Marketplace version
  because newer releases changed the web PDF viewer.
- Git and Markdown Preview are provided by the pinned code-server build; no
  separate Git Graph or host VS Code installation is required.

### Reproducibility

Application framework dependencies intentionally keep compatible ranges in
`package.json`; the release lockfile records the exact graph used by `npm ci`.
Release builds must use `npm ci`, then run the checks listed in `README.md`.
Runtime data, generated PDFs, credentials, code-server binaries, and extension
downloads stay outside Git and are provisioned during deployment.
