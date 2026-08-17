import { describe, expect, it } from "vitest";
import {
  parseStoredLocale,
  parseStoredTheme,
  readStoredPreferences,
  writeStoredPreferences,
} from "../src/features/preferences/preference-storage";

function createStorage() {
  const values = new Map<string, string>();
  return {
    getItem(key: string) {
      return values.get(key) ?? null;
    },
    setItem(key: string, value: string) {
      values.set(key, value);
    },
  };
}

describe("界面偏好存储", () => {
  it("只接受受支持的已保存偏好", () => {
    expect(parseStoredLocale("zh-CN")).toBe("zh-CN");
    expect(parseStoredLocale("en-US")).toBe("en-US");
    expect(parseStoredLocale("fr-FR")).toBeNull();
    expect(parseStoredTheme("dark")).toBe("dark");
    expect(parseStoredTheme("system")).toBeNull();
  });

  it("可以完整读写语言和主题", () => {
    const storage = createStorage();
    writeStoredPreferences(storage, { locale: "en-US", theme: "dark" });
    expect(readStoredPreferences(storage)).toEqual({
      locale: "en-US",
      theme: "dark",
    });
  });
});
