import { expect, type Page, test } from "@playwright/test";

function testCredentials() {
  const token = process.env.SEOOS_E2E_TOKEN;
  const api = process.env.SEOOS_API_ORIGIN;
  if (!token || !api) throw new Error("Run seoos/scripts/test_dashboard.py with an isolated PostgreSQL database.");
  return { token, api };
}

async function signIn(page: Page, token: string) {
  await page.goto("/seo/login");
  const credential = page.getByLabel("访问凭据", { exact: true });
  await credential.fill(token);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("table", { name: "已登记站点" })).toBeVisible();
}

test("SEO login disables credential submission before JavaScript is ready", async ({ browser, baseURL }) => {
  const context = await browser.newContext({ baseURL, javaScriptEnabled: false });
  try {
    const page = await context.newPage();
    await page.goto("/seo/login");
    await expect(page.locator("form")).toHaveAttribute("method", "post");
    await expect(page.getByLabel("访问凭据", { exact: true })).toBeDisabled();
    await expect(page.getByRole("button", { name: "登录", exact: true })).toBeDisabled();
  } finally {
    await context.close();
  }
});

test("shared ARTEX shell operates on the real SEO API", async ({ page, browser, request }) => {
  const { token, api } = testCredentials();
  const legacyRequests: string[] = [];
  const browserErrors: string[] = [];
  page.on("request", (r) => {
    if (new URL(r.url()).pathname.startsWith("/api/")) legacyRequests.push(new URL(r.url()).pathname);
  });
  page.on("pageerror", (error) => browserErrors.push(error.message));
  await page.goto("/seo/sites");
  await expect(page.getByRole("heading", { name: "登录运营后台" })).toBeVisible();
  await page.getByLabel("访问凭据", { exact: true }).fill("invalid-local-test-credential-00000000000");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "访问凭据无效或已撤销。" })).toBeVisible();
  await signIn(page, token);
  const cookie = (await page.context().cookies()).find((c) => c.name === "artex_seo_session");
  expect(cookie?.httpOnly).toBe(true);
  expect(cookie?.secure).toBe(true);
  expect(cookie?.sameSite).toBe("Lax");
  expect(cookie?.path).toBe("/seo");
  expect(await page.evaluate(() => document.cookie.includes("artex_seo_session"))).toBe(false);

  const csrfCheck = await page.request.post("/seo/api/sites", {
    headers: { Origin: "https://untrusted.example.test", "X-SEOOS-Request": "1" },
    data: {},
  });
  expect(csrfCheck.status()).toBe(403);
  const legacy = await request.get("/api/health");
  expect(legacy.status()).toBe(404);

  const name = "后台验收站点";
  await page.getByRole("button", { name: "添加站点" }).click();
  let editor = page.getByRole("dialog", { name: "添加站点" });
  await editor.getByLabel("站点名称").fill(name);
  await editor.getByLabel("站点 URL").fill("https://dashboard.example.test");
  await editor.getByLabel("国家或地区").fill("JP");
  await editor.getByLabel("语言标签").fill("ja-JP");
  await editor.getByRole("button", { name: "保存站点" }).click();
  await expect(page.getByRole("row").filter({ hasText: name })).toBeVisible();

  await page.getByRole("button", { name: `编辑 ${name}`, exact: true }).click();
  editor = page.getByRole("dialog", { name: "编辑站点" });
  await editor.getByLabel("站点名称").fill(`${name}（已更新）`);
  await editor.getByRole("button", { name: "保存站点" }).click();
  const updated = page.getByRole("row").filter({ hasText: `${name}（已更新）` });
  await expect(updated).toBeVisible();
  await updated.getByRole("link", { name: /创建任务/ }).click();
  const jobCreated = page.waitForResponse(
    (reply) => new URL(reply.url()).pathname === "/seo/api/jobs" && reply.request().method() === "POST",
  );
  await page.getByRole("dialog", { name: "创建任务" }).getByRole("button", { name: "提交任务" }).click();
  const { job: inventory } = await (await jobCreated).json();
  expect(inventory?.id).toBeTruthy();
  const auth = { Authorization: `Bearer ${token}` };
  await expect
    .poll(async () => {
      const reply = await request.get(`${api}/api/v1/jobs/${inventory.id}`, { headers: auth });
      const { job } = await reply.json();
      return job?.state;
    })
    .toBe("succeeded");
  await page.getByRole("button", { name: "刷新" }).click();
  const inventoryRow = page.getByRole("row").filter({ has: page.getByTitle(inventory.id, { exact: true }) });
  await expect(inventoryRow).toContainText("完成");
  await inventoryRow.getByRole("button", { name: "查看结果" }).click();
  await expect(page.getByRole("dialog").locator("pre")).toContainText("dashboard.example.test");
  await page.getByRole("dialog").getByRole("button", { name: "关闭", exact: true }).click();

  await page.getByRole("link", { name: "Provider", exact: true }).click();
  await page.getByRole("button", { name: "添加 Provider" }).click();
  let provider = page.getByRole("dialog", { name: "添加 Provider" });
  await provider.getByLabel("名称", { exact: true }).fill("后台测试配置");
  await provider.getByLabel("OpenAI 模型").fill("test-fixture-model");
  await provider.getByLabel("API 密钥").fill("dashboard-no-network-test-key");
  await provider.getByRole("switch", { name: "启用" }).click();
  const providerCreated = page.waitForResponse(
    (reply) => new URL(reply.url()).pathname === "/seo/api/providers" && reply.request().method() === "POST",
  );
  await provider.getByRole("button", { name: "保存 Provider" }).click();
  const { provider: addedProvider } = await (await providerCreated).json();
  expect(addedProvider?.id).toBeTruthy();
  const savedProvider = page.getByRole("row").filter({ hasText: "后台测试配置" });
  await expect(savedProvider).toContainText("停用");
  await savedProvider.getByRole("button", { name: "编辑 后台测试配置" }).click();
  provider = page.getByRole("dialog", { name: "编辑 Provider" });
  await expect(provider.getByLabel("API 密钥")).toHaveValue("");
  await provider.getByLabel("超时（秒）").fill("25");
  await provider.getByRole("button", { name: "保存 Provider" }).click();
  await expect(savedProvider).toContainText("25 秒");

  await page.getByRole("link", { name: "成员", exact: true }).click();
  await page.getByRole("button", { name: "添加成员" }).click();
  const adding = page.getByRole("dialog", { name: "添加成员" });
  await adding.getByLabel("邮箱").fill("dashboard-viewer@example.test");
  await adding.getByRole("button", { name: "添加成员" }).click();
  const issuance = page.getByRole("dialog", { name: "访问凭据" });
  const code = issuance.getByTestId("issued-credential");
  await expect(code).toBeVisible();
  const viewerToken = await code.textContent();
  if (!viewerToken?.startsWith("seo_")) throw new Error("Expected a local member credential.");
  await issuance.getByRole("button", { name: "完成" }).click();
  await page.getByRole("link", { name: "审计日志" }).click();
  await expect(page.getByText("site.update", { exact: true })).toBeVisible();
  const audit = page.getByRole("row").filter({ hasText: "provider.create" }).filter({ hasText: addedProvider.id });
  await audit.getByRole("button", { name: "查看记录" }).click();
  await expect(page.getByRole("dialog").locator("pre")).toContainText("后台测试配置");
  await expect(page.getByRole("dialog").locator("pre")).not.toContainText("dashboard-no-network-test-key");

  const viewer = await browser.newPage({ baseURL: page.url() });
  try {
    await signIn(viewer, viewerToken);
    await expect(viewer.getByText(`${name}（已更新）`, { exact: true })).toBeVisible();
    await expect(viewer.getByRole("button", { name: "添加站点" })).toHaveCount(0);
    await expect(viewer.getByRole("link", { name: "成员", exact: true })).toHaveCount(0);
    await viewer.goto("/seo/audit");
    await expect(viewer.getByText("审计日志需要所有者权限")).toBeVisible();
    const denied = await request.post(`${api}/api/v1/sites`, {
      headers: { Authorization: `Bearer ${viewerToken}` },
      data: {},
    });
    expect(denied.status()).toBe(403);
    await viewer.getByRole("button", { name: "账号菜单" }).click();
    await viewer.getByRole("menuitem", { name: "退出登录" }).click();
    await expect(viewer.getByRole("heading", { name: "登录运营后台" })).toBeVisible();
  } finally {
    await viewer.close();
  }
  expect(legacyRequests).toEqual([]);
  expect(browserErrors).toEqual([]);
});

