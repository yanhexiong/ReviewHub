"use client";
import Link from "next/link";
import { Languages } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { usePreferences } from "@/features/preferences";
export default function LoginPage() {
  const { locale, setLocale, t } = usePreferences();
  const [email, setEmail] = useState("");
  const [error, setError] = useState("");
  const [formReady, setFormReady] = useState(false);
  const [setupRequired, setSetupRequired] = useState(false);
  useEffect(() => {
    setFormReady(true);
    fetch("/api/auth/setup/status")
      .then((response) => response.json())
      .then((body) => setSetupRequired(body.setupRequired === true))
      .catch(() => setSetupRequired(false));
  }, []);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    const submittedEmail = String(form.get("email") ?? "").trim();
    const res = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: submittedEmail,
        password: form.get("password"),
        rememberMe: form.get("rememberMe") === "on",
      }),
    });
    if (!res.ok) {
      const passwordInput = formElement.elements.namedItem("password");
      if (passwordInput instanceof HTMLInputElement) passwordInput.value = "";
      return setError((await res.json()).error.message);
    }
    location.assign("/projects");
  }
  return (
    <main className="auth">
      <form onSubmit={(event) => void submit(event)}>
        <div className="auth-heading">
          <div>
            <p className="eyebrow">REVIEW HUB / {t("auth.loginSection")}</p>
            <h1>{t("auth.loginTitle")}</h1>
          </div>
          <button
            aria-label={`${t("preferences.language")}: ${
              locale === "zh-CN"
                ? t("preferences.english")
                : t("preferences.chinese")
            }`}
            className="auth-language-toggle"
            title={t("preferences.language")}
            type="button"
            onClick={() => setLocale(locale === "zh-CN" ? "en-US" : "zh-CN")}
          >
            <Languages size={16} aria-hidden="true" />
            <span>
              {locale === "zh-CN"
                ? t("preferences.english")
                : t("preferences.chinese")}
            </span>
          </button>
        </div>
        <label>
          {t("auth.email")}
          <input
            name="email"
            type="email"
            required
            autoComplete="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </label>
        <label>
          {t("auth.password")}
          <input
            name="password"
            type="password"
            required
            autoComplete="current-password"
          />
        </label>
        <label className="auth-remember">
          <input name="rememberMe" type="checkbox" />
          <span>
            {t("auth.remember")} <small>{t("auth.rememberHint")}</small>
          </span>
        </label>
        {error && <p className="error">{error}</p>}
        <div className="auth-actions">
          <button type="submit" disabled={!formReady} aria-busy={!formReady}>
            {t("auth.login")}
          </button>
          <Link className="auth-register-button" href="/register">
            {t("auth.register")}
          </Link>
        </div>
        {setupRequired ? (
          <p className="muted">
            {t("auth.setupHint")}{" "}
            <Link href="/setup">{t("auth.createAdmin")}</Link>
          </p>
        ) : (
          <p className="muted">{t("auth.localOnly")}</p>
        )}
      </form>
    </main>
  );
}
