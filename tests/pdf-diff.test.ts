import { describe, expect, it } from "vitest";
import { diffTokenKeys, normalizeComparisonToken } from "../src/lib/pdf-diff";

function describeDiff(before: string[], after: string[]) {
  return diffTokenKeys(before, after).map((operation) => ({
    after:
      operation.afterIndex === undefined
        ? undefined
        : after[operation.afterIndex],
    before:
      operation.beforeIndex === undefined
        ? undefined
        : before[operation.beforeIndex],
    type: operation.type,
  }));
}

describe("PDF 全文文字序列比对", () => {
  it("归一化换行断词而不忽略空格、大小写和标点", () => {
    expect(normalizeComparisonToken("near-synonymous")).toBe(
      normalizeComparisonToken("nearsynonymous"),
    );
    expect(normalizeComparisonToken("soft\u00adhyphen")).toBe(
      normalizeComparisonToken("softhyphen"),
    );
    expect(normalizeComparisonToken("A B")).not.toBe(
      normalizeComparisonToken("AB"),
    );
    expect(normalizeComparisonToken("Word")).not.toBe(
      normalizeComparisonToken("word"),
    );
  });

  it("前文插入不会把后续所有文字判为新增", () => {
    expect(
      describeDiff(
        ["introduction", "method", "result"],
        ["introduction", "new", "method", "result"],
      ),
    ).toEqual([
      { after: "introduction", before: "introduction", type: "equal" },
      { after: "new", before: undefined, type: "added" },
      { after: "method", before: "method", type: "equal" },
      { after: "result", before: "result", type: "equal" },
    ]);
  });

  it("识别中间删除", () => {
    expect(describeDiff(["a", "removed", "b"], ["a", "b"])).toEqual([
      { after: "a", before: "a", type: "equal" },
      { after: undefined, before: "removed", type: "removed" },
      { after: "b", before: "b", type: "equal" },
    ]);
  });

  it("替换内容同时产生删除和新增", () => {
    expect(describeDiff(["a", "old", "c"], ["a", "new", "c"])).toEqual([
      { after: "a", before: "a", type: "equal" },
      { after: "new", before: undefined, type: "added" },
      { after: undefined, before: "old", type: "removed" },
      { after: "c", before: "c", type: "equal" },
    ]);
  });

  it("处理相同与空序列", () => {
    expect(describeDiff(["a", "b"], ["a", "b"])).toEqual([
      { after: "a", before: "a", type: "equal" },
      { after: "b", before: "b", type: "equal" },
    ]);
    expect(describeDiff([], ["new"])).toEqual([
      { after: "new", before: undefined, type: "added" },
    ]);
    expect(describeDiff(["old"], [])).toEqual([
      { after: undefined, before: "old", type: "removed" },
    ]);
  });

  it("保留重复 token 的前后序列和下标", () => {
    const before = ["a", "same", "old", "same", "z"];
    const after = ["a", "same", "new", "same", "z", "tail"];
    const operations = diffTokenKeys(before, after);

    expect(
      operations
        .filter((operation) => operation.type !== "removed")
        .map((operation) => after[operation.afterIndex!]),
    ).toEqual(after);
    expect(
      operations
        .filter((operation) => operation.type !== "added")
        .map((operation) => before[operation.beforeIndex!]),
    ).toEqual(before);
  });
});
