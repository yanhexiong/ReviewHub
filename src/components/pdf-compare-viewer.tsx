"use client";

import { useState } from "react";
import { SpecialZoomLevel, Viewer, Worker } from "@react-pdf-viewer/core";
import {
  type HighlightArea,
  highlightPlugin,
} from "@react-pdf-viewer/highlight";
import { pageNavigationPlugin } from "@react-pdf-viewer/page-navigation";
import { zoomPlugin } from "@react-pdf-viewer/zoom";
import {
  ChevronLeft,
  ChevronRight,
  MessageCircle,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import type { PdfDiffHighlight, PdfDiffKind } from "@/lib/pdf-diff";

export type CompareComment = {
  id: string;
  page_number: number;
  anchor_type: string;
  normalized_x: number | null;
  normalized_y: number | null;
  normalized_width: number | null;
  normalized_height: number | null;
  selected_text: string | null;
  content: string;
  category: string;
  priority: string;
  status: string;
  author_display_name: string;
};

type Props = {
  fileUrl: string;
  title: string;
  mode: PdfDiffKind | "composite";
  layers: Array<{
    kind: PdfDiffKind;
    highlights: PdfDiffHighlight[];
  }>;
  comments: CompareComment[];
};

function ComparePdfPane({ fileUrl, title, mode, layers, comments }: Props) {
  const pageNavigation = pageNavigationPlugin({ enableShortcuts: true });
  const zoom = zoomPlugin({ enableShortcuts: true });
  const [selectedCommentId, setSelectedCommentId] = useState<string | null>(
    null,
  );
  const highlight = highlightPlugin({
    renderHighlights: ({ getCssProperties, pageIndex, rotation }) => (
      <>
        {layers.flatMap((layer) =>
          layer.highlights
            .filter((item) => item.pageNumber === pageIndex + 1)
            .map((item) => {
              const area: HighlightArea = {
                height: item.normalizedHeight * 100,
                left: item.normalizedX * 100,
                pageIndex,
                top: item.normalizedY * 100,
                width: item.normalizedWidth * 100,
              };
              return (
                <span
                  className={`pdf-diff-highlight ${layer.kind}`}
                  data-diff-highlight={item.id}
                  key={`${layer.kind}-${item.id}`}
                  style={getCssProperties(area, rotation)}
                  title={item.text}
                />
              );
            }),
        )}
        {comments
          .filter((comment) => comment.page_number === pageIndex + 1)
          .map((comment) => {
            const hasAnchor =
              comment.anchor_type !== "page_note" &&
              comment.normalized_x !== null &&
              comment.normalized_y !== null &&
              comment.normalized_width !== null &&
              comment.normalized_height !== null;
            const left = hasAnchor ? comment.normalized_x! * 100 : 3;
            const top = hasAnchor ? comment.normalized_y! * 100 : 3;
            const width = hasAnchor ? comment.normalized_width! * 100 : 1;
            const height = hasAnchor ? comment.normalized_height! * 100 : 1;
            const markerArea: HighlightArea = {
              height: 2,
              left: Math.min(96, Math.max(1, left + width)),
              pageIndex,
              top: Math.min(94, Math.max(1, top)),
              width: 2,
            };
            return (
              <span className="pdf-compare-comment-annotation" key={comment.id}>
                {hasAnchor && (
                  <span
                    className={`pdf-compare-comment-highlight ${comment.priority}`}
                    data-compare-comment-highlight={comment.id}
                    style={getCssProperties(
                      { height, left, pageIndex, top, width },
                      rotation,
                    )}
                    title={`${comment.selected_text ?? "区域审阅"}\n${comment.content}`}
                  />
                )}
                <button
                  aria-label={`打开母版本第 ${comment.page_number} 页的审阅批注`}
                  aria-pressed={selectedCommentId === comment.id}
                  className={`pdf-compare-comment-marker ${comment.priority} ${selectedCommentId === comment.id ? "selected" : ""}`}
                  data-compare-comment-marker={comment.id}
                  style={getCssProperties(markerArea, rotation)}
                  title={`${comment.author_display_name}：${comment.content}`}
                  type="button"
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    setSelectedCommentId((current) =>
                      current === comment.id ? null : comment.id,
                    );
                  }}
                >
                  <MessageCircle size={12} />
                </button>
              </span>
            );
          })}
      </>
    ),
  });
  const { CurrentPageInput, GoToNextPage, GoToPreviousPage, NumberOfPages } =
    pageNavigation;
  const { ZoomIn: ZoomInButton, ZoomOut: ZoomOutButton } = zoom;
  const [loaded, setLoaded] = useState(false);

  return (
    <section className={`pdf-compare-pane ${mode}`}>
      <header className="pdf-compare-pane-header">
        <div>
          <strong>{title}</strong>
          <span>
            {mode === "composite"
              ? "红色：母版本删除 · 绿色：子版本新增（按母版上下文定位）"
              : mode === "removed"
                ? "红色：母版本中被删除"
                : "绿色：子版本中新增"}
          </span>
        </div>
        <span className="pdf-compare-change-count">
          {layers.reduce((count, layer) => count + layer.highlights.length, 0)}{" "}
          处
        </span>
      </header>
      <div className="pdf-compare-toolbar" aria-label={`${title} PDF 工具`}>
        <GoToPreviousPage>
          {({ isDisabled, onClick }) => (
            <button
              disabled={isDisabled}
              title="上一页"
              type="button"
              onClick={onClick}
            >
              <ChevronLeft size={16} />
            </button>
          )}
        </GoToPreviousPage>
        <span className="pdf-compare-page-input">
          <CurrentPageInput />
          <span>/</span>
          <NumberOfPages>
            {({ numberOfPages }) => <span>{numberOfPages}</span>}
          </NumberOfPages>
        </span>
        <GoToNextPage>
          {({ isDisabled, onClick }) => (
            <button
              disabled={isDisabled}
              title="下一页"
              type="button"
              onClick={onClick}
            >
              <ChevronRight size={16} />
            </button>
          )}
        </GoToNextPage>
        <ZoomOutButton>
          {({ onClick }) => (
            <button title="缩小" type="button" onClick={onClick}>
              <ZoomOut size={16} />
            </button>
          )}
        </ZoomOutButton>
        <ZoomInButton>
          {({ onClick }) => (
            <button title="放大" type="button" onClick={onClick}>
              <ZoomIn size={16} />
            </button>
          )}
        </ZoomInButton>
      </div>
      <div className="pdf-compare-pane-viewer">
        {!loaded && <span className="pdf-compare-loading">正在加载 PDF…</span>}
        <Worker workerUrl="/api/pdf-worker">
          <Viewer
            defaultScale={SpecialZoomLevel.PageWidth}
            enableSmoothScroll={false}
            fileUrl={fileUrl}
            plugins={[pageNavigation, zoom, highlight]}
            onDocumentLoad={() => setLoaded(true)}
            renderError={(error) => (
              <div className="reader-error">
                PDF 加载失败：{error.message || "未知错误"}
              </div>
            )}
          />
        </Worker>
      </div>
      {comments.length > 0 && (
        <div
          className="pdf-compare-comment-list"
          data-testid="pdf-compare-mother-comments"
        >
          <div className="pdf-compare-comment-list-heading">
            <strong>母版本批注</strong>
            <span>{comments.length} 条</span>
          </div>
          {comments.map((comment) => (
            <article
              className={`pdf-compare-comment-card ${comment.priority} ${selectedCommentId === comment.id ? "selected" : ""}`}
              data-compare-comment-id={comment.id}
              key={comment.id}
            >
              <header>
                <strong>
                  第 {comment.page_number} 页 · {comment.author_display_name}
                </strong>
                <span>
                  {comment.category} ·{" "}
                  {comment.status === "open" ? "待处理" : comment.status}
                </span>
              </header>
              {comment.selected_text && (
                <blockquote>{comment.selected_text}</blockquote>
              )}
              <p>{comment.content}</p>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

export function PdfCompareViewer({
  projectId,
  beforeSnapshotId,
  afterSnapshotId,
  beforeTitle,
  afterTitle,
  added,
  removed,
  compositeAdded,
  motherComments,
}: {
  projectId: string;
  beforeSnapshotId: string;
  afterSnapshotId: string;
  beforeTitle: string;
  afterTitle: string;
  added: PdfDiffHighlight[];
  removed: PdfDiffHighlight[];
  compositeAdded: PdfDiffHighlight[];
  motherComments: CompareComment[];
}) {
  const [viewMode, setViewMode] = useState<"composite" | "dual">("composite");

  return (
    <>
      <div
        className="pdf-compare-mode-switch"
        role="tablist"
        aria-label="PDF 比对视图模式"
      >
        <button
          aria-selected={viewMode === "composite"}
          className={viewMode === "composite" ? "selected" : ""}
          data-testid="compare-mode-composite"
          role="tab"
          type="button"
          onClick={() => setViewMode("composite")}
        >
          合成视图
        </button>
        <button
          aria-selected={viewMode === "dual"}
          className={viewMode === "dual" ? "selected" : ""}
          data-testid="compare-mode-dual"
          role="tab"
          type="button"
          onClick={() => setViewMode("dual")}
        >
          双视图预览
        </button>
      </div>
      {viewMode === "composite" ? (
        <div
          className="pdf-compare-viewer composite"
          data-testid="pdf-compare-viewer"
        >
          <ComparePdfPane
            fileUrl={`/api/projects/${projectId}/snapshots/${beforeSnapshotId}/pdf`}
            layers={[
              { kind: "removed", highlights: removed },
              { kind: "added", highlights: compositeAdded },
            ]}
            mode="composite"
            title={`${beforeTitle} · 合成视图`}
            comments={motherComments}
          />
        </div>
      ) : (
        <div
          className="pdf-compare-viewer dual"
          data-testid="pdf-compare-viewer"
        >
          <ComparePdfPane
            fileUrl={`/api/projects/${projectId}/snapshots/${beforeSnapshotId}/pdf`}
            layers={[{ kind: "removed", highlights: removed }]}
            mode="removed"
            title={beforeTitle}
            comments={motherComments}
          />
          <ComparePdfPane
            fileUrl={`/api/projects/${projectId}/snapshots/${afterSnapshotId}/pdf`}
            layers={[{ kind: "added", highlights: added }]}
            mode="added"
            title={afterTitle}
            comments={[]}
          />
        </div>
      )}
    </>
  );
}
