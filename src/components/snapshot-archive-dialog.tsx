"use client";

import { useEffect, useState } from "react";
import { Check, FilePlus2, RefreshCw, X } from "lucide-react";

type PdfFileOption = {
  id: string;
  label: string;
  fileName: string;
  relativePath: string | null;
  configured: boolean;
};

type CreatedSnapshot = {
  id: string;
  version_number: number;
};

type Props = {
  projectId: string;
  open: boolean;
  onClose: () => void;
  onCreated?: (snapshot: CreatedSnapshot) => void;
};

export function SnapshotArchiveDialog({
  projectId,
  open,
  onClose,
  onCreated,
}: Props) {
  const [options, setOptions] = useState<PdfFileOption[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [manualPath, setManualPath] = useState("");
  const [label, setLabel] = useState("");
  const [note, setNote] = useState("");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    let active = true;
    setOptions([]);
    setSelectedId("");
    setManualPath("");
    setLabel("");
    setNote("");
    setError("");
    setLoading(true);
    fetch(`/api/projects/${projectId}/snapshots?includePdfFiles=1`, {
      cache: "no-store",
    })
      .then(async (response) => {
        const body = (await response.json()) as {
          pdfFiles?: PdfFileOption[];
          error?: { message?: string };
        };
        if (!response.ok)
          throw new Error(body.error?.message ?? "PDF 文件列表加载失败");
        if (!active) return;
        const next = Array.isArray(body.pdfFiles) ? body.pdfFiles : [];
        setOptions(next);
        setSelectedId(
          next.find((item) => item.configured)?.id ?? next[0]?.id ?? "",
        );
      })
      .catch((reason) => {
        if (active)
          setError(
            reason instanceof Error ? reason.message : "PDF 文件列表加载失败",
          );
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [open, projectId]);

  if (!open) return null;

  const manualMode = !selectedId;
  const canSubmit =
    !loading && !saving && (!manualMode || Boolean(manualPath.trim()));

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (manualMode && !manualPath.trim()) {
      setError("请选择一个项目内的 PDF，或填写项目安全目录内的 PDF 路径。");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/projects/${projectId}/snapshots`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          label: label.trim() || undefined,
          note: note.trim() || undefined,
          ...(selectedId
            ? { pdfFileId: selectedId }
            : manualPath.trim()
              ? { pdfPath: manualPath.trim() }
              : {}),
        }),
      });
      const body = (await response.json()) as {
        snapshot?: CreatedSnapshot;
        error?: { message?: string };
      };
      if (!response.ok || !body.snapshot)
        throw new Error(body.error?.message ?? "创建审阅版本失败");
      onCreated?.(body.snapshot);
      onClose();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "创建审阅版本失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="project-edit-backdrop">
      <section
        aria-labelledby="snapshot-archive-dialog-title"
        aria-modal="true"
        className="project-edit-dialog snapshot-archive-dialog"
        role="dialog"
      >
        <header>
          <div>
            <p className="eyebrow">文件与版本</p>
            <h2 id="snapshot-archive-dialog-title">直接归档 PDF</h2>
          </div>
          <button
            aria-label="关闭归档窗口"
            className="icon-button"
            title="关闭"
            type="button"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </header>
        <p className="muted">
          只归档已经生成的 PDF，不会触发 TeX
          编译。选择后的文件会复制为不可变的新版本。
        </p>
        <form onSubmit={(event) => void submit(event)}>
          <label>
            PDF 文件
            {loading ? (
              <span className="snapshot-file-loading">
                <RefreshCw size={15} /> 正在扫描项目中的 PDF...
              </span>
            ) : options.length ? (
              <select
                aria-describedby="snapshot-pdf-help"
                value={selectedId}
                onChange={(event) => {
                  setSelectedId(event.target.value);
                  setManualPath("");
                }}
              >
                {options.map((option) => (
                  <option key={option.id} value={option.id}>
                    {option.label}
                    {option.configured ? "（当前配置）" : ""}
                  </option>
                ))}
                <option value="">手动输入路径（高级）</option>
              </select>
            ) : (
              <span className="snapshot-file-empty">
                项目中暂未扫描到 PDF，请使用下方的手动路径。
              </span>
            )}
          </label>
          {!loading && (!options.length || !selectedId) && (
            <label>
              PDF 路径（高级）
              <input
                value={manualPath}
                placeholder="例如 build/main.pdf"
                onChange={(event) => setManualPath(event.target.value)}
              />
              <small id="snapshot-pdf-help">
                仅填写项目安全目录内的路径；优先使用上面的文件选项，避免输入部署环境路径。
              </small>
            </label>
          )}
          <div className="project-config-grid">
            <label>
              版本别名
              <input
                value={label}
                maxLength={100}
                placeholder="例如：2026-08-03 编译版"
                onChange={(event) => setLabel(event.target.value)}
              />
            </label>
            <label>
              版本备注
              <input
                value={note}
                maxLength={500}
                placeholder="可选，说明这次编译内容"
                onChange={(event) => setNote(event.target.value)}
              />
            </label>
          </div>
          {error && <p className="error">{error}</p>}
          <div className="dialog-actions">
            <button
              className="secondary-button"
              type="button"
              onClick={onClose}
            >
              取消
            </button>
            <button disabled={!canSubmit} type="submit">
              <FilePlus2 size={16} />
              {saving ? "归档中..." : "归档并创建版本"}
            </button>
          </div>
          {!saving && !error && options.length > 0 && selectedId && (
            <p className="snapshot-file-selected">
              <Check size={14} /> 已选择：
              {options.find((item) => item.id === selectedId)?.label}
            </p>
          )}
        </form>
      </section>
    </div>
  );
}