test("ARTEX theme presets, font persistence, search, and mobile sidebar work in SEO", async ({ page }) => {
  await signIn(page, testCredentials().token);
  const root = page.locator("html");
  await expect(root).toHaveAttribute("data-theme-mode", "light");
  await page.getByRole("button", { name: /^Current theme: light/ }).click();
  await expect(root).toHaveClass(/\bdark\b/);
  await page
    .getByRole("button")
    .filter({ has: page.locator("svg.lucide-settings") })
    .click();
  const preferences = page.locator('[data-slot="popover-content"]');
  await expect(preferences.getByText("Preferences", { exact: true })).toBeVisible();
  const presets = preferences.getByRole("combobox").first();
  for (const [title, value] of [
    ["Brutalist", "brutalist"],
    ["Soft Pop", "soft-pop"],
    ["Tangerine", "tangerine"],
  ]) {
    await presets.click();
    await page.getByRole("option", { name: title, exact: true }).click();
    await expect(root).toHaveAttribute("data-theme-preset", value);
  }
  await preferences.getByRole("combobox").nth(1).click();
  await page.getByRole("option", { name: "Geist Mono", exact: true }).click();
  await expect(root).toHaveAttribute("data-font", "geistMono");
  await page.reload();
  await expect(page.getByRole("table", { name: "已登记站点" })).toBeVisible();
  await expect(root).toHaveAttribute("data-theme-preset", "tangerine");
  await expect(root).toHaveAttribute("data-font", "geistMono");
  await expect(root).toHaveClass(/\bdark\b/);

  await page.getByRole("button", { name: /^Search/ }).click();
  await page.getByRole("option", { name: "Provider" }).click();
  await expect(page.getByRole("heading", { name: "Provider", exact: true })).toBeVisible();

  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  await page.getByRole("link", { name: "站点", exact: true }).click();
  await expect(page.getByRole("heading", { name: "站点", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "站点", exact: true })).not.toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await page.getByRole("button", { name: "添加站点" }).click();
  await expect(page.getByRole("dialog", { name: "添加站点" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "添加站点" })).not.toBeVisible();
});
