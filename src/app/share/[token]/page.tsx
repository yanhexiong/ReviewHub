"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { PdfReviewer, type ReviewComment } from "@/components/pdf-reviewer";
import { PreferencesControls } from "@/features/preferences";
import { useShareTokenFromLocation } from "@/lib/static-route";

type SharedComment = ReviewComment & { author_display_name: string };

type SharedData = {
  project: { name: string; description: string | null };
  snapshot: {
    id: string;
    version_number: number;
    page_count: number;
    snapshot_label: string | null;
    snapshot_note: string | null;
  };
  permission: "view" | "comment";
  comments: SharedComment[];
};
type SharedRealtimeEvent = {
  type: "comment.created" | "comment.updated" | "comment.deleted";
  projectId: string;
  snapshotId: string;
  commentId: string;
  comment?: SharedComment;
};

export default function SharedReviewPage() {
  const token = useShareTokenFromLocation();
  const [data, setData] = useState<SharedData | null>(null);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [page, setPage] = useState(1);
  const [focusedCommentId, setFocusedCommentId] = useState<string | null>(null);
  const [immersiveMode, setImmersiveMode] = useState(false);
  useEffect(() => {
    if (!token) return;
    fetch(`/api/share/${token}`)
      .then(async (response) => {
        if (response.ok) setData(await response.json());
      })
      .catch(() => setError("无法读取分享链接"));
  }, [token]);
  useEffect(() => {
    if (!token || !data?.snapshot.id) return;
    const source = new EventSource(`/api/share/${token}/events`);
    const handleEvent = (event: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(event.data) as SharedRealtimeEvent;
        if (payload.snapshotId !== data.snapshot.id) return;
        if (
          (payload.type === "comment.created" ||
            payload.type === "comment.updated") &&
          payload.comment
        ) {
          setData((current) => {
            if (!current) return current;
            const index = current.comments.findIndex(
              (comment) => comment.id === payload.comment?.id,
            );
            if (index === -1)
              return {
                ...current,
                comments: [payload.comment!, ...current.comments],
              };
            return {
              ...current,
              comments: current.comments.map((comment, itemIndex) =>
                itemIndex === index ? payload.comment! : comment,
              ),
            };
          });
        }
        if (payload.type === "comment.deleted")
          setData((current) =>
            current
              ? {
                  ...current,
                  comments: current.comments.filter(
                    (comment) => comment.id !== payload.commentId,
                  ),
                }
              : current,
          );
      } catch {
        // Ignore malformed events and keep the current shared review visible.
      }
    };
    source.addEventListener("comment.created", handleEvent);
    source.addEventListener("comment.updated", handleEvent);
    source.addEventListener("comment.deleted", handleEvent);
    return () => source.close();
  }, [data?.snapshot.id, token]);
  async function unlock(form: FormData) {
    setError("");
    const response = await fetch(`/api/share/${token}/access`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password: form.get("password") }),
    });
    if (!response.ok) {
      setError((await response.json()).error?.message ?? "密码错误");
      return;
    }
    const review = await fetch(`/api/share/${token}`);
    if (!review.ok) {
      setError("分享访问验证失败");
      return;
    }
    setData(await review.json());
  }
  async function addComment(form: FormData) {
    setError("");
    const response = await fetch(`/api/share/${token}/comments`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        pageNumber: Number(form.get("pageNumber")),
        content: form.get("content"),
        category: form.get("category"),
        priority: form.get("priority"),
      }),
    });
    if (!response.ok) {
      setError((await response.json()).error?.message ?? "批注提交失败");
      return;
    }
    const review = await fetch(`/api/share/${token}`);
    setData(await review.json());
    (document.querySelector(".share-comment-form") as HTMLFormElement)?.reset();
  }
  if (!data)
    return (
      <main className="auth">
        <form action={unlock}>
          <p className="eyebrow">REVIEW HUB / 受保护分享</p>
          <h1>输入访问密码</h1>
          <p className="muted">此审阅链接受密码保护，密码由文档所有者提供。</p>
          <label>
            访问密码
            <input
              name="password"
              type="password"
              minLength={6}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
              autoFocus
            />
          </label>
          {error && <p className="error">{error}</p>}
          <button type="submit">验证并查看</button>
          <p className="muted">
            <Link href="/login">返回登录</Link>
          </p>
        </form>
      </main>
    );
  return (
    <main className="shared-review">
      <header className="share-header">
        <div>
          <p className="eyebrow">REVIEW HUB / 受保护分享</p>
          <h1>{data.project.name}</h1>
          <p className="muted">
            {data.snapshot.snapshot_label ||
              `版本 ${data.snapshot.version_number}`}{" "}
            ·{data.permission === "comment" ? " 可发表评论" : " 仅查看"}
          </p>
        </div>
        <div className="share-header-actions">
          <PreferencesControls placement="toolbar" />
          <Link className="toolbar-link" href="/login">
            登录工作区
          </Link>
        </div>
      </header>
      <div className={`shared-grid ${immersiveMode ? "immersive" : ""}`}>
        <PdfReviewer
          comments={data.comments}
          fileUrl={`/api/share/${token}/pdf`}
          page={page}
          focusedCommentId={focusedCommentId}
          immersiveMode={immersiveMode}
          onPageChange={setPage}
          onOutlineChange={() => {}}
          onTextSelection={(selection) => setPage(selection.pageNumber)}
          onAreaSelection={(selection) => setPage(selection.pageNumber)}
          onToggleImmersive={() => setImmersiveMode((value) => !value)}
        />
        <aside className="shared-comments">
          <h2>审阅批注</h2>
          <p className="muted">
            共 {data.comments.length} 条 · 访问权限：
            {data.permission === "comment" ? "可发表评论" : "仅查看"}
          </p>
          {data.comments.map((comment) => (
            <article className="comment" key={comment.id}>
              <button
                className="shared-comment-location"
                type="button"
                onClick={() => {
                  setPage(comment.page_number);
                  setFocusedCommentId(comment.id);
                }}
              >
                第 {comment.page_number} 页 · {comment.category}
              </button>
              <small>审阅人：{comment.author_display_name}</small>
              <p>{comment.content}</p>
            </article>
          ))}
          {data.permission === "comment" && (
            <form className="share-comment-form" action={addComment}>
              <h3>添加页级意见</h3>
              <label>
                页码
                <input
                  name="pageNumber"
                  type="number"
                  min="1"
                  max={data.snapshot.page_count}
                  value={page}
                  onChange={(event) => setPage(Number(event.target.value))}
                  required
                />
              </label>
              <label>
                类别
                <input name="category" defaultValue="其他" required />
              </label>
              <label>
                优先级
                <select name="priority" defaultValue="medium">
                  <option value="low">低</option>
                  <option value="medium">中</option>
                  <option value="high">高</option>
                </select>
              </label>
              <label>
                意见
                <textarea name="content" rows={4} required />
              </label>
              <button type="submit">提交批注</button>
            </form>
          )}
          {error && <p className="error">{error}</p>}
        </aside>
      </div>
    </main>
  );
}
