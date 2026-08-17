"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Check,
  Copy,
  Eye,
  EyeOff,
  LoaderCircle,
  RefreshCw,
  Share2,
  X,
} from "lucide-react";

type SnapshotOption = {
  id: string;
  version_number: number;
  snapshot_label: string | null;
  snapshot_note: string | null;
  archived_at: number;
};

type ShareHistoryEntry = {
  id: string;
  snapshot_id: string;
  version_number: number;
  snapshot_label: string | null;
  permission: "view" | "comment";
  status: string;
  created_at: number;
  last_used_at: number | null;
  token: string | null;
  password: string | null;
};

type CreatedShare = {
  id: string;
  token: string;
  password: string;
  permission: "view" | "comment";
};

function generatePassword() {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  const bytes = new Uint8Array(6);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (byte) => alphabet[byte % alphabet.length]).join("");
}

function formatDate(timestamp: number) {
  return new Date(timestamp).toLocaleString("zh-CN");
}

export function ProjectShareDialog({
  projectId,
  projectName,
  onClose,
}: {
  projectId: string;
  projectName: string;
  onClose: () => void;
}) {
  const [snapshots, setSnapshots] = useState<SnapshotOption[]>([]);
  const [selectedSnapshotId, setSelectedSnapshotId] = useState("");
  const [permission, setPermission] = useState<"view" | "comment">("view");
  const [password, setPassword] = useState("");
  const [history, setHistory] = useState<ShareHistoryEntry[]>([]);
  const [revealedIds, setRevealedIds] = useState<Set<string>>(() => new Set());
  const [createdShare, setCreatedShare] = useState<CreatedShare | null>(null);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState<"link" | "password" | null>(null);

  const selectedSnapshot = useMemo(
    () => snapshots.find((snapshot) => snapshot.id === selectedSnapshotId),
    [selectedSnapshotId, snapshots],
  );

  async function loadHistory() {
    setHistoryLoading(true);
    try {
      const response = await fetch("/api/projects/" + projectId + "/shares");
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "无法读取分享历史");
        return;
      }
      setHistory(body.shares ?? []);
    } catch {
      setError("无法读取分享历史，请稍后重新加载");
    } finally {
      setHistoryLoading(false);
    }
  }

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setHistoryLoading(true);
    setError("");
    setCreatedShare(null);
    setPassword(generatePassword());
    Promise.all([
      fetch("/api/projects/" + projectId + "/snapshots"),
      fetch("/api/projects/" + projectId + "/shares"),
    ])
      .then(async ([snapshotResponse, historyResponse]) => {
        const snapshotBody = await snapshotResponse.json();
        const historyBody = await historyResponse.json();
        if (cancelled) return;
        if (!snapshotResponse.ok) {
          setError(snapshotBody.error?.message ?? "无法读取项目快照");
        } else {
          const nextSnapshots = (snapshotBody.snapshots ??
            []) as SnapshotOption[];
          setSnapshots(nextSnapshots);
          setSelectedSnapshotId(nextSnapshots[0]?.id ?? "");
        }
        if (!historyResponse.ok) {
          setError(historyBody.error?.message ?? "无法读取分享历史");
        } else {
          setHistory((historyBody.shares ?? []) as ShareHistoryEntry[]);
        }
      })
      .catch(() => {
        if (!cancelled) setError("项目分享数据加载失败，请检查服务连接状态");
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
          setHistoryLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [projectId]);

  async function copy(value: string, kind: "link" | "password") {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(kind);
      window.setTimeout(() => setCopied(null), 1600);
    } catch {
      setError("复制操作失败，请选择文本后手动复制");
    }
  }

  async function createShare() {
    setError("");
    if (!selectedSnapshotId) {
      setError("该项目尚无 PDF 快照，请先在项目首页归档 PDF");
      return;
    }
    if (password.trim().length < 6) {
      setError("访问密码至少需要 6 位");
      return;
    }
    setCreating(true);
    try {
      const response = await fetch("/api/projects/" + projectId + "/shares", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          snapshotId: selectedSnapshotId,
          password: password.trim(),
          permission,
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "分享链接创建失败");
        return;
      }
      setCreatedShare(body.share as CreatedShare);
      setPassword(body.share.password);
      await loadHistory();
    } catch {
      setError("分享链接创建失败，请检查服务连接");
    } finally {
      setCreating(false);
    }
  }

  async function revokeShare(shareId: string) {
    if (
      !window.confirm(
        "删除后此链接会立即失效，访问码也无法恢复。是否继续执行？",
      )
    )
      return;
    setError("");
    try {
      const response = await fetch(
        "/api/projects/" + projectId + "/shares/" + shareId,
        { method: "DELETE" },
      );
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "分享链接删除失败");
        return;
      }
      setHistory((current) =>
        current.map((share) =>
          share.id === shareId
            ? { ...share, status: "revoked", token: null, password: null }
            : share,
        ),
      );
      setRevealedIds((current) => {
        const next = new Set(current);
        next.delete(shareId);
        return next;
      });
      if (createdShare?.id === shareId) setCreatedShare(null);
    } catch {
      setError("分享链接删除失败，请稍后重新操作");
    }
  }

  function shareUrl(token: string | null) {
    if (!token || typeof window === "undefined") return "";
    return window.location.origin + "/share/" + token;
  }

  return (
    <div className="project-edit-backdrop" onMouseDown={onClose}>
      <section
        className="project-edit-dialog project-share-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="project-share-dialog-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header>
          <div>
            <p className="eyebrow">项目分享</p>
            <h2 id="project-share-dialog-title">
              创建“{projectName}”的分享链接
            </h2>
          </div>
          <button
            className="icon-button"
            type="button"
            title="关闭"
            aria-label="关闭项目分享"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </header>

        {error && <p className="error project-share-error">{error}</p>}
        <form
          className="project-share-form"
          onSubmit={(event) => {
            event.preventDefault();
            void createShare();
          }}
        >
          <label>
            选择 PDF 版本
            {loading ? (
              <span className="project-share-loading">
                <LoaderCircle size={15} /> 正在加载快照...
              </span>
            ) : snapshots.length ? (
              <select
                value={selectedSnapshotId}
                onChange={(event) => setSelectedSnapshotId(event.target.value)}
              >
                {snapshots.map((snapshot) => (
                  <option key={snapshot.id} value={snapshot.id}>
                    {snapshot.snapshot_label ||
                      "版本 " + snapshot.version_number}{" "}
                    ·{" "}
                    {new Date(snapshot.archived_at).toLocaleDateString("zh-CN")}
                  </option>
                ))}
              </select>
            ) : (
              <span className="project-share-empty">
                暂无可分享的 PDF 快照。请先在项目首页归档
                PDF，然后创建分享链接。
              </span>
            )}
          </label>
          {selectedSnapshot && selectedSnapshot.snapshot_note && (
            <p className="muted project-share-snapshot-note">
              版本备注：{selectedSnapshot.snapshot_note}
            </p>
          )}
          <label>
            访问密码
            <input
              value={password}
              minLength={6}
              maxLength={200}
              required
              onChange={(event) => setPassword(event.target.value)}
            />
            <small className="muted">
              访问密码单独验证，不会写入分享链接。可以生成新的随机密码。
            </small>
          </label>
          <button
            className="secondary-button project-share-generate"
            type="button"
            onClick={() => setPassword(generatePassword())}
          >
            <RefreshCw size={15} /> 生成新的访问密码
          </button>
          <label>
            访问权限
            <select
              value={permission}
              onChange={(event) =>
                setPermission(event.target.value as "view" | "comment")
              }
            >
              <option value="view">仅查看 PDF 和已有批注</option>
              <option value="comment">查看 PDF 并发表评论</option>
            </select>
          </label>
          <div className="dialog-actions">
            <button
              type="submit"
              disabled={creating || loading || !snapshots.length}
            >
              {creating ? (
                <LoaderCircle className="spin" size={16} />
              ) : (
                <Share2 size={16} />
              )}
              {creating ? "正在生成..." : "生成分享链接"}
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={onClose}
            >
              关闭
            </button>
          </div>
        </form>

        {createdShare && (
          <section
            className="share-result project-share-result"
            aria-label="新建分享结果"
          >
            <strong>分享链接创建成功</strong>
            <label>
              分享链接
              <input readOnly value={shareUrl(createdShare.token)} />
            </label>
            <button
              type="button"
              onClick={() => void copy(shareUrl(createdShare.token), "link")}
            >
              {copied === "link" ? <Check size={15} /> : <Copy size={15} />}
              {copied === "link" ? "复制成功" : "复制链接"}
            </button>
            <label>
              访问密码
              <input readOnly value={createdShare.password} />
            </label>
            <button
              type="button"
              onClick={() => void copy(createdShare.password, "password")}
            >
              {copied === "password" ? <Check size={15} /> : <Copy size={15} />}
              {copied === "password" ? "复制成功" : "复制访问密码"}
            </button>
          </section>
        )}

        <section
          className="share-history project-share-history"
          aria-label="历史分享链接"
        >
          <div className="share-history-heading">
            <strong>历史分享链接</strong>
            <button
              className="secondary-button"
              type="button"
              onClick={() => void loadHistory()}
            >
              <RefreshCw size={14} /> 刷新
            </button>
          </div>
          {historyLoading && <p className="muted">正在加载分享历史...</p>}
          {!historyLoading && !history.length && (
            <p className="muted">尚未创建分享链接。</p>
          )}
          {history.map((share) => {
            const url = shareUrl(share.token);
            const revealed = revealedIds.has(share.id);
            return (
              <article className="share-history-item" key={share.id}>
                <div className="share-history-item-heading">
                  <strong>
                    {share.snapshot_label || "版本 " + share.version_number}
                  </strong>
                  <small>
                    {share.permission === "comment" ? "可评论" : "仅查看"} ·{" "}
                    {share.status === "active" ? "有效" : "已撤销"}
                  </small>
                </div>
                <small className="muted">
                  创建时间：{formatDate(share.created_at)}
                </small>
                <label>
                  分享链接
                  <input readOnly value={url || "链接已撤销，无法访问"} />
                </label>
                <button
                  type="button"
                  disabled={!url}
                  onClick={() => void copy(url, "link")}
                >
                  <Copy size={14} /> 复制链接
                </button>
                <div className="share-secret-line">
                  <span>
                    访问码：{revealed ? share.password || "不可恢复" : "••••••"}
                  </span>
                  <button
                    type="button"
                    title={revealed ? "隐藏访问码" : "显示访问码"}
                    disabled={!share.password}
                    onClick={() =>
                      setRevealedIds((current) => {
                        const next = new Set(current);
                        if (next.has(share.id)) next.delete(share.id);
                        else next.add(share.id);
                        return next;
                      })
                    }
                  >
                    {revealed ? <EyeOff size={15} /> : <Eye size={15} />}
                  </button>
                </div>
                <button
                  className="secondary-button"
                  type="button"
                  disabled={share.status !== "active"}
                  onClick={() => void revokeShare(share.id)}
                >
                  撤销链接
                </button>
              </article>
            );
          })}
        </section>
      </section>
    </div>
  );
}
