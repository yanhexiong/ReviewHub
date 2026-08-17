"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { AdminUpdatePanel } from "@/components/admin-update-panel";

type Account = {
  id: string;
  email: string;
  display_name: string;
  role: string;
  is_active: number;
  project_limit: number | null;
  project_count: number;
  created_at: number;
  updated_at: number;
  last_login_at: number | null;
};
type Metrics = {
  users: number;
  projects: number;
  snapshots: number;
  comments: number;
  activeShares: number;
};
type Project = {
  id: string;
  name: string;
  slug: string;
  owner_email: string;
  snapshot_count: number;
  comment_count: number;
  active_share_count: number;
};
type Share = {
  id: string;
  project_id: string;
  project_name: string;
  version_number: number;
  snapshot_label: string | null;
  permission: "view" | "comment";
  status: string;
  created_at: number;
  last_used_at: number | null;
};
type AuditEvent = {
  id: string;
  project_id: string | null;
  entity_type: string;
  entity_id: string;
  action: string;
  level: "info" | "warning" | "error" | "critical";
  after_json: string | null;
  created_at: number;
  actor_name: string | null;
  actor_email: string | null;
};
type AuditSettings = {
  retentionDays: number;
  maxEntryBytes: number;
};
type ResourceSettings = {
  maxPdfBytes: number;
  maxImportBytes: number;
  maxProjectsPerUser: number;
  maxUsers: number;
};
type ListenerSettings = {
  host: string;
  port: number;
};
type LogFilters = {
  from: string;
  to: string;
  level: string;
  entityType: string;
  action: string;
  search: string;
};
type OperationsInfo = {
  dataDirectoryExists: boolean;
  databaseBytes: number;
  sqliteVersion: { version: string };
  schemaVersion: number;
  latestMigration: string | null;
  migrationSummary: {
    authority: "go";
    version: number;
  };
  migrations: { version: number; status: "applied" }[];
  backups: { file: string; bytes: number; modifiedAt: string }[];
  maxPdfBytes: number;
  maxImportBytes: number;
  maxProjectsPerUser: number;
  maxUsers: number;
  nodeVersion: string;
  platform: string;
};
type TexLiveProfile = {
  id: string;
  name: string;
  engine: "pdflatex" | "xelatex" | "lualatex" | "tectonic";
  buildTool: "latexmk" | "tectonic" | "engine";
  outputDirectory: string;
  shellEscape: boolean;
  texliveBinConfigured: boolean;
  createdAt: number;
  updatedAt: number;
};
type TexLiveProfileDraft = {
  id: string;
  name: string;
  engine: TexLiveProfile["engine"];
  buildTool: TexLiveProfile["buildTool"];
  outputDirectory: string;
  shellEscape: boolean;
  texliveBinPath: string;
};
type AdminSection =
  | "platform"
  | "policies"
  | "texlive"
  | "accounts"
  | "projects"
  | "logs"
  | "updates"
  | "operations";
const adminSections: Array<{ id: AdminSection; label: string }> = [
  { id: "platform", label: "平台设置" },
  { id: "policies", label: "资源策略" },
  { id: "texlive", label: "TeX Live 配置" },
  { id: "accounts", label: "账户管理" },
  { id: "projects", label: "项目与分享" },
  { id: "logs", label: "审计日志" },
  { id: "updates", label: "应用更新" },
  { id: "operations", label: "数据运维" },
];

const formatTime = (value: number | null) =>
  value ? new Date(value).toLocaleString("zh-CN") : "从未";
