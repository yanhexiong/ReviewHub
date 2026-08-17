"use client";

import { useState } from "react";
import { FileUp, GitCommitHorizontal, X } from "lucide-react";

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

export function ExternalPdfDialog({
  projectId,
  open,
  onClose,
  onCreated,
}: Props) {
  const [file, setFile] = useState<File | null>(null);
  const [label, setLabel] = useState("");
  const [note, setNote] = useState("");
  const [gitAssociation, setGitAssociation] = useState<"none" | "manual">(
    "none",
  );
  const [gitBranch, setGitBranch] = useState("");
  const [gitCommitSha, setGitCommitSha] = useState("");
  const [gitCommitMessage, setGitCommitMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  function reset() {
    setFile(null);
    setLabel("");
    setNote("");
    setGitAssociation("none");
    setGitBranch("");
    setGitCommitSha("");
    setGitCommitMessage("");
    setError("");
  }

  function close() {
    if (saving) return;
    reset();
    onClose();
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file) {
      setError("请选择 PDF 文件。");
      return;
    }
    if (gitAssociation === "manual" && !gitCommitSha.trim()) {
      setError("手动关联 Git 时必须填写提交 SHA。");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const form = new FormData();
      form.set("pdf", file);
      form.set("label", label.trim());
      form.set("note", note.trim());
      form.set("gitAssociation", gitAssociation);
      if (gitAssociation === "manual") {
        form.set("gitBranch", gitBranch.trim());
        form.set("gitCommitSha", gitCommitSha.trim());
        form.set("gitCommitMessage", gitCommitMessage.trim());
      }
      const response = await fetch(`/api/projects/${projectId}/snapshots`, {
        method: "POST",
        body: form,
      });
      const body = (await response.json()) as {
        snapshot?: CreatedSnapshot;
        error?: { message?: string };
      };
      if (!response.ok || !body.snapshot)
        throw new Error(body.error?.message ?? "添加外部 PDF 失败");
      onCreated?.(body.snapshot);
      reset();
      onClose();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "添加外部 PDF 失败");
    } finally {
      setSaving(false);
    }
  }

  if (!open) return null;

  return (
    <div className="project-edit-backdrop">
      <section
        aria-labelledby="external-pdf-dialog-title"
        aria-modal="true"
        className="project-edit-dialog snapshot-archive-dialog external-pdf-dialog"
        role="dialog"
      >
        <header>
          <div>
            <p className="eyebrow">独立审阅文件</p>
            <h2 id="external-pdf-dialog-title">添加外部 PDF</h2>
          </div>
          <button
            aria-label="关闭添加外部 PDF 窗口"
            className="icon-button"
            disabled={saving}
            title="关闭"
            type="button"
            onClick={close}
          >
            <X size={18} />
          </button>
        </header>
        <p className="muted">
          上传的 PDF
          会直接保存为新的不可变审阅版本，不读取项目仓库，也不会修改或提交
          Git。默认不关联 Git；如需保留来源，可选择手动填写一个提交版本。
        </p>
        <form onSubmit={(event) => void submit(event)}>
          <label className="external-pdf-file-picker">
            PDF 文件
            <input
              accept="application/pdf,.pdf"
              type="file"
              onChange={(event) => {
                setFile(event.target.files?.[0] ?? null);
                setError("");
              }}
            />
            {file ? (
              <span>
                {file.name} · {(file.size / 1024 / 1024).toFixed(1)} MB
              </span>
            ) : (
              <small>仅接受 PDF 文件，大小受管理员资源策略限制。</small>
            )}
          </label>
          <div className="project-config-grid">
            <label>
              版本别名
              <input
                maxLength={100}
                placeholder="例如：外部终稿"
                value={label}
                onChange={(event) => setLabel(event.target.value)}
              />
            </label>
            <label>
              版本备注
              <input
                maxLength={500}
                placeholder="可选，说明 PDF 来源"
                value={note}
                onChange={(event) => setNote(event.target.value)}
              />
            </label>
          </div>
          <fieldset className="external-git-association">
            <legend>
              <GitCommitHorizontal size={15} /> Git 来源（可选）
            </legend>
            <div className="source-mode-tabs" role="tablist">
              <button
                aria-selected={gitAssociation === "none"}
                className={gitAssociation === "none" ? "selected" : ""}
                role="tab"
                type="button"
                onClick={() => setGitAssociation("none")}
              >
                不关联 Git
              </button>
              <button
                aria-selected={gitAssociation === "manual"}
                className={gitAssociation === "manual" ? "selected" : ""}
                role="tab"
                type="button"
                onClick={() => setGitAssociation("manual")}
              >
                手动关联版本
              </button>
            </div>
            {gitAssociation === "manual" && (
              <div className="project-config-grid external-git-fields">
                <label>
                  提交 SHA
                  <input
                    required
                    inputMode="text"
                    pattern="[0-9a-fA-F]{7,64}"
                    placeholder="例如 a1b2c3d"
                    value={gitCommitSha}
                    onChange={(event) => setGitCommitSha(event.target.value)}
                  />
                </label>
                <label>
                  分支（可选）
                  <input
                    placeholder="例如 main"
                    value={gitBranch}
                    onChange={(event) => setGitBranch(event.target.value)}
                  />
                </label>
                <label className="project-config-wide">
                  提交说明（可选）
                  <input
                    maxLength={500}
                    placeholder="例如 camera-ready 版本"
                    value={gitCommitMessage}
                    onChange={(event) =>
                      setGitCommitMessage(event.target.value)
                    }
                  />
                </label>
                <small className="muted project-config-wide">
                  这里只保存来源说明，不会验证远程仓库，也不会自动切换编辑器源码。
                </small>
              </div>
            )}
          </fieldset>
          {error && <p className="error">{error}</p>}
          <div className="dialog-actions">
            <button className="secondary-button" type="button" onClick={close}>
              取消
            </button>
            <button disabled={saving || !file} type="submit">
              <FileUp size={16} />
              {saving ? "上传并归档中..." : "上传并创建版本"}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
