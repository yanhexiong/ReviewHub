import { test, expect, type Page } from "@playwright/test";

async function openLogin(page: Page) {
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  await expect(
    page.getByRole("heading", { name: "登录审阅工作区" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "登录" })).toHaveAttribute(
    "aria-busy",
    "false",
  );
}

test("登录页不会暴露匿名审阅界面", async ({ page }) => {
  await openLogin(page);
  await expect(page.getByLabel(/记住密码/)).toBeVisible();
  await expect(page.getByRole("link", { name: "注册新账户" })).toBeVisible();
});

test("登录卡片可以切换界面语言", async ({ page }) => {
  await openLogin(page);
  await page.getByRole("button", { name: "语言: English" }).click();

  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  await expect(
    page.getByRole("heading", { name: "Sign in to Review Hub" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Switch to dark mode" }),
  ).toBeVisible();
});

test("注册入口始终可以进入注册页面", async ({ page }) => {
  await openLogin(page);
  await page.getByRole("link", { name: "注册新账户" }).click();
  await expect(page).toHaveURL(/\/register\/?$/);
  await expect(
    page.getByRole("heading", { name: "创建审阅账户" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "返回登录" })).toBeVisible();
});

test("登录失败后保留邮箱并清除密码", async ({ page }) => {
  const email = "not-a-real-user@example.test";

  await openLogin(page);
  await page.locator('input[name="email"]').fill(email);
  await page.locator('input[name="password"]').fill("incorrect-password");
  const loginResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/auth/login") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "登录" }).click();

  expect((await loginResponse).status()).toBe(401);
  await expect(page.getByText("邮箱或密码错误")).toBeVisible();
  await expect(page.locator('input[name="email"]')).toHaveValue(email);
  await expect(page.locator('input[name="password"]')).toHaveValue("");
});
