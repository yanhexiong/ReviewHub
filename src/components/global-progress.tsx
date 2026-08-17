"use client";

import { useEffect, useState } from "react";
import { usePreferences } from "@/features/preferences";

/**
 * Shows a non-blocking top progress indicator for requests that take longer
 * than a short interaction threshold. The native fetch function is restored
 * during cleanup so Strict Mode and hot reload do not stack wrappers.
 */
export function GlobalProgress() {
  const { locale } = usePreferences();
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const nativeFetch = window.fetch.bind(window);
    let pending = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const start = () => {
      pending += 1;
      if (pending !== 1) return;
      timer = setTimeout(() => setBusy(true), 250);
    };
    const finish = () => {
      pending = Math.max(0, pending - 1);
      if (pending !== 0) return;
      if (timer) clearTimeout(timer);
      timer = undefined;
      setBusy(false);
    };

    window.fetch = async (...args: Parameters<typeof fetch>) => {
      const request = args[0];
      const init = args[1];
      const headers = new Headers(
        init?.headers ??
          (request instanceof Request ? request.headers : undefined),
      );
      if (headers.get("X-Review-Hub-Embedded-Progress") === "1")
        return nativeFetch(...args);
      start();
      try {
        return await nativeFetch(...args);
      } finally {
        finish();
      }
    };

    return () => {
      window.fetch = nativeFetch;
      if (timer) clearTimeout(timer);
      setBusy(false);
    };
  }, []);

  if (!busy) return null;
  return (
    <div
      aria-label={locale === "en-US" ? "Processing request" : "正在处理请求"}
      className="global-progress"
      data-testid="global-progress"
      role="status"
    />
  );
}
