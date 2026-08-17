"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePreferences } from "@/features/preferences";

type Defaults = {
  userName: string;
  userEmail: string;
  hasAccessToken: boolean;
  hasSshPrivateKey: boolean;
  engine: "pdflatex" | "xelatex" | "lualatex" | "tectonic";
  buildTool: "latexmk" | "tectonic" | "engine";
  outputDirectory: string;
  autoBuild: "off" | "on-save" | "on-start";
  shellEscape: boolean;
  texliveBinPath: string;
};

const emptyDefaults: Defaults = {
  userName: "",
  userEmail: "",
  hasAccessToken: false,
  hasSshPrivateKey: false,
  engine: "pdflatex",
  buildTool: "latexmk",
  outputDirectory: "build",
  autoBuild: "off",
  shellEscape: false,
  texliveBinPath: "",
};

export default function AccountPage() {
  const { locale, setLocale, t } = usePreferences();
  const [defaults, setDefaults] = useState<Defaults>(emptyDefaults);
  const [accessToken, setAccessToken] = useState("");
  const [sshPrivateKey, setSshPrivateKey] = useState("");
  const [clearAccessToken, setClearAccessToken] = useState(false);
  const [clearSshPrivateKey, setClearSshPrivateKey] = useState(false);
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    fetch("/api/me/vscode-defaults")
      .then(async (response) => {
        if (!response.ok) throw new Error(t("account.loadError"));
        setDefaults((await response.json()).defaults);
      })
      .catch((error) =>
        setMessage(
          error instanceof Error
            ? error.message
            : t("account.loadSettingsError"),
        ),
      );
  }, []);

  async function save() {
    setSaving(true);
    setMessage("");
    try {
      const response = await fetch("/api/me/vscode-defaults", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          userName: defaults.userName,
          userEmail: defaults.userEmail,
          accessToken,
          sshPrivateKey,
          clearAccessToken,
          clearSshPrivateKey,
          engine: defaults.engine,
          buildTool: defaults.buildTool,
          outputDirectory: defaults.outputDirectory,
          autoBuild: defaults.autoBuild,
          shellEscape: defaults.shellEscape,
          texliveBinPath: defaults.texliveBinPath,
        }),
      });
      const body = (await response.json()) as {
        defaults?: Defaults;
        error?: { message?: string };
      };
      if (!response.ok || !body.defaults)
        throw new Error(body.error?.message ?? t("account.saveError"));
      setDefaults(body.defaults);
      setAccessToken("");
      setSshPrivateKey("");
      setClearAccessToken(false);
      setClearSshPrivateKey(false);
      setMessage(t("account.saved"));
    } catch (error) {
      setMessage(
        error instanceof Error ? error.message : t("account.saveError"),
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <main className="projects">
      <header>
        <div>
          <p className="eyebrow">REVIEW HUB / {t("account.section")}</p>
          <h1>{t("account.title")}</h1>
          <p className="muted">{t("account.description")}</p>
        </div>
        <div className="page-actions">
          <Link className="toolbar-link" href="/projects">
            {t("account.back")}
          </Link>
        </div>
      </header>
      {message && <p className="notice">{message}</p>}
      <section className="project-config app-preferences-settings">
        <div>
          <h2>{t("preferences.title")}</h2>
          <p className="muted">{t("preferences.description")}</p>
        </div>
        <div className="app-preferences-settings-grid app-preferences-settings-grid--single">
          <label>
            {t("preferences.language")}
            <select
              aria-label={t("preferences.language")}
              value={locale}
              onChange={(event) => {
                const value = event.target.value;
                if (value === "zh-CN" || value === "en-US") setLocale(value);
              }}
            >
              <option value="zh-CN">{t("preferences.chinese")}</option>
              <option value="en-US">{t("preferences.english")}</option>
            </select>
          </label>
        </div>
      </section>
      <section className="project-config">
        <div className="project-config-grid">
          <label>
            {t("account.gitName")}
            <input
              value={defaults.userName}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  userName: event.target.value,
                }))
              }
            />
          </label>
          <label>
            {t("account.gitEmail")}
            <input
              type="email"
              value={defaults.userEmail}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  userEmail: event.target.value,
                }))
              }
            />
          </label>
          <label>
            {t("account.accessToken")}
            <input
              type="password"
              autoComplete="new-password"
              placeholder={
                defaults.hasAccessToken ? t("account.keepToken") : "ghp_..."
              }
              value={accessToken}
              onChange={(event) => setAccessToken(event.target.value)}
            />
          </label>
          {defaults.hasAccessToken && (
            <label className="project-config-checkbox">
              <input
                type="checkbox"
                checked={clearAccessToken}
                onChange={(event) => setClearAccessToken(event.target.checked)}
              />
              {t("account.clearToken")}
            </label>
          )}
          <label className="project-config-wide">
            {t("account.sshKey")}
            <textarea
              rows={6}
              placeholder={
                defaults.hasSshPrivateKey
                  ? t("account.keepSshKey")
                  : "-----BEGIN OPENSSH PRIVATE KEY-----"
              }
              value={sshPrivateKey}
              onChange={(event) => setSshPrivateKey(event.target.value)}
            />
          </label>
          {defaults.hasSshPrivateKey && (
            <label className="project-config-checkbox">
              <input
                type="checkbox"
                checked={clearSshPrivateKey}
                onChange={(event) =>
                  setClearSshPrivateKey(event.target.checked)
                }
              />
              {t("account.clearSshKey")}
            </label>
          )}
          <label>
            {t("account.engine")}
            <select
              value={defaults.engine}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  engine: event.target.value as Defaults["engine"],
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
            {t("account.buildTool")}
            <select
              value={defaults.buildTool}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  buildTool: event.target.value as Defaults["buildTool"],
                }))
              }
            >
              <option value="latexmk">{t("account.buildRecommended")}</option>
              <option value="engine">{t("account.directEngine")}</option>
              <option value="tectonic">tectonic</option>
            </select>
          </label>
          <label>
            {t("account.outputDirectory")}
            <input
              value={defaults.outputDirectory}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  outputDirectory: event.target.value,
                }))
              }
            />
          </label>
          <label>
            {t("account.autoBuild")}
            <select
              value={defaults.autoBuild}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  autoBuild: event.target.value as Defaults["autoBuild"],
                }))
              }
            >
              <option value="off">{t("account.off")}</option>
              <option value="on-save">{t("account.onSave")}</option>
              <option value="on-start">{t("account.onStart")}</option>
            </select>
          </label>
          <label>
            {t("account.compilerPath")}
            <input
              placeholder={t("account.compilerPathHint")}
              type="password"
              value={defaults.texliveBinPath}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  texliveBinPath: event.target.value,
                }))
              }
            />
          </label>
          <label className="project-config-checkbox">
            <input
              type="checkbox"
              checked={defaults.shellEscape}
              onChange={(event) =>
                setDefaults((value) => ({
                  ...value,
                  shellEscape: event.target.checked,
                }))
              }
            />
            {t("account.shellEscape")}
          </label>
        </div>
        <button disabled={saving} type="button" onClick={() => void save()}>
          {saving ? t("account.saving") : t("account.saveSettings")}
        </button>
      </section>
    </main>
  );
}
