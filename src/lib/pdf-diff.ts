import { diffArrays } from "diff";
import { GlobalWorkerOptions, getDocument } from "pdfjs-dist";

export type PdfDiffKind = "added" | "removed";

export type PdfDiffHighlight = {
  id: string;
  kind: PdfDiffKind;
  pageNumber: number;
  normalizedX: number;
  normalizedY: number;
  normalizedWidth: number;
  normalizedHeight: number;
  text: string;
};

export type PdfDiffResult = {
  added: PdfDiffHighlight[];
  removed: PdfDiffHighlight[];
  compositeAdded: PdfDiffHighlight[];
  addedTokenCount: number;
  removedTokenCount: number;
  truncated: boolean;
};

type DiffOperation = {
  type: "equal" | "added" | "removed";
  beforeIndex?: number;
  afterIndex?: number;
};

type DiffToken = {
  key: string;
  text: string;
  pageNumber: number;
  lineKey: string;
  lineY: number;
  bounds: Array<{
    x: number;
    y: number;
    width: number;
    height: number;
  }>;
  lineBreakAfter: boolean;
};

const maxHighlightsPerKind = 3000;

function normalizeToken(value: string) {
  return value
    .replace(/\u00ad/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .normalize("NFKC");
}

export function normalizeComparisonToken(value: string) {
  // A visible hyphen may be emitted either as part of a compound word or as
  // TeX's discretionary line-break marker. Treating those forms alike keeps
  // a reflowed paragraph from becoming a full-document replacement. Spaces,
  // punctuation and case remain significant.
  return normalizeToken(value).replace(/[\u002d\u2010-\u2013\u2212]/gu, "");
}

function clamp(value: number, minimum = 0, maximum = 1) {
  return Math.min(maximum, Math.max(minimum, value));
}

function tokenBounds(
  item: any,
  text: string,
  start: number,
  length: number,
  viewport: { width: number; height: number },
) {
  const transform = Array.isArray(item.transform) ? item.transform : [];
  const x = Number(transform[4]) || 0;
  const baseline = Number(transform[5]) || 0;
  const fontHeight = Math.max(
    0.5,
    Math.hypot(Number(transform[2]) || 0, Number(transform[3]) || 0) ||
      Math.abs(Number(transform[3]) || 0) ||
      8,
  );
  const itemWidth = Math.max(Number(item.width) || 0, fontHeight * 0.15);
  const ratioStart = start / Math.max(1, text.length);
  const ratioEnd = (start + length) / Math.max(1, text.length);
  const rawLeft = x + itemWidth * ratioStart;
  const rawTop = viewport.height - baseline - fontHeight;
  return {
    x: clamp(rawLeft / viewport.width),
    y: clamp(rawTop / viewport.height),
    width: clamp(
      Math.max(0.002, (itemWidth * (ratioEnd - ratioStart)) / viewport.width),
      0,
      1,
    ),
    height: clamp(Math.max(0.004, fontHeight / viewport.height), 0, 1),
  };
}

async function extractTokens(bytes: ArrayBuffer | Uint8Array) {
  GlobalWorkerOptions.workerSrc = "/api/pdf-worker";
  const document = await getDocument({
    data: bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes),
  }).promise;
  const tokens: DiffToken[] = [];
  try {
    for (let pageIndex = 1; pageIndex <= document.numPages; pageIndex += 1) {
      const page = await document.getPage(pageIndex);
      const viewport = page.getViewport({ scale: 1 });
      const content = await page.getTextContent();
      for (const item of content.items as any[]) {
        const text = typeof item.str === "string" ? item.str : "";
        if (!text.trim()) continue;
        const matches = /\S+/g;
        let match: RegExpExecArray | null;
        const itemTokenIndexes: number[] = [];
        const lineKey = normalizeComparisonToken(text);
        while ((match = matches.exec(text))) {
          const normalized = normalizeComparisonToken(match[0]);
          if (!normalized) continue;
          const bounds = tokenBounds(
            item,
            text,
            match.index,
            match[0].length,
            viewport,
          );
          tokens.push({
            key: normalized,
            text: match[0],
            pageNumber: pageIndex,
            lineKey,
            lineY: bounds.y,
            bounds: [bounds],
            lineBreakAfter: false,
          });
          itemTokenIndexes.push(tokens.length - 1);
        }
        if (item.hasEOL && itemTokenIndexes.length > 0)
          tokens[itemTokenIndexes[itemTokenIndexes.length - 1]].lineBreakAfter =
            true;
      }
    }
  } finally {
    await document.destroy();
  }
  return mergeLineBreakTokens(tokens);
}

function isWordContinuation(value: string) {
  return /^[\p{L}\p{N}]/u.test(value);
}

function isLineBreak(left: DiffToken, right: DiffToken) {
  if (left.pageNumber !== right.pageNumber) return false;
  const leftBounds = left.bounds[left.bounds.length - 1];
  const rightBounds = right.bounds[0];
  const geometricBreak =
    rightBounds.y - leftBounds.y >
      Math.max(leftBounds.height, rightBounds.height) * 0.65 ||
    rightBounds.x + 0.02 < leftBounds.x;
  return (
    geometricBreak ||
    (left.lineBreakAfter &&
      rightBounds.y - leftBounds.y >
        Math.max(leftBounds.height, rightBounds.height) * 0.2)
  );
}

