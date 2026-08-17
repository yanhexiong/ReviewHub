"use client";

import { type PointerEvent, useEffect, useRef, useState } from "react";
import {
  type DocumentLoadEvent,
  type PdfJs,
  SpecialZoomLevel,
  Viewer,
  Worker,
} from "@react-pdf-viewer/core";
import {
  type HighlightArea,
  type SelectionData,
  highlightPlugin,
} from "@react-pdf-viewer/highlight";
import { pageNavigationPlugin } from "@react-pdf-viewer/page-navigation";
import { searchPlugin } from "@react-pdf-viewer/search";
import { zoomPlugin } from "@react-pdf-viewer/zoom";
import {
  ChevronLeft,
  ChevronRight,
  Crop,
  MessageCircle,
  MessageSquarePlus,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  ZoomIn,
  ZoomOut,
} from "lucide-react";

export type ReviewComment = {
  id: string;
  page_number: number;
  anchor_type: string;
  normalized_x: number | null;
  normalized_y: number | null;
  normalized_width: number | null;
  normalized_height: number | null;
  selected_text: string | null;
  content: string;
  priority: string;
  category?: string;
  status?: string;
  author_display_name?: string;
};

export type TextSelectionAnchor = {
  kind: "text";
  pageNumber: number;
  normalizedX: number;
  normalizedY: number;
  normalizedWidth: number;
  normalizedHeight: number;
  selectedText: string;
  selectedTextContext: string;
};

export type RectangleSelectionAnchor = {
  kind: "rectangle";
  pageNumber: number;
  normalizedX: number;
  normalizedY: number;
  normalizedWidth: number;
  normalizedHeight: number;
};

type AreaDrag = {
  pageNumber: number;
  rect: DOMRect;
  x: number;
  y: number;
};

export type PdfOutlineEntry = {
  id: string;
  title: string;
  pageNumber: number | null;
  depth: number;
};

type Props = {
  fileUrl: string;
  page: number;
  comments: ReviewComment[];
  onPageChange: (page: number) => void;
  onTextSelection: (selection: TextSelectionAnchor) => void;
  onAreaSelection: (selection: RectangleSelectionAnchor) => void;
  onOutlineChange: (outline: PdfOutlineEntry[]) => void;
  focusedCommentId: string | null;
  immersiveMode: boolean;
  onToggleImmersive: () => void;
};

async function resolveOutlinePage(
  doc: PdfJs.PdfDocument,
  destination: PdfJs.Outline["dest"],
) {
  const resolved =
    typeof destination === "string"
      ? await doc.getDestination(destination)
      : destination;
  if (!resolved?.length) return null;
  const reference = resolved[0];
  if (typeof reference === "number") return reference + 1;
  return (await doc.getPageIndex(reference)) + 1;
}

async function flattenOutline(
  doc: PdfJs.PdfDocument,
  items: PdfJs.Outline[],
  depth = 0,
  parentId = "",
): Promise<PdfOutlineEntry[]> {
  const entries: PdfOutlineEntry[] = [];
  for (const [index, item] of items.entries()) {
    const id = parentId ? `${parentId}.${index}` : `${index}`;
    let pageNumber: number | null = null;
    try {
      pageNumber = await resolveOutlinePage(doc, item.dest);
    } catch {
      // Some PDF bookmarks point to external URLs or malformed destinations.
    }
    entries.push({
      id,
      title: item.title || "未命名目录项",
      pageNumber,
      depth,
    });
    entries.push(
      ...(await flattenOutline(doc, item.items ?? [], depth + 1, id)),
    );
  }
  return entries;
}

const toAnchor = (
  selectedText: string,
  selectionData: SelectionData | undefined,
  areas: HighlightArea[],
  fallback: HighlightArea,
): TextSelectionAnchor | null => {
  const text = selectedText.trim().slice(0, 5000);
  if (!text) return null;
  const startPage = selectionData?.startPageIndex ?? areas[0]?.pageIndex;
  const pageAreas = areas.filter(
    (candidate) => candidate.pageIndex === startPage,
  );
  const area = pageAreas.length
    ? pageAreas.slice(1).reduce<HighlightArea>((bounds, candidate) => {
        const right = Math.max(
          bounds.left + bounds.width,
          candidate.left + candidate.width,
        );
        const bottom = Math.max(
          bounds.top + bounds.height,
          candidate.top + candidate.height,
        );
        const left = Math.min(bounds.left, candidate.left);
        const top = Math.min(bounds.top, candidate.top);
        return {
          height: bottom - top,
          left,
          pageIndex: candidate.pageIndex,
          top,
          width: right - left,
        };
      }, pageAreas[0])
    : fallback;
  const context = (
    selectionData?.divTexts.map((item) => item.textContent).join(" ") ?? text
  )
    .trim()
    .slice(0, 5000);
  return {
    pageNumber: area.pageIndex + 1,
    kind: "text",
    normalizedX: area.left / 100,
    normalizedY: area.top / 100,
    normalizedWidth: area.width / 100,
    normalizedHeight: area.height / 100,
    selectedText: text,
    selectedTextContext: context,
  };
};

