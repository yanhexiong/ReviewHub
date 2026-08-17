"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { usePreferences } from "@/features/preferences";

export default function RegisterPage() {
  const { t } = usePreferences();
  const [open, setOpen] = useState<boolean | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    fetch("/api/auth/register/status")
      .then((response) => response.json())
      .then((body) => setOpen(body.registrationOpen === true))
      .catch(() => setOpen(false));
  }, []);
  async function submit(form: FormData) {
    setError("");
    const response = await fetch("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: form.get("email"),
        password: form.get("password"),
      }),
    });
    const body = await response.json();
    if (!response.ok) {
      setError(body.error?.message ?? "注册失败");
      return;
    }
    location.assign("/projects");
  }
  return (
    <main className="auth">
      <form action={submit}>
        <p className="eyebrow">REVIEW HUB / {t("auth.registerSection")}</p>
        <h1>{t("auth.registerTitle")}</h1>
        {open === false ? (
          <p className="error">{t("auth.registrationClosed")}</p>
        ) : (
          <>
            <p className="muted">{t("auth.registerHint")}</p>
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
            {error && <p className="error">{error}</p>}
            <button type="submit">{t("auth.registerSubmit")}</button>
          </>
        )}
        <p className="muted">
          {t("auth.hasAccount")}{" "}
          <Link href="/login">{t("auth.backToLogin")}</Link>
        </p>
      </form>
    </main>
  );
}
