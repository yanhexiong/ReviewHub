"use client";

import { useEffect, useRef, useState } from "react";

type PendingState = {
  id: string;
  candidates: string[];
};

type ImportProgressState = {
  progress: number;
  stage: string;
  done: boolean;
  error?: string;
};

type PublicTexLiveProfile = {
  id: string;
  name: string;
  engine: string;
  buildTool: string;
  outputDirectory: string;
  shellEscape: boolean;
  texliveBinConfigured: boolean;
};

export default function NewProjectPage() {
  const [error, setError] = useState("");
  const [mode, setMode] = useState<"local" | "archive" | "github">("local");
  const [pending, setPending] = useState(false);
  const [pendingState, setPendingState] = useState<PendingState | null>(null);
  const [pendingTexRoot, setPendingTexRoot] = useState("");
  const [localPdfPath, setLocalPdfPath] = useState("");
  const [progressId, setProgressId] = useState<string | null>(null);
  const [importProgress, setImportProgress] =
    useState<ImportProgressState | null>(null);
  const [texProfiles, setTexProfiles] = useState<PublicTexLiveProfile[]>([]);
  const [selectedTexProfileId, setSelectedTexProfileId] = useState("");
  const [form, setForm] = useState({
    name: "",
    slug: "",
    description: "",
    repositoryPath: ".",
    sourcePdfPath: "",
    githubUrl: "",
    githubBranch: "",
    githubToken: "",
    texRootPath: "",
    gitUserName: "",
    gitUserEmail: "",
  });
  const gitIdentityTouched = useRef({ userName: false, userEmail: false });

  function beginImportProgress() {
    const id = crypto.randomUUID();
    setProgressId(id);
    setImportProgress({ progress: 0, stage: "正在准备导入", done: false });
    return id;
  }

  function progressHeaders(id: string) {
    return {
      "X-Review-Hub-Progress-Id": id,
      "X-Review-Hub-Embedded-Progress": "1",
    };
  }

  function renderImportProgress() {
    if (!importProgress) return null;
    return (
      <div
        aria-live="polite"
        aria-label="项目导入进度"
        className="project-import-progress"
        role="status"
      >
        <div className="project-import-progress-heading">
          <strong>{importProgress.stage}</strong>
          <span>{importProgress.progress}%</span>
        </div>
        <progress max={100} value={importProgress.progress} />
        {importProgress.error && (
          <p className="project-import-progress-error">
            {importProgress.error}
          </p>
        )}
      </div>
    );
  }
  const setField =
    (key: keyof typeof form) =>
    (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      if (key === "gitUserName") gitIdentityTouched.current.userName = true;
      if (key === "gitUserEmail") gitIdentityTouched.current.userEmail = true;
      setForm((current) => ({ ...current, [key]: event.target.value }));
    };

  function selectMode(nextMode: typeof mode) {
    setMode(nextMode);
    setError("");
    // A repository PDF path is relative to an imported workspace, while a
    // local project's path is an allowed server path. Do not carry one mode's
    // value into another mode by accident.
    setForm((current) => ({ ...current, sourcePdfPath: "" }));
  }

  async function submit(formData: FormData) {
    if (pendingState) {
      setError("当前仓库已准备好，请先完成上方操作或取消后再重新创建。");
      return;
    }
    setError("");
    setPending(true);
    const currentProgressId = beginImportProgress();
    try {
      const response = await fetch("/api/projects", {
        method: "POST",
        ...(mode === "local"
          ? {
              headers: {
                ...progressHeaders(currentProgressId),
                "Content-Type": "application/json",
              },
              body: JSON.stringify({
                name: formData.get("name"),
                slug: formData.get("slug"),
                description: formData.get("description"),
                repositoryPath: formData.get("repositoryPath"),
                sourcePdfPath: formData.get("sourcePdfPath"),
                gitUserName: formData.get("gitUserName"),
                gitUserEmail: formData.get("gitUserEmail"),
              }),
            }
          : { body: formData, headers: progressHeaders(currentProgressId) }),
      });
      const body = await response.json();
      if (response.status === 202 && body.pending?.id) {
        const candidates = Array.isArray(body.texCandidates)
          ? body.texCandidates.filter(
              (candidate: unknown): candidate is string =>
                typeof candidate === "string",
            )
          : [];
        setPendingState({
          id: body.pending.id,
          candidates,
        });
        setPendingTexRoot(candidates[0] ?? "");
        return;
      }
      if (!response.ok) {
        setError(body.error?.message ?? "创建项目失败");
        return;
      }
      location.assign(`/projects/${body.project.id}`);
    } catch {
      setError("创建项目失败，请检查服务连接。");
    } finally {
      setPending(false);
    }
  }

  useEffect(() => {
    if (!progressId) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try {
        const response = await fetch(`/api/projects/progress/${progressId}`, {
          headers: { "X-Review-Hub-Embedded-Progress": "1" },
        });
        if (!response.ok) return;
        const next = (await response.json()) as ImportProgressState;
        if (stopped) return;
        setImportProgress(next);
        if (!next.done) timer = setTimeout(() => void poll(), 450);
      } catch {
        if (!stopped) timer = setTimeout(() => void poll(), 900);
      }
    };
    void poll();
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    };
  }, [progressId]);

  useEffect(() => {
    let active = true;
    fetch("/api/me/vscode-defaults")
      .then(async (response) => {
        if (!response.ok) return null;
        const body = (await response.json()) as {
          defaults?: { userName?: string; userEmail?: string };
        };
        return body.defaults ?? null;
      })
      .then((defaults) => {
        if (!active || !defaults) return;
        setForm((current) => ({
          ...current,
          gitUserName:
            !gitIdentityTouched.current.userName && !current.gitUserName
              ? defaults.userName || ""
              : current.gitUserName,
          gitUserEmail:
            !gitIdentityTouched.current.userEmail && !current.gitUserEmail
              ? defaults.userEmail || ""
              : current.gitUserEmail,
        }));
      })
      .catch(() => {
        // Account defaults are optional; the fields remain editable when the
        // defaults endpoint is unavailable.
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    fetch("/api/texlive-profiles", { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) return [];
        const body = (await response.json()) as {
          profiles?: PublicTexLiveProfile[];
        };
        return body.profiles ?? [];
      })
      .then((profiles) => {
        if (active) setTexProfiles(profiles);
      })
      .catch(() => {
        // Profiles are optional; the account defaults remain available.
      });
    return () => {
      active = false;
    };
  }, []);

  async function compilePdf() {
    if (!pendingState) return;
    if (!pendingTexRoot) {
      setError("仓库中没有可用的 .tex 文件，无法在站内编译。");
      return;
    }
    setError("");
    setPending(true);
    const currentProgressId = beginImportProgress();
    try {
      const response = await fetch(
        `/api/projects/pending/${pendingState.id}/compile`,
        {
          method: "POST",
          headers: {
            ...progressHeaders(currentProgressId),
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            texRootPath: pendingTexRoot,
            texProfileId: selectedTexProfileId || undefined,
          }),
        },
      );
      const body = await response.json();
      if (!response.ok) throw new Error(body.error?.message ?? "编译失败");
      location.assign(`/projects/${body.project.id}`);
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : "编译失败，请重试或选择已有 PDF",
      );
    } finally {
      setPending(false);
    }
  }

  async function applyLocalPdf() {
    if (!pendingState) return;
    setError("");
    setPending(true);
    const currentProgressId = beginImportProgress();
    try {
      const response = await fetch(
        `/api/projects/pending/${pendingState.id}/source-pdf`,
        {
          method: "POST",
          headers: {
            ...progressHeaders(currentProgressId),
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            sourcePdfPath: localPdfPath.trim(),
            texRootPath: pendingTexRoot || undefined,
          }),
        },
      );
      const body = await response.json();
      if (!response.ok) throw new Error(body.error?.message ?? "使用 PDF 失败");
      location.assign(`/projects/${body.project.id}`);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "使用 PDF 失败");
    } finally {
      setPending(false);
    }
  }

  async function cancelPending() {
    if (!pendingState) return;
    try {
      await fetch(`/api/projects/pending/${pendingState.id}`, {
        method: "DELETE",
      });
    } catch {
      // The pending record may already be gone; reset the UI regardless.
    }
    setPendingState(null);
    setPendingTexRoot("");
    setLocalPdfPath("");
    setProgressId(null);
    setImportProgress(null);
    setError("");
  }

  return (
    <main className="projects">
      <header>
        <div>
          <p className="eyebrow">REVIEW HUB / 项目配置</p>
          <h1>创建审阅项目</h1>
        </div>
        <div className="page-actions">
          <button onClick={() => location.assign("/projects")}>返回项目</button>
        </div>
      </header>
      <form
        aria-busy={pending}
        className="project-form project-create-form"
        action={submit}
      >
        <label>
          项目名称
          <input
            name="name"
            required
            placeholder="例如：论文修订第二轮"
            value={form.name}
            onChange={setField("name")}
          />
        </label>
        <label>
          URL 标识
          <input
            name="slug"
            required
            pattern="[a-z0-9-]+"
            placeholder="revision-round-2"
            value={form.slug}
            onChange={setField("slug")}
          />
        </label>
        <label>
          说明
          <textarea
            name="description"
            rows={3}
            placeholder="可选的项目说明"
            value={form.description}
            onChange={setField("description")}
          />
        </label>
        <fieldset className="source-mode-fieldset">
          <legend>项目来源</legend>
          <div
            className="source-mode-tabs"
            role="tablist"
            aria-label="项目来源"
          >
            {[
              ["local", "已有本地仓库"],
              ["archive", "Git 仓库压缩包"],
              ["github", "GitHub 同步"],
            ].map(([value, label]) => (
              <button
                className={mode === value ? "selected" : ""}
                disabled={pending || Boolean(pendingState)}
                key={value}
                type="button"
                role="tab"
                aria-selected={mode === value}
                onClick={() => selectMode(value as typeof mode)}
              >
                {label}
              </button>
            ))}
          </div>
          <input type="hidden" name="sourceType" value={mode} />
          {mode === "local" && (
            <div className="source-mode-content">
              <label>
                论文仓库位置
                <input
                  name="repositoryPath"
                  required
                  value={form.repositoryPath}
                  onChange={setField("repositoryPath")}
                />
                <small>
                  只能使用管理员配置的安全目录；该路径只属于当前项目，不会继承其他项目。
                </small>
              </label>
              <label>
                当前 PDF 文件位置
                <input
                  name="sourcePdfPath"
                  placeholder="main.pdf"
                  required
                  value={form.sourcePdfPath}
                  onChange={setField("sourcePdfPath")}
                />
                <small>
                  归档时会读取该文件，但不会修改它。每个项目独立保存自己的 PDF
                  路径。
                </small>
              </label>
            </div>
          )}
          {mode === "archive" && (
            <div className="source-mode-content">
              <label>
                Git 仓库 ZIP 文件
                <input
                  name="repositoryArchive"
                  type="file"
                  accept=".zip,application/zip"
                  required
                />
                <small>文件会解压到应用管理的工作区，不会修改上传文件。</small>
              </label>
              <label>
                压缩包内 PDF 文件位置
                <input
                  name="sourcePdfPath"
                  placeholder="可选，填写仓库内的相对位置"
                  value={form.sourcePdfPath}
                  onChange={setField("sourcePdfPath")}
                />
                <small>
                  可留空；以解压后的仓库根目录为基准。留空时如果找不到
                  main.pdf，会保留源码并询问是否编译。
                </small>
              </label>
              <label>
                TeX 编译入口（可选）
                <input
                  name="texRootPath"
                  placeholder="留空自动检测 main.tex"
                  value={form.texRootPath}
                  onChange={setField("texRootPath")}
                />
                <small>
                  仓库没有 main.tex 时，创建后会让您从候选文件中选择。
                </small>
              </label>
            </div>
          )}
          {mode === "github" && (
            <div className="source-mode-content">
              <label>
                GitHub 仓库地址
                <input
                  name="githubUrl"
                  type="url"
                  placeholder="https://github.com/owner/repository"
                  required
                  value={form.githubUrl}
                  onChange={setField("githubUrl")}
                />
              </label>
              <label>
                分支
                <input
                  name="githubBranch"
                  placeholder="留空自动使用仓库默认分支"
                  value={form.githubBranch}
                  onChange={setField("githubBranch")}
                />
              </label>
              <label>
                审阅用 PDF 文件位置
                <input
                  name="sourcePdfPath"
                  placeholder="可选，填写仓库内的相对位置"
                  value={form.sourcePdfPath}
                  onChange={setField("sourcePdfPath")}
                />
                <small>
                  可留空。填写时使用仓库内路径（如 main.pdf 或
                  build/main.pdf）；留空且仓库没有 main.pdf
                  时，创建后会保留源码并 询问是否编译生成。
                </small>
              </label>
              <label>
                TeX 编译入口（可选）
                <input
                  name="texRootPath"
                  placeholder="留空自动检测 main.tex"
                  value={form.texRootPath}
                  onChange={setField("texRootPath")}
                />
                <small>
                  仓库没有 main.tex 时，创建后会让您从候选文件中选择。
                </small>
              </label>
              <label>
                私有仓库访问令牌（可选）
                <input
                  name="githubToken"
                  type="password"
                  autoComplete="new-password"
                  placeholder="留空使用账户默认令牌或公开仓库访问"
                  value={form.githubToken}
                  onChange={setField("githubToken")}
                />
                <small>
                  令牌只在服务端加密保存，不会返回给浏览器；建议使用仅仓库读取权限的
                  fine-grained token。
                </small>
              </label>
            </div>
          )}
        </fieldset>
        <fieldset className="source-mode-fieldset">
          <legend>项目 Git 提交信息（可选）</legend>
          <div className="project-config-grid">
            <label>
              Git 提交姓名
              <input
                name="gitUserName"
                autoComplete="name"
                placeholder="默认使用账户设置"
                value={form.gitUserName}
                onChange={setField("gitUserName")}
              />
            </label>
            <label>
              Git 提交邮箱
              <input
                name="gitUserEmail"
                type="email"
                autoComplete="email"
                placeholder="默认使用账户设置"
                value={form.gitUserEmail}
                onChange={setField("gitUserEmail")}
              />
            </label>
          </div>
          <small>
            创建时可以直接填写项目专用的 Git
            身份，也可以清空后跳过。留空的字段会在审阅和在线编辑器中回退到账户默认设置，不会读取宿主机的
            Git 全局配置。
          </small>
        </fieldset>
        {!pendingState && renderImportProgress()}
        {error && <p className="error">{error}</p>}
        <button disabled={pending || Boolean(pendingState)} type="submit">
          {pending
            ? "正在准备项目..."
            : pendingState
              ? "请先完成上方操作"
              : "创建项目"}
        </button>
      </form>

      {pendingState && (
        <div className="project-import-overlay">
          <section
            aria-labelledby="pending-project-title"
            aria-modal="true"
            className="project-import-dialog"
            role="dialog"
          >
            <div className="project-import-dialog-header">
              <div>
                <p className="eyebrow">REVIEW HUB / 导入步骤</p>
                <h2 id="pending-project-title">仓库中没有找到 PDF</h2>
              </div>
              <span className="project-import-dialog-step">下一步</span>
            </div>
            <p className="muted">
              源码已经下载并保留，你填写的项目信息也没有丢失。请选择下一步：
            </p>
            {renderImportProgress()}
            {pendingState.candidates.length > 0 && (
              <label>
                TeX 编译入口
                <select
                  id="pending-tex-root"
                  value={pendingTexRoot}
                  onChange={(event) => setPendingTexRoot(event.target.value)}
                >
                  {pendingState.candidates.map((candidate) => (
                    <option key={candidate} value={candidate}>
                      {candidate}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <label>
              TeX Live 编译配置
              <select
                value={selectedTexProfileId}
                onChange={(event) =>
                  setSelectedTexProfileId(event.target.value)
                }
              >
                <option value="">使用我的账户默认设置</option>
                {texProfiles.map((profile) => (
                  <option key={profile.id} value={profile.id}>
                    {profile.name} · {profile.engine} / {profile.buildTool}
                    {profile.texliveBinConfigured ? " · 编译器目录已配置" : ""}
                  </option>
                ))}
              </select>
              {!texProfiles.length && (
                <small className="muted">
                  管理员尚未发布可选的 TeX Live 配置。
                </small>
              )}
            </label>
            {pendingState.candidates.length === 0 && (
              <p className="warning">
                仓库中没有找到 `.tex` 文件，因此无法由网站编译。请重新同步包含
                TeX 源码的分支，或联系管理员配置可用的 PDF 文件。
              </p>
            )}
            <button
              disabled={pending || !pendingTexRoot}
              type="button"
              onClick={() => void compilePdf()}
            >
              {pending ? "编译中..." : "编译生成 PDF 并创建项目"}
            </button>
            <details className="pending-fallback">
              <summary>使用已配置位置中的已有 PDF</summary>
              <label>
                PDF 文件位置
                <input
                  placeholder="填写已允许的 PDF 文件位置"
                  value={localPdfPath}
                  onChange={(event) => setLocalPdfPath(event.target.value)}
                />
                <small>
                  仅允许读取管理员配置范围内的文件，页面不会显示实际部署位置。
                </small>
              </label>
              <button
                disabled={pending || !localPdfPath.trim()}
                type="button"
                onClick={() => void applyLocalPdf()}
              >
                使用该 PDF 并创建项目
              </button>
            </details>
            <button
              className="danger-button"
              disabled={pending}
              type="button"
              onClick={() => void cancelPending()}
            >
              取消并清理
            </button>
          </section>
        </div>
      )}
    </main>
  );
}