const formatBytes = (value: number) => {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024)
    return `${(value / 1024 / 1024).toFixed(1)} MB`;
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} GB`;
};
const formatMegabytes = (value: number) =>
  Math.max(1, Math.round(value / 1024 / 1024));
const levelLabels = {
  info: "INFO",
  warning: "WARNING",
  error: "ERROR",
  critical: "CRITICAL",
} as const;
const emptyLogFilters: LogFilters = {
  from: "",
  to: "",
  level: "",
  entityType: "",
  action: "",
  search: "",
};
const emptyTexLiveProfile: TexLiveProfileDraft = {
  id: "",
  name: "",
  engine: "pdflatex",
  buildTool: "latexmk",
  outputDirectory: "build",
  shellEscape: false,
  texliveBinPath: "",
};

export default function AdminPage() {
  const [activeSection, setActiveSection] = useState<AdminSection>("platform");
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [users, setUsers] = useState<Account[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [shares, setShares] = useState<Share[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [rootsText, setRootsText] = useState("");
  const [rootsCount, setRootsCount] = useState(0);
  const [rootsVisible, setRootsVisible] = useState(false);
  const [registrationOpen, setRegistrationOpen] = useState(false);
  const [listenerSettings, setListenerSettings] = useState<ListenerSettings>({
    host: "0.0.0.0",
    port: 3000,
  });
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [rootsMessage, setRootsMessage] = useState("");
  const [loading, setLoading] = useState(true);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [operations, setOperations] = useState<OperationsInfo | null>(null);
  const [resourceSettings, setResourceSettings] =
    useState<ResourceSettings | null>(null);
  const [auditTotal, setAuditTotal] = useState(0);
  const [auditSettings, setAuditSettings] = useState<AuditSettings | null>(
    null,
  );
  const [logFilters, setLogFilters] = useState<LogFilters>(emptyLogFilters);
  const [logEntityTypes, setLogEntityTypes] = useState<string[]>([]);
  const [logPage, setLogPage] = useState(0);
  const [logLoading, setLogLoading] = useState(false);
  const [accountQuery, setAccountQuery] = useState("");
  const [accountRoleFilter, setAccountRoleFilter] = useState("");
  const [accountStatusFilter, setAccountStatusFilter] = useState("");
  const [texLiveProfiles, setTexLiveProfiles] = useState<TexLiveProfile[]>([]);
  const [texLiveProfileForm, setTexLiveProfileForm] =
    useState<TexLiveProfileDraft>(emptyTexLiveProfile);

  function logQuery(filters: LogFilters, page: number) {
    const query = new URLSearchParams({
      limit: "50",
      offset: String(page * 50),
    });
    Object.entries(filters).forEach(([key, value]) => {
      if (value) query.set(key, value);
    });
    return query.toString();
  }

  async function loadLogs(filters = logFilters, page = logPage) {
    setLogLoading(true);
    try {
      const response = await fetch(
        `/api/admin/logs?${logQuery(filters, page)}`,
      );
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "日志加载失败");
        return;
      }
      setAudit(body.audit ?? []);
      setAuditTotal(body.total ?? 0);
      setAuditSettings(body.settings ?? null);
      setLogEntityTypes(body.entityTypes ?? []);
      setLogPage(page);
    } catch {
      setError("日志加载失败，请检查服务连接。");
    } finally {
      setLogLoading(false);
    }
  }

  async function load() {
    setLoading(true);
    try {
      const [
        adminResponse,
        operationsResponse,
        logsResponse,
        profilesResponse,
      ] = await Promise.all([
        fetch("/api/admin"),
        fetch("/api/admin/operations"),
        fetch(`/api/admin/logs?${logQuery(logFilters, 0)}`),
        fetch("/api/admin/texlive-profiles"),
      ]);
      const body = await adminResponse.json();
      const operationsBody = await operationsResponse.json();
      const logsBody = await logsResponse.json();
      if (!adminResponse.ok || !operationsResponse.ok || !logsResponse.ok) {
        setError(
          body.error?.message ??
            operationsBody.error?.message ??
            logsBody.error?.message ??
            "无法加载网站后台",
        );
        return;
      }
      setMetrics(body.metrics);
      setUsers(body.users ?? []);
      setProjects(body.projects ?? []);
      setResourceSettings(body.settings ?? null);
      setAudit(logsBody.audit ?? []);
      setAuditTotal(logsBody.total ?? 0);
      setAuditSettings(logsBody.settings ?? null);
      setLogEntityTypes(logsBody.entityTypes ?? []);
      setRegistrationOpen(body.registrationOpen === true);
      if (body.listener) setListenerSettings(body.listener as ListenerSettings);
      setShares(body.shares ?? []);
      setRootsText("");
      setRootsCount(Number(body.configuredRootCount ?? 0));
      setOperations(operationsBody);
      if (profilesResponse.ok) {
        const profilesBody = (await profilesResponse.json()) as {
          profiles?: TexLiveProfile[];
        };
        setTexLiveProfiles(profilesBody.profiles ?? []);
      }
    } catch {
      setError("无法加载网站后台，请检查本地服务连接。");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function toggleRegistration() {
    setError("");
    if (!resourceSettings) {
      setError("资源策略尚未加载，请先刷新后台。");
      return;
    }
    setPendingAction("registration");
    try {
      const response = await fetch("/api/admin/settings", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...resourceSettings,
          registrationOpen: !registrationOpen,
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "注册开关保存失败");
        return;
      }
      setRegistrationOpen(body.registrationOpen);
      setMessage(body.registrationOpen ? "注册已开放。" : "注册已关闭。 ");
    } finally {
      setPendingAction(null);
    }
  }

  async function saveResourceSettings(form: FormData) {
    const maxPdfMb = Number(form.get("maxPdfMb"));
    const maxImportMb = Number(form.get("maxImportMb"));
    const maxProjectsPerUser = Number(form.get("maxProjectsPerUser"));
    const maxUsers = Number(form.get("maxUsers"));
    if (
      !resourceSettings ||
      !Number.isInteger(maxPdfMb) ||
      !Number.isInteger(maxImportMb) ||
      !Number.isInteger(maxProjectsPerUser) ||
      !Number.isInteger(maxUsers)
    ) {
      setError("资源限制必须填写整数。");
      return;
    }
    setPendingAction("resource-settings");
    try {
      const response = await fetch("/api/admin/settings", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          registrationOpen,
          maxPdfBytes: maxPdfMb * 1024 * 1024,
          maxImportBytes: maxImportMb * 1024 * 1024,
          maxProjectsPerUser,
          maxUsers,
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "资源限制保存失败");
        return;
      }
      setResourceSettings({
        maxPdfBytes: body.maxPdfBytes,
        maxImportBytes: body.maxImportBytes,
        maxProjectsPerUser: body.maxProjectsPerUser,
        maxUsers: body.maxUsers,
      });
      setMessage("资源限制已保存，后续上传和新建项目会立即使用新策略。");
    } finally {
      setPendingAction(null);
    }
  }

  async function saveListenerSettings(form: FormData) {
    setError("");
    setPendingAction("listener-settings");
    try {
      const response = await fetch("/api/admin/listener", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          host: form.get("host"),
          port: form.get("port"),
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "服务监听配置保存失败");
        return;
      }
      setListenerSettings(body.listener);
      setMessage("服务监听配置已保存，重启服务后生效。");
    } finally {
      setPendingAction(null);
    }
  }

  async function addAdmin(form: FormData) {
    setError("");
    setPendingAction("admin");
    try {
      const response = await fetch("/api/admin", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email: form.get("email"),
          password: form.get("password"),
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "管理员创建失败");
        return;
      }
      setUsers((current) => [...current, body.user]);
      setMessage("新管理员已创建。");
    } finally {
      setPendingAction(null);
    }
  }

  async function createAccount(form: FormData) {
    setError("");
    const projectLimitValue = String(form.get("projectLimit") ?? "").trim();
    const projectLimit = projectLimitValue ? Number(projectLimitValue) : null;
    if (projectLimit !== null && !Number.isInteger(projectLimit)) {
      setError("项目配额必须是整数；留空表示继承全局策略。");
      return;
    }
    setPendingAction("account-create");
    try {
      const response = await fetch("/api/admin", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email: form.get("email"),
          displayName: form.get("displayName"),
          password: form.get("password"),
          role: form.get("role"),
          isActive: form.get("isActive") === "on",
          projectLimit,
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "账户创建失败");
        return;
      }
      setUsers((current) => [body.user, ...current]);
      setMessage("账户创建成功。");
    } finally {
      setPendingAction(null);
    }
  }

  async function saveAccount(account: Account, form: FormData) {
    const projectLimitValue = String(form.get("projectLimit") ?? "").trim();
    const projectLimit = projectLimitValue ? Number(projectLimitValue) : null;
    if (projectLimit !== null && !Number.isInteger(projectLimit)) {
      setError("项目配额必须是整数，留空表示继承全局策略。");
      return;
    }
    setPendingAction(`user:${account.id}`);
    try {
      const response = await fetch(`/api/admin/users/${account.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          displayName: form.get("displayName"),
          role: form.get("role"),
          isActive: form.get("isActive") === "on",
          projectLimit,
          newPassword: String(form.get("newPassword") ?? ""),
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "账户保存失败");
        return;
      }
      setUsers((current) =>
        current.map((item) => (item.id === account.id ? body.user : item)),
      );
      setMessage(`账户 ${account.email} 已更新。`);
    } finally {
      setPendingAction(null);
    }
  }

  async function saveRoots(form: FormData) {
    setRootsMessage("");
    const nextRoots = String(form.get("roots") ?? "")
      .split("\n")
      .map((root) => root.trim())
      .filter(Boolean);
    if (!nextRoots.length) {
      setRootsMessage("请至少填写一个安全目录。");
      return;
    }
    setPendingAction("roots");
    try {
      const response = await fetch("/api/settings/allowed-roots", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ roots: nextRoots }),
      });
      const body = await response.json();
      if (!response.ok) {
        setRootsMessage(body.error?.message ?? "安全目录保存失败");
        return;
      }
      setRootsCount(Number(body.configuredCount ?? nextRoots.length));
      setRootsMessage("安全目录已保存。");
    } finally {
      setPendingAction(null);
    }
  }

  async function saveTexLiveProfile(form: FormData) {
    setError("");
    setPendingAction("texlive-profile");
    const id = texLiveProfileForm.id;
    try {
      const response = await fetch(
        id
          ? `/api/admin/texlive-profiles/${id}`
          : "/api/admin/texlive-profiles",
        {
          method: id ? "PATCH" : "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            name: form.get("name"),
            engine: form.get("engine"),
            buildTool: form.get("buildTool"),
            outputDirectory: form.get("outputDirectory"),
            shellEscape: form.get("shellEscape") === "on",
            texliveBinPath: form.get("texliveBinPath"),
          }),
        },
      );
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "TeX Live 配置保存失败");
        return;
      }
      const profile = body.profile as TexLiveProfile;
      setTexLiveProfiles((current) =>
        id
          ? current.map((item) => (item.id === id ? profile : item))
          : [...current, profile],
      );
      setTexLiveProfileForm(emptyTexLiveProfile);
      setMessage(id ? "TeX Live 配置已更新。" : "TeX Live 配置已创建。 ");
    } finally {
      setPendingAction(null);
    }
  }

  async function deleteTexLiveProfile(profile: TexLiveProfile) {
    if (
      !window.confirm(
        `删除“${profile.name}”后，使用它的项目会回退到已保存的手动设置。继续吗？`,
      )
    )
      return;
    setPendingAction(`texlive-delete:${profile.id}`);
    try {
      const response = await fetch(
        `/api/admin/texlive-profiles/${profile.id}`,
        {
          method: "DELETE",
        },
      );
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "TeX Live 配置删除失败");
        return;
      }
      setTexLiveProfiles((current) =>
        current.filter((item) => item.id !== profile.id),
      );
      if (texLiveProfileForm.id === profile.id)
        setTexLiveProfileForm(emptyTexLiveProfile);
      setMessage("TeX Live 配置已删除。 ");
    } finally {
      setPendingAction(null);
    }
  }

  async function toggleRootsVisibility() {
    if (rootsVisible) {
      setRootsVisible(false);
      setRootsText("");
      return;
    }
    setPendingAction("roots-load");
    try {
      const response = await fetch("/api/settings/allowed-roots?reveal=1");
      const body = await response.json();
      if (!response.ok)
        throw new Error(body.error?.message ?? "安全目录读取失败");
      setRootsText((body.roots ?? []).join("\n"));
      setRootsCount(Number(body.configuredCount ?? body.roots?.length ?? 0));
      setRootsVisible(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "安全目录读取失败");
    } finally {
      setPendingAction(null);
    }
  }

  async function revokeShare(share: Share) {
    if (!window.confirm("撤销后该分享 URL 会立即失效，继续吗？")) return;
    setPendingAction(`revoke:${share.id}`);
    try {
      const response = await fetch(
        `/api/projects/${share.project_id}/shares/${share.id}`,
        { method: "DELETE" },
      );
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "分享撤销失败");
        return;
      }
      setShares((current) =>
        current.map((item) =>
          item.id === share.id ? { ...item, status: "revoked" } : item,
        ),
      );
      setMessage("分享 URL 已撤销。");
    } finally {
      setPendingAction(null);
    }
  }

  async function restoreBackup(form: FormData) {
    const file = form.get("backup");
    if (!(file instanceof File) || !file.size) {
      setError("请选择迁移包 ZIP 文件。");
      return;
    }
    if (
      !window.confirm(
        "恢复会替换当前数据库、PDF 快照和受管工作区，恢复前系统会自动保存一份备份。确定继续吗？",
      )
    )
      return;
    setError("");
    setPendingAction("restore");
    try {
      const payload = new FormData();
      payload.set("backup", file);
      const response = await fetch("/api/admin/restore", {
        method: "POST",
        body: payload,
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "迁移包恢复失败");
        return;
      }
      setMessage(
        `恢复完成。恢复前备份：${body.preRestoreBackup ?? "已保存"}。`,
      );
      await load();
    } catch {
      setError("迁移包恢复失败，请检查服务日志。");
    } finally {
      setPendingAction(null);
    }
  }

  async function applyLogFilters(form: FormData) {
    const nextFilters: LogFilters = {
      from: String(form.get("from") ?? ""),
      to: String(form.get("to") ?? ""),
      level: String(form.get("level") ?? ""),
      entityType: String(form.get("entityType") ?? ""),
      action: String(form.get("action") ?? ""),
      search: String(form.get("search") ?? ""),
    };
    setLogFilters(nextFilters);
    await loadLogs(nextFilters, 0);
  }

  async function saveLogSettings(form: FormData) {
    setPendingAction("log-settings");
    try {
      const response = await fetch("/api/admin/logs", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          retentionDays: Number(form.get("retentionDays")),
          maxEntryBytes: Number(form.get("maxEntryBytes")),
        }),
      });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "日志策略保存失败");
        return;
      }
      setAuditSettings(body.settings);
      setMessage(`日志策略已保存，清理了 ${body.purged ?? 0} 条过期记录。`);
      await loadLogs(logFilters, 0);
    } finally {
      setPendingAction(null);
    }
  }

  async function purgeLogs() {
    if (!window.confirm("只会删除超过保留期限的日志，确定立即清理吗？")) return;
    setPendingAction("log-purge");
    try {
      const response = await fetch("/api/admin/logs/purge", { method: "POST" });
      const body = await response.json();
      if (!response.ok) {
        setError(body.error?.message ?? "日志清理失败");
        return;
      }
      setMessage(`已清理 ${body.purged ?? 0} 条过期日志。`);
      await loadLogs(logFilters, 0);
    } finally {
      setPendingAction(null);
    }
  }

  const visibleUsers = users.filter((user) => {
    const query = accountQuery.trim().toLowerCase();
    const matchesQuery =
      !query ||
      user.email.toLowerCase().includes(query) ||
      user.display_name.toLowerCase().includes(query);
    const matchesRole = !accountRoleFilter || user.role === accountRoleFilter;
    const matchesStatus =
      !accountStatusFilter ||
      (accountStatusFilter === "active"
        ? user.is_active === 1
        : user.is_active === 0);
    return matchesQuery && matchesRole && matchesStatus;
  });

  return (
    <main className="projects admin-console">
      <header>
        <div>
          <p className="eyebrow">REVIEW HUB / ADMIN</p>
          <h1>网站后台</h1>
        </div>
        <div className="admin-header-actions">
          <Link className="toolbar-link" href="/projects">
            项目工作区
          </Link>
          <button disabled={loading} type="button" onClick={() => void load()}>
            {loading ? "正在读取..." : "刷新数据"}
          </button>
        </div>
      </header>
      <nav className="admin-nav" aria-label="后台模块" role="tablist">
        {adminSections.map((section) => (
          <button
            aria-controls={"admin-panel-" + section.id}
            aria-selected={activeSection === section.id}
            className={activeSection === section.id ? "active" : ""}
            id={"admin-tab-" + section.id}
            key={section.id}
            role="tab"
            type="button"
            onClick={() => setActiveSection(section.id)}
          >
            {section.label}
          </button>
        ))}
      </nav>
      {error && <p className="error">{error}</p>}
      {message && <p className="notice">{message}</p>}
      <section className="admin-dashboard-grid" aria-label="平台概览">
        {[
          ["账户", metrics?.users ?? "—"],
          ["项目", metrics?.projects ?? "—"],
          ["PDF 快照", metrics?.snapshots ?? "—"],
          ["有效批注", metrics?.comments ?? "—"],
          ["有效分享", metrics?.activeShares ?? "—"],
        ].map(([label, value]) => (
          <div className="admin-metric" key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
          </div>
        ))}
      </section>
      {activeSection === "platform" && (
        <div
          id="admin-panel-platform"
          className="admin-panel"
          role="tabpanel"
          aria-labelledby="admin-tab-platform"
        >
          <div className="admin-columns">
            <section className="admin-section">
              <h2>平台设置</h2>
              <div className="admin-setting-row">
                <div>
                  <strong>开放注册</strong>
                  <p className="muted">关闭后，新用户无法从注册页创建账户。</p>
                </div>
                <button
                  disabled={pendingAction !== null}
                  type="button"
                  onClick={toggleRegistration}
                >
                  {pendingAction === "registration"
                    ? "正在保存..."
                    : registrationOpen
                      ? "关闭注册"
                      : "开放注册"}
                </button>
              </div>
              <strong className="admin-state">
                当前状态：{registrationOpen ? "开放" : "关闭"}
              </strong>
              <form
                className="admin-form listener-settings-form"
                action={saveListenerSettings}
              >
                <div>
                  <strong>服务监听</strong>
                  <p className="muted">
                    默认监听 0.0.0.0:3000。保存后重启服务生效；显式设置的 HOST
                    或 PORT 环境变量优先于此处配置。
                  </p>
                </div>
                <div className="listener-settings-grid">
                  <label>
                    监听地址
                    <input
                      name="host"
                      required
                      value={listenerSettings.host}
                      onChange={(event) =>
                        setListenerSettings((current) => ({
                          ...current,
                          host: event.target.value,
                        }))
                      }
                    />
                  </label>
                  <label>
                    端口
                    <input
                      name="port"
                      type="number"
                      min={1}
                      max={65535}
                      required
                      value={listenerSettings.port}
                      onChange={(event) =>
                        setListenerSettings((current) => ({
                          ...current,
                          port: Number(event.target.value),
                        }))
                      }
                    />
                  </label>
                </div>
                <button disabled={pendingAction !== null} type="submit">
                  {pendingAction === "listener-settings"
                    ? "正在保存..."
                    : "保存监听配置"}
                </button>
              </form>
              <div className="admin-form admin-roots-form">
                <div className="admin-setting-row">
                  <div>
                    <strong>PDF 安全目录</strong>
                    <p className="muted">
                      已配置 {rootsCount} 个安全目录； 具体位置默认隐藏。
                    </p>
                  </div>
                  <button
                    type="button"
                    disabled={pendingAction !== null}
                    onClick={() => void toggleRootsVisibility()}
                  >
                    {rootsVisible ? "隐藏目录" : "显示并编辑"}
                  </button>
                </div>
                {rootsVisible && (
                  <form action={saveRoots}>
                    <label>
                      安全目录配置
                      <textarea
                        name="roots"
                        rows={4}
                        value={rootsText}
                        onChange={(event) => setRootsText(event.target.value)}
                      />
                      <small className="muted">
                        每行一个目录，项目位置必须位于其中。完成后再次点击“隐藏目录”。
                      </small>
                    </label>
                    {rootsMessage && <p className="muted">{rootsMessage}</p>}
                    <button disabled={pendingAction !== null} type="submit">
                      {pendingAction === "roots"
                        ? "正在保存..."
                        : "保存安全目录"}
                    </button>
                  </form>
                )}
              </div>
            </section>
            <section className="admin-section">
              <h2>新增管理员</h2>
              <form className="admin-form" action={addAdmin}>
                <label>
                  邮箱
                  <input name="email" type="email" required />
                </label>
                <label>
                  初始密码
                  <input
                    name="password"
                    type="password"
                    minLength={12}
                    required
                  />
                  <small className="muted">至少 12 个字符。</small>
                </label>
                <button disabled={pendingAction !== null} type="submit">
                  {pendingAction === "admin" ? "正在创建..." : "创建管理员"}
                </button>
              </form>
            </section>
          </div>
        </div>
      )}
      {activeSection === "policies" && (
        <section
          id="admin-panel-policies"
          className="admin-section resource-policy-section"
          role="tabpanel"
          aria-labelledby="admin-tab-policies"
        >
          <div className="section-heading-row">
            <div>
              <h2>资源与配额策略</h2>
              <p className="muted">
                这些限制由服务端强制执行，单位使用 MB；项目配额填 0 表示不限额。
                留空用户配额时继承这里的全局值。
              </p>
            </div>
            <span className="policy-badge">实时生效</span>
          </div>
          <form className="resource-policy-form" action={saveResourceSettings}>
            <label>
              单个 PDF 上限（MB）
              <input
                name="maxPdfMb"
                type="number"
                min={1}
                max={4096}
                required
                defaultValue={
                  resourceSettings
                    ? formatMegabytes(resourceSettings.maxPdfBytes)
                    : ""
                }
                key={`max-pdf-${resourceSettings?.maxPdfBytes ?? "empty"}`}
              />
            </label>
            <label>
              导入/备份 ZIP 上限（MB）
              <input
                name="maxImportMb"
                type="number"
                min={1}
                max={8192}
                required
                defaultValue={
                  resourceSettings
                    ? formatMegabytes(resourceSettings.maxImportBytes)
                    : ""
                }
                key={`max-import-${resourceSettings?.maxImportBytes ?? "empty"}`}
              />
            </label>
            <label>
              每个账户项目数上限
              <input
                name="maxProjectsPerUser"
                type="number"
                min={0}
                max={100000}
                required
                defaultValue={resourceSettings?.maxProjectsPerUser ?? ""}
                key={`max-projects-${resourceSettings?.maxProjectsPerUser ?? "empty"}`}
              />
            </label>
            <label>
              账户总数上限
              <input
                name="maxUsers"
                type="number"
                min={1}
                max={1000000}
                required
                defaultValue={resourceSettings?.maxUsers ?? ""}
                key={`max-users-${resourceSettings?.maxUsers ?? "empty"}`}
              />
            </label>
            <div className="resource-policy-actions">
              <button disabled={pendingAction !== null} type="submit">
                {pendingAction === "resource-settings"
                  ? "保存中..."
                  : "保存资源策略"}
              </button>
              {operations && (
                <small className="muted">
                  当前生效：PDF {formatBytes(operations.maxPdfBytes)} · ZIP{" "}
                  {formatBytes(operations.maxImportBytes)} · 每账户{" "}
                  {operations.maxProjectsPerUser || "不限"} 个项目 ·{" "}
                  {operations.maxUsers} 个账户
                </small>
              )}
            </div>
          </form>
        </section>
      )}
      {activeSection === "texlive" && (
        <section
          id="admin-panel-texlive"
          className="admin-section texlive-admin-section"
          role="tabpanel"
          aria-labelledby="admin-tab-texlive"
        >
          <div className="section-heading-row">
            <div>
              <h2>TeX Live 编译配置</h2>
              <p className="muted">
                发布可复用的编译器配置，用户在导入项目或项目编辑器中可以直接选择。
                编译器目录只在服务端保存，不会显示给普通用户。
              </p>
            </div>
            <span className="policy-badge">管理员管理</span>
          </div>
          <form className="texlive-profile-form" action={saveTexLiveProfile}>
            <input type="hidden" name="id" value={texLiveProfileForm.id} />
            <label>
              配置名称
              <input
                name="name"
                required
                maxLength={80}
                placeholder="例如：MiKTeX ICLR"
                value={texLiveProfileForm.name}
                onChange={(event) =>
                  setTexLiveProfileForm((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              编译引擎
              <select
                name="engine"
                value={texLiveProfileForm.engine}
                onChange={(event) =>
                  setTexLiveProfileForm((current) => ({
                    ...current,
                    engine: event.target.value as TexLiveProfileDraft["engine"],
                  }))
                }
              >
                <option value="pdflatex">pdfLaTeX</option>
                <option value="xelatex">XeLaTeX</option>
                <option value="lualatex">LuaLaTeX</option>
                <option value="tectonic">Tectonic</option>
              </select>
            </label>
            <label>
              构建工具
              <select
                name="buildTool"
                value={texLiveProfileForm.buildTool}
                onChange={(event) =>
                  setTexLiveProfileForm((current) => ({
                    ...current,
                    buildTool: event.target
                      .value as TexLiveProfileDraft["buildTool"],
                  }))
                }
              >
                <option value="latexmk">latexmk</option>
                <option value="engine">直接调用引擎</option>
                <option value="tectonic">Tectonic</option>
              </select>
            </label>
            <label>
              输出目录
              <input
                name="outputDirectory"
                required
                maxLength={160}
                placeholder="build"
                value={texLiveProfileForm.outputDirectory}
                onChange={(event) =>
                  setTexLiveProfileForm((current) => ({
                    ...current,
                    outputDirectory: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              TeX Live bin 目录
              <input
                name="texliveBinPath"
                maxLength={500}
                type="password"
                placeholder={
                  texLiveProfileForm.id &&
                  texLiveProfiles.some(
                    (profile) =>
                      profile.id === texLiveProfileForm.id &&
                      profile.texliveBinConfigured,
                  )
                    ? "已配置，留空保持不变"
                    : "留空使用服务默认 PATH"
                }
                value={texLiveProfileForm.texliveBinPath}
                onChange={(event) =>
                  setTexLiveProfileForm((current) => ({
                    ...current,
                    texliveBinPath: event.target.value,
                  }))
                }
              />
            </label>
            <label className="admin-checkbox-row">
              <input
                name="shellEscape"
                type="checkbox"
                checked={texLiveProfileForm.shellEscape}
                onChange={(event) =>
                  setTexLiveProfileForm((current) => ({
                    ...current,
                    shellEscape: event.target.checked,
                  }))
                }
              />
              允许 shell escape
            </label>
            <div className="texlive-profile-actions">
              <button disabled={pendingAction !== null} type="submit">
                {pendingAction === "texlive-profile"
                  ? "保存中..."
                  : texLiveProfileForm.id
                    ? "更新配置"
                    : "新增配置"}
              </button>
              {texLiveProfileForm.id && (
                <button
                  type="button"
                  onClick={() => setTexLiveProfileForm(emptyTexLiveProfile)}
                >
                  取消编辑
                </button>
              )}
            </div>
          </form>
          <div className="texlive-profile-list">
            <h3>已发布配置</h3>
            {!texLiveProfiles.length && (
              <p className="muted">
                还没有配置。先创建一个供项目选择的编译环境。
              </p>
            )}
            {texLiveProfiles.map((profile) => (
              <article className="texlive-profile-row" key={profile.id}>
                <div>
                  <strong>{profile.name}</strong>
                  <small>
                    {profile.engine} · {profile.buildTool} · 输出{" "}
                    {profile.outputDirectory}
                    {profile.shellEscape ? " · shell escape" : ""}
                  </small>
                  <small>
                    编译器目录：
                    {profile.texliveBinConfigured
                      ? "已配置"
                      : "使用服务默认 PATH"}
                  </small>
                </div>
                <div className="texlive-profile-row-actions">
                  <button
                    type="button"
                    onClick={() =>
                      setTexLiveProfileForm({
                        id: profile.id,
                        name: profile.name,
                        engine: profile.engine,
                        buildTool: profile.buildTool,
                        outputDirectory: profile.outputDirectory,
                        shellEscape: profile.shellEscape,
                        texliveBinPath: "",
                      })
                    }
                  >
                    编辑
                  </button>
                  <button
                    disabled={pendingAction !== null}
                    type="button"
                    onClick={() => void deleteTexLiveProfile(profile)}
                  >
                    {pendingAction === `texlive-delete:${profile.id}`
                      ? "删除中..."
                      : "删除"}
                  </button>
                </div>
              </article>
            ))}
          </div>
        </section>
      )}
      {activeSection === "accounts" && (
        <section
          id="admin-panel-accounts"
          className="admin-section"
          role="tabpanel"
          aria-labelledby="admin-tab-accounts"
        >
          <h2>账户管理</h2>
          <p className="muted">
            管理员可以调整角色、停用账户、重置密码和设置单独的项目配额。停用只会阻止登录，
            不会删除该用户已有的项目或审阅记录。
          </p>
          <details className="account-create-panel">
            <summary>创建账户</summary>
            <form className="account-create-form" action={createAccount}>
              <label>
                邮箱
                <input
                  name="email"
                  type="email"
                  required
                  autoComplete="email"
                />
              </label>
              <label>
                显示名称
                <input name="displayName" maxLength={80} required />
              </label>
              <label>
                初始密码
                <input
                  name="password"
                  type="password"
                  minLength={12}
                  required
                  autoComplete="new-password"
                />
                <small className="muted">至少 12 个字符。</small>
              </label>
              <label>
                角色
                <select name="role" defaultValue="reviewer">
                  <option value="admin">管理员</option>
                  <option value="author">作者</option>
                  <option value="reviewer">审阅人</option>
                  <option value="viewer">只读用户</option>
                </select>
              </label>
              <label>
                项目配额
                <input
                  name="projectLimit"
                  type="number"
                  min={0}
                  max={100000}
                  placeholder="继承全局策略"
                />
              </label>
              <label className="admin-checkbox-row">
                <input name="isActive" type="checkbox" defaultChecked />
                允许登录
              </label>
              <div className="account-create-actions">
                <button disabled={pendingAction !== null} type="submit">
                  {pendingAction === "account-create"
                    ? "正在创建..."
                    : "创建账户"}
                </button>
              </div>
            </form>
          </details>
          <div className="admin-list-toolbar">
            <input
              value={accountQuery}
              onChange={(event) => setAccountQuery(event.target.value)}
              placeholder="搜索邮箱或显示名称"
              aria-label="搜索账户"
            />
            <select
              value={accountRoleFilter}
              onChange={(event) => setAccountRoleFilter(event.target.value)}
              aria-label="按角色筛选账户"
            >
              <option value="">全部角色</option>
              <option value="admin">管理员</option>
              <option value="author">作者</option>
              <option value="reviewer">审阅人</option>
              <option value="viewer">只读用户</option>
            </select>
            <select
              value={accountStatusFilter}
              onChange={(event) => setAccountStatusFilter(event.target.value)}
              aria-label="按状态筛选账户"
            >
              <option value="">全部状态</option>
              <option value="active">已启用</option>
              <option value="inactive">已停用</option>
            </select>
            <span className="muted">
              显示 {visibleUsers.length} / {users.length}
            </span>
          </div>
          <div className="account-list">
            {visibleUsers.map((user) => (
              <form
                className="account-row account-management-row"
                key={user.id}
                action={(form) => void saveAccount(user, form)}
              >
                <div className="account-identity">
                  <strong>{user.email}</strong>
                  <small>
                    创建于 {formatTime(user.created_at)} · 最后登录：
                    {formatTime(user.last_login_at)}
                  </small>
                </div>
                <label>
                  显示名称
                  <input
                    name="displayName"
                    defaultValue={user.display_name}
                    required
                  />
                </label>
                <label>
                  角色
                  <select name="role" defaultValue={user.role}>
                    <option value="admin">管理员</option>
                    <option value="author">作者</option>
                    <option value="reviewer">审阅人</option>
                    <option value="viewer">只读用户</option>
                  </select>
                </label>
                <label>
                  项目配额
                  <input
                    name="projectLimit"
                    type="number"
                    min={0}
                    max={100000}
                    placeholder="继承全局"
                    defaultValue={user.project_limit ?? ""}
                  />
                </label>
                <div className="account-usage">
                  <strong>{user.project_count}</strong>
                  <small>
                    个项目 /{" "}
                    {user.project_limit ??
                      resourceSettings?.maxProjectsPerUser ??
                      "—"}
                  </small>
                </div>
                <label className="account-active-toggle">
                  <input
                    name="isActive"
                    type="checkbox"
                    defaultChecked={user.is_active === 1}
                  />
                  启用登录
                </label>
                <label>
                  新密码（可选）
                  <input
                    name="newPassword"
                    type="password"
                    minLength={12}
                    placeholder="留空不修改"
                    autoComplete="new-password"
                  />
                </label>
                <button disabled={pendingAction !== null} type="submit">
                  {pendingAction === `user:${user.id}`
                    ? "保存中..."
                    : "保存账户"}
                </button>
              </form>
            ))}
            {!visibleUsers.length && (
              <p className="muted">
                {users.length ? "没有符合筛选条件的账户。" : "暂无本地账户。"}
              </p>
            )}
          </div>
        </section>
      )}
      {activeSection === "projects" && (
        <section
          id="admin-panel-projects"
          className="admin-section"
          role="tabpanel"
          aria-labelledby="admin-tab-projects"
        >
          <h2>项目与分享</h2>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>项目</th>
                  <th>所有者</th>
                  <th>快照</th>
                  <th>批注</th>
                  <th>有效分享</th>
                </tr>
              </thead>
              <tbody>
                {projects.map((project) => (
                  <tr key={project.id}>
                    <td>
                      <Link href={`/projects/${project.id}`}>
                        {project.name}
                      </Link>
                      <small>{project.slug}</small>
                    </td>
                    <td>{project.owner_email}</td>
                    <td>{project.snapshot_count}</td>
                    <td>{project.comment_count}</td>
                    <td>{project.active_share_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="admin-share-list">
            {shares.map((share) => (
              <div className="admin-share-row" key={share.id}>
                <div>
                  <strong>
                    {share.project_name} ·{" "}
                    {share.snapshot_label || `版本 ${share.version_number}`}
                  </strong>
                  <small>
                    {share.permission === "comment" ? "可评论" : "仅查看"} ·
                    创建于 {formatTime(share.created_at)} · 最后访问{" "}
                    {formatTime(share.last_used_at)}
                  </small>
                </div>
                <button
                  disabled={share.status !== "active" || pendingAction !== null}
                  type="button"
                  onClick={() => void revokeShare(share)}
                >
                  {pendingAction === `revoke:${share.id}`
                    ? "正在撤销..."
                    : share.status === "active"
                      ? "撤销分享"
                      : "已撤销"}
                </button>
              </div>
            ))}
            {!shares.length && <p className="muted">暂无分享记录。</p>}
          </div>
        </section>
      )}
      {activeSection === "logs" && (
        <section
          id="admin-panel-logs"
          className="admin-section audit-section"
          role="tabpanel"
          aria-labelledby="admin-tab-logs"
        >
          <div className="audit-heading">
            <div>
              <h2>审计日志</h2>
              <p className="muted">
                可按时间、等级、实体类别和动作筛选；日志详情受条目大小限制。
              </p>
            </div>
            <span className="audit-total">匹配 {auditTotal} 条</span>
          </div>
          <form className="log-filter-grid" action={applyLogFilters}>
            <label>
              开始时间
              <input
                name="from"
                type="datetime-local"
                value={logFilters.from}
                onChange={(event) =>
                  setLogFilters((current) => ({
                    ...current,
                    from: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              结束时间
              <input
                name="to"
                type="datetime-local"
                value={logFilters.to}
                onChange={(event) =>
                  setLogFilters((current) => ({
                    ...current,
                    to: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              日志等级
              <select
                name="level"
                value={logFilters.level}
                onChange={(event) =>
                  setLogFilters((current) => ({
                    ...current,
                    level: event.target.value,
                  }))
                }
              >
                <option value="">全部等级</option>
                <option value="info">INFO</option>
                <option value="warning">WARNING</option>
                <option value="error">ERROR</option>
                <option value="critical">CRITICAL</option>
              </select>
            </label>
            <label>
              实体类别
              <select
                name="entityType"
                value={logFilters.entityType}
                onChange={(event) =>
                  setLogFilters((current) => ({
                    ...current,
                    entityType: event.target.value,
                  }))
                }
              >
                <option value="">全部类别</option>
                {logEntityTypes.map((entityType) => (
                  <option key={entityType} value={entityType}>
                    {entityType}
                  </option>
                ))}
              </select>
            </label>
            <label>
              动作
              <input
                name="action"
                value={logFilters.action}
                onChange={(event) =>
                  setLogFilters((current) => ({
                    ...current,
                    action: event.target.value,
                  }))
                }
                placeholder="例如 deleted"
              />
            </label>
            <label>
              搜索
              <input
                name="search"
                value={logFilters.search}
                onChange={(event) =>
                  setLogFilters((current) => ({
                    ...current,
                    search: event.target.value,
                  }))
                }
                placeholder="实体 ID、操作者或详情"
              />
            </label>
            <div className="log-filter-actions">
              <button disabled={logLoading} type="submit">
                {logLoading ? "查询中..." : "筛选日志"}
              </button>
              <button
                type="button"
                onClick={() => {
                  setLogFilters(emptyLogFilters);
                  void loadLogs(emptyLogFilters, 0);
                }}
              >
                清除筛选
              </button>
              <a
                className="admin-inline-action"
                href={
                  "/api/admin/logs?" + logQuery(logFilters, 0) + "&format=csv"
                }
                download
              >
                导出 CSV
              </a>
              <a
                className="admin-inline-action"
                href={
                  "/api/admin/logs?" + logQuery(logFilters, 0) + "&format=json"
                }
                download
              >
                导出 JSON
              </a>
            </div>
          </form>
          <div className="audit-list">
            {audit.map((event) => (
              <article className="audit-row" key={event.id}>
                <time>{formatTime(event.created_at)}</time>
                <div>
                  <strong>
                    <span className={`log-level ${event.level}`}>
                      {levelLabels[event.level] ?? event.level}
                    </span>{" "}
                    {event.action}
                  </strong>
                  <small>
                    {event.entity_type} / {event.entity_id.slice(0, 12)} ·{" "}
                    {event.actor_name || event.actor_email || "系统"}
                  </small>
                </div>
                {event.after_json && (
                  <details>
                    <summary>详情</summary>
                    <code>{event.after_json}</code>
                  </details>
                )}
              </article>
            ))}
            {!audit.length && <p className="muted">没有匹配的日志。</p>}
          </div>
          <div className="audit-pagination">
            <button
              disabled={logPage === 0 || logLoading}
              type="button"
              onClick={() => void loadLogs(logFilters, logPage - 1)}
            >
              上一页
            </button>
            <span>第 {logPage + 1} 页</span>
            <button
              disabled={(logPage + 1) * 50 >= auditTotal || logLoading}
              type="button"
              onClick={() => void loadLogs(logFilters, logPage + 1)}
            >
              下一页
            </button>
          </div>
          <div className="audit-policy">
            <div>
              <h3>日志保留策略</h3>
              <p className="muted">
                超过保留期限的日志会在后台写入日志时自动清理，也可手动立即清理。
              </p>
            </div>
            <form className="audit-policy-form" action={saveLogSettings}>
              <label>
                保留天数
                <input
                  name="retentionDays"
                  type="number"
                  min={1}
                  max={3650}
                  defaultValue={auditSettings?.retentionDays ?? 365}
                  key={`retention-${auditSettings?.retentionDays ?? 365}`}
                  required
                />
              </label>
              <label>
                单条上限（字节）
                <input
                  name="maxEntryBytes"
                  type="number"
                  min={256}
                  max={1048576}
                  defaultValue={auditSettings?.maxEntryBytes ?? 16384}
                  key={`entry-${auditSettings?.maxEntryBytes ?? 16384}`}
                  required
                />
              </label>
              <button disabled={pendingAction !== null} type="submit">
                {pendingAction === "log-settings" ? "保存中..." : "保存策略"}
              </button>
              <button
                disabled={pendingAction !== null}
                type="button"
                onClick={() => void purgeLogs()}
              >
                {pendingAction === "log-purge"
                  ? "清理中..."
                  : "立即清理过期日志"}
              </button>
            </form>
          </div>
        </section>
      )}
      {activeSection === "updates" && <AdminUpdatePanel />}
      {activeSection === "operations" && (
        <section
          id="admin-panel-operations"
          className="admin-section operations-section"
          role="tabpanel"
          aria-labelledby="admin-tab-operations"
        >
          <div className="operations-heading">
            <div>
              <h2>网站运维与数据迁移</h2>
              <p className="muted">
                备份包含 SQLite 数据库、PDF
                快照、受管工作区和导出文件。恢复会覆盖当前数据，
                并自动生成恢复前备份。
              </p>
            </div>
            <a className="toolbar-link" href="/api/admin/backup">
              下载完整备份 ZIP
            </a>
          </div>
          {operations && (
            <div className="operations-grid">
              <div>
                <span>数据库</span>
                <strong>{formatBytes(operations.databaseBytes)}</strong>
                <small>应用数据库已配置</small>
              </div>
              <div>
                <span>SQLite / Schema</span>
                <strong>
                  {operations.sqliteVersion.version} / v
                  {operations.schemaVersion}
                </strong>
                <small>
                  Go 数据库迁移：{operations.latestMigration ?? "未记录"}
                </small>
              </div>
              <div>
                <span>上传限制</span>
                <strong>
                  PDF {formatBytes(operations.maxPdfBytes)} · 迁移包{" "}
                  {formatBytes(operations.maxImportBytes)}
                </strong>
                <small>运行时：{operations.nodeVersion}</small>
              </div>
              <div>
                <span>数据存储</span>
                <strong>
                  {operations.dataDirectoryExists ? "正常" : "缺失"}
                </strong>
                <small>存储位置已隐藏</small>
              </div>
            </div>
          )}
          <div className="operations-columns">
            <div>
              <h3>Go 数据库迁移状态</h3>
              {operations?.migrationSummary && (
                <div className="operations-list">
                  <div>
                    <strong>
                      Go schema v{operations.migrationSummary.version}
                    </strong>
                    <small>由 Go API 启动时校验并补齐，当前状态：已应用</small>
                  </div>
                </div>
              )}
              <p className="muted">
                数据库 schema 由 Go API
                统一维护，服务启动时会校验当前版本并补齐必要结构。
              </p>
            </div>
            <div>
              <h3>恢复迁移包</h3>
              <form className="admin-form" action={restoreBackup}>
                <label>
                  备份或迁移 ZIP
                  <input
                    name="backup"
                    type="file"
                    accept=".zip,application/zip"
                    required
                  />
                </label>
                <button disabled={pendingAction !== null} type="submit">
                  {pendingAction === "restore" ? "正在恢复..." : "上传并恢复"}
                </button>
              </form>
              <p className="muted">
                仅接受 Review Hub 自己生成的 ZIP；不会执行 Git
                命令，也不会写入论文源仓库。
              </p>
            </div>
          </div>
          <h3>最近备份</h3>
          <div className="operations-list">
            {(operations?.backups ?? []).map((backup) => (
              <div key={backup.file}>
                <strong>{backup.file}</strong>
                <small>
                  {formatBytes(backup.bytes)} ·{" "}
                  {new Date(backup.modifiedAt).toLocaleString("zh-CN")}
                </small>
              </div>
            ))}
            {!operations?.backups.length && (
              <p className="muted">暂无恢复前备份。</p>
            )}
          </div>
        </section>
      )}
    </main>
  );
}