export function PdfReviewer({
  fileUrl,
  page,
  comments,
  onPageChange,
  onTextSelection,
  onAreaSelection,
  onOutlineChange,
  focusedCommentId,
  immersiveMode,
  onToggleImmersive,
}: Props) {
  const [documentLoaded, setDocumentLoaded] = useState(false);
  const [areaSelectionMode, setAreaSelectionMode] = useState(false);
  const [areaDrag, setAreaDrag] = useState<AreaDrag | null>(null);
  const [selectedCommentId, setSelectedCommentId] = useState<string | null>(
    null,
  );
  const viewerPageRef = useRef(page);
  const viewerShellRef = useRef<HTMLDivElement>(null);
  const pageNavigationPluginInstance = pageNavigationPlugin({
    enableShortcuts: true,
  });
  const zoomPluginInstance = zoomPlugin({ enableShortcuts: true });
  const searchPluginInstance = searchPlugin({ enableShortcuts: true });
  const highlightPluginInstance = highlightPlugin({
    renderHighlightTarget: (props) => (
      <button
        className="text-review-target"
        style={{
          left: `${props.selectionRegion.left}%`,
          top: `${props.selectionRegion.top + props.selectionRegion.height}%`,
        }}
        type="button"
        onClick={() => {
          const selection = toAnchor(
            props.selectedText,
            props.selectionData,
            props.highlightAreas,
            props.selectionRegion,
          );
          props.cancel();
          if (selection) onTextSelection(selection);
        }}
      >
        <MessageSquarePlus size={15} />
        添加审阅
      </button>
    ),
    renderHighlights: ({ getCssProperties, pageIndex, rotation }) => (
      <>
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
            const markerLeft = Math.min(96, Math.max(1, left + width));
            const markerTop = Math.min(94, Math.max(1, top));
            const isSelected = selectedCommentId === comment.id;
            const markerArea: HighlightArea = {
              height: 2,
              left: markerLeft,
              pageIndex,
              top: markerTop,
              width: 2,
            };
            return (
              <span className="pdf-comment-annotation" key={comment.id}>
                {hasAnchor && (
                  <span
                    className={`saved-text-highlight ${comment.priority}`}
                    data-comment-highlight={comment.id}
                    style={getCssProperties(
                      { height, left, pageIndex, top, width },
                      rotation,
                    )}
                    title={`${comment.selected_text ?? "区域审阅"}\n${comment.content}`}
                  />
                )}
                <button
                  aria-label={`打开第 ${comment.page_number} 页的审阅批注`}
                  className={`pdf-comment-marker ${comment.priority} ${isSelected ? "selected" : ""}`}
                  data-comment-marker={comment.id}
                  style={getCssProperties(markerArea, rotation)}
                  title={`${comment.author_display_name ?? "审阅人"}：${comment.content}`}
                  type="button"
                  onPointerDown={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                  }}
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    setSelectedCommentId((current) =>
                      current === comment.id ? null : comment.id,
                    );
                  }}
                >
                  <MessageCircle size={14} />
                </button>
              </span>
            );
          })}
      </>
    ),
  });

  useEffect(() => {
    if (!documentLoaded || page === viewerPageRef.current) return;
    viewerPageRef.current = page;
    pageNavigationPluginInstance.jumpToPage(page - 1);
  }, [documentLoaded, page]);

  useEffect(() => {
    if (!documentLoaded || !focusedCommentId) return;
    const comment = comments.find((item) => item.id === focusedCommentId);
    if (!comment) return;
    const viewerShell = viewerShellRef.current;
    if (!viewerShell) return;
    const hasAnchor =
      comment.anchor_type !== "page_note" &&
      comment.normalized_x !== null &&
      comment.normalized_y !== null &&
      comment.normalized_width !== null &&
      comment.normalized_height !== null;
    setSelectedCommentId(comment.id);
    highlightPluginInstance.jumpToHighlightArea({
      height: hasAnchor ? comment.normalized_height! * 100 : 1,
      left: hasAnchor ? comment.normalized_x! * 100 : 3,
      pageIndex: comment.page_number - 1,
      top: hasAnchor ? comment.normalized_y! * 100 : 3,
      width: hasAnchor ? comment.normalized_width! * 100 : 1,
    });
    let animationFrame = 0;
    let attempts = 0;
    let stableFrames = 0;
    let previousLayout = "";
    const maxAttempts = 300;
    const centerTarget = (
      target: { left: number; top: number; width: number; height: number },
      container: HTMLElement,
    ) => {
      const containerRect = container.getBoundingClientRect();
      const viewportWidth = container.clientWidth;
      const viewportHeight = container.clientHeight;
      if (!viewportWidth || !viewportHeight) return;
      const left = Math.max(
        0,
        Math.min(
          container.scrollWidth - viewportWidth,
          container.scrollLeft +
            target.left -
            containerRect.left -
            (viewportWidth - target.width) / 2,
        ),
      );
      const top = Math.max(
        0,
        Math.min(
          container.scrollHeight - viewportHeight,
          container.scrollTop +
            target.top -
            containerRect.top -
            (viewportHeight - target.height) / 2,
        ),
      );
      container.scrollTo({
        behavior: "auto",
        left,
        top,
      });
    };
    const continueCentering = () => {
      if (attempts < maxAttempts) {
        animationFrame = window.requestAnimationFrame(centerComment);
      }
    };
    const centerComment = () => {
      attempts += 1;
      const highlight = viewerShell.querySelector<HTMLElement>(
        `[data-comment-highlight="${comment.id}"]`,
      );
      const marker = viewerShell.querySelector<HTMLElement>(
        `[data-comment-marker="${comment.id}"]`,
      );
      const element = highlight ?? marker;
      if (element && element.getBoundingClientRect().height > 0) {
        const container = element.closest<HTMLElement>(
          ".rpv-core__inner-pages",
        );
        if (container) {
          const rect = element.getBoundingClientRect();
          centerTarget(rect, container);
          const layout = [
            container.scrollHeight,
            container.clientHeight,
            Math.round(rect.left * 10),
            Math.round(rect.top * 10),
            Math.round(rect.width * 10),
            Math.round(rect.height * 10),
          ].join(":");
          stableFrames = layout === previousLayout ? stableFrames + 1 : 0;
          previousLayout = layout;
          if (attempts < 8 || stableFrames < 4) continueCentering();
        } else if (attempts < 8) {
          element.scrollIntoView({ behavior: "auto", block: "center" });
          continueCentering();
        }
        return;
      }

      const pageLayer = viewerShell.querySelector<HTMLElement>(
        `[data-testid="core__page-layer-${comment.page_number - 1}"]`,
      );
      const container = pageLayer?.closest<HTMLElement>(
        ".rpv-core__inner-pages",
      );
      if (pageLayer && container && pageLayer.getBoundingClientRect().height) {
        const pageRect = pageLayer.getBoundingClientRect();
        const left =
          pageRect.left +
          (hasAnchor ? comment.normalized_x! : 0.03) * pageRect.width;
        const top =
          pageRect.top +
          (hasAnchor ? comment.normalized_y! : 0.03) * pageRect.height;
        const width = hasAnchor
          ? comment.normalized_width! * pageRect.width
          : 2;
        const height = hasAnchor
          ? comment.normalized_height! * pageRect.height
          : 2;
        centerTarget({ height, left, top, width }, container);
        const layout = [
          container.scrollHeight,
          container.clientHeight,
          Math.round(pageRect.top * 10),
          Math.round(pageRect.height * 10),
        ].join(":");
        stableFrames = layout === previousLayout ? stableFrames + 1 : 0;
        previousLayout = layout;
        // PDF.js can render the page canvas before the annotation overlay.
        // Keep the page-level fallback active until the precise target mounts.
        continueCentering();
        return;
      }

      continueCentering();
    };
    animationFrame = window.requestAnimationFrame(centerComment);
    return () => {
      window.cancelAnimationFrame(animationFrame);
    };
  }, [comments, documentLoaded, focusedCommentId]);

  const { CurrentPageInput, GoToNextPage, GoToPreviousPage, NumberOfPages } =
    pageNavigationPluginInstance;
  const {
    CurrentScale,
    ZoomIn: ViewerZoomIn,
    ZoomOut: ViewerZoomOut,
  } = zoomPluginInstance;
  const { ShowSearchPopover } = searchPluginInstance;

  async function handleDocumentLoad({ doc }: DocumentLoadEvent) {
    setDocumentLoaded(true);
    try {
      onOutlineChange(await flattenOutline(doc, await doc.getOutline()));
    } catch {
      onOutlineChange([]);
    }
  }

  function pageTarget(target: EventTarget | null) {
    if (!(target instanceof Element)) return null;
    const element = target.closest<HTMLElement>(
      "[data-testid^='core__page-layer-']",
    );
    const value = element?.dataset.testid?.replace("core__page-layer-", "");
    const pageIndex = value ? Number(value) : Number.NaN;
    return element && Number.isInteger(pageIndex)
      ? { element, pageIndex }
      : null;
  }

  function startAreaSelection(event: PointerEvent<HTMLDivElement>) {
    if (!areaSelectionMode || event.button !== 0) return;
    const target = pageTarget(event.target);
    if (!target) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const rect = target.element.getBoundingClientRect();
    setAreaDrag({
      pageNumber: target.pageIndex + 1,
      rect,
      x: Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)),
      y: Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height)),
    });
  }

  function finishAreaSelection(event: PointerEvent<HTMLDivElement>) {
    if (!areaDrag) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
    const endX = Math.max(
      0,
      Math.min(1, (event.clientX - areaDrag.rect.left) / areaDrag.rect.width),
    );
    const endY = Math.max(
      0,
      Math.min(1, (event.clientY - areaDrag.rect.top) / areaDrag.rect.height),
    );
    const normalizedX = Math.min(areaDrag.x, endX);
    const normalizedY = Math.min(areaDrag.y, endY);
    const normalizedWidth = Math.abs(endX - areaDrag.x);
    const normalizedHeight = Math.abs(endY - areaDrag.y);
    setAreaDrag(null);
    if (normalizedWidth < 0.01 || normalizedHeight < 0.01) return;
    setAreaSelectionMode(false);
    onAreaSelection({
      kind: "rectangle",
      pageNumber: areaDrag.pageNumber,
      normalizedX,
      normalizedY,
      normalizedWidth,
      normalizedHeight,
    });
  }

  const currentPageComments = comments.filter(
    (comment) => comment.page_number === page,
  );
  const leftMarginComments = currentPageComments.filter(
    (_, index) => index % 2 === 0,
  );
  const rightMarginComments = currentPageComments.filter(
    (_, index) => index % 2 === 1,
  );
  function handleViewerPageChange(currentPage: number) {
    const nextPage = currentPage + 1;
    viewerPageRef.current = nextPage;
    onPageChange(nextPage);
  }
  function zoomKeepingViewport(onZoom: () => void) {
    const container = document.querySelector<HTMLElement>(
      ".pdf-viewer-shell .rpv-core__inner-pages",
    );
    if (!container) {
      onZoom();
      return;
    }
    const rect = container.getBoundingClientRect();
    const contentWidth = Math.max(container.scrollWidth, rect.width);
    const contentHeight = Math.max(container.scrollHeight, rect.height);
    const centerX = (container.scrollLeft + rect.width / 2) / contentWidth;
    const centerY = (container.scrollTop + rect.height / 2) / contentHeight;
    onZoom();
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        const nextRect = container.getBoundingClientRect();
        const nextLeft = centerX * container.scrollWidth - nextRect.width / 2;
        const nextTop = centerY * container.scrollHeight - nextRect.height / 2;
        container.scrollTo({
          behavior: "auto",
          left: Math.max(
            0,
            Math.min(nextLeft, container.scrollWidth - nextRect.width),
          ),
          top: Math.max(
            0,
            Math.min(nextTop, container.scrollHeight - nextRect.height),
          ),
        });
      });
    });
  }
  const marginComment = (comment: ReviewComment, index: number) => {
    const top =
      comment.normalized_y === null
        ? Math.min(78, 8 + index * 22)
        : Math.min(78, Math.max(2, comment.normalized_y * 100));
    const isSelected = selectedCommentId === comment.id;
    return (
      <button
        className={`pdf-margin-comment ${comment.priority} ${isSelected ? "selected" : ""}`}
        key={comment.id}
        style={{ top: `${top}%` }}
        type="button"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          setSelectedCommentId((current) =>
            current === comment.id ? null : comment.id,
          );
        }}
      >
        <span className="pdf-margin-comment-head">
          <strong>{comment.author_display_name ?? "审阅人"}</strong>
          <small>{comment.category ?? "审阅"}</small>
        </span>
        {comment.selected_text && (
          <span className="pdf-margin-comment-quote">
            {comment.selected_text}
          </span>
        )}
        <span className="pdf-margin-comment-body">{comment.content}</span>
        <small className="pdf-margin-comment-status">
          {comment.status === "resolved" ? "已解决" : "待处理"}
        </small>
      </button>
    );
  };

  return (
    <section className="reader">
      <div className="reader-tools" aria-label="PDF 阅读工具">
        <GoToPreviousPage>
          {({ isDisabled, onClick }) => (
            <button
              disabled={isDisabled}
              title="上一页"
              type="button"
              onClick={onClick}
            >
              <ChevronLeft size={18} />
            </button>
          )}
        </GoToPreviousPage>
        <span className="page-jump">
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
              <ChevronRight size={18} />
            </button>
          )}
        </GoToNextPage>
        <ViewerZoomOut>
          {({ onClick }) => (
            <button
              title="缩小"
              type="button"
              onClick={() => zoomKeepingViewport(onClick)}
            >
              <ZoomOut size={18} />
            </button>
          )}
        </ViewerZoomOut>
        <CurrentScale>
          {({ scale }) => <span>{Math.round(scale * 100)}%</span>}
        </CurrentScale>
        <ViewerZoomIn>
          {({ onClick }) => (
            <button
              title="放大"
              type="button"
              onClick={() => zoomKeepingViewport(onClick)}
            >
              <ZoomIn size={18} />
            </button>
          )}
        </ViewerZoomIn>
        <ShowSearchPopover>
          {({ onClick }) => (
            <button title="搜索 PDF" type="button" onClick={onClick}>
              <Search size={18} />
            </button>
          )}
        </ShowSearchPopover>
        <button
          className={areaSelectionMode ? "selected-tool" : ""}
          title="框选图片、图表、公式或任意区域"
          type="button"
          onClick={() => setAreaSelectionMode((current) => !current)}
        >
          <Crop size={18} />
        </button>
        <button
          aria-pressed={immersiveMode}
          className={immersiveMode ? "selected-tool" : ""}
          title={immersiveMode ? "退出沉浸模式" : "进入沉浸模式"}
          type="button"
          onClick={onToggleImmersive}
        >
          {immersiveMode ? (
            <PanelLeftOpen size={18} />
          ) : (
            <PanelLeftClose size={18} />
          )}
        </button>
      </div>
      <div className={`pdf-review-stage ${immersiveMode ? "immersive" : ""}`}>
        <aside className="pdf-comment-rail left" aria-label="PDF 左侧批注">
          {leftMarginComments.map(marginComment)}
        </aside>
        <div
          className={`pdf-viewer-shell ${areaSelectionMode ? "area-selection-mode" : ""}`}
          ref={viewerShellRef}
          onPointerDownCapture={startAreaSelection}
          onPointerUpCapture={finishAreaSelection}
        >
          <Worker workerUrl="/api/pdf-worker">
            <Viewer
              defaultScale={SpecialZoomLevel.PageWidth}
              enableSmoothScroll={false}
              fileUrl={fileUrl}
              initialPage={page - 1}
              plugins={[
                pageNavigationPluginInstance,
                zoomPluginInstance,
                searchPluginInstance,
                highlightPluginInstance,
              ]}
              onDocumentLoad={handleDocumentLoad}
              onPageChange={({ currentPage }) =>
                handleViewerPageChange(currentPage)
              }
              renderError={(error) => (
                <div className="reader-error">
                  PDF 加载失败：{error.message || "未知错误"}
                </div>
              )}
            />
          </Worker>
        </div>
        <aside className="pdf-comment-rail right" aria-label="PDF 右侧批注">
          {rightMarginComments.map(marginComment)}
        </aside>
      </div>
    </section>
  );
}
