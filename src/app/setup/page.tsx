"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { usePreferences } from "@/features/preferences";

export default function SetupPage() {
  const { t } = usePreferences();
  const [error, setError] = useState("");
  const [databaseStatus, setDatabaseStatus] = useState<
    "loading" | "ready" | "error"
  >("loading");

  useEffect(() => {
    fetch("/api/system/health", { cache: "no-store" })
      .then((response) => {
        setDatabaseStatus(response.ok ? "ready" : "error");
      })
      .catch(() => setDatabaseStatus("error"));
  }, []);

  async function submit(form: FormData) {
    setError("");
    if (form.get("password") !== form.get("confirmPassword")) {
      setError("两次输入的密码不一致");
      return;
    }
    const response = await fetch("/api/auth/setup", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: form.get("email"),
        displayName: form.get("displayName"),
        password: form.get("password"),
        confirmPassword: form.get("confirmPassword"),
        listener: {
          host: form.get("listenHost"),
          port: form.get("listenPort"),
        },
      }),
    });
    const body = await response.json();
    if (!response.ok) {
      setError(body.error?.message ?? "管理员创建失败");
      return;
    }
    location.assign("/projects");
  }

  return (
    <main className="auth">
      <form action={submit}>
        <p className="eyebrow">REVIEW HUB / {t("auth.setupSection")}</p>
        <h1>{t("auth.setupTitle")}</h1>
        <p className="muted">{t("auth.setupDescription")}</p>
        <p className={databaseStatus === "error" ? "error" : "muted"}>
          {t("auth.database")}：
          {databaseStatus === "loading"
            ? t("auth.checking")
            : databaseStatus === "ready"
              ? t("auth.ready")
              : t("auth.unavailable")}
        </p>
        <label>
          {t("auth.displayName")}
          <input name="displayName" required autoComplete="name" />
        </label>
        <label>
          {t("auth.email")}
          <input name="email" type="email" required autoComplete="email" />
        </label>
        <label>
          {t("auth.password")}
          <input
            name="password"
            type="password"
            minLength={12}
            required
            autoComplete="new-password"
          />
          <small className="muted">{t("auth.passwordHint")}</small>
        </label>
        <label>
          {t("auth.confirmPassword")}
          <input
            name="confirmPassword"
            type="password"
            minLength={12}
            required
            autoComplete="new-password"
          />
        </label>
        <fieldset className="setup-listener-fields">
          <legend>{t("auth.listenerTitle")}</legend>
          <label>
            {t("auth.listenHost")}
            <input
              name="listenHost"
              defaultValue="0.0.0.0"
              required
              autoComplete="off"
            />
          </label>
          <label>
            {t("auth.listenPort")}
            <input
              name="listenPort"
              type="number"
              min={1}
              max={65535}
              defaultValue={3000}
              required
            />
          </label>
          <small className="muted">{t("auth.listenRestartHint")}</small>
        </fieldset>
        {error && <p className="error">{error}</p>}
        <button type="submit">{t("auth.createAdminSubmit")}</button>
        <p className="muted">
          {t("auth.alreadyAccount")}{" "}
          <Link href="/login">{t("auth.backToLogin")}</Link>
        </p>
      </form>
    </main>
  );
}
