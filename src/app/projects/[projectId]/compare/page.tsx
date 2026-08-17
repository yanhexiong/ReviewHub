"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, GitCompareArrows, MessageCircle } from "lucide-react";
import {
  PdfCompareViewer,
  type CompareComment,
} from "@/components/pdf-compare-viewer";
import { PreferencesControls } from "@/features/preferences";
import { comparePdfBytes, type PdfDiffResult } from "@/lib/pdf-diff";
import { useProjectIDFromLocation } from "@/lib/static-route";

type Snapshot = {
  id: string;
  version_number: number;
  snapshot_label: string | null;
  git_commit_short_sha: string | null;
  page_count: number;
};

type PageStatus = {
  page: number;
  status: "unchanged" | "modified" | "added" | "removed";
};

type CommentRow = {
  id: string;
  page_number: number;
  content: string;
  category: string;
  status: string;
  priority: string;
  author_display_name: string;
  reply_count?: number;
};

type CompareResult = {
  snapshotA: Snapshot;
  snapshotB: Snapshot;
  pages: PageStatus[];
  changedPages: number;
  comments: {
    added: CommentRow[];
    removed: CommentRow[];
    modified: Array<{ before: CommentRow; after: CommentRow }>;
    addedCount: number;
    removedCount: number;
    modifiedCount: number;
  };
  motherComments: CompareComment[];
  textDiff: PdfDiffResult;
  notice: string;
};

const emptyTextDiff = (): PdfDiffResult => ({
  added: [],
  removed: [],
  compositeAdded: [],
  addedTokenCount: 0,
  removedTokenCount: 0,
  truncated: false,
});

const statusLabel: Record<PageStatus["status"], string> = {
  unchanged: "无变化",
  modified: "内容修改",
  added: "新增页",
  removed: "移除页",
};

