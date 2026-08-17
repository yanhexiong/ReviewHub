"use client";

import { Moon, Sun } from "lucide-react";
import { usePathname } from "next/navigation";
import { usePreferences } from "./preferences-provider";

type PreferencesControlsProps = {
  placement?: "floating" | "toolbar";
};

function usesWorkspaceToolbar(pathname: string) {
  return (
    /^\/projects\/[^/]+\/(review|compare|editor)(?:\/|$)/.test(pathname) ||
    pathname.startsWith("/share/")
  );
}

export function PreferencesControls({
  placement = "floating",
}: PreferencesControlsProps) {
  const pathname = usePathname();
  const { theme, toggleTheme, t } = usePreferences();
  const nextThemeLabel =
    theme === "dark" ? t("preferences.light") : t("preferences.dark");

  // Full-screen workspaces provide this control in their own toolbar so it
  // cannot cover reader, comparison, or editor actions.
  if (placement === "floating" && usesWorkspaceToolbar(pathname)) return null;

  return (
    <button
      aria-label={
        theme === "dark"
          ? t("preferences.switchToLight")
          : t("preferences.switchToDark")
      }
      className={`app-preferences app-preferences--${placement}`}
      data-testid="app-preferences"
      title={`${t("preferences.theme")}: ${nextThemeLabel}`}
      type="button"
      onClick={toggleTheme}
    >
      {theme === "dark" ? (
        <Sun size={17} aria-hidden="true" />
      ) : (
        <Moon size={17} aria-hidden="true" />
      )}
      <span className="sr-only">{nextThemeLabel}</span>
    </button>
  );
}
