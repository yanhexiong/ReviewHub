"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { Pencil, Plus, Save, Share2, Trash2, X } from "lucide-react";
import { usePreferences } from "@/features/preferences";
import { ProjectShareDialog } from "@/components/project-share-dialog";
type Project = {
  id: string;
  name: string;
  description: string | null;
  slug: string;
  source_type: "local" | "archive" | "github";
  access?: {
    collaborator_permission: "owner" | "review" | "manage";
    can_manage: boolean;
    is_owner: boolean;
  };
};
export default function Projects() {
  const { t } = usePreferences();
  const [projects, setProjects] = useState<Project[]>([]);
  const [error, setError] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [editingProjectId, setEditingProjectId] = useState<string | null>(null);
  const [projectMessage, setProjectMessage] = useState("");
  const [projectPending, setProjectPending] = useState<string | null>(null);
  const [shareProject, setShareProject] = useState<Project | null>(null);
  useEffect(() => {
    fetch("/api/projects").then(async (r) =>
      r.ok
        ? setProjects((await r.json()).projects)
        : setError(t("projects.loadError")),
    );
    fetch("/api/auth/me").then(async (response) => {
      if (response.ok)
        setIsAdmin((await response.json()).user?.role === "admin");
    });
  }, []);
  async function saveProject(projectId: string, form: FormData) {
    setProjectMessage("");
    setProjectPending(projectId);
    try {
      const response = await fetch(`/api/projects/${projectId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: form.get("name"),
          slug: form.get("slug"),
          description: form.get("description"),
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setProjectMessage(body.error?.message ?? t("projects.saveError"));
        return;
      }
      setProjects((current) =>
        current.map((project) =>
          project.id === projectId ? { ...project, ...body.project } : project,
        ),
      );
      setEditingProjectId(null);
      setProjectMessage(t("projects.saved"));
    } catch {
      setProjectMessage(t("projects.saveConnectionError"));
    } finally {
      setProjectPending(null);
    }
  }
  async function deleteProject(project: Project) {
    if (!window.confirm(t("projects.deleteConfirm", { name: project.name })))
      return;
    setProjectMessage("");
    setProjectPending(project.id);
    try {
      const response = await fetch(`/api/projects/${project.id}`, {
        method: "DELETE",
      });
      const body = await response.json();
      if (!response.ok) {
        setProjectMessage(body.error?.message ?? t("projects.deleteError"));
        return;
      }
      setProjects((current) =>
        current.filter((item) => item.id !== project.id),
      );
      setProjectMessage(t("projects.deleted", { name: project.name }));
    } catch {
      setProjectMessage(t("projects.deleteConnectionError"));
    } finally {
      setProjectPending(null);
    }
  }
  return (
    <main className="projects">
      <header>
        <div>
          <p className="eyebrow">REVIEW HUB</p>
          <h1>{t("projects.title")}</h1>
        </div>
        <div className="page-actions">
          <button
            className="new-project-button"
            onClick={() => location.assign("/projects/new")}
          >
            <Plus size={16} aria-hidden="true" /> {t("projects.new")}
          </button>
          <Link className="toolbar-link" href="/account">
            {t("projects.account")}
          </Link>
          {isAdmin && (
            <Link className="toolbar-link" href="/admin">
              {t("projects.admin")}
            </Link>
          )}
        </div>
      </header>
      {error && <p className="error">{error}</p>}
      {projectMessage && <p className="notice">{projectMessage}</p>}
      <section className="project-list">
        {projects.length ? (
          projects.map((p) => (
            <article className="project-row" key={p.id}>
              <Link className="project-row-main" href={`/projects/${p.id}`}>
                <strong>{p.name}</strong>
                <span>{p.description || t("projects.noDescription")}</span>
                <code>{p.slug}</code>
              </Link>
              <div className="project-row-actions">
                {p.access?.can_manage && (
                  <button
                    disabled={projectPending !== null}
                    type="button"
                    onClick={() => setEditingProjectId(p.id)}
                  >
                    <Pencil size={14} /> {t("projects.edit")}
                  </button>
                )}
                {p.access?.can_manage && (
                  <button
                    disabled={projectPending !== null}
                    type="button"
                    onClick={() => setShareProject(p)}
                    title={t("projects.shareTitle", { name: p.name })}
                  >
                    <Share2 size={14} /> {t("projects.share")}
                  </button>
                )}
                {p.access?.is_owner && (
                  <button
                    className="danger-button"
                    disabled={projectPending !== null}
                    type="button"
                    onClick={() => void deleteProject(p)}
                  >
                    <Trash2 size={14} /> {t("projects.delete")}
                  </button>
                )}
                {p.access?.collaborator_permission !== "owner" && (
                  <span className="permission-badge">
                    {p.access?.collaborator_permission === "manage"
                      ? t("projects.manageCollaborator")
                      : t("projects.reviewCollaborator")}
                  </span>
                )}
              </div>
              {editingProjectId === p.id && (
                <form
                  className="project-row-editor"
                  action={(form) => void saveProject(p.id, form)}
                >
                  <label>
                    {t("projects.name")}
                    <input name="name" defaultValue={p.name} required />
                  </label>
                  <label>
                    {t("projects.slug")}
                    <input
                      name="slug"
                      defaultValue={p.slug}
                      pattern="[a-z0-9-]+"
                      required
                    />
                  </label>
                  <label className="project-row-editor-wide">
                    {t("projects.note")}
                    <textarea
                      name="description"
                      defaultValue={p.description ?? ""}
                      rows={2}
                    />
                  </label>
                  <div className="project-row-editor-actions">
                    <button disabled={projectPending !== null} type="submit">
                      <Save size={14} />
                      {projectPending === p.id
                        ? t("projects.saving")
                        : t("projects.save")}
                    </button>
                    <button
                      type="button"
                      onClick={() => setEditingProjectId(null)}
                    >
                      <X size={14} /> {t("projects.cancel")}
                    </button>
                  </div>
                </form>
              )}
            </article>
          ))
        ) : (
          <div className="empty">
            <h2>{t("projects.emptyTitle")}</h2>
            <p>{t("projects.emptyDescription")}</p>
          </div>
        )}
      </section>
      {shareProject && (
        <ProjectShareDialog
          projectId={shareProject.id}
          projectName={shareProject.name}
          onClose={() => setShareProject(null)}
        />
      )}
    </main>
  );
}
