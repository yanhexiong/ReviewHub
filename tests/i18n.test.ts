import { describe, expect, it } from "vitest";
import { isLocale, translate } from "../src/i18n";
import { isTheme } from "../src/features/preferences/preference-storage";

describe("界面偏好与多语言", () => {
  it("只接受支持的语言和主题值", () => {
    expect(isLocale("zh-CN")).toBe(true);
    expect(isLocale("en-US")).toBe(true);
    expect(isLocale("ja-JP")).toBe(false);
    expect(isTheme("light")).toBe(true);
    expect(isTheme("dark")).toBe(true);
    expect(isTheme("system")).toBe(false);
  });

  it("可以返回中英文文案并替换变量", () => {
    expect(translate("zh-CN", "projects.title")).toBe("论文审阅项目");
    expect(translate("en-US", "projects.title")).toBe("Paper review projects");
    expect(translate("en-US", "projects.deleted", { name: "Demo" })).toBe(
      "Project “Demo” was deleted.",
    );
  });
});
