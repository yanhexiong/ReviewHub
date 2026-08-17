"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import {
  ArrowLeft,
  Check,
  ChevronLeft,
  ChevronRight,
  ChevronDown,
  Eye,
  EyeOff,
  Download,
  FilePlus2,
  FileJson,
  FileSpreadsheet,
  FileText,
  GitCompareArrows,
  History,
  MapPin,
  MessageCircle,
  MessageSquarePlus,
  Package,
  Pencil,
  RotateCcw,
  Save,
  Share2,
  Settings2,
  Trash2,
  UserRound,
  X,
} from "lucide-react";
import {
  PdfReviewer,
  type PdfOutlineEntry,
  type RectangleSelectionAnchor,
  type ReviewComment,
  type TextSelectionAnchor,
} from "@/components/pdf-reviewer";
import { SnapshotArchiveDialog } from "@/components/snapshot-archive-dialog";
import { PreferencesControls } from "@/features/preferences";

const defaultCategories = [
  ["content", "内容"],
  ["wording", "表述"],
  ["formula", "公式"],
  ["figure", "图"],
  ["table", "表格"],
  ["layout", "排版"],
  ["citation", "引用"],
  ["question", "问题"],
  ["other", "其他"],
] as const;

const categoryLabel = (category: string) =>
  defaultCategories.find(([value]) => value === category)?.[1] ?? category;