/**
 * PDF text extraction preserves a discretionary line-break hyphen in one
 * version and joins the word in another. Merge those fragments before the
 * global diff so reflow does not look like a word replacement. Bounds stay as
 * separate fragments because a single rectangle would cover the entire gap
 * between the two lines.
 */
function mergeLineBreakTokens(tokens: DiffToken[]) {
  const merged: DiffToken[] = [];
  for (const token of tokens) {
    const previous = merged[merged.length - 1];
    if (
      previous &&
      /[-\u2010-\u2013\u2212\u2014]$/u.test(previous.text) &&
      isWordContinuation(token.text) &&
      isLineBreak(previous, token)
    ) {
      const lastCharacter = previous.text[previous.text.length - 1];
      const removesLineBreakMarker = /[-\u2010-\u2013\u2212]$/u.test(
        lastCharacter,
      );
      const text = `${removesLineBreakMarker ? previous.text.slice(0, -1) : previous.text}${token.text}`;
      previous.text = text;
      previous.key = normalizeComparisonToken(text);
      previous.bounds.push(...token.bounds);
      previous.lineBreakAfter = token.lineBreakAfter;
      continue;
    }
    merged.push(token);
  }
  return merged;
}

/**
 * Headers and page numbers are layout chrome rather than manuscript text.
 * When a paragraph grows, page chrome moves with it and would otherwise be
 * reported as a document-wide change. Only remove a top line when the exact
 * same line is repeated on at least two pages of that snapshot.
 */
function filterLayoutTokens(tokens: DiffToken[]) {
  const pagesByLine = new Map<string, Set<number>>();
  for (const token of tokens) {
    if (token.lineY >= 0.08) continue;
    const pages = pagesByLine.get(token.lineKey) ?? new Set<number>();
    pages.add(token.pageNumber);
    pagesByLine.set(token.lineKey, pages);
  }
  const repeatedHeaderLines = new Set(
    [...pagesByLine.entries()]
      .filter(([, pages]) => pages.size >= 2)
      .map(([line]) => line),
  );
  return tokens.filter((token) => {
    if (token.lineY < 0.08 && repeatedHeaderLines.has(token.lineKey))
      return false;
    if (token.lineY > 0.93 && /^\d+$/.test(token.lineKey)) return false;
    return true;
  });
}

/**
 * A library-provided Myers diff keeps the comparison global across page breaks.
 * This is important when an insertion on page one pushes later content onto
 * different pages: matching is based on the text sequence, not page number.
 */
export function diffTokenKeys(
  before: string[],
  after: string[],
): DiffOperation[] {
  const operations: DiffOperation[] = [];
  let beforeIndex = 0;
  let afterIndex = 0;
  const changes = diffArrays(before, after);

  for (let changeIndex = 0; changeIndex < changes.length; changeIndex += 1) {
    const change = changes[changeIndex];
    const following = changes[changeIndex + 1];

    // Keep replacement output compatible with the existing highlighter: show
    // the new text before the removed text while retaining the library's LCS.
    if (change.removed && following?.added) {
      for (let index = 0; index < following.value.length; index += 1) {
        operations.push({ type: "added", afterIndex });
        afterIndex += 1;
      }
      for (let index = 0; index < change.value.length; index += 1) {
        operations.push({ type: "removed", beforeIndex });
        beforeIndex += 1;
      }
      changeIndex += 1;
      continue;
    }

    if (change.added) {
      for (let index = 0; index < change.value.length; index += 1) {
        operations.push({ type: "added", afterIndex });
        afterIndex += 1;
      }
      continue;
    }
    if (change.removed) {
      for (let index = 0; index < change.value.length; index += 1) {
        operations.push({ type: "removed", beforeIndex });
        beforeIndex += 1;
      }
      continue;
    }
    for (let index = 0; index < change.value.length; index += 1) {
      operations.push({ type: "equal", beforeIndex, afterIndex });
      beforeIndex += 1;
      afterIndex += 1;
    }
  }
  return operations;
}

function mergeHighlight(
  highlights: PdfDiffHighlight[],
  token: DiffToken,
  kind: PdfDiffKind,
) {
  for (const bounds of token.bounds) {
    const last = highlights[highlights.length - 1];
    const sameLine =
      last &&
      last.pageNumber === token.pageNumber &&
      Math.abs(last.normalizedY - bounds.y) <=
        Math.max(last.normalizedHeight, bounds.height) * 0.8;
    const gap = last ? bounds.x - (last.normalizedX + last.normalizedWidth) : 0;
    if (sameLine && gap >= -0.01 && gap <= 0.035) {
      const right = Math.max(
        last.normalizedX + last.normalizedWidth,
        bounds.x + bounds.width,
      );
      const bottom = Math.max(
        last.normalizedY + last.normalizedHeight,
        bounds.y + bounds.height,
      );
      last.normalizedX = Math.min(last.normalizedX, bounds.x);
      last.normalizedY = Math.min(last.normalizedY, bounds.y);
      last.normalizedWidth = right - last.normalizedX;
      last.normalizedHeight = bottom - last.normalizedY;
      last.text = `${last.text} ${token.text}`;
      continue;
    }
    highlights.push({
      id: `${kind}-${highlights.length}-${token.pageNumber}-${bounds.x.toFixed(4)}`,
      kind,
      pageNumber: token.pageNumber,
      normalizedX: bounds.x,
      normalizedY: bounds.y,
      normalizedWidth: bounds.width,
      normalizedHeight: bounds.height,
      text: token.text,
    });
  }
}

