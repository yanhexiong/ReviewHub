"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  Code2,
  Download,
  FileUp,
  Pencil,
  FileText,
  FolderOpen,
  Home,
  MessageCircle,
  Package,
  RefreshCw,
} from "lucide-react";
import { SnapshotArchiveDialog } from "@/components/snapshot-archive-dialog";
import { ExternalPdfDialog } from "@/components/external-pdf-dialog";

type Project = {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  repository_configured: boolean;
  pdf_configured: boolean;
  source_file_name: string | null;
  source_type: "local" | "archive" | "github";
  source_uri: string | null;
  source_branch: string | null;
  has_source_credential: boolean;
  access: {
    collaborator_permission: "owner" | "review" | "manage";
    can_review: boolean;
    can_manage: boolean;
    is_owner: boolean;
  };
};
type Snapshot = {
  id: string;
  version_number: number;
  page_count: number;
  archived_at: number;
  snapshot_label: string | null;
  snapshot_note: string | null;
  git_branch: string | null;
  git_commit_short_sha: string | null;
  git_worktree_dirty: number;
  source_kind: "project" | "external";
  has_git_source: boolean;
  has_source_patch: boolean;
  comment_count: number;
};
type Collaborator = {
  user_id: string;
  email: string;
  display_name: string;
  permission: "review" | "manage";
  is_active: number;
};

const formatDate = (value: number) => new Date(value).toLocaleString("zh-CN");