type Project = {
  id: string;
  created_by_user_id: string;
  name: string;
  slug: string;
  description: string | null;
  repository_configured: boolean;
  pdf_configured: boolean;
  source_file_name: string | null;
  access?: {
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
  git_branch: string | null;
  git_commit_short_sha: string | null;
  git_worktree_dirty: number;
  source_kind: "project" | "external";
  has_git_source: boolean;
  has_source_patch: boolean;
  sha256: string;
  archived_at: number;
  snapshot_label: string | null;
  snapshot_note: string | null;
};
type Comment = ReviewComment & {
  id: string;
  author_id: string;
  snapshot_id: string;
  page_number: number;
  status: string;
  priority: string;
  content: string;
  category: string;
  reply_count?: number;
  resolution_note: string | null;
  author_display_name: string;
};
type CommentSelection = TextSelectionAnchor | RectangleSelectionAnchor;
type Reply = {
  id: string;
  content: string;
  display_name: string;
  created_at: number;
};
type CurrentUser = {
  id: string;
  display_name: string;
  role: "admin" | "author" | "reviewer" | "viewer";
};
type RealtimeEvent = {
  type:
    "comment.created" | "comment.updated" | "comment.deleted" | "reply.created";
  projectId: string;
  snapshotId: string;
  commentId: string;
  comment?: Comment;
  reply?: Reply;
  replyCount?: number;
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
export function ReviewWorkspace({
  projectId,
  initialSnapshotId,
}: {
  projectId: string;
  initialSnapshotId?: string;
}) {
  const [project, setProject] = useState<Project | null>(null),
    [snapshots, setSnapshots] = useState<Snapshot[]>([]),
    [active, setActive] = useState<Snapshot | null>(null),
    [comments, setComments] = useState<Comment[]>([]),
    [page, setPage] = useState(1),
    [focusedCommentId, setFocusedCommentId] = useState<string | null>(null),
    [status, setStatus] = useState(""),
    [filter, setFilter] = useState("all"),
    [categoryFilter, setCategoryFilter] = useState("all"),
    [commentScope, setCommentScope] = useState<"page" | "snapshot">("page"),
    [navigationView, setNavigationView] = useState<"outline" | "pages">(
      "outline",
    ),
    [outline, setOutline] = useState<PdfOutlineEntry[]>([]),
    [replies, setReplies] = useState<Record<string, Reply[]>>({}),
    [replyingTo, setReplyingTo] = useState<string | null>(null),
    [commentActionError, setCommentActionError] = useState(""),
    [editing, setEditing] = useState(false),
    [editError, setEditError] = useState(""),
    [snapshotEditor, setSnapshotEditor] = useState<"edit" | null>(null),
    [snapshotArchiveOpen, setSnapshotArchiveOpen] = useState(false),
    [snapshotMetadataError, setSnapshotMetadataError] = useState(""),
    [commenting, setCommenting] = useState(false),
    [commentError, setCommentError] = useState(""),
    [selection, setSelection] = useState<CommentSelection | null>(null),
    [categoryChoice, setCategoryChoice] = useState("content"),
    [currentUser, setCurrentUser] = useState<CurrentUser | null>(null),
    [immersiveMode, setImmersiveMode] = useState(false),
    [snapshotHistoryOpen, setSnapshotHistoryOpen] = useState(false),
    [exportMenuOpen, setExportMenuOpen] = useState(false),
    [profileEditor, setProfileEditor] = useState(false),
    [profileError, setProfileError] = useState(""),
    [shareEditor, setShareEditor] = useState(false),
    [sharePermission, setSharePermission] = useState<"view" | "comment">(
      "view",
    ),
    [sharePassword, setSharePassword] = useState(""),
    [shareHistory, setShareHistory] = useState<ShareHistoryEntry[]>([]),
    [shareHistoryLoading, setShareHistoryLoading] = useState(false),
    [revealedShareIds, setRevealedShareIds] = useState<Set<string>>(
      () => new Set(),
    ),
    [shareRecordId, setShareRecordId] = useState<string | null>(null),
    [shareLink, setShareLink] = useState(""),
    [shareError, setShareError] = useState(""),
    [realtimeState, setRealtimeState] = useState<
      "connecting" | "connected" | "offline"
    >("connecting");
  const canReview = project?.access?.can_review ?? false;
  const canManage = project?.access?.can_manage ?? false;
  const refresh = async () => {
    const [projectResponse, userResponse, snapshotsResponse, commentsResponse] =
      await Promise.all([
        fetch(`/api/projects/${projectId}`),
        fetch("/api/auth/me"),
        fetch(`/api/projects/${projectId}/snapshots`),
        fetch(`/api/projects/${projectId}/comments?limit=1000`),
      ]);
    const projectBody = await projectResponse.json();
    if (!projectResponse.ok)
      throw new Error(projectBody.error?.message ?? "项目加载失败");
    setProject(projectBody.project);
    if (userResponse.ok) setCurrentUser((await userResponse.json()).user);
    const all = (await snapshotsResponse.json()).snapshots as Snapshot[];
    setSnapshots(all);
    setActive(
      (old) =>
        all.find((x) => x.id === initialSnapshotId) ??
        all.find((x) => x.id === old?.id) ??
        all[0] ??
        null,
    );
    const c = await commentsResponse.json();
    setComments(c.comments ?? []);
  };
  useEffect(() => {
    refresh().catch(() => setStatus("无法加载审阅数据。"));
  }, [initialSnapshotId]);
  useEffect(() => {
    const source = new EventSource(`/api/projects/${projectId}/events`);
    const handleEvent = (event: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(event.data) as RealtimeEvent;
        if (payload.projectId !== projectId) return;
        if (
          (payload.type === "comment.created" ||
            payload.type === "comment.updated") &&
          payload.comment
        ) {
          setComments((current) => {
            const index = current.findIndex(
              (comment) => comment.id === payload.comment?.id,
            );
            if (index === -1) return [payload.comment!, ...current];
            return current.map((comment, itemIndex) =>
              itemIndex === index ? payload.comment! : comment,
            );
          });
        }
        if (payload.type === "comment.deleted") {
          setComments((current) =>
            current.filter((comment) => comment.id !== payload.commentId),
          );
          setReplies((current) => {
            const next = { ...current };
            delete next[payload.commentId];
            return next;
          });
        }
        if (payload.type === "reply.created") {
          if (payload.reply) {
            setReplies((current) => {
              const existing = current[payload.commentId];
              if (!existing) return current;
              if (existing.some((reply) => reply.id === payload.reply?.id))
                return current;
              return {
                ...current,
                [payload.commentId]: [...existing, payload.reply!],
              };
            });
          }
          if (payload.replyCount !== undefined)
            setComments((current) =>
              current.map((comment) =>
                comment.id === payload.commentId
                  ? { ...comment, reply_count: payload.replyCount }
                  : comment,
              ),
            );
        }
      } catch {
        // Ignore malformed events and keep the existing review state.
      }
    };
    source.addEventListener("connected", () => setRealtimeState("connected"));
    source.addEventListener("comment.created", handleEvent);
    source.addEventListener("comment.updated", handleEvent);
    source.addEventListener("comment.deleted", handleEvent);
    source.addEventListener("reply.created", handleEvent);
    source.onerror = () => setRealtimeState("offline");
    return () => source.close();
  }, [projectId]);
  useEffect(() => {
    setPage(1);
    setFocusedCommentId(null);
    setOutline([]);
    setReplies({});
    setReplyingTo(null);
  }, [active?.id]);
  const snapshotComments = useMemo(
    () => comments.filter((comment) => comment.snapshot_id === active?.id),
    [active?.id, comments],
  );
  const shown = useMemo(
    () =>
      snapshotComments.filter(
        (c) =>
          (filter === "all" || c.status === filter) &&
          (categoryFilter === "all" || c.category === categoryFilter) &&
          (commentScope === "snapshot" || c.page_number === page),
      ),
    [categoryFilter, commentScope, filter, page, snapshotComments],
  );
  const categories = useMemo(
    () =>
      Array.from(
        new Set([
          ...defaultCategories.map(([value]) => value),
          ...comments.map((comment) => comment.category),
        ]),
      ),
    [comments],
  );
  const outlineCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const [index, entry] of outline.entries()) {
      if (!entry.pageNumber) continue;
      const next = outline
        .slice(index + 1)
        .find(
          (candidate) =>
            candidate.depth <= entry.depth && candidate.pageNumber !== null,
        );
      const lastPage = next
        ? Math.max(entry.pageNumber, next.pageNumber! - 1)
        : (active?.page_count ?? entry.pageNumber);
      counts[entry.id] = snapshotComments.filter(
        (comment) =>
          comment.page_number >= entry.pageNumber! &&
          comment.page_number <= lastPage,
      ).length;
    }
    return counts;
  }, [active?.page_count, outline, snapshotComments]);
  const activeSnapshotIndex = snapshots.findIndex(
    (snapshot) => snapshot.id === active?.id,
  );
  const newerSnapshot =
    activeSnapshotIndex > 0 ? snapshots[activeSnapshotIndex - 1] : null;
  const olderSnapshot =
    activeSnapshotIndex >= 0 && activeSnapshotIndex < snapshots.length - 1
      ? snapshots[activeSnapshotIndex + 1]
      : null;
  function selectSnapshot(snapshot: Snapshot) {
    setActive(snapshot);
    setSnapshotHistoryOpen(false);
    setExportMenuOpen(false);
    setStatus(`已切换到 Snapshot #${snapshot.version_number}。`);
  }

  async function switchToSnapshotSource(snapshot: Snapshot) {
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
      setStatus(error instanceof Error ? error.message : "切换源码版本失败");
    }
  }
  function focusComment(comment: Comment) {
    setPage(comment.page_number);
    setFocusedCommentId(comment.id);
    setStatus(`已定位到第 ${comment.page_number} 页的审阅批注。`);
  }
  async function saveSnapshotMetadata(form: FormData) {
    if (!active) return;
    setSnapshotMetadataError("");
    const response = await fetch(
      `/api/projects/${projectId}/snapshots/${active.id}`,
      {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          label: String(form.get("label") ?? "").trim(),
          note: String(form.get("note") ?? "").trim(),
        }),
      },
    );
    const body = await response.json();
    if (!response.ok) {
      setSnapshotMetadataError(body.error?.message ?? "版本信息保存失败");
      return;
    }
    setSnapshots((current) =>
      current.map((snapshot) =>
        snapshot.id === active.id ? body.snapshot : snapshot,
      ),
    );
    setActive(body.snapshot);
    setSnapshotEditor(null);
    setStatus("版本别名和审阅备注已保存。");
  }
  async function saveProject(form: FormData) {
    setEditError("");
    const repositoryPath = String(form.get("repositoryPath") ?? "").trim();
    const sourcePdfPath = String(form.get("sourcePdfPath") ?? "").trim();
    try {
      const response = await fetch(`/api/projects/${projectId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: form.get("name"),
          slug: form.get("slug"),
          description: form.get("description"),
          ...(repositoryPath ? { repositoryPath } : {}),
          ...(sourcePdfPath ? { sourcePdfPath } : {}),
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setEditError(body.error?.message ?? "项目配置保存失败");
        return;
      }
      setProject(body.project);
      setEditing(false);
      setStatus("项目配置已保存。");
    } catch {
      setEditError("项目配置保存失败，请检查服务连接。");
    }
  }
  async function createComment(form: FormData) {
    if (!active) return;
    setCommentError("");
    const content = String(form.get("content") ?? "").trim();
    if (!content) {
      setCommentError("请输入审阅内容");
      return;
    }
    const anchor = selection
      ? {
          pageNumber: selection.pageNumber,
          normalizedX: selection.normalizedX,
          normalizedY: selection.normalizedY,
          normalizedWidth: selection.normalizedWidth,
          normalizedHeight: selection.normalizedHeight,
          ...(selection.kind === "text"
            ? {
                selectedText: selection.selectedText,
                selectedTextContext: selection.selectedTextContext,
              }
            : {}),
        }
      : {};
    const response = await fetch(`/api/projects/${projectId}/comments`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        snapshotId: active.id,
        pageNumber: selection?.pageNumber ?? page,
        anchorType:
          selection?.kind === "text"
            ? "text_selection"
            : selection
              ? "rectangle"
              : "page_note",
        ...anchor,
        content,
        category:
          form.get("category") === "custom"
            ? form.get("customCategory")
            : form.get("category"),
        priority: form.get("priority"),
      }),
    });
    const body = await response.json();
    if (!response.ok) {
      setCommentError(body.error?.message ?? "审阅保存失败");
      return;
    }
    setComments((current) => [body.comment, ...current]);
    setCommenting(false);
    setSelection(null);
    setStatus(`已在第 ${selection?.pageNumber ?? page} 页添加审阅。`);
  }
  const openTextSelectionComment = useCallback(
    (anchor: TextSelectionAnchor) => {
      setSelection(anchor);
      setCategoryChoice("wording");
      setCommentError("");
      setCommenting(true);
    },
    [],
  );
  const openAreaSelectionComment = useCallback(
    (anchor: RectangleSelectionAnchor) => {
      setSelection(anchor);
      setCategoryChoice("figure");
      setCommentError("");
      setCommenting(true);
    },
    [],
  );
  const handleOutlineChange = useCallback((next: PdfOutlineEntry[]) => {
    setOutline(next);
  }, []);
  async function toggleReplies(commentId: string) {
    setCommentActionError("");
    if (replyingTo === commentId) {
      setReplyingTo(null);
      return;
    }
    if (!replies[commentId]) {
      const response = await fetch(`/api/comments/${commentId}/replies`);
      const body = await response.json();
      if (!response.ok) {
        setCommentActionError(body.error?.message ?? "无法读取回复");
        return;
      }
      setReplies((current) => ({ ...current, [commentId]: body.replies }));
    }
    setReplyingTo(commentId);
  }
  async function createReply(commentId: string, form: FormData) {
    const content = String(form.get("reply") ?? "").trim();
    if (!content) return;
    setCommentActionError("");
    const response = await fetch(`/api/comments/${commentId}/replies`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content }),
    });
    const body = await response.json();
    if (!response.ok) {
      setCommentActionError(body.error?.message ?? "回复保存失败");
      return;
    }
    setReplies((current) => ({
      ...current,
      [commentId]: [
        ...(current[commentId] ?? []),
        { id: body.id, content, display_name: "我", created_at: Date.now() },
      ],
    }));
    setComments((current) =>
      current.map((comment) =>
        comment.id === commentId
          ? { ...comment, reply_count: (comment.reply_count ?? 0) + 1 }
          : comment,
      ),
    );
  }
  async function transitionComment(
    commentId: string,
    nextStatus: "resolved" | "reopened" | "obsolete",
  ) {
    setCommentActionError("");
    const response = await fetch(`/api/comments/${commentId}/transition`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: nextStatus }),
    });
    const body = await response.json();
    if (!response.ok) {
      setCommentActionError(body.error?.message ?? "评论状态更新失败");
      return;
    }
    setComments((current) =>
      current.map((comment) =>
        comment.id === commentId ? { ...comment, status: nextStatus } : comment,
      ),
    );
  }
  async function deleteComment(comment: Comment) {
    if (!window.confirm("删除后该批注将从审阅和导出中隐藏，是否继续？")) return;
    setCommentActionError("");
    const response = await fetch(`/api/comments/${comment.id}`, {
      method: "DELETE",
    });
    const body = await response.json();
    if (!response.ok) {
      setCommentActionError(body.error?.message ?? "无法删除批注");
      return;
    }
    setComments((current) => current.filter((item) => item.id !== comment.id));
    setReplies((current) => {
      const remaining = { ...current };
      delete remaining[comment.id];
      return remaining;
    });
    setStatus("批注已删除。");
  }
  async function saveProfile(form: FormData) {
    setProfileError("");
    const response = await fetch("/api/auth/me", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ displayName: form.get("displayName") }),
    });
    const body = await response.json();
    if (!response.ok) {
      setProfileError(body.error?.message ?? "署名保存失败");
      return;
    }
    setCurrentUser(body.user);
    setComments((current) =>
      current.map((comment) =>
        comment.author_id === body.user.id
          ? { ...comment, author_display_name: body.user.display_name }
          : comment,
      ),
    );
    setProfileEditor(false);
    setStatus("审阅署名已更新。");
  }
  async function createShareLink(form: FormData) {
    if (!active) return;
    setShareError("");
    const response = await fetch(`/api/projects/${projectId}/shares`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        snapshotId: active.id,
        password: form.get("password"),
        permission: form.get("permission"),
      }),
    });
    const body = await response.json();
    if (!response.ok) {
      setShareError(body.error?.message ?? "分享链接创建失败");
      return;
    }
    setShareRecordId(body.share.id);
    setSharePassword(body.share.password);
    setShareLink(`${window.location.origin}/share/${body.share.token}`);
    await loadShareHistory();
  }
  async function loadShareHistory() {
    setShareHistoryLoading(true);
    try {
      const response = await fetch(`/api/projects/${projectId}/shares`);
      const body = await response.json();
      if (!response.ok) {
        setShareError(body.error?.message ?? "无法读取分享历史");
        return;
      }
      setShareHistory(body.shares ?? []);
    } catch {
      setShareError("无法读取分享历史，请重新加载");
    } finally {
      setShareHistoryLoading(false);
    }
  }
  async function deleteShareHistory(shareId: string) {
    if (
      !window.confirm(
        "删除后此链接会立即失效，访问码也无法恢复。是否继续执行？",
      )
    )
      return;
    setShareError("");
    const response = await fetch(
      `/api/projects/${projectId}/shares/${shareId}`,
      { method: "DELETE" },
    );
    const body = await response.json();
    if (!response.ok) {
      setShareError(body.error?.message ?? "分享链接删除失败");
      return;
    }
    setShareHistory((current) =>
      current.map((share) =>
        share.id === shareId
          ? { ...share, status: "revoked", token: null, password: null }
          : share,
      ),
    );
    setRevealedShareIds((current) => {
      const next = new Set(current);
      next.delete(shareId);
      return next;
    });
    if (shareRecordId === shareId) {
      setShareRecordId(null);
      setShareLink("");
      setSharePassword("");
    }
    setStatus("分享链接已撤销，访问码已清除。");
  }
  function generateSharePassword() {
    const alphabet =
      "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
    const bytes = new Uint8Array(6);
    crypto.getRandomValues(bytes);
    setSharePassword(
      Array.from(bytes, (byte) => alphabet[byte % alphabet.length]).join(""),
    );
  }
  return (
    <main className="workspace">
      <header className="toolbar">
        <div className="workspace-title">
          <nav className="workspace-back-nav" aria-label="返回导航">
            <Link href={`/projects/${projectId}`} title="返回项目首页">
              <ArrowLeft size={15} aria-hidden="true" />
              项目首页
            </Link>
          </nav>
          <div>
            <p className="eyebrow">REVIEW HUB / 本地审阅</p>
            <h1>{project?.name ?? "审阅工作区"}</h1>
          </div>
        </div>
        <div className="snapshot-meta">
          <Link
            className="snapshot-compare-toolbar-link"
            href={`/projects/${projectId}/compare`}
            title="比对任意两个审阅版本的 PDF 内容和批注"
          >
            <GitCompareArrows size={15} />
            版本比对
          </Link>
          <div className="snapshot-history-control">
            <button
              aria-expanded={snapshotHistoryOpen}
              className="snapshot-history-button"
              type="button"
              onClick={() => setSnapshotHistoryOpen((open) => !open)}
            >
              <History size={16} />
              快照历史
            </button>
            {snapshotHistoryOpen && (
              <div className="snapshot-history-menu" role="menu">
                <strong>选择要审阅的版本</strong>
                {snapshots.length ? (
                  snapshots.map((snapshot) => (
                    <div className="snapshot-history-item" key={snapshot.id}>
                      <button
                        aria-checked={snapshot.id === active?.id}
                        className={snapshot.id === active?.id ? "selected" : ""}
                        role="menuitemradio"
                        type="button"
                        onClick={() => selectSnapshot(snapshot)}
                      >
                        <span>
                          {snapshot.snapshot_label ||
                            `版本 ${snapshot.version_number}`}
                        </span>
                        <small>
                          Snapshot #{snapshot.version_number} ·{" "}
                          {snapshot.snapshot_note || "无备注"}
                          {snapshot.git_worktree_dirty === 1
                            ? snapshot.has_source_patch
                              ? " · 含未提交修改（已归档）"
                              : " · 含未提交修改（未归档源码）"
                            : ""}
                        </small>
                      </button>
                      {canManage && snapshot.has_git_source && (
                        <button
                          className="snapshot-switch-source"
                          type="button"
                          onClick={() => void switchToSnapshotSource(snapshot)}
                        >
                          切换到该版本源码
                        </button>
                      )}
                    </div>
                  ))
                ) : (
                  <span className="muted">尚未创建快照</span>
                )}
              </div>
            )}
          </div>
          {active ? (
            <>
              <div className="snapshot-navigation" aria-label="快照版本导航">
                <button
                  disabled={!olderSnapshot}
                  title={
                    olderSnapshot
                      ? `回退至 Snapshot #${olderSnapshot.version_number}`
                      : "已是最早的 Snapshot"
                  }
                  type="button"
                  onClick={() => olderSnapshot && selectSnapshot(olderSnapshot)}
                >
                  <ChevronLeft size={16} />
                  上一版本
                </button>
                <select
                  aria-label="选择 Snapshot"
                  value={active.id}
                  onChange={(event) => {
                    const snapshot = snapshots.find(
                      (item) => item.id === event.target.value,
                    );
                    if (snapshot) selectSnapshot(snapshot);
                  }}
                >
                  {snapshots.map((snapshot) => (
                    <option key={snapshot.id} value={snapshot.id}>
                      {snapshot.snapshot_label ||
                        `版本 ${snapshot.version_number}`}{" "}
                      · Snapshot #{snapshot.version_number}
                    </option>
                  ))}
                </select>
                <button
                  disabled={!newerSnapshot}
                  title={
                    newerSnapshot
                      ? `前进至 Snapshot #${newerSnapshot.version_number}`
                      : "已是最新的 Snapshot"
                  }
                  type="button"
                  onClick={() => newerSnapshot && selectSnapshot(newerSnapshot)}
                >
                  下一版本
                  <ChevronRight size={16} />
                </button>
                {canManage && (
                  <button
                    title="编辑版本别名和审阅备注"
                    type="button"
                    onClick={() => {
                      setSnapshotMetadataError("");
                      setSnapshotEditor("edit");
                    }}
                  >
                    <Pencil size={15} />
                    版本信息
                  </button>
                )}
              </div>
              <b>{active.snapshot_label || `版本 ${active.version_number}`}</b>
              <span>Snapshot #{active.version_number}</span>
              <span>{active.git_branch ?? "Git 不可用"}</span>
              <code>{active.git_commit_short_sha ?? "-"}</code>
              {active.source_kind === "external" && (
                <span className="snapshot-origin-badge external">外部 PDF</span>
              )}
              {active.snapshot_note && (
                <span className="snapshot-note" title={active.snapshot_note}>
                  {active.snapshot_note}
                </span>
              )}
            </>
          ) : (
            <span>请选择或创建快照</span>
          )}
        </div>
        <div className="actions">
          <PreferencesControls placement="toolbar" />
          <button
            title="修改我的审阅署名"
            type="button"
            onClick={() => {
              setProfileError("");
              setProfileEditor(true);
            }}
          >
            <UserRound size={16} />
            {currentUser?.display_name ?? "我的署名"}
          </button>
          {canManage && (
            <>
              <button
                title="分享当前快照"
                type="button"
                disabled={!active}
                onClick={() => {
                  setShareError("");
                  setShareLink("");
                  setSharePermission("view");
                  generateSharePassword();
                  setShareRecordId(null);
                  void loadShareHistory();
                  setShareEditor(true);
                }}
              >
                <Share2 size={16} />
                分享审阅
              </button>
              <button
                title="编辑项目配置"
                onClick={() => {
                  setEditError("");
                  setEditing(true);
                }}
              >
                <Settings2 size={16} />
                编辑路径
              </button>
              <button
                title="创建审阅快照"
                onClick={() => {
                  setSnapshotArchiveOpen(true);
                }}
              >
                <FilePlus2 size={16} />
                直接归档 PDF
              </button>
            </>
          )}
          {canReview && (
            <button
              title="在当前页添加审阅"
              disabled={!active}
              onClick={() => {
                setCommentError("");
                setSelection(null);
                setCategoryChoice("content");
                setCommenting(true);
              }}
            >
              <MessageSquarePlus size={16} />
              添加页级审阅
            </button>
          )}
          <div className="export-control">
            <button
              aria-expanded={exportMenuOpen}
              aria-haspopup="menu"
              className="export-button"
              disabled={!active}
              title={active ? "选择导出模式" : "请先选择一个快照"}
              type="button"
              onClick={() => setExportMenuOpen((open) => !open)}
            >
              <Download size={16} />
              导出
              <ChevronDown size={14} aria-hidden="true" />
            </button>
            {exportMenuOpen && active && (
              <div aria-label="导出模式" className="export-menu" role="menu">
                <div className="export-menu-group">
                  <strong>审阅记录</strong>
                  <a
                    href={`/api/projects/${projectId}/export?format=markdown&snapshotId=${active.id}`}
                    role="menuitem"
                    onClick={() => setExportMenuOpen(false)}
                  >
                    <FileText size={15} />
                    Markdown 审阅记录
                  </a>
                  <a
                    href={`/api/projects/${projectId}/export?format=json&snapshotId=${active.id}`}
                    role="menuitem"
                    onClick={() => setExportMenuOpen(false)}
                  >
                    <FileJson size={15} />
                    JSON 审阅数据
                  </a>
                  <a
                    href={`/api/projects/${projectId}/export?format=csv&snapshotId=${active.id}`}
                    role="menuitem"
                    onClick={() => setExportMenuOpen(false)}
                  >
                    <FileSpreadsheet size={15} />
                    CSV 审阅记录
                  </a>
                </div>
                <div className="export-menu-group">
                  <strong>PDF 与打包</strong>
                  <a
                    href={`/api/projects/${projectId}/export?format=source-pdf&snapshotId=${active.id}`}
                    role="menuitem"
                    onClick={() => setExportMenuOpen(false)}
                  >
                    <FileText size={15} />
                    原始 PDF
                  </a>
                  <a
                    href={`/api/projects/${projectId}/export?format=annotated-pdf&snapshotId=${active.id}`}
                    role="menuitem"
                    onClick={() => setExportMenuOpen(false)}
                  >
                    <FileText size={15} />
                    带批注 PDF
                  </a>
                  <a
                    href={`/api/projects/${projectId}/export?format=bundle&snapshotId=${active.id}`}
                    role="menuitem"
                    onClick={() => setExportMenuOpen(false)}
                  >
                    <Package size={15} />
                    记录 + 原始 PDF
                  </a>
                </div>
              </div>
            )}
          </div>
        </div>
      </header>
      {status && <p className="notice">{status}</p>}
      <p className={`realtime-status ${realtimeState}`} role="status">
        <span aria-hidden="true" />
        {realtimeState === "connected"
          ? "实时更新已连接"
          : realtimeState === "offline"
            ? "实时更新重连中"
            : "正在连接实时更新"}
      </p>
      {editing && project && (
        <div className="project-edit-backdrop">
          <section
            className="project-edit-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="project-edit-title"
          >
            <header>
              <div>
                <p className="eyebrow">项目配置</p>
                <h2 id="project-edit-title">修改项目路径</h2>
              </div>
              <button
                className="icon-button"
                type="button"
                title="关闭"
                onClick={() => setEditing(false)}
              >
                <X size={18} />
              </button>
            </header>
            <p className="muted">
              路径必须位于管理员配置的安全目录内。历史快照不会改变，新的路径从下一次归档开始生效。
            </p>
            <form action={saveProject}>
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
              <label>
                说明
                <textarea
                  name="description"
                  defaultValue={project.description ?? ""}
                  rows={3}
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
                <small>页面不会回显已保存的位置；留空表示保持当前配置。</small>
              </label>
              {editError && <p className="error">{editError}</p>}
              <div className="dialog-actions">
                <button type="submit">
                  <Save size={16} />
                  保存项目配置
                </button>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => setEditing(false)}
                >
                  取消
                </button>
              </div>
            </form>
          </section>
        </div>
      )}
      {snapshotEditor && (
        <div className="project-edit-backdrop">
          <section
            className="project-edit-dialog snapshot-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="snapshot-dialog-title"
          >
            <header>
              <div>
                <p className="eyebrow">面向审阅者的版本说明</p>
                <h2 id="snapshot-dialog-title">编辑版本信息</h2>
              </div>
              <button
                className="icon-button"
                type="button"
                title="关闭"
                onClick={() => setSnapshotEditor(null)}
              >
                <X size={18} />
              </button>
            </header>
            <p className="muted">
              修改名称或备注不会改变已归档的 PDF、批注或 Git 元数据。
            </p>
            <form
              key={`${snapshotEditor}-${active?.id ?? "new"}`}
              action={saveSnapshotMetadata}
            >
              <label>
                版本别名
                <input
                  name="label"
                  defaultValue={active?.snapshot_label ?? ""}
                  maxLength={100}
                  placeholder="例如：7 月 31 日作者修订版"
                />
              </label>
              <label>
                审阅备注
                <textarea
                  name="note"
                  defaultValue={active?.snapshot_note ?? ""}
                  maxLength={500}
                  placeholder="例如：已根据第一轮意见重写实验设置，供第二轮审阅"
                  rows={4}
                />
              </label>
              {snapshotMetadataError && (
                <p className="error">{snapshotMetadataError}</p>
              )}
              <div className="dialog-actions">
                <button type="submit">
                  <Save size={16} />
                  保存版本信息
                </button>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => setSnapshotEditor(null)}
                >
                  取消
                </button>
              </div>
            </form>
          </section>
        </div>
      )}
      <SnapshotArchiveDialog
        open={snapshotArchiveOpen}
        projectId={projectId}
        onClose={() => setSnapshotArchiveOpen(false)}
        onCreated={(snapshot) => {
          setStatus(`已创建 Snapshot #${snapshot.version_number}。`);
          void refresh();
        }}
      />
      {profileEditor && currentUser && (
        <div className="project-edit-backdrop">
          <section
            className="project-edit-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="profile-dialog-title"
          >
            <header>
              <div>
                <p className="eyebrow">个人设置</p>
                <h2 id="profile-dialog-title">我的审阅署名</h2>
              </div>
              <button
                className="icon-button"
                title="关闭"
                type="button"
                onClick={() => setProfileEditor(false)}
              >
                <X size={18} />
              </button>
            </header>
            <form action={saveProfile}>
              <label>
                显示名称
                <input
                  name="displayName"
                  defaultValue={currentUser.display_name}
                  maxLength={80}
                  required
                />
              </label>
              <p className="muted">此名称会显示在你已有和新建的审阅批注中。</p>
              {profileError && <p className="error">{profileError}</p>}
              <div className="dialog-actions">
                <button type="submit">
                  <Save size={16} />
                  保存署名
                </button>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => setProfileEditor(false)}
                >
                  取消
                </button>
              </div>
            </form>
          </section>
        </div>
      )}
      {shareEditor && active && (
        <div className="project-edit-backdrop">
          <section
            className="project-edit-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="share-dialog-title"
          >
            <header>
              <div>
                <p className="eyebrow">当前快照分享</p>
                <h2 id="share-dialog-title">创建受保护审阅链接</h2>
              </div>
              <button
                className="icon-button"
                title="关闭"
                type="button"
                onClick={() => setShareEditor(false)}
              >
                <X size={18} />
              </button>
            </header>
            <section className="share-history" aria-label="分享历史">
              <div className="share-history-heading">
                <strong>历史分享链接</strong>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => void loadShareHistory()}
                >
                  刷新
                </button>
              </div>
              {shareHistoryLoading && (
                <p className="muted">正在加载分享历史...</p>
              )}
              {!shareHistoryLoading && !shareHistory.length && (
                <p className="muted">尚未创建分享链接。</p>
              )}
              {shareHistory.map((share) => {
                const revealed = revealedShareIds.has(share.id);
                const url = share.token
                  ? `${window.location.origin}/share/${share.token}`
                  : "";
                return (
                  <article className="share-history-item" key={share.id}>
                    <div className="share-history-item-heading">
                      <strong>
                        {share.snapshot_label || `版本 ${share.version_number}`}
                      </strong>
                      <small>
                        {share.permission === "comment" ? "可评论" : "仅查看"} ·{" "}
                        {share.status === "active" ? "有效" : "已撤销"}
                      </small>
                    </div>
                    <label>
                      分享链接
                      <input
                        readOnly
                        value={url || "旧记录：链接密钥不可恢复"}
                        onFocus={(event) => event.currentTarget.select()}
                      />
                    </label>
                    <button
                      disabled={!url}
                      type="button"
                      onClick={() => navigator.clipboard?.writeText(url)}
                    >
                      复制链接
                    </button>
                    <div className="share-secret-line">
                      <span>
                        访问码：
                        {revealed ? share.password || "不可恢复" : "••••••"}
                      </span>
                      <button
                        disabled={!share.password}
                        title={revealed ? "隐藏访问码" : "显示访问码"}
                        type="button"
                        onClick={() =>
                          setRevealedShareIds((current) => {
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
                      disabled={share.status !== "active"}
                      type="button"
                      onClick={() => void deleteShareHistory(share.id)}
                    >
                      撤销链接
                    </button>
                  </article>
                );
              })}
            </section>
            {!shareLink ? (
              <form action={createShareLink}>
                <p className="muted">
                  链接中的随机令牌负责定位当前快照；访问码单独验证，不会写入链接或数据库明文。
                </p>
                <label>
                  访问密码
                  <input
                    name="password"
                    type="text"
                    value={sharePassword}
                    minLength={6}
                    maxLength={200}
                    required
                    autoFocus
                    onChange={(event) => setSharePassword(event.target.value)}
                  />
                  <small className="muted">
                    已生成 6 位访问码，可根据需要修改。
                  </small>
                </label>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={generateSharePassword}
                >
                  <RotateCcw size={15} />
                  重新生成访问码
                </button>
                <label>
                  访问权限
                  <select
                    name="permission"
                    value={sharePermission}
                    onChange={(event) =>
                      setSharePermission(
                        event.target.value as "view" | "comment",
                      )
                    }
                  >
                    <option value="view">仅查看 PDF 和已有批注</option>
                    <option value="comment">
                      查看并发表评论（不能修改或删除他人批注）
                    </option>
                  </select>
                </label>
                {shareError && <p className="error">{shareError}</p>}
                <div className="dialog-actions">
                  <button type="submit">
                    <Share2 size={16} />
                    生成分享链接
                  </button>
                  <button
                    className="secondary-button"
                    type="button"
                    onClick={() => setShareEditor(false)}
                  >
                    取消
                  </button>
                </div>
              </form>
            ) : (
              <div className="share-result">
                <p className="muted">
                  链接创建成功。请分别向审阅人发送链接和访问密码。
                </p>
                <label>
                  分享链接
                  <input
                    readOnly
                    value={shareLink}
                    onFocus={(event) => event.currentTarget.select()}
                  />
                </label>
                <button
                  type="button"
                  onClick={() => navigator.clipboard?.writeText(shareLink)}
                >
                  复制链接
                </button>
                <label>
                  访问密码
                  <input readOnly value={sharePassword} />
                </label>
                <button
                  type="button"
                  onClick={() => navigator.clipboard?.writeText(sharePassword)}
                >
                  复制访问码
                </button>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => setShareEditor(false)}
                >
                  完成
                </button>
              </div>
            )}
          </section>
        </div>
      )}
      {commenting && active && (
        <div className="project-edit-backdrop">
          <section
            className="project-edit-dialog comment-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="comment-dialog-title"
          >
            <header>
              <div>
                <p className="eyebrow">Snapshot #{active.version_number}</p>
                <h2 id="comment-dialog-title">
                  {selection
                    ? selection.kind === "text"
                      ? `审阅第 ${selection.pageNumber} 页选中内容`
                      : `审阅第 ${selection.pageNumber} 页框选区域`
                    : `添加第 ${page} 页审阅`}
                </h2>
              </div>
              <button
                className="icon-button"
                type="button"
                title="关闭"
                onClick={() => {
                  setCommenting(false);
                  setSelection(null);
                }}
              >
                <X size={18} />
              </button>
            </header>
            <p className="muted">
              {selection
                ? selection.kind === "text"
                  ? "此审阅绑定所选文本和当前 PDF 快照，不会自动迁移到其他版本。"
                  : "此审阅绑定框选的图片、图表、公式或区域和当前 PDF 快照，不会自动迁移到其他版本。"
                : "这是页级审阅意见，绑定当前 PDF 快照，不会自动迁移到其他版本。"}
            </p>
            {selection?.kind === "text" && (
              <blockquote className="selected-text-preview">
                {selection.selectedText}
              </blockquote>
            )}
            {selection?.kind === "rectangle" && (
              <div
                className="selected-area-preview"
                aria-label="已框选图片或区域"
              >
                <span>已框选第 {selection.pageNumber} 页的图片/区域</span>
              </div>
            )}
            <form action={createComment}>
              <label>
                审阅内容
                <textarea
                  name="content"
                  rows={5}
                  autoFocus
                  placeholder="写下需要作者处理的内容、问题或修改建议"
                  required
                />
              </label>
              <label>
                分类
                <select
                  name="category"
                  value={categoryChoice}
                  onChange={(event) => setCategoryChoice(event.target.value)}
                >
                  {categories.map((category) => (
                    <option key={category} value={category}>
                      {categoryLabel(category)}
                    </option>
                  ))}
                  <option value="custom">其他（手动输入）</option>
                </select>
                {categoryChoice === "custom" && (
                  <input
                    autoFocus
                    name="customCategory"
                    maxLength={64}
                    placeholder="例如：实验设计、可复现性、伦理声明"
                    required
                  />
                )}
                <small className="muted">
                  可直接输入新类别，保存后会在本项目中复用。
                </small>
              </label>
              <label>
                优先级
                <select name="priority" defaultValue="medium">
                  <option value="low">低</option>
                  <option value="medium">中</option>
                  <option value="high">高</option>
                </select>
              </label>
              {commentError && <p className="error">{commentError}</p>}
              <div className="dialog-actions">
                <button type="submit">
                  <MessageSquarePlus size={16} />
                  保存审阅
                </button>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => {
                    setCommenting(false);
                    setSelection(null);
                  }}
                >
                  取消
                </button>
              </div>
            </form>
          </section>
        </div>
      )}
      <div className={`review-grid ${immersiveMode ? "immersive" : ""}`}>
        <aside className="left-panel">
          <div
            className="navigation-tabs"
            role="tablist"
            aria-label="PDF 导航方式"
          >
            <button
              aria-selected={navigationView === "outline"}
              className={navigationView === "outline" ? "selected" : ""}
              role="tab"
              type="button"
              onClick={() => setNavigationView("outline")}
            >
              目录
            </button>
            <button
              aria-selected={navigationView === "pages"}
              className={navigationView === "pages" ? "selected" : ""}
              role="tab"
              type="button"
              onClick={() => setNavigationView("pages")}
            >
              页面
            </button>
          </div>
          {navigationView === "outline" ? (
            <div className="outline-list" role="tree" aria-label="PDF 目录">
              {outline.map((entry) => (
                <button
                  className={page === entry.pageNumber ? "selected" : ""}
                  disabled={!entry.pageNumber}
                  key={entry.id}
                  aria-selected={page === entry.pageNumber}
                  role="treeitem"
                  style={{ paddingLeft: `${14 + entry.depth * 14}px` }}
                  title={
                    entry.pageNumber
                      ? `跳转到第 ${entry.pageNumber} 页`
                      : "此目录项没有页码目标"
                  }
                  type="button"
                  onClick={() => entry.pageNumber && setPage(entry.pageNumber)}
                >
                  <span className="outline-entry-title">{entry.title}</span>
                  <em className="outline-comment-count">
                    ({outlineCounts[entry.id] ?? 0})
                  </em>
                  <strong className="outline-page-number">
                    {entry.pageNumber ?? "—"}
                  </strong>
                </button>
              ))}
              {!outline.length && (
                <div className="empty small">
                  当前 PDF 未提供目录。可切换到“页面”按页定位。
                </div>
              )}
            </div>
          ) : (
            <div className="page-list">
              {Array.from({ length: active?.page_count ?? 0 }, (_, i) => (
                <button
                  className={page === i + 1 ? "selected" : ""}
                  onClick={() => setPage(i + 1)}
                  key={i}
                >
                  第 {i + 1} 页{" "}
                  <small>
                    {
                      snapshotComments.filter(
                        (comment) => comment.page_number === i + 1,
                      ).length
                    }
                  </small>
                </button>
              ))}
            </div>
          )}
        </aside>
        {active ? (
          <PdfReviewer
            key={active.id}
            comments={snapshotComments}
            fileUrl={`/api/projects/${projectId}/snapshots/${active.id}/pdf`}
            page={page}
            focusedCommentId={focusedCommentId}
            immersiveMode={immersiveMode}
            onPageChange={setPage}
            onOutlineChange={handleOutlineChange}
            onTextSelection={openTextSelectionComment}
            onAreaSelection={openAreaSelectionComment}
            onToggleImmersive={() => setImmersiveMode((value) => !value)}
          />
        ) : (
          <section className="reader">
            <div className="reader-empty">
              创建第一份 PDF 快照后在此处审阅。
            </div>
          </section>
        )}
        <aside className="comments">
          <div className="comment-head">
            <strong>评论与任务</strong>
          </div>
          <div className="comment-filters">
            <select
              aria-label="评论状态"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            >
              <option value="all">全部状态</option>
              <option value="open">待处理</option>
              <option value="reopened">已重开</option>
              <option value="resolved">已解决</option>
              <option value="obsolete">已过期</option>
            </select>
            <select
              aria-label="评论类别"
              value={categoryFilter}
              onChange={(event) => setCategoryFilter(event.target.value)}
            >
              <option value="all">全部类别</option>
              {categories.map((category) => (
                <option key={category} value={category}>
                  {categoryLabel(category)}
                </option>
              ))}
            </select>
          </div>
          <div className="comment-scope" role="tablist" aria-label="评论范围">
            <button
              aria-selected={commentScope === "page"}
              className={commentScope === "page" ? "selected" : ""}
              role="tab"
              type="button"
              onClick={() => setCommentScope("page")}
            >
              当前页
            </button>
            <button
              aria-selected={commentScope === "snapshot"}
              className={commentScope === "snapshot" ? "selected" : ""}
              role="tab"
              type="button"
              onClick={() => setCommentScope("snapshot")}
            >
              当前快照
            </button>
          </div>
          <p className="muted">
            {commentScope === "page" ? `第 ${page} 页` : "当前快照"} ·{" "}
            {shown.length} 条
          </p>
          {commentActionError && <p className="error">{commentActionError}</p>}
          {shown.map((c) => {
            const canManage =
              currentUser?.role === "admin" ||
              currentUser?.id === project?.created_by_user_id ||
              currentUser?.id === c.author_id;
            return (
              <article key={c.id} className={`comment ${c.priority}`}>
                <div className="comment-meta">
                  <button type="button" onClick={() => focusComment(c)}>
                    <MapPin size={13} />第 {c.page_number} 页
                  </button>
                  <span>
                    {c.status} · {categoryLabel(c.category)}
                  </span>
                </div>
                <p className="comment-author">
                  审阅人：{c.author_display_name}
                </p>
                {c.anchor_type === "rectangle" && (
                  <p className="comment-selected-text">
                    已框选图片、图表、公式或页面区域
                  </p>
                )}
                {c.selected_text && (
                  <blockquote className="comment-selected-text">
                    {c.selected_text}
                  </blockquote>
                )}
                <button
                  className="comment-locate"
                  type="button"
                  onClick={() => focusComment(c)}
                >
                  {c.content}
                </button>
                {c.resolution_note && (
                  <p className="resolution-note">
                    处理说明：{c.resolution_note}
                  </p>
                )}
                <div className="comment-actions">
                  <button type="button" onClick={() => toggleReplies(c.id)}>
                    <MessageCircle size={14} />
                    回复 {c.reply_count ?? 0}
                  </button>
                  {canManage &&
                    (c.status === "open" || c.status === "reopened") && (
                      <button
                        type="button"
                        title="标记为已解决"
                        onClick={() => transitionComment(c.id, "resolved")}
                      >
                        <Check size={14} />
                        解决
                      </button>
                    )}
                  {canManage && c.status === "resolved" && (
                    <button
                      type="button"
                      title="重新打开评论"
                      onClick={() => transitionComment(c.id, "reopened")}
                    >
                      <RotateCcw size={14} />
                      重开
                    </button>
                  )}
                  {canManage && (
                    <button
                      type="button"
                      title="删除批注"
                      onClick={() => deleteComment(c)}
                    >
                      <Trash2 size={14} />
                      删除
                    </button>
                  )}
                </div>
                {replyingTo === c.id && (
                  <div className="reply-thread">
                    {(replies[c.id] ?? []).map((reply) => (
                      <p key={reply.id}>
                        <b>{reply.display_name}</b>
                        {reply.content}
                      </p>
                    ))}
                    <form action={(form) => createReply(c.id, form)}>
                      <input
                        aria-label="回复内容"
                        maxLength={10000}
                        name="reply"
                        placeholder="回复此审阅意见"
                        required
                      />
                      <button type="submit">发送</button>
                    </form>
                  </div>
                )}
              </article>
            );
          })}
          {!shown.length && (
            <div className="empty small">此页暂无符合筛选条件的评论。</div>
          )}
        </aside>
      </div>
    </main>
  );
}
