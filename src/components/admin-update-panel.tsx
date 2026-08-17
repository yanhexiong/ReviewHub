"use client";

import { RefreshCw, Upload } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";

type ReleaseSummary = {
  version: string;
  name: string;
  notes: string;
  publishedAt: string | null;
  hasAppImage: boolean;
  hasOfflineBundle: boolean;
  hasUpdate?: boolean;
};

type UpdateOverview = {
  repository: string;
  repositoryUrl: string;
  runtime: { available: boolean; reason: string | null };
  currentVersion: string;
  latest: ReleaseSummary | null;
  rollbackVersions: ReleaseSummary[];
  localRollbackAvailable: boolean;
  result: {
    status: "succeeded" | "failed";
    version?: string;
    message: string;
    updatedAt: number;
  } | null;
  checkError: string | null;
};

const endpoint = "/api/admin/updates";

function releaseTime(value: string | null) {
  if (!value) return "未提供";
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "未提供" : date.toLocaleString("zh-CN");
}

async function responseBody(response: Response) {
  try {
    return (await response.json()) as {
      error?: { message?: string };
      version?: string;
    };
  } catch {
    return {};
  }
}

function uploadOfflinePackage(file: File, onProgress: (value: number) => void) {
  return new Promise<{
    status: number;
    body: { error?: { message?: string } };
  }>((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("POST", `${endpoint}/offline`);
    request.responseType = "json";
    request.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable)
        onProgress(
          Math.min(100, Math.round((event.loaded / event.total) * 100)),
        );
    });
    request.addEventListener("load", () => {
      const body =
        request.response && typeof request.response === "object"
          ? request.response
          : {};
      resolve({ status: request.status, body });
    });
    request.addEventListener("error", () => reject(new Error("network")));
    const payload = new FormData();
    payload.append("package", file);
    request.send(payload);
  });
}