export default function ComparePage() {
  const projectId = useProjectIDFromLocation();
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const [aId, setAId] = useState("");
  const [bId, setBId] = useState("");
  const [result, setResult] = useState<CompareResult | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!projectId) return;
    fetch(`/api/projects/${projectId}/snapshots`)
      .then(async (response) => {
        if (!response.ok) throw new Error("快照加载失败");
        const all = (await response.json()).snapshots as Snapshot[];
        setSnapshots(all);
        if (all.length >= 2) {
          setAId(all[1].id);
          setBId(all[0].id);
        } else if (all.length === 1) {
          setAId(all[0].id);
          setBId(all[0].id);
        }
      })
      .catch((reason) =>
        setError(reason instanceof Error ? reason.message : "快照加载失败"),
      );
  }, [projectId]);

  const loadCompare = useCallback(
    async (a: string, b: string) => {
      if (!a || !b) return;
      setLoading(true);
      setError("");
      try {
        const response = await fetch(
          `/api/projects/${projectId}/compare?snapshotA=${encodeURIComponent(a)}&snapshotB=${encodeURIComponent(b)}`,
        );
        const body = (await response.json()) as CompareResult & {
          error?: { message?: string };
        };
        if (!response.ok) throw new Error(body.error?.message ?? "比对失败");
        let textDiff = body.textDiff;
        let notice = body.notice;
        try {
          const [beforeResponse, afterResponse] = await Promise.all([
            fetch(
              `/api/projects/${projectId}/snapshots/${encodeURIComponent(a)}/pdf`,
            ),
            fetch(
              `/api/projects/${projectId}/snapshots/${encodeURIComponent(b)}/pdf`,
            ),
          ]);
          if (!beforeResponse.ok || !afterResponse.ok)
            throw new Error("PDF 文件下载失败");
          const [beforeBytes, afterBytes] = await Promise.all([
            beforeResponse.arrayBuffer(),
            afterResponse.arrayBuffer(),
          ]);
          textDiff = await comparePdfBytes(beforeBytes, afterBytes);
        } catch (pdfError) {
          textDiff = emptyTextDiff();
          const detail =
            pdfError instanceof Error ? `（${pdfError.message}）` : "";
          notice = `${notice} 浏览器无法提取 PDF 文字几何差异，已降级为页级与批注比对${detail}。`;
        }
        setResult({ ...body, notice, textDiff });
      } catch (reason) {
        setError(reason instanceof Error ? reason.message : "比对失败");
      } finally {
        setLoading(false);
      }
    },
    [projectId],
  );

  useEffect(() => {
    if (aId && bId) void loadCompare(aId, bId);
  }, [aId, bId, loadCompare]);

  if (!projectId) return null;

  const snapshotName = (snapshot: Snapshot | undefined, fallback: string) =>
    snapshot
      ? `${snapshot.snapshot_label || `版本 ${snapshot.version_number}`}（#${snapshot.version_number}）`
      : fallback;

  return (
    <main className="projects compare-page-shell">
      <header>
        <div>
          <p className="eyebrow">REVIEW HUB / 版本比对</p>
          <h1>版本内容与批注比对</h1>
        </div>
        <div className="page-actions">
          <PreferencesControls placement="toolbar" />
          <Link className="toolbar-link" href={`/projects/${projectId}`}>
            <ArrowLeft size={16} /> 返回项目首页
          </Link>
        </div>
      </header>
      {error && <p className="error">{error}</p>}
      {snapshots.length >= 2 ? (
        <section className="compare-controls">
          <label>
            母版本（基准）
            <select
              value={aId}
              onChange={(event) => setAId(event.target.value)}
            >
              {snapshots.map((snapshot) => (
                <option key={snapshot.id} value={snapshot.id}>
                  {snapshotName(snapshot, "")}
                </option>
              ))}
            </select>
          </label>
          <GitCompareArrows size={20} aria-hidden="true" />
          <label>
            子版本（相对母版本）
            <select
              value={bId}
              onChange={(event) => setBId(event.target.value)}
            >
              {snapshots.map((snapshot) => (
                <option key={snapshot.id} value={snapshot.id}>
                  {snapshotName(snapshot, "")}
                </option>
              ))}
            </select>
          </label>
        </section>
      ) : (
        <p className="muted">至少需要两个审阅版本才能进行比对。</p>
      )}

      {loading && <p className="muted">正在比对...</p>}
      {result && (
        <>
          <section className="compare-summary">
            <div>
              <span>内容变更页</span>
              <strong>{result.changedPages}</strong>
              <small>
                {result.pages.length} 页中
                {result.pages.filter((p) => p.status === "added").length}{" "}
                页新增、
                {result.pages.filter((p) => p.status === "removed").length}{" "}
                页移除
              </small>
            </div>
            <div>
              <span>新增批注</span>
              <strong>{result.comments.addedCount}</strong>
            </div>
            <div>
              <span>删除批注</span>
              <strong>{result.comments.removedCount}</strong>
            </div>
            <div>
              <span>修改批注</span>
              <strong>{result.comments.modifiedCount}</strong>
            </div>
          </section>

          <section className="compare-section compare-pdf-section">
            <div className="compare-section-heading">
              <div>
                <h2>PDF 文字差异</h2>
                <p className="muted">
                  以母版本为基准：子版本新增内容显示在右侧 PDF
                  的绿色标注中，母版本被删除的内容显示在左侧 PDF
                  的红色标注中。左侧同时保留母版本的原批注，右侧不叠加这些批注，方便直接核对批注要求与修改结果。文字按全文序列对齐，不会因为前文插入导致后续分页变化而把整页误判为新增。
                  下方可切换为合成视图或双视图预览；合成视图以母版本为底图，将子版本新增内容映射到母版上下文位置。跨行断词、连字符、重复页眉和页码会按排版差异归一化。
                </p>
              </div>
              <div className="compare-diff-legend" aria-label="差异颜色说明">
                <span className="compare-diff-legend-added">绿色：新增</span>
                <span className="compare-diff-legend-removed">红色：删除</span>
              </div>
            </div>
            {result.textDiff.truncated && (
              <p className="warning">
                变更内容过多，页面只显示前 {result.textDiff.added.length}{" "}
                处新增和 {result.textDiff.removed.length}{" "}
                处删除；统计仍基于完整文字序列。
              </p>
            )}
            {result.textDiff.addedTokenCount === 0 &&
              result.textDiff.removedTokenCount === 0 && (
                <p className="muted">
                  两个 PDF
                  的可提取文字完全一致，仍保留双栏视图以显示母版本批注。
                </p>
              )}
            <PdfCompareViewer
              added={result.textDiff.added}
              afterSnapshotId={result.snapshotB.id}
              afterTitle={snapshotName(result.snapshotB, "子版本")}
              beforeSnapshotId={result.snapshotA.id}
              beforeTitle={snapshotName(result.snapshotA, "母版本")}
              compositeAdded={result.textDiff.compositeAdded}
              motherComments={result.motherComments}
              projectId={projectId}
              removed={result.textDiff.removed}
            />
          </section>

          <section className="compare-section">
            <h2>页面内容差异</h2>
            <p className="muted">{result.notice}</p>
            <div className="compare-page-list">
              {result.pages.map((page) => (
                <div
                  className={`compare-page compare-${page.status}`}
                  key={page.page}
                >
                  <strong>第 {page.page} 页</strong>
                  <span>{statusLabel[page.status]}</span>
                </div>
              ))}
            </div>
          </section>

          <section className="compare-section">
            <h2>批注差异</h2>
            {result.comments.added.length > 0 && (
              <div className="compare-comment-group">
                <h3>新增批注（仅出现在版本 B）</h3>
                {result.comments.added.map((comment) => (
                  <article className="compare-comment added" key={comment.id}>
                    <p>
                      <MessageCircle size={14} />第 {comment.page_number} 页 ·{" "}
                      {comment.author_display_name} · {comment.category}
                    </p>
                    <blockquote>{comment.content}</blockquote>
                  </article>
                ))}
              </div>
            )}
            {result.comments.removed.length > 0 && (
              <div className="compare-comment-group">
                <h3>删除批注（仅出现在版本 A）</h3>
                {result.comments.removed.map((comment) => (
                  <article className="compare-comment removed" key={comment.id}>
                    <p>
                      第 {comment.page_number} 页 ·{" "}
                      {comment.author_display_name} · {comment.category}
                    </p>
                    <blockquote>{comment.content}</blockquote>
                  </article>
                ))}
              </div>
            )}
            {result.comments.modified.length > 0 && (
              <div className="compare-comment-group">
                <h3>修改批注（同一批注在两个版本中内容或状态不同）</h3>
                {result.comments.modified.map(({ before, after }) => (
                  <article className="compare-comment modified" key={after.id}>
                    <div>
                      <strong>版本 A</strong>
                      <blockquote>{before.content}</blockquote>
                    </div>
                    <div>
                      <strong>版本 B</strong>
                      <blockquote>{after.content}</blockquote>
                    </div>
                  </article>
                ))}
              </div>
            )}
            {result.comments.addedCount +
              result.comments.removedCount +
              result.comments.modifiedCount ===
              0 && <p className="muted">两个版本的批注内容完全一致。</p>}
          </section>
        </>
      )}
    </main>
  );
}
