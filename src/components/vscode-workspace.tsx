"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import {
  ArrowLeft,
  ExternalLink,
  FilePlus2,
  Power,
  RefreshCw,
  Settings,
  X,
} from "lucide-react";
import {
  defaultTexLiveConfig,
  type TexAutoBuild,
  type TexBuildTool,
  type TexEngine,
  type TexLiveConfig,
} from "@/lib/texlive-shared";
import { SnapshotArchiveDialog } from "@/components/snapshot-archive-dialog";
import { PreferencesControls } from "@/features/preferences";

type SourceVersion = {
  snapshotId: string;
  versionNumber?: number;
  label?: string | null;
  shortSha?: string | null;
} | null;

type WorkspaceState =
  | { status: "loading" }
  | {
      status: "ready";
      url: string;
      sourceVersion: SourceVersion;
    }
  | { status: "error"; message: string; detail?: string };

type GitSettings = {
  userName: string;
  userEmail: string;
  hasAccessToken: boolean;
  hasSshPrivateKey: boolean;
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

type SettingsState = {
  open: boolean;
  tab: "tex" | "git" | "json";
  saving: boolean;
  error?: string;
};

const defaultGitSettings: GitSettings = {
  userName: "",
  userEmail: "",
  hasAccessToken: false,
  hasSshPrivateKey: false,
};

export function VscodeWorkspace({ projectId }: { projectId: string }) {
  const [workspace, setWorkspace] = useState<WorkspaceState>({
    status: "loading",
  });
  const [frameKey, setFrameKey] = useState(0);
  const [settingsState, setSettingsState] = useState<SettingsState>({
    open: false,
    tab: "tex",
    saving: false,
  });
  const [texForm, setTexForm] = useState<TexLiveConfig>(defaultTexLiveConfig);
  const [texProfiles, setTexProfiles] = useState<PublicTexLiveProfile[]>([]);
  const [gitForm, setGitForm] = useState<GitSettings>(defaultGitSettings);
  const [accessToken, setAccessToken] = useState("");
  const [sshPrivateKey, setSshPrivateKey] = useState("");
  const [clearAccessToken, setClearAccessToken] = useState(false);
  const [clearSshPrivateKey, setClearSshPrivateKey] = useState(false);
  const [settingsJson, setSettingsJson] = useState("");
  const [snapshotArchiveOpen, setSnapshotArchiveOpen] = useState(false);
  const [snapshotMessage, setSnapshotMessage] = useState("");

  const loadWorkspace = useCallback(async () => {
    setWorkspace({ status: "loading" });
    try {
      const response = await fetch(`/api/projects/${projectId}/vscode`, {
        cache: "no-store",
      });
      const body = (await response.json()) as {
        url?: string;
        sourceVersion?: SourceVersion;
        error?: { message?: string; detail?: string };
      };
      if (!response.ok || !body.url)
        throw Object.assign(
          new Error(body.error?.message ?? "VS Code Server 启动失败"),
          { detail: body.error?.detail },
        );
      setWorkspace({
        status: "ready",
        url: body.url,
        sourceVersion: body.sourceVersion ?? null,
      });
    } catch (error) {
      setWorkspace({
        status: "error",
        message: error instanceof Error ? error.message : "在线编辑器加载失败",
        detail:
          error instanceof Error &&
          typeof (error as Error & { detail?: unknown }).detail === "string"
            ? (error as Error & { detail: string }).detail
            : undefined,
      });
    }
  }, [projectId]);

  useEffect(() => {
    void loadWorkspace();
  }, [loadWorkspace]);

  const openSettings = useCallback(async () => {
    setSettingsState({
      open: true,
      tab: "tex",
      saving: false,
      error: undefined,
    });
    try {
      const [texResponse, gitResponse, jsonResponse] = await Promise.all([
        fetch(`/api/projects/${projectId}/texlive-config`, {
          cache: "no-store",
        }),
        fetch(`/api/projects/${projectId}/git-config`, {
          cache: "no-store",
        }),
        fetch(`/api/projects/${projectId}/vscode/settings`, {
          cache: "no-store",
        }),
      ]);
      if (!texResponse.ok || !gitResponse.ok || !jsonResponse.ok)
        throw new Error("无法读取项目设置，请确认你有该项目的管理权限");
      const texBody = (await texResponse.json()) as {
        config: TexLiveConfig;
      };
      const gitBody = (await gitResponse.json()) as {
        settings: GitSettings;
      };
      const jsonBody = (await jsonResponse.json()) as { content: string };
      setTexForm(texBody.config);
      const profilesResponse = await fetch("/api/texlive-profiles", {
        cache: "no-store",
      });
      if (profilesResponse.ok) {
        const profilesBody = (await profilesResponse.json()) as {
          profiles?: PublicTexLiveProfile[];
        };
        setTexProfiles(profilesBody.profiles ?? []);
      }
      setGitForm(gitBody.settings);
      setSettingsJson(jsonBody.content);
      setAccessToken("");
      setSshPrivateKey("");
      setClearAccessToken(false);
      setClearSshPrivateKey(false);
    } catch (error) {
      setSettingsState((state) => ({
        ...state,
        error: error instanceof Error ? error.message : "加载项目设置失败",
      }));
    }
  }, [projectId]);

  const saveSettings = useCallback(async () => {
    setSettingsState((state) => ({ ...state, saving: true, error: undefined }));
    try {
      if (settingsState.tab === "json") {
        const jsonResponse = await fetch(
          `/api/projects/${projectId}/vscode/settings`,
          {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ content: settingsJson }),
          },
        );
        if (!jsonResponse.ok) {
          const body = (await jsonResponse.json().catch(() => null)) as {
            error?: { message?: string };
          } | null;
          throw new Error(body?.error?.message ?? "settings.json 保存失败");
        }
        const restartResponse = await fetch(
          `/api/projects/${projectId}/vscode/restart`,
          { method: "POST" },
        );
        if (!restartResponse.ok)
          throw new Error("settings.json 已保存，但编辑器重启失败，请刷新页面");
        setSettingsState({ open: false, tab: "json", saving: false });
        setFrameKey((value) => value + 1);
        return;
      }
      const [texResponse, gitResponse] = await Promise.all([
        fetch(`/api/projects/${projectId}/texlive-config`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            profileId: texForm.profileId,
            engine: texForm.engine,
            buildTool: texForm.buildTool,
            outputDirectory: texForm.outputDirectory,
            autoBuild: texForm.autoBuild,
            pdfPreview: texForm.pdfPreview,
            syncTex: texForm.syncTex,
            shellEscape: texForm.shellEscape,
            texliveBinPath: texForm.texliveBinPath,
            texRootPath: texForm.texRootPath,
          }),
        }),
        fetch(`/api/projects/${projectId}/git-config`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            userName: gitForm.userName,
            userEmail: gitForm.userEmail,
            accessToken,
            sshPrivateKey,
            clearAccessToken,
            clearSshPrivateKey,
          }),
        }),
      ]);
      if (!texResponse.ok || !gitResponse.ok) {
        const texError = (await texResponse.json().catch(() => null)) as {
          error?: { message?: string };
        } | null;
        const gitError = (await gitResponse.json().catch(() => null)) as {
          error?: { message?: string };
        } | null;
        throw new Error(
          texError?.error?.message ??
            gitError?.error?.message ??
            "保存项目设置失败",
        );
      }
      const restartResponse = await fetch(
        `/api/projects/${projectId}/vscode/restart`,
        { method: "POST" },
      );
      if (!restartResponse.ok)
        throw new Error("设置已保存，但编辑器重启失败，请手动刷新页面");
      setSettingsState({ open: false, tab: "tex", saving: false });
      setFrameKey((value) => value + 1);
    } catch (error) {
      setSettingsState((state) => ({
        ...state,
        saving: false,
        error: error instanceof Error ? error.message : "保存项目设置失败",
      }));
    }
  }, [
    projectId,
    texForm,
    gitForm,
    accessToken,
    sshPrivateKey,
    clearAccessToken,
    clearSshPrivateKey,
    settingsState.tab,
    settingsJson,
  ]);

  const closeSettings = () =>
    setSettingsState({
      open: false,
      tab: "tex",
      saving: false,
      error: undefined,
    });

  const restartEditor = useCallback(async () => {
    try {
      const response = await fetch(
        `/api/projects/${projectId}/vscode/restart`,
        { method: "POST" },
      );
      if (!response.ok) throw new Error("重启失败");
      setFrameKey((value) => value + 1);
      await loadWorkspace();
    } catch (error) {
      setWorkspace({
        status: "error",
        message:
          error instanceof Error
            ? error.message
            : "VS Code 重启失败，请刷新页面",
      });
    }
  }, [projectId, loadWorkspace]);

  const returnToProjectSource = useCallback(async () => {
    try {
      const response = await fetch(
        `/api/projects/${projectId}/vscode/return-source`,
        { method: "POST" },
      );
      const body = (await response.json()) as {
        url?: string;
        error?: { message?: string };
      };
      if (!response.ok || !body.url)
        throw new Error(body.error?.message ?? "返回项目源码失败");
      setFrameKey((value) => value + 1);
      await loadWorkspace();
    } catch (error) {
      setWorkspace({
        status: "error",
        message:
          error instanceof Error
            ? error.message
            : "返回项目源码失败，请刷新页面",
      });
    }
  }, [projectId, loadWorkspace]);

  const setEngine = (engine: TexEngine) =>
    setTexForm((form) => ({ ...form, engine }));
  const setBuildTool = (buildTool: TexBuildTool) =>
    setTexForm((form) => ({ ...form, buildTool }));
  const setAutoBuild = (autoBuild: TexAutoBuild) =>
    setTexForm((form) => ({ ...form, autoBuild }));

  return (
    <main className="vscode-workspace-page">
      <header className="vscode-workspace-toolbar">
        <Link
          aria-label="返回项目"
          className="vscode-workspace-back"
          href={`/projects/${projectId}`}
          title="返回项目"
        >
          <ArrowLeft size={17} aria-hidden="true" />
          <span>返回项目</span>
        </Link>
        <div className="vscode-workspace-title">
          <strong>在线编辑</strong>
          <span>VS Code Server</span>
        </div>
        <div className="vscode-workspace-actions">
          <PreferencesControls placement="toolbar" />
          <button
            aria-label="项目设置"
            className="vscode-workspace-icon"
            title="项目设置"
            type="button"
            onClick={() => void openSettings()}
          >
            <Settings size={16} aria-hidden="true" />
          </button>
          {workspace.status === "ready" && (
            <>
              <button
                aria-label="重启 VS Code"
                className="vscode-workspace-icon"
                title="重启 VS Code"
                type="button"
                onClick={() => void restartEditor()}
              >
                <Power size={16} aria-hidden="true" />
              </button>
              <button
                aria-label="直接归档指定 PDF 为新版本"
                className="vscode-workspace-icon"
                title="直接归档指定 PDF 为新版本（不触发编译）"
                type="button"
                onClick={() => {
                  setSnapshotMessage("");
                  setSnapshotArchiveOpen(true);
                }}
              >
                <FilePlus2 size={16} aria-hidden="true" />
              </button>
              <button
                aria-label="重新加载编辑器"
                className="vscode-workspace-icon"
                title="重新加载编辑器"
                type="button"
                onClick={() => setFrameKey((value) => value + 1)}
              >
                <RefreshCw size={16} aria-hidden="true" />
              </button>
              <a
                className="vscode-workspace-open"
                href={workspace.url}
                rel="noreferrer"
                target="_blank"
              >
                <ExternalLink size={15} aria-hidden="true" />
                <span>新窗口打开</span>
              </a>
            </>
          )}
        </div>
      </header>

      <section className="vscode-workspace-frame" aria-label="VS Code 编辑器">
        {workspace.status === "loading" && (
          <div className="vscode-workspace-state">
            <strong>正在启动 VS Code Server...</strong>
            <span>首次打开可能需要几秒钟。</span>
          </div>
        )}
        {workspace.status === "error" && (
          <div className="vscode-workspace-state vscode-workspace-error">
            <strong>{workspace.message}</strong>
            <span>
              {workspace.detail ??
                "编辑器服务暂不可用，请联系管理员检查配置后重试。"}
            </span>
            <button type="button" onClick={() => void loadWorkspace()}>
              <RefreshCw size={15} aria-hidden="true" />
              重试
            </button>
          </div>
        )}
        {workspace.status === "ready" && (
          <>
            {workspace.sourceVersion && (
              <div className="vscode-source-banner">
                <span>
                  当前打开：Snapshot #
                  {workspace.sourceVersion.versionNumber ?? "?"} 的源码
                  {workspace.sourceVersion.label
                    ? `（${workspace.sourceVersion.label}）`
                    : ""}
                  {workspace.sourceVersion.shortSha
                    ? ` · ${workspace.sourceVersion.shortSha}`
                    : ""}
                </span>
                <button
                  type="button"
                  onClick={() => void returnToProjectSource()}
                >
                  返回项目源码
                </button>
              </div>
            )}
            <iframe
              key={frameKey}
              allow="clipboard-read; clipboard-write"
              className="vscode-workspace-iframe"
              referrerPolicy="same-origin"
              src={workspace.url}
              title="VS Code Server"
            />
          </>
        )}
      </section>

      {snapshotMessage && (
        <div className="vscode-workspace-notice" role="status">
          {snapshotMessage}
        </div>
      )}

      <SnapshotArchiveDialog
        open={snapshotArchiveOpen}
        projectId={projectId}
        onClose={() => setSnapshotArchiveOpen(false)}
        onCreated={(snapshot) =>
          setSnapshotMessage(
            `已创建 Snapshot #${snapshot.version_number}，可返回项目首页查看。`,
          )
        }
      />

      {settingsState.open && (
        <div
          aria-label="项目设置"
          aria-modal="true"
          className="vscode-settings-overlay"
          role="dialog"
        >
          <div className="vscode-settings-panel">
            <header className="vscode-settings-header">
              <strong>项目设置</strong>
              <button
                aria-label="关闭项目设置"
                className="vscode-settings-close"
                title="关闭"
                type="button"
                onClick={closeSettings}
              >
                <X size={16} aria-hidden="true" />
              </button>
            </header>
            <nav className="vscode-settings-tabs">
              <button
                className={settingsState.tab === "tex" ? "active" : ""}
                type="button"
                onClick={() =>
                  setSettingsState((state) => ({ ...state, tab: "tex" }))
                }
              >
                TeX 编译
              </button>
              <button
                className={settingsState.tab === "git" ? "active" : ""}
                type="button"
                onClick={() =>
                  setSettingsState((state) => ({ ...state, tab: "git" }))
                }
              >
                Git 身份与密钥
              </button>
              <button
                className={settingsState.tab === "json" ? "active" : ""}
                type="button"
                onClick={() =>
                  setSettingsState((state) => ({ ...state, tab: "json" }))
                }
              >
                settings.json
              </button>
            </nav>
            <div className="vscode-settings-body">
              {settingsState.error && (
                <div className="vscode-settings-error">
                  {settingsState.error}
                </div>
              )}
              {settingsState.tab === "json" ? (
                <div className="vscode-settings-form">
                  <label>
                    全局 settings.json（文本编辑，保存后自动重启编辑器）
                    <textarea
                      className="vscode-settings-json"
                      rows={18}
                      spellCheck={false}
                      value={settingsJson}
                      onChange={(event) => setSettingsJson(event.target.value)}
                    />
                  </label>
                </div>
              ) : settingsState.tab === "tex" ? (
                <div className="vscode-settings-form">
                  <label>
                    管理员编译配置
                    <select
                      value={texForm.profileId}
                      onChange={(event) =>
                        setTexForm((form) => ({
                          ...form,
                          profileId: event.target.value,
                        }))
                      }
                    >
                      <option value="">使用项目手动设置</option>
                      {texProfiles.map((profile) => (
                        <option key={profile.id} value={profile.id}>
                          {profile.name} · {profile.engine} /{" "}
                          {profile.buildTool}
                        </option>
                      ))}
                    </select>
                    {!texProfiles.length && (
                      <small className="muted">管理员尚未发布可用配置。</small>
                    )}
                  </label>
                  <label>
                    引擎
                    <select
                      value={texForm.engine}
                      onChange={(event) =>
                        setEngine(event.target.value as TexEngine)
                      }
                    >
                      <option value="pdflatex">pdfLaTeX</option>
                      <option value="xelatex">XeLaTeX</option>
                      <option value="lualatex">LuaLaTeX</option>
                      <option value="tectonic">Tectonic</option>
                    </select>
                  </label>
                  <label>
                    构建方式
                    <select
                      value={texForm.buildTool}
                      onChange={(event) =>
                        setBuildTool(event.target.value as TexBuildTool)
                      }
                    >
                      <option value="latexmk">latexmk（推荐）</option>
                      <option value="engine">直接调用引擎</option>
                      <option value="tectonic">tectonic</option>
                    </select>
                  </label>
                  <label>
                    编译器目录
                    <input
                      placeholder="留空使用应用配置的默认编译环境"
                      type="password"
                      value={texForm.texliveBinPath}
                      onChange={(event) =>
                        setTexForm((form) => ({
                          ...form,
                          texliveBinPath: event.target.value,
                        }))
                      }
                    />
                  </label>
                  <label>
                    输出目录
                    <input
                      placeholder="build"
                      value={texForm.outputDirectory}
                      onChange={(event) =>
                        setTexForm((form) => ({
                          ...form,
                          outputDirectory: event.target.value,
                        }))
                      }
                    />
                  </label>
                  <label>
                    TeX 入口文件
                    <input
                      placeholder="main.tex（留空由 LaTeX Workshop 自动检测）"
                      value={texForm.texRootPath}
                      onChange={(event) =>
                        setTexForm((form) => ({
                          ...form,
                          texRootPath: event.target.value,
                        }))
                      }
                    />
                  </label>
                  <label>
                    自动构建
                    <select
                      value={texForm.autoBuild}
                      onChange={(event) =>
                        setAutoBuild(event.target.value as TexAutoBuild)
                      }
                    >
                      <option value="off">关闭</option>
                      <option value="on-save">保存时</option>
                      <option value="on-start">启动时</option>
                    </select>
                  </label>
                  <label className="vscode-settings-check">
                    <input
                      checked={texForm.shellEscape}
                      type="checkbox"
                      onChange={(event) =>
                        setTexForm((form) => ({
                          ...form,
                          shellEscape: event.target.checked,
                        }))
                      }
                    />
                    允许 shell escape
                  </label>
                </div>
              ) : (
                <div className="vscode-settings-form">
                  <label>
                    Git 提交姓名
                    <input
                      placeholder="Zhang San"
                      value={gitForm.userName}
                      onChange={(event) =>
                        setGitForm((form) => ({
                          ...form,
                          userName: event.target.value,
                        }))
                      }
                    />
                  </label>
                  <label>
                    Git 提交邮箱
                    <input
                      placeholder="you@example.com"
                      type="email"
                      value={gitForm.userEmail}
                      onChange={(event) =>
                        setGitForm((form) => ({
                          ...form,
                          userEmail: event.target.value,
                        }))
                      }
                    />
                  </label>
                  <label>
                    GitHub Access Token
                    <input
                      placeholder={
                        gitForm.hasAccessToken
                          ? "已配置，留空保持不变"
                          : "ghp_..."
                      }
                      type="password"
                      value={accessToken}
                      onChange={(event) => setAccessToken(event.target.value)}
                    />
                  </label>
                  {gitForm.hasAccessToken && (
                    <label className="vscode-settings-check">
                      <input
                        checked={clearAccessToken}
                        type="checkbox"
                        onChange={(event) =>
                          setClearAccessToken(event.target.checked)
                        }
                      />
                      清除已保存的 Access Token
                    </label>
                  )}
                  <label>
                    SSH 私钥
                    <textarea
                      placeholder={
                        gitForm.hasSshPrivateKey
                          ? "已配置，留空保持不变"
                          : "-----BEGIN OPENSSH PRIVATE KEY-----"
                      }
                      rows={7}
                      value={sshPrivateKey}
                      onChange={(event) => setSshPrivateKey(event.target.value)}
                    />
                  </label>
                  {gitForm.hasSshPrivateKey && (
                    <label className="vscode-settings-check">
                      <input
                        checked={clearSshPrivateKey}
                        type="checkbox"
                        onChange={(event) =>
                          setClearSshPrivateKey(event.target.checked)
                        }
                      />
                      清除已保存的 SSH 私钥
                    </label>
                  )}
                </div>
              )}
            </div>
            <footer className="vscode-settings-footer">
              <button
                className="vscode-settings-cancel"
                type="button"
                onClick={closeSettings}
              >
                取消
              </button>
              <button
                className="vscode-settings-save"
                disabled={settingsState.saving}
                type="button"
                onClick={() => void saveSettings()}
              >
                {settingsState.saving ? "保存中..." : "保存并重启编辑器"}
              </button>
            </footer>
          </div>
        </div>
      )}
    </main>
  );
}