export function AdminUpdatePanel() {
  const [overview, setOverview] = useState<UpdateOverview | null>(null);
  const [offlinePackage, setOfflinePackage] = useState<File | null>(null);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<string | null>(null);
  const [uploadProgress, setUploadProgress] = useState<number | null>(null);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  async function load(force = false) {
    setLoading(true);
    setError("");
    try {
      const response = await fetch(`${endpoint}${force ? "?force=1" : ""}`, {
        cache: "no-store",
      });
      const body = await responseBody(response);
      if (!response.ok) {
        setError(body.error?.message ?? "无法读取应用更新信息");
        return;
      }
      setOverview(body as UpdateOverview);
    } catch {
      setError("无法读取应用更新信息，请检查服务连接。");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function schedule(
    action: "apply" | "rollback-local" | "rollback-release",
    version?: string,
  ) {
    const prompt =
      action === "apply"
        ? "将从官方发行源下载、校验并安装新版本，随后服务会重启。确认继续吗？"
        : "将校验并切换到选定的历史版本，随后服务会重启。确认继续吗？";
    if (!window.confirm(prompt)) return;
    setPending(action + (version ?? ""));
    setError("");
    setMessage("");
    try {
      const response = await fetch(endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action, ...(version ? { version } : {}) }),
      });
      const body = await responseBody(response);
      if (!response.ok) {
        setError(body.error?.message ?? "更新任务未能安排");
        return;
      }
      setMessage(
        "更新文件已完成校验，服务正在重启。请稍候刷新本页面确认结果。",
      );
    } catch {
      setError("更新任务未能安排，请检查服务连接。");
    } finally {
      setPending(null);
    }
  }

  async function installOfflinePackage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!offlinePackage) {
      setError("请选择官方离线更新包。");
      return;
    }
    if (
      !window.confirm(
        "将上传、校验并安装所选离线更新包，随后服务会重启。确认继续吗？",
      )
    )
      return;
    setPending("offline");
    setUploadProgress(0);
    setError("");
    setMessage("");
    try {
      const result = await uploadOfflinePackage(
        offlinePackage,
        setUploadProgress,
      );
      if (result.status < 200 || result.status >= 300) {
        setError(result.body.error?.message ?? "离线更新任务未能安排");
        return;
      }
      setUploadProgress(100);
      setMessage(
        "离线更新包已完成校验，服务正在重启。请稍候刷新本页面确认结果。",
      );
    } catch {
      setError("离线更新包上传失败，请检查服务连接。");
    } finally {
      setPending(null);
    }
  }

  return (
    <section
      id="admin-panel-updates"
      className="admin-section update-section"
      role="tabpanel"
      aria-labelledby="admin-tab-updates"
    >
      <div className="update-heading">
        <div>
          <h2>应用更新</h2>
          <p className="muted">
            仅信任 Review Hub 官方 GitHub
            发行版。在线和离线更新均会校验版本、平台、文件名和
            SHA-256，再由独立更新助手执行替换与健康检查。
          </p>
        </div>
        <button
          disabled={loading || pending !== null}
          type="button"
          onClick={() => void load(true)}
        >
          <RefreshCw size={15} aria-hidden="true" />
          {loading ? "正在检查..." : "检查更新"}
        </button>
      </div>

      {error && <p className="error">{error}</p>}
      {message && <p className="notice">{message}</p>}

      {overview && (
        <>
          <div className="update-source" aria-label="官方更新来源">
            <span>官方发行源</span>
            <a href={overview.repositoryUrl} target="_blank" rel="noreferrer">
              {overview.repository}
            </a>
            <small>此来源已固化在应用内，不能由管理界面更改。</small>
          </div>

          <div className="update-status-grid">
            <div>
              <span>当前版本</span>
              <strong>v{overview.currentVersion}</strong>
            </div>
            <div>
              <span>自动替换</span>
              <strong>{overview.runtime.available ? "可用" : "不可用"}</strong>
              {!overview.runtime.available && overview.runtime.reason && (
                <small>{overview.runtime.reason}</small>
              )}
            </div>
            <div>
              <span>上次更新</span>
              <strong>
                {overview.result?.status === "succeeded"
                  ? "已通过健康检查"
                  : overview.result?.status === "failed"
                    ? "未完成，已保留原版本"
                    : "暂无记录"}
              </strong>
              {overview.result?.updatedAt && (
                <small>
                  {new Date(overview.result.updatedAt).toLocaleString("zh-CN")}
                </small>
              )}
            </div>
          </div>

          {overview.checkError && (
            <p className="error">{overview.checkError}</p>
          )}

          {overview.latest && (
            <article className="update-release-card">
              <div>
                <span
                  className={
                    overview.latest.hasUpdate
                      ? "update-badge available"
                      : "update-badge"
                  }
                >
                  {overview.latest.hasUpdate ? "可更新" : "已是最新"}
                </span>
                <h3>{overview.latest.name}</h3>
                <p>
                  v{overview.latest.version} · 发布于{" "}
                  {releaseTime(overview.latest.publishedAt)}
                </p>
              </div>
              <button
                disabled={
                  pending !== null ||
                  !overview.runtime.available ||
                  !overview.latest.hasUpdate ||
                  !overview.latest.hasAppImage
                }
                type="button"
                onClick={() => void schedule("apply")}
              >
                {pending === "apply" ? "正在校验发行文件..." : "下载并更新"}
              </button>
              {!overview.latest.hasAppImage && (
                <p className="error">
                  该发行版本未提供完整的 Linux x86_64 AppImage 与校验清单。
                </p>
              )}
              <details>
                <summary>查看发行说明</summary>
                <p>{overview.latest.notes}</p>
              </details>
            </article>
          )}

          <form
            className="update-offline-form"
            onSubmit={(event) => void installOfflinePackage(event)}
          >
            <div>
              <h3>离线更新</h3>
              <p className="muted">
                选择官方发行版附带的 <code>.update.tar.gz</code>{" "}
                更新包。系统只接受与当前 Linux x86_64 平台匹配的受控包。
              </p>
            </div>
            <label className="update-file-input">
              <span>离线更新包</span>
              <input
                accept=".update.tar.gz,application/gzip,application/x-gzip"
                type="file"
                onChange={(event) =>
                  setOfflinePackage(event.target.files?.[0] ?? null)
                }
              />
              <small>
                {offlinePackage
                  ? `${offlinePackage.name} · ${Math.ceil(offlinePackage.size / 1024 / 1024)} MB`
                  : "尚未选择文件"}
              </small>
            </label>
            {uploadProgress !== null && (
              <div className="update-upload-progress" aria-live="polite">
                <div>
                  <span>上传并校验</span>
                  <strong>{uploadProgress}%</strong>
                </div>
                <progress max="100" value={uploadProgress} />
              </div>
            )}
            <button
              disabled={
                pending !== null ||
                !overview.runtime.available ||
                !offlinePackage
              }
              type="submit"
            >
              <Upload size={15} aria-hidden="true" />
              {pending === "offline" ? "正在上传并校验..." : "安装离线更新包"}
            </button>
          </form>

          <div className="update-rollback">
            <div>
              <h3>版本回退</h3>
              <p className="muted">
                本地回退会交换已保留的上一版；历史回退会重新从官方发行源下载并校验对应稳定版本。
              </p>
            </div>
            <div className="update-actions">
              <button
                disabled={
                  pending !== null ||
                  !overview.runtime.available ||
                  !overview.localRollbackAvailable
                }
                type="button"
                onClick={() => void schedule("rollback-local")}
              >
                {pending === "rollback-local"
                  ? "正在安排回退..."
                  : "回退到上一版"}
              </button>
            </div>
            <div className="update-history-list">
              {overview.rollbackVersions.map((release) => (
                <div key={release.version}>
                  <span>v{release.version}</span>
                  <small>{releaseTime(release.publishedAt)}</small>
                  <button
                    disabled={
                      pending !== null ||
                      !overview.runtime.available ||
                      !release.hasAppImage
                    }
                    type="button"
                    onClick={() =>
                      void schedule("rollback-release", release.version)
                    }
                  >
                    回退到此版本
                  </button>
                </div>
              ))}
              {!overview.rollbackVersions.length && (
                <p className="muted">暂无可供回退的历史稳定版本。</p>
              )}
            </div>
          </div>
        </>
      )}
    </section>
  );
}