function nearestBeforeTokenIndex(
  operations: DiffOperation[],
  operationIndex: number,
) {
  for (let index = operationIndex - 1; index >= 0; index -= 1) {
    const operation = operations[index];
    if (operation.type === "equal" && operation.beforeIndex !== undefined)
      return operation.beforeIndex;
  }
  for (let index = operationIndex + 1; index < operations.length; index += 1) {
    const operation = operations[index];
    if (operation.type === "equal" && operation.beforeIndex !== undefined)
      return operation.beforeIndex;
  }
  return undefined;
}

function mapAddedTokenToBefore(
  token: DiffToken,
  anchor: DiffToken | undefined,
): DiffToken {
  if (!anchor) return token;
  const anchorBounds = anchor.bounds[anchor.bounds.length - 1];
  return {
    ...token,
    pageNumber: anchor.pageNumber,
    bounds: [
      {
        x: anchorBounds.x,
        y: anchorBounds.y,
        width: Math.max(anchorBounds.width, token.bounds[0]?.width ?? 0.01),
        height: Math.max(anchorBounds.height, token.bounds[0]?.height ?? 0.01),
      },
    ],
  };
}

function mergeCompositeHighlight(
  highlights: PdfDiffHighlight[],
  token: DiffToken,
  anchor: DiffToken | undefined,
) {
  const mapped = mapAddedTokenToBefore(token, anchor);
  const bounds = mapped.bounds[0];
  const last = highlights[highlights.length - 1];
  const sameAnchor =
    last &&
    last.pageNumber === mapped.pageNumber &&
    Math.abs(last.normalizedX - bounds.x) <= 0.002 &&
    Math.abs(last.normalizedY - bounds.y) <= 0.002;
  if (sameAnchor) {
    last.normalizedWidth = Math.max(last.normalizedWidth, bounds.width);
    last.normalizedHeight = Math.max(last.normalizedHeight, bounds.height);
    last.text = `${last.text} ${token.text}`;
    return;
  }
  mergeHighlight(highlights, mapped, "added");
}

export async function comparePdfBytes(
  beforeBytes: ArrayBuffer | Uint8Array,
  afterBytes: ArrayBuffer | Uint8Array,
): Promise<PdfDiffResult> {
  const [beforeTokens, afterTokens] = await Promise.all([
    extractTokens(beforeBytes),
    extractTokens(afterBytes),
  ]);
  const comparableBeforeTokens = filterLayoutTokens(beforeTokens);
  const comparableAfterTokens = filterLayoutTokens(afterTokens);
  const operations = diffTokenKeys(
    comparableBeforeTokens.map((token) => token.key),
    comparableAfterTokens.map((token) => token.key),
  );
  const added: PdfDiffHighlight[] = [];
  const removed: PdfDiffHighlight[] = [];
  const compositeAdded: PdfDiffHighlight[] = [];
  for (const [operationIndex, operation] of operations.entries()) {
    if (operation.type === "added" && operation.afterIndex !== undefined)
      mergeHighlight(
        added,
        comparableAfterTokens[operation.afterIndex],
        "added",
      );
    if (operation.type === "added" && operation.afterIndex !== undefined) {
      const beforeIndex = nearestBeforeTokenIndex(operations, operationIndex);
      mergeCompositeHighlight(
        compositeAdded,
        comparableAfterTokens[operation.afterIndex],
        beforeIndex === undefined
          ? comparableBeforeTokens[0]
          : comparableBeforeTokens[beforeIndex],
      );
    }
    if (operation.type === "removed" && operation.beforeIndex !== undefined)
      mergeHighlight(
        removed,
        comparableBeforeTokens[operation.beforeIndex],
        "removed",
      );
  }
  const addedTokenCount = operations.filter(
    (item) => item.type === "added",
  ).length;
  const removedTokenCount = operations.filter(
    (item) => item.type === "removed",
  ).length;
  const truncated =
    added.length > maxHighlightsPerKind ||
    removed.length > maxHighlightsPerKind ||
    compositeAdded.length > maxHighlightsPerKind;
  return {
    added: added.slice(0, maxHighlightsPerKind),
    removed: removed.slice(0, maxHighlightsPerKind),
    compositeAdded: compositeAdded.slice(0, maxHighlightsPerKind),
    addedTokenCount,
    removedTokenCount,
    truncated,
  };
}