export function ProjectHome({ projectId }: { projectId: string }) {
  const [project, setProject] = useState<Project | null>(null);
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const [error, setError] = useState("");
  const [configMessage, setConfigMessage] = useState("");
  const [syncPending, setSyncPending] = useState(false);
  const [snapshotArchiveOpen, setSnapshotArchiveOpen] = useState(false);
  const [externalPdfOpen, setExternalPdfOpen] = useState(false);
  const [snapshotMessage, setSnapshotMessage] = useState("");
  const [configPending, setConfigPending] = useState(false);
  const [collaborators, setCollaborators] = useState<Collaborator[]>([]);
  const [owner, setOwner] = useState<{
    email: string;
    display_name: string;
  } | null>(null);
  const [collaboratorEmail, setCollaboratorEmail] = useState("");
  const [collaboratorPermission, setCollaboratorPermission] = useState<
    "review" | "manage"
  >("review");
  const [collaboratorPending, setCollaboratorPending] = useState(false);
  const [removingCollaborator, setRemovingCollaborator] = useState<
    string | null
  >(null);
  const [collaboratorMessage, setCollaboratorMessage] = useState("");
  const [deletingSnapshotId, setDeletingSnapshotId] = useState<string | null>(
    null,
  );
  const configRef = useRef<HTMLDetailsElement>(null);

  useEffect(() => {
    Promise.all([
      fetch(`/api/projects/${projectId}`),
      fetch(`/api/projects/${projectId}/snapshots`),
    ])
      .then(async ([projectResponse, snapshotsResponse]) => {
        const projectBody = await projectResponse.json();
        if (!projectResponse.ok)
          throw new Error(projectBody.error?.message ?? "项目加载失败");
        setProject(projectBody.project);
        if (projectBody.project.access?.can_manage) {
          const collaboratorsResponse = await fetch(
            `/api/projects/${projectId}/collaborators`,
          );
          if (collaboratorsResponse.ok) {
            const collaboratorsBody = await collaboratorsResponse.json();
            setOwner(collaboratorsBody.owner ?? null);
            setCollaborators(collaboratorsBody.collaborators ?? []);
          }
        }
        const snapshotsBody = await snapshotsResponse.json();
        if (!snapshotsResponse.ok)
          throw new Error(snapshotsBody.error?.message ?? "快照加载失败");
        setSnapshots(snapshotsBody.snapshots ?? []);
      })
      .catch((reason) =>
        setError(reason instanceof Error ? reason.message : "项目加载失败"),
      );
  }, [projectId]);

  const commentCount = useMemo(
    () =>
      snapshots.reduce((total, snapshot) => total + snapshot.comment_count, 0),
    [snapshots],
  );
  const latest = snapshots[0];
  async function saveProject(form: FormData) {
    setConfigMessage("");
    setConfigPending(true);
    try {
      const repositoryPath = String(form.get("repositoryPath") ?? "").trim();
      const sourcePdfPath = String(form.get("sourcePdfPath") ?? "").trim();
      const response = await fetch(`/api/projects/${projectId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: form.get("name"),
          slug: form.get("slug"),
          description: form.get("description"),
          ...(repositoryPath ? { repositoryPath } : {}),
          ...(sourcePdfPath ? { sourcePdfPath } : {}),
          githubToken: form.get("githubToken") || undefined,
          clearGithubToken: form.get("clearGithubToken") === "on",
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setConfigMessage(body.error?.message ?? "项目配置保存失败");
        return;
      }
      setProject(body.project);
      setConfigMessage("项目配置已保存。");
    } catch {
      setConfigMessage("项目配置保存失败，请检查服务连接。");
    } finally {
      setConfigPending(false);
    }
  }
  async function syncGithub() {
    if (!project || project.source_type !== "github") return;
    if (
      !window.confirm(
        "同步会替换当前 GitHub 受管工作区，但不会修改已有快照或远程仓库。是否继续执行？",
      )
    )
      return;
    setConfigMessage("");
    setSyncPending(true);
    try {
      const response = await fetch(`/api/projects/${projectId}/sync`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ branch: project.source_branch || "main" }),
      });
      const body = await response.json();
      if (!response.ok) {
        setConfigMessage(body.error?.message ?? "GitHub 同步失败");
        return;
      }
      setProject(body.project);
      setConfigMessage(
        `GitHub ${body.project.source_branch || "main"} 分支已同步，当前源文件路径已更新。`,
      );
    } catch {
      setConfigMessage("GitHub 同步失败，请检查服务连接。");
    } finally {
      setSyncPending(false);
    }
  }
  async function saveCollaborator(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const email = collaboratorEmail.trim();
    if (!email) {
      setCollaboratorMessage("请输入已注册账户的邮箱。");
      return;
    }
    setCollaboratorPending(true);
    setCollaboratorMessage("");
    try {
      const response = await fetch(`/api/projects/${projectId}/collaborators`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, permission: collaboratorPermission }),
      });
      const body = await response.json();
      if (!response.ok)
        throw new Error(body.error?.message ?? "协作者保存失败");
      setOwner(body.owner ?? owner);
      setCollaborators(body.collaborators ?? []);
      setCollaboratorEmail("");
      setCollaboratorMessage("协作者权限已保存。");
    } catch (error) {
      setCollaboratorMessage(
        error instanceof Error ? error.message : "协作者保存失败",
      );
    } finally {
      setCollaboratorPending(false);
    }
  }
  async function removeCollaborator(item: Collaborator) {
    if (
      !window.confirm(
        `移除 ${item.display_name || item.email} 的项目访问权限？`,
      )
    )
      return;
    setRemovingCollaborator(item.user_id);
    setCollaboratorMessage("");
    try {
      const response = await fetch(
        `/api/projects/${projectId}/collaborators/${item.user_id}`,
        { method: "DELETE" },
      );
      const body = await response.json();
      if (!response.ok)
        throw new Error(body.error?.message ?? "移除协作者失败");
      setCollaborators((current) =>
        current.filter((collaborator) => collaborator.user_id !== item.user_id),
      );
      setCollaboratorMessage("协作者已移除。");
    } catch (error) {
      setCollaboratorMessage(
        error instanceof Error ? error.message : "移除协作者失败",
      );
    } finally {
      setRemovingCollaborator(null);
    }
  }
  async function refreshSnapshots() {
    const response = await fetch(`/api/projects/${projectId}/snapshots`, {
      cache: "no-store",
    });
    if (!response.ok) return;
    setSnapshots((await response.json()).snapshots ?? []);
  }
  async function deleteSnapshot(snapshot: Snapshot) {
    if (
      !window.confirm(
        `删除 Snapshot #${snapshot.version_number} 会同时移除其批注、回复和分享链接，且无法撤销。是否继续？`,
      )
    )
      return;
    setDeletingSnapshotId(snapshot.id);
    setConfigMessage("");
    try {
      const response = await fetch(
        `/api/projects/${projectId}/snapshots/${snapshot.id}`,
        { method: "DELETE" },
      );
      const body = (await response.json()) as { error?: { message?: string } };
      if (!response.ok) throw new Error(body.error?.message ?? "快照删除失败");
      setSnapshots((current) =>
        current.filter((item) => item.id !== snapshot.id),
      );
      setConfigMessage(`Snapshot #${snapshot.version_number} 已删除。`);
    } catch (error) {
      setConfigMessage(error instanceof Error ? error.message : "快照删除失败");
    } finally {
      setDeletingSnapshotId(null);
    }
  }
  async function switchToSnapshot(snapshot: Snapshot) {
    setConfigMessage("");
    try {
      const response = await fetch(
        `/api/projects/${projectId}/vscode/switch-source`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ snapshotId: snapshot.id }),
        },
      );
      const body = (await response.json()) as {
        url?: string;
        error?: { message?: string };
      };
      if (!response.ok || !body.url)
        throw new Error(body.error?.message ?? "切换源码版本失败");
      location.href = body.url;
    } catch (error) {
      setConfigMessage(
        error instanceof Error ? error.message : "切换源码版本失败",
      );
    }
  }
  if (!project && !error)
    return (
      <main className="projects">
        <p className="muted">正在加载项目...</p>
      </main>
    );
  if (!project)
    return (
      <main className="projects">
        <p className="error">{error}</p>
      </main>
    );

  return (
    <main className="projects project-home">
      <header>
        <div>
          <p className="eyebrow">REVIEW HUB / 项目首页</p>
          <h1>{project.name}</h1>
          <p className="muted">{project.description || "未添加项目说明"}</p>
        </div>
        <div className="project-home-actions">
          <Link className="toolbar-link" href="/projects">
            <Home size={16} /> 网站首页
          </Link>
          <Link
            className="toolbar-link"
            href={`/projects/${projectId}/review${latest ? `?snapshotId=${latest.id}` : ""}`}
          >
            <FolderOpen size={16} /> 进入审阅
          </Link>
          {project.access.can_manage && (
            <>
              <Link
                className="toolbar-link"
                href={`/projects/${projectId}/editor`}
                title="编辑论文源文件"
              >
                <Code2 size={16} /> 在线编辑
              </Link>
              {project.source_type === "github" && (
                <button
                  className="toolbar-link"
                  disabled={syncPending}
                  type="button"
                  onClick={() => void syncGithub()}
                >
                  <RefreshCw size={16} />
                  {syncPending ? "同步中..." : "同步 GitHub"}
                </button>
              )}
              <a
                className="toolbar-link"
                href="#project-config"
                onClick={(event) => {
                  event.preventDefault();
                  if (configRef.current) {
                    configRef.current.open = true;
                    configRef.current.scrollIntoView({ behavior: "smooth" });
                  }
                }}
              >
                <Pencil size={16} /> 编辑项目
              </a>
            </>
          )}
        </div>
      </header>
      {error && <p className="error">{error}</p>}
      <section className="project-home-summary" aria-label="项目概览">
        <div>
          <span>审阅文件</span>
          <strong>{snapshots.length}</strong>
        </div>
        <div>
          <span>全部批注</span>
          <strong>{commentCount}</strong>
        </div>
        <div>
          <span>当前版本</span>
          <strong>{latest ? `#${latest.version_number}` : "—"}</strong>
        </div>
      </section>
      <section className="project-home-paths">
        <div>
          <strong>论文仓库</strong>
          <span className="muted">
            {project.repository_configured
              ? "已配置（具体位置已隐藏）"
              : "未配置"}
          </span>
        </div>
        <div>
          <strong>当前 PDF 源文件</strong>
          <span className="muted">
            {project.pdf_configured
              ? `已配置${project.source_file_name ? ` · ${project.source_file_name}` : ""}`
              : "未配置"}
          </span>
        </div>
        {project.source_type === "github" && project.source_uri && (
          <div>
            <strong>GitHub 来源</strong>
            <span className="project-home-source-meta">
              {project.source_uri} · {project.source_branch || "main"}
            </span>
          </div>
        )}
      </section>
      {project.access.can_manage && (
        <details className="project-config" id="project-config" ref={configRef}>
          <summary>
            <Pencil size={15} /> 编辑项目配置
          </summary>
          <form action={saveProject}>
            <div className="project-config-grid">
              <label>
                项目名称
                <input name="name" defaultValue={project.name} required />
              </label>
              <label>
                URL 标识
                <input
                  name="slug"
                  defaultValue={project.slug}
                  pattern="[a-z0-9-]+"
                  required
                />
              </label>
              <label className="project-config-wide">
                项目说明
                <textarea
                  name="description"
                  rows={2}
                  defaultValue={project.description ?? ""}
                />
              </label>
              <label>
                论文仓库位置
                <input
                  name="repositoryPath"
                  placeholder="留空表示保持当前配置"
                />
              </label>
              <label>
                当前 PDF 文件位置
                <input
                  name="sourcePdfPath"
                  placeholder="留空表示保持当前配置"
                />
                <small>位置只用于读取文件，页面不会回显已保存的位置。</small>
              </label>
              {project.source_type === "github" && (
                <label className="project-config-wide">
                  私有仓库访问令牌
                  <input
                    name="githubToken"
                    type="password"
                    autoComplete="new-password"
                    placeholder={
                      project.has_source_credential
                        ? "已配置令牌；输入新令牌可替换"
                        : "留空则使用账户默认令牌或公开仓库访问"
                    }
                  />
                  <small className="muted">
                    令牌不会回显。需要删除已保存令牌时勾选右侧选项。
                  </small>
                  <span className="project-config-checkbox">
                    <input name="clearGithubToken" type="checkbox" />
                    清除已保存的 GitHub 令牌
                  </span>
                </label>
              )}
            </div>
            {configMessage && <p className="muted">{configMessage}</p>}
            <button disabled={configPending} type="submit">
              {configPending ? "保存中..." : "保存项目配置"}
            </button>
          </form>
        </details>
      )}
      {project.access?.can_manage && (
        <section
          className="collaborators-section"
          aria-labelledby="collaborators-title"
        >
          <div className="project-file-heading">
            <div>
              <p className="eyebrow">项目访问</p>
              <h2 id="collaborators-title">协作者</h2>
            </div>
            <span className="muted">已注册账户才能加入项目</span>
          </div>
          <div className="collaborator-owner">
            <strong>项目所有者</strong>
            <span>{owner?.display_name || owner?.email || "已配置"}</span>
          </div>
          <form className="collaborator-form" onSubmit={saveCollaborator}>
            <label>
              协作者邮箱
              <input
                type="email"
                value={collaboratorEmail}
                onChange={(event) => setCollaboratorEmail(event.target.value)}
                placeholder="name@example.com"
                required
              />
            </label>
            <label>
              权限
              <select
                value={collaboratorPermission}
                onChange={(event) =>
                  setCollaboratorPermission(
                    event.target.value as "review" | "manage",
                  )
                }
              >
                <option value="review">仅审阅：查看、批注和回复</option>
                {project.access.is_owner && (
                  <option value="manage">项目管理：包含配置和版本管理</option>
                )}
              </select>
            </label>
            <button disabled={collaboratorPending} type="submit">
              {collaboratorPending ? "保存中..." : "添加或更新"}
            </button>
          </form>
          {collaboratorMessage && (
            <p className="notice">{collaboratorMessage}</p>
          )}
          {collaborators.length ? (
            <div className="collaborator-list">
              {collaborators.map((item) => (
                <div className="collaborator-row" key={item.user_id}>
                  <div>
                    <strong>{item.display_name || item.email}</strong>
                    <small>{item.email}</small>
                  </div>
                  <span className="permission-badge">
                    {item.permission === "manage" ? "项目管理" : "仅审阅"}
                  </span>
                  {(project.access.is_owner ||
                    item.permission === "review") && (
                    <button
                      className="danger-button"
                      disabled={removingCollaborator === item.user_id}
                      type="button"
                      onClick={() => void removeCollaborator(item)}
                    >
                      {removingCollaborator === item.user_id
                        ? "移除中..."
                        : "移除"}
                    </button>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <p className="muted">尚未添加协作者。</p>
          )}
        </section>
      )}
      <section className="project-file-section">
        <div className="project-file-heading">
          <div>
            <p className="eyebrow">文件与快照</p>
            <h2>审阅文件管理</h2>
          </div>
          {project.access.can_manage && (
            <>
              <Link
                className="toolbar-link"
                href={`/projects/${projectId}/review`}
              >
                新建或归档快照
              </Link>
              <button
                className="toolbar-link"
                type="button"
                onClick={() => {
                  setSnapshotMessage("");
                  setSnapshotArchiveOpen(true);
                }}
              >
                <RefreshCw size={15} />
                直接归档指定 PDF 为新版本
              </button>
              <button
                className="toolbar-link"
                type="button"
                onClick={() => setExternalPdfOpen(true)}
              >
                <FileUp size={15} />
                添加外部 PDF
              </button>
            </>
          )}
        </div>
        <p className="muted">
          直接归档只会保存已经生成的 PDF，不会触发 TeX
          编译；打开窗口后会扫描项目仓库中的
          PDF，也可以填写本次编译输出的位置。外部 PDF 会独立归档，可选择不关联
          Git 或手动记录来源提交。
        </p>
        {snapshotMessage && <p className="notice">{snapshotMessage}</p>}
        {snapshots.length ? (
          <div className="project-file-list">
            {snapshots.map((snapshot) => (
              <article className="project-file-row" key={snapshot.id}>
                <div className="project-file-main">
                  <FileText size={19} />
                  <div>
                    <strong>
                      {snapshot.snapshot_label ||
                        `版本 ${snapshot.version_number}`}
                    </strong>
                    <small>
                      Snapshot #{snapshot.version_number} ·{" "}
                      {snapshot.page_count} 页 ·{" "}
                      {formatDate(snapshot.archived_at)}
                    </small>
                    {snapshot.snapshot_note && <p>{snapshot.snapshot_note}</p>}
                  </div>
                </div>
                <div className="project-file-meta">
                  <span>
                    <MessageCircle size={14} /> {snapshot.comment_count} 条批注
                  </span>
                  <span>
                    {snapshot.source_kind === "external"
                      ? snapshot.git_commit_short_sha
                        ? `手动关联 Git · ${snapshot.git_commit_short_sha}`
                        : "外部 PDF · 未关联 Git"
                      : snapshot.git_commit_short_sha ||
                        snapshot.git_branch ||
                        "Git 不可用"}
                  </span>
                  <span
                    className={`snapshot-origin-badge ${snapshot.source_kind === "external" ? "external" : "project"}`}
                  >
                    {snapshot.source_kind === "external"
                      ? "外部 PDF"
                      : "项目 PDF"}
                  </span>
                  {snapshot.git_worktree_dirty === 1 && (
                    <span className="snapshot-dirty-badge">
                      {snapshot.has_source_patch
                        ? "含未提交修改（已归档）"
                        : "含未提交修改（未归档源码）"}
                    </span>
                  )}
                </div>
                <div className="project-file-actions">
                  <Link
                    href={`/projects/${projectId}/review?snapshotId=${snapshot.id}`}
                  >
                    打开审阅
                  </Link>
                  <a
                    href={`/api/projects/${projectId}/export?format=json&snapshotId=${snapshot.id}`}
                  >
                    <Download size={14} /> 导出记录
                  </a>
                  <a
                    href={`/api/projects/${projectId}/export?format=source-pdf&snapshotId=${snapshot.id}`}
                  >
                    <Download size={14} /> 原始 PDF
                  </a>
                  <a
                    href={`/api/projects/${projectId}/export?format=annotated-pdf&snapshotId=${snapshot.id}`}
                  >
                    <Download size={14} /> 带批注 PDF
                  </a>
                  <a
                    href={`/api/projects/${projectId}/export?format=bundle&snapshotId=${snapshot.id}`}
                  >
                    <Package size={14} /> 记录 + 原始 PDF
                  </a>
                  {project.access.can_manage && (
                    <>
                      <button
                        className="danger-button"
                        disabled={deletingSnapshotId !== null}
                        type="button"
                        onClick={() => void deleteSnapshot(snapshot)}
                      >
                        {deletingSnapshotId === snapshot.id
                          ? "删除中..."
                          : "删除版本"}
                      </button>
                      {snapshot.has_git_source && (
                        <button
                          disabled={deletingSnapshotId !== null}
                          type="button"
                          onClick={() => void switchToSnapshot(snapshot)}
                        >
                          切换到该版本源码
                        </button>
                      )}
                    </>
                  )}
                </div>
              </article>
            ))}
          </div>
        ) : (
          <div className="empty">
            尚未归档审阅文件。进入审阅工作区后创建第一份快照。
          </div>
        )}
      </section>
      <SnapshotArchiveDialog
        open={snapshotArchiveOpen}
        projectId={projectId}
        onClose={() => setSnapshotArchiveOpen(false)}
        onCreated={(snapshot) => {
          setSnapshotMessage(
            `已创建 Snapshot #${snapshot.version_number}，现在可以进入审阅。`,
          );
          void refreshSnapshots();
        }}
      />
      <ExternalPdfDialog
        open={externalPdfOpen}
        projectId={projectId}
        onClose={() => setExternalPdfOpen(false)}
        onCreated={(snapshot) => {
          setSnapshotMessage(
            `已添加外部 PDF 并创建 Snapshot #${snapshot.version_number}。`,
          );
          void refreshSnapshots();
        }}
      />
    </main>
  );
}
