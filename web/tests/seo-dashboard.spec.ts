import { expect, type Page, test } from "@playwright/test";

import { seoPages } from "../src/lib/seo/routes";
import { join } from "node:path";

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
  await expect(page.getByRole("heading", { name: "运营仪表盘", level: 1 })).toBeVisible();
}

async function showGroup(page: Page, group: string) {
  const sidebar = page.locator('[data-sidebar="sidebar"]:visible');
  const disclosure = sidebar.getByRole("button", { name: group, exact: true });
  if (!(await disclosure.isVisible())) await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  if ((await disclosure.getAttribute("aria-expanded")) !== "true") await disclosure.click();
  return sidebar;
}

async function openNavigation(page: Page, group: string, destination: string) {
  const sidebar = await showGroup(page, group);
  await sidebar.getByRole("link", { name: destination, exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: destination, exact: true })).toBeVisible();
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
  await openNavigation(page, "站点管理", "全部站点");
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

  await openNavigation(page, "AI 与系统", "大模型服务商");
  await page.getByRole("button", { name: "添加大模型" }).click();
  let provider = page.getByRole("dialog", { name: "添加大模型" });
  await provider.getByLabel("名称", { exact: true }).fill("后台测试配置");
  await provider.getByLabel("OpenAI 模型 ID").fill("test-fixture-model");
  await expect(provider.getByLabel("DataForSEO 登录名")).toHaveCount(0);
  await provider.getByLabel("API 密钥").fill("dashboard-no-network-test-key");
  await provider.getByRole("switch", { name: "启用" }).click();
  const providerCreated = page.waitForResponse(
    (reply) => new URL(reply.url()).pathname === "/seo/api/providers" && reply.request().method() === "POST",
  );
  await provider.getByRole("button", { name: "保存配置" }).click();
  const { provider: addedProvider } = await (await providerCreated).json();
  expect(addedProvider?.id).toBeTruthy();
  const savedProvider = page.getByRole("row").filter({ hasText: "后台测试配置" });
  await expect(savedProvider).toContainText("停用");
  const probe = savedProvider.getByRole("button", { name: "测试连接", exact: true });
  await expect(probe).toBeVisible();
  if (process.env.SEOOS_E2E_PROVIDER_TEST === "1") {
    const tested = page.waitForResponse(
      (res) => new URL(res.url()).pathname === `/seo/api/providers/${addedProvider.id}/test`,
    );
    await probe.click();
    expect(await (await tested).json()).toMatchObject({ status: "ok", provider_version: 1 });
    await expect(savedProvider.getByRole("status")).toHaveText(/连接成功（\d+ ms）/);
  }
  await savedProvider.getByRole("button", { name: "编辑 后台测试配置" }).click();
  provider = page.getByRole("dialog", { name: "编辑大模型" });
  await expect(provider.getByLabel("API 密钥")).toHaveValue("");
  await provider.getByLabel("OpenAI 模型 ID").fill("missing-fixture-model");
  await provider.getByLabel("超时（秒）").fill("25");
  await provider.getByRole("button", { name: "保存配置" }).click();
  await expect(savedProvider).toContainText("25 秒");
  await expect(savedProvider.getByRole("status")).toHaveText("");
  if (process.env.SEOOS_E2E_PROVIDER_TEST === "1") {
    await probe.click();
    await expect(savedProvider.getByRole("status")).toContainText("在厂商处查不到该模型");
  }

  await openNavigation(page, "AI 与系统", "成员与权限");
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
    await openNavigation(viewer, "站点管理", "全部站点");
    await expect(viewer.getByText(`${name}（已更新）`, { exact: true })).toBeVisible();
    await expect(viewer.getByRole("button", { name: "添加站点" })).toHaveCount(0);
    const viewerSystem = await showGroup(viewer, "AI 与系统");
    await expect(viewerSystem.getByRole("link", { name: "成员与权限", exact: true })).toHaveCount(0);
    await expect(viewerSystem.getByRole("link", { name: "审计日志", exact: true })).toHaveCount(0);
    await viewer.goto("/seo/audit");
    await expect(viewer.getByText("审计日志需要所有者权限")).toBeVisible();
    const denied = await request.post(`${api}/api/v1/sites`, {
      headers: { Authorization: `Bearer ${viewerToken}` },
      data: {},
    });
    expect(denied.status()).toBe(403);
    const probeDenied = await request.post(`${api}/api/v1/providers/${addedProvider.id}/test`, {
      headers: { Authorization: `Bearer ${viewerToken}` },
    });
    expect(probeDenied.status()).toBe(403);
    await viewer.goto("/seo/providers");
    await expect(viewer.getByRole("row").filter({ hasText: "后台测试配置" })).toBeVisible();
    await expect(viewer.getByRole("button", { name: "测试连接", exact: true })).toHaveCount(0);
    await expect(viewer.getByRole("tab", { name: "请求诊断" })).toHaveCount(0);
    const receiptsDenied = await request.get(`${api}/api/v1/providers/receipts`, {
      headers: { Authorization: `Bearer ${viewerToken}` },
    });
    expect(receiptsDenied.status()).toBe(403);
    await viewer.getByRole("button", { name: "账号菜单" }).click();
    await viewer.getByRole("menuitem", { name: "退出登录" }).click();
    await expect(viewer.getByRole("heading", { name: "登录运营后台" })).toBeVisible();
  } finally {
    await viewer.close();
  }
  expect(legacyRequests).toEqual([]);
  expect(browserErrors).toEqual([]);
});

test("OpenAI Compatible capabilities persist and URL changes require a new credential", async ({ page }) => {
  await signIn(page, testCredentials().token);
  await openNavigation(page, "AI 与系统", "大模型服务商");
  await page.getByRole("button", { name: "添加大模型" }).click();
  let dialog = page.getByRole("dialog", { name: "添加大模型" });
  await dialog.getByLabel("模型厂商").selectOption("openai_compatible");
  await dialog.getByLabel("名称", { exact: true }).fill("独立兼容模型");
  await dialog.getByLabel("OpenAI 兼容 模型 ID").fill("fixture/custom-model");
  const testKey = "compatible-fixture-credential";
  await dialog.getByLabel("API 密钥").fill(testKey);
  await dialog.getByLabel("API Base URL").fill("https://api.example.com/gateway/v1");
  await dialog.getByLabel("认证 Header", { exact: true }).fill("X-Vendor-Key");
  await dialog.getByLabel("认证方式").selectOption("raw");
  await dialog.getByLabel("输出 Token 参数").selectOption("max_completion_tokens");
  await dialog.getByLabel("推理强度").selectOption("high");
  await dialog.getByLabel("输出格式").selectOption("json_schema");
  const screenshots = process.env.SEOOS_E2E_PROVIDER_SCREENSHOTS;
  if (screenshots) {
    await dialog.screenshot({
      path: join(screenshots, "ARTEX-OpenAI兼容配置-本地.png"),
      mask: [dialog.getByLabel("API 密钥")],
    });
  }
  const created = page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/seo/api/providers" && res.request().method() === "POST",
  );
  await dialog.getByRole("button", { name: "保存配置" }).click();
  const reply = await created;
  const body = await reply.json();
  expect(reply.status()).toBe(201);
  expect(body.provider.model_options).toMatchObject({
    output_format: "json_schema",
    auth_header: "X-Vendor-Key",
    reasoning_effort: "high",
  });
  expect(JSON.stringify(body)).not.toContain(testKey);
  await expect(page.getByRole("row").filter({ hasText: "独立兼容模型" })).toBeVisible();
  await page.getByRole("button", { name: "编辑 独立兼容模型" }).click();
  dialog = page.getByRole("dialog", { name: "编辑大模型" });
  await expect(dialog.getByLabel("API 密钥")).toHaveValue("");
  await expect(dialog.getByLabel("API Base URL")).toHaveValue("https://api.example.com/gateway/v1");
  await dialog.getByLabel("API Base URL").fill("https://other.example.com/v1");
  await dialog.getByRole("button", { name: "保存配置" }).click();
  await expect(dialog.getByRole("alert")).toContainText("需要重新输入密钥");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await page.getByRole("tab", { name: "请求诊断" }).click();
  const diagnostics = page.getByRole("table", { name: "Provider 请求诊断" });
  await expect(diagnostics).toBeVisible();
  const iaScreenshots = process.env.SEOOS_E2E_IA_SCREENSHOTS;
  if (iaScreenshots) {
    await page.screenshot({
      path: join(iaScreenshots, "desktop-provider-diagnostics.png"),
      fullPage: true,
      animations: "disabled",
      mask: [diagnostics.locator("tbody tr td:nth-child(4)")],
    });
  }
});

test("WordPress installation can be edited within its registered HTTPS site", async ({ page, request }) => {
  const { token, api } = testCredentials();
  async function register(name: string, baseURL: string) {
    const response = await request.post(`${api}/api/v1/sites`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { name, base_url: baseURL, cms: "wordpress", country_code: "US", language_tag: "en-US" },
    });
    expect(response.status()).toBe(201);
    return (await response.json()).site.id as string;
  }
  const insecure = await register("WordPress HTTP fixture", "http://wordpress-form.example.test/blog");
  const secureURL = "https://wordpress-form.example.test/blog";
  const secure = await register("WordPress HTTPS fixture", secureURL);
  await signIn(page, token);
  await openNavigation(page, "站点管理", "CMS 连接");
  const sites = page.getByLabel("执行站点", { exact: true });
  await expect(sites).toBeVisible();
  async function fillConnection(site: string) {
    await sites.selectOption(site);
    await page.getByRole("button", { name: "新建连接" }).click();
    const dialog = page.getByRole("dialog", { name: "新建连接" });
    await dialog.getByLabel("连接名称").fill("WordPress 接入测试");
    await dialog.getByLabel("WordPress 用户名").fill("local-editor");
    await dialog.getByLabel("WordPress 应用程序密码", { exact: true }).fill("abcd efgh ijkl mnop qrst uvwx");
    await dialog.getByRole("switch", { name: "启用连接" }).click();
    await expect(dialog.getByRole("switch", { name: "授权此连接写入站点" })).not.toBeChecked();
    return dialog;
  }
  let dialog = await fillConnection(insecure);
  const invalid = page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/seo/api/execution/connections" && res.request().method() === "POST",
  );
  await dialog.getByRole("button", { name: "保存连接" }).click();
  expect(await (await invalid).json()).toMatchObject({ error: { code: "invalid_request", field: "base_url" } });
  await expect(dialog.getByRole("alert")).toContainText("HTTPS 安装地址");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();

  dialog = await fillConnection(secure);
  const address = dialog.getByLabel("WordPress 站点地址", { exact: true });
  await expect(address).toHaveValue(secureURL);
  await expect(address).toBeEditable();
  await address.fill(`${secureURL}/cms`);
  const saved = page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/seo/api/execution/connections" && res.request().method() === "POST",
  );
  await dialog.getByRole("button", { name: "保存连接" }).click();
  const response = await saved;
  expect(response.status()).toBe(201);
  const body = await response.json();
  expect(body.connection).toMatchObject({
    kind: "wordpress",
    base_url: `${secureURL}/cms`,
    configured: true,
    allow_writes: false,
  });
  expect(body.connection.credential).toBeUndefined();
  const row = page.getByRole("row").filter({ hasText: "WordPress 接入测试" });
  await expect(row.getByRole("button", { name: "测试连接" })).toBeVisible();
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "编辑连接" });
  const editAddress = dialog.getByLabel("WordPress 站点地址", { exact: true });
  await expect(editAddress).toHaveValue(`${secureURL}/cms`);
  await editAddress.fill(`${secureURL}/new-cms`);
  await dialog.getByRole("button", { name: "保存连接" }).click();
  await expect(dialog.getByRole("alert")).toContainText("更改安装地址");
  await dialog.getByLabel("WordPress 用户名").fill("local-editor");
  await dialog.getByLabel("WordPress 应用程序密码").fill("abcd efgh ijkl mnop qrst uvwx");
  await dialog.getByRole("button", { name: "保存连接" }).click();
  await expect(dialog).toBeHidden();
  await expect(row).toContainText(`${secureURL}/new-cms`);
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
  await expect(page.getByRole("heading", { name: "运营仪表盘", level: 1 })).toBeVisible();
  await expect(root).toHaveAttribute("data-theme-preset", "tangerine");
  await expect(root).toHaveAttribute("data-font", "geistMono");
  await expect(root).toHaveClass(/\bdark\b/);

  await page.getByRole("button", { name: /^Search/ }).click();
  await page.getByRole("option", { name: /大模型服务商$/u }).click();
  await expect(page.getByRole("heading", { name: "大模型服务商", exact: true })).toBeVisible();

  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("tab", { name: "DataForSEO 数据源" }).click();
  await expect(page.getByRole("table", { name: "DataForSEO 数据源" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await openNavigation(page, "站点管理", "全部站点");
  await expect(page.locator('[data-mobile="true"][data-sidebar="sidebar"]')).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await page.getByRole("button", { name: "添加站点" }).click();
  await expect(page.getByRole("dialog", { name: "添加站点" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "添加站点" })).not.toBeVisible();
});

test("SEO navigation preserves scoped pages and renders API metrics on desktop and mobile", async ({
  page,
  browser,
  baseURL,
  request,
}) => {
  test.setTimeout(100_000);
  const { api, token } = testCredentials();
  const headers = { Authorization: `Bearer ${token}` };
  const output = process.env.SEOOS_E2E_IA_SCREENSHOTS;
  const workspace = [
    { group: "运营总览", title: "运营仪表盘", path: "/seo/dashboard", file: "01-Dashboard" },
    { group: "站点管理", title: "站点配置", path: "/seo/sites/settings", file: "02-Sites" },
    { group: "SEO 研究", title: "关键词研究", path: "/seo/research", file: "03-Research" },
    { group: "内容中心", title: "内容项目", path: "/seo/content", file: "04-Content" },
    { group: "SEO 执行", title: "发布中心", path: "/seo/execution/publishing", file: "05-Execution" },
    { group: "AI 与系统", title: "大模型服务商", path: "/seo/system/providers", file: "06-AI-System" },
  ];
  async function ready(visit: Page, path: string) {
    const tableNames: Record<string, string> = {
      "/seo/research": "种子关键词",
      "/seo/content": "内容项目列表",
      "/seo/execution/publishing": "CMS 发布记录",
      "/seo/system/providers": "大模型服务",
    };
    const table = tableNames[path];
    if (table) await expect(visit.getByRole("table", { name: table, exact: true })).toBeVisible();
    else if (path === "/seo/dashboard") await expect(visit.locator('[aria-label="站点运营指标"]')).toBeVisible();
    else await expect(visit.getByText("https://ia-docs.example.test", { exact: true })).toBeVisible();
  }
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));

  async function site(name: string, domain: string, status: "active" | "paused", country = "US", language = "en-US") {
    const reply = await request.post(`${api}/api/v1/sites`, {
      headers,
      data: {
        name,
        base_url: `https://${domain}.example.test`,
        cms: "wordpress",
        country_code: country,
        language_tag: language,
        status,
      },
    });
    expect(reply.status()).toBe(201);
    return (await reply.json()).site as { id: string; name: string; base_url: string };
  }

  const active = await site("信息架构 · 文档站", "ia-docs", "active");
  const paused = await site("信息架构 · 已暂停站点", "ia-paused", "paused", "FR", "fr-FR");
  const inventory = await request.post(`${api}/api/v1/jobs`, {
    headers,
    data: { site_id: active.id, kind: "site_inventory", idempotency_key: "ia-dashboard-inventory" },
  });
  expect(inventory.status()).toBe(201);
  const { job } = await inventory.json();
  await expect
    .poll(async () => (await (await request.get(`${api}/api/v1/jobs/${job.id}`, { headers })).json()).job.state)
    .toBe("succeeded");

  await page.setViewportSize({ width: 1440, height: 1000 });
  await signIn(page, token);
  const topLevel = ["运营总览", "站点管理", "SEO 研究", "内容中心", "SEO 执行", "AI 与系统"];
  const sections = [
    ["运营仪表盘", "站群健康", "待处理事项", "最近活动"],
    ["全部站点", "站群分组", "CMS 连接", "站点配置", "内容资产清单"],
    ["关键词研究", "关键词拓展", "关键词聚类", "搜索意图分析", "SERP 竞争分析", "关键词与页面映射", "关键词机会评分"],
    ["内容项目", "内容策略", "内容简报", "研究与资料来源", "文章编辑", "事实核验", "质量审核", "版本管理", "旧文优化"],
    [
      "发布中心",
      "站内链接（规划）",
      "站群互链（规划）",
      "技术 SEO",
      "索引管理",
      "站点地图",
      "索引提交",
      "谷歌搜索数据",
      "谷歌流量分析",
      "必应站长工具",
      "执行任务",
    ],
    [
      "AI 图片工作室（规划）",
      "WordPress 主题工厂（规划）",
      "大模型服务商",
      "AI Agent 管理（规划）",
      "自动化工作流（规划）",
      "任务中心",
      "成员与权限",
      "审计日志",
      "系统设置",
    ],
  ];
  const mainSidebar = page.locator('[data-sidebar="sidebar"]:visible');
  await expect(
    mainSidebar.locator('[data-slot="sidebar-content"] [data-sidebar="menu-button"][aria-expanded]'),
  ).toHaveText(topLevel);
  for (const [index, name] of topLevel.entries()) {
    const group = await showGroup(page, name);
    const button = group.getByRole("button", { name, exact: true });
    await expect(button.locator("xpath=ancestor::li[1]").getByRole("link")).toHaveText(sections[index]);
    if (name !== "运营总览") await button.click();
  }
  const allMetrics = page.locator('[aria-label="组织运营指标"]');
  await expect(allMetrics.getByText("网站总数", { exact: true }).locator("..").locator("dd").first()).toHaveText(
    /^\d+$/u,
  );
  expect(
    Number(await allMetrics.getByText("网站总数", { exact: true }).locator("..").locator("dd").first().textContent()),
  ).toBeGreaterThanOrEqual(2);
  await page.getByRole("button", { name: "站点范围", exact: true }).click();
  const popover = page.locator('[data-slot="popover-content"]');
  await popover.getByLabel("站群分组", { exact: true }).selectOption("FR:fr-FR");
  await expect(page).toHaveURL(
    (url) => url.searchParams.get("site_group") === "FR:fr-FR" && url.searchParams.get("site_id") === "all",
  );
  await expect(popover.getByLabel("全局站点", { exact: true }).locator("option")).toHaveText(["全部站点", paused.name]);
  await popover.getByLabel("国家或地区", { exact: true }).selectOption("US");
  await popover.getByLabel("语言", { exact: true }).selectOption("en-US");
  await expect(page).toHaveURL(
    (url) =>
      url.searchParams.get("site_country") === "US" &&
      url.searchParams.get("site_language") === "en-US" &&
      !url.searchParams.has("site_group"),
  );
  await expect(
    popover.getByLabel("全局站点", { exact: true }).locator("option").filter({ hasText: paused.name }),
  ).toHaveCount(0);
  if (output) await page.screenshot({ path: join(output, "desktop-site-scope.png"), animations: "disabled" });
  await popover.getByRole("button", { name: "设置研究市场", exact: true }).click();
  const globalMarket = page.getByRole("dialog", { name: "筛选市场", exact: true });
  await globalMarket.getByLabel("国家或地区").fill("US");
  await globalMarket.getByLabel("语言代码").fill("en");
  await globalMarket.getByLabel("DataForSEO 地区代码").fill("2840");
  await globalMarket.getByLabel("设备", { exact: true }).selectOption("mobile");
  await globalMarket.getByRole("button", { name: "应用筛选", exact: true }).click();
  await expect(page).toHaveURL(
    (url) =>
      url.searchParams.get("location_code") === "2840" &&
      url.searchParams.get("device") === "mobile" &&
      url.searchParams.get("site_country") === "US",
  );
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  await page.getByRole("button", { name: "SEO 研究", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: "关键词拓展", exact: true })).toBeVisible();
  if (output) await page.screenshot({ path: join(output, "desktop-collapsed-navigation.png"), animations: "disabled" });
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  const selector = page.getByLabel("统计站点", { exact: true });
  await page.goto(`/seo/dashboard?site_id=${active.id}`);
  await expect(selector).toHaveValue(active.id);
  await expect(page.getByRole("heading", { name: "SEO 表现", exact: true })).toBeVisible();
  const overview = page.locator('[aria-label="组织运营指标"]');
  const metric = (label: string) => overview.getByText(label, { exact: true }).locator("..").locator("dd").first();
  await expect(metric("网站总数")).toHaveText(/^\d+$/u);
  await expect(metric("网站总数")).toHaveText("1");
  await expect(metric("今日执行任务")).toHaveText(/^\d+$/u);
  expect(Number(await metric("今日执行任务").textContent())).toBeGreaterThanOrEqual(1);
  const search = page.locator('[aria-label="站点搜索指标"]');
  await expect(search.getByText("GSC 点击", { exact: true }).locator("..")).toContainText("未连接");
  await expect(search.getByText("自然搜索流量", { exact: true }).locator("..")).toContainText("未连接");

  const requestNewSite = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return url.pathname === "/seo/api/dashboard" && url.searchParams.get("site_id") === paused.id;
  });
  await selector.selectOption(paused.id);
  await requestNewSite;
  await expect(page).toHaveURL((url) => url.searchParams.get("site_id") === paused.id);
  await expect(metric("今日执行任务")).toHaveText("0");
  await page.goBack();
  await expect(selector).toHaveValue(active.id);
  await page.goForward();
  await expect(selector).toHaveValue(paused.id);
  await selector.selectOption(active.id);

  for (const item of workspace) {
    await openNavigation(page, item.group, item.title);
    await expect(page).toHaveURL((url) => url.pathname === item.path && url.searchParams.get("site_id") === active.id);
    await expect(
      page.locator('[data-sidebar="sidebar"]:visible').getByRole("link", { name: item.title, exact: true }),
    ).toHaveAttribute("aria-current", "page");
    await ready(page, item.path);
    if (output)
      await page.screenshot({ path: join(output, `desktop-${item.file}.png`), fullPage: true, animations: "disabled" });
  }

  await page
    .locator('[data-sidebar="sidebar"]:visible')
    .getByRole("link", { name: /ARTEX SEO OS/u })
    .click();
  await expect(page).toHaveURL(
    (url) => url.pathname === "/seo/dashboard" && url.searchParams.get("site_id") === active.id,
  );
  await page.goBack();
  await expect(page.getByRole("heading", { name: "大模型服务商", level: 1 })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "发布中心", level: 1 })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "大模型服务商", level: 1 })).toBeVisible();
  await page.reload();
  await expect(page).toHaveURL(
    (url) => url.pathname === "/seo/system/providers" && url.searchParams.get("site_id") === active.id,
  );
  await page.goto("/seo/dashboard/health");
  const health = page.getByRole("table", { name: "站群健康" });
  await expect(health.getByRole("row").filter({ hasText: active.name })).toContainText("完成");
  await expect(health.getByRole("row").filter({ hasText: paused.name })).toContainText("暂停");
  await page.goto(`/seo/dashboard/inbox?site_id=${active.id}`);
  await expect(page.getByRole("table", { name: "失败任务" })).toBeVisible();
  await expect(page.getByRole("table", { name: "待审核文章" })).toBeVisible();
  await page.goto("/seo/dashboard/activity");
  await page
    .getByRole("table", { name: "最近任务活动" })
    .getByRole("link", { name: "站点资料快照", exact: true })
    .first()
    .click();
  await expect(page.getByRole("dialog", { name: "站点资料快照" })).toBeVisible();
  await page.goto(`/seo/system/jobs?site_id=${active.id}`);
  const filteredJobs = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.pathname === "/seo/api/jobs" &&
      url.searchParams.get("site_id") === active.id &&
      url.searchParams.get("state") === "succeeded" &&
      url.searchParams.get("limit") === "51"
    );
  });
  await page.getByLabel("任务状态", { exact: true }).selectOption("succeeded");
  expect((await filteredJobs).ok()).toBe(true);
  await expect(page.getByRole("table", { name: "任务队列" })).toContainText("站点资料快照");
  await page.goto("/seo/system/theme-factory");
  await expect(page.getByRole("heading", { name: "WordPress 主题工厂", level: 1 })).toBeVisible();
  await expect(page.getByText("尚未开放", { exact: true })).toBeVisible();

  const unsupportedAdmin = await request.post(`${api}/api/v1/members`, {
    headers,
    data: { role: "admin", email: "ui-unsupported-admin@example.test" },
  });
  expect(unsupportedAdmin.status()).toBe(400);
  const editor = await request.post(`${api}/api/v1/members`, {
    headers,
    data: { role: "editor", email: "ia-editor@example.test" },
  });
  expect(editor.status()).toBe(201);
  const member = await editor.json();
  const editorPage = await browser.newPage({ baseURL });
  try {
    await editorPage.goto(`/seo/sites/settings?site_id=${paused.id}`);
    await expect(editorPage.getByRole("heading", { name: "登录运营后台" })).toBeVisible();
    await editorPage.getByLabel("访问凭据", { exact: true }).fill(member.token);
    await editorPage.getByRole("button", { name: "登录", exact: true }).click();
    await expect(editorPage).toHaveURL(
      (url) => url.pathname === "/seo/sites/settings" && url.searchParams.get("site_id") === paused.id,
    );
    await expect(editorPage.getByRole("button", { name: "编辑站点配置" })).toBeVisible();
    const editorNavigation = await showGroup(editorPage, "AI 与系统");
    await expect(editorNavigation.getByRole("link", { name: "成员与权限" })).toHaveCount(0);
    await editorPage.goto("/seo/system/audit");
    await expect(editorPage.getByText("审计日志需要所有者权限")).toBeVisible();
  } finally {
    await editorPage.close();
  }

  const mobile = await browser.newContext({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    baseURL,
  });
  try {
    const phone = await mobile.newPage();
    await signIn(phone, token);
    await phone.goto(`/seo/dashboard?site_id=${active.id}`);
    await expect(phone.getByLabel("统计站点", { exact: true })).toHaveValue(active.id);
    await phone.getByRole("button", { name: /^Current theme: light/ }).click();
    await expect(phone.locator("html")).toHaveClass(/\bdark\b/u);
    const menu = await showGroup(phone, "运营总览");
    if (output) await phone.screenshot({ path: join(output, "mobile-sidebar.png"), animations: "disabled" });
    await menu.getByRole("button", { name: "运营总览", exact: true }).press("Escape");
    await expect(phone.locator('[data-mobile="true"][data-sidebar="sidebar"]')).toHaveCount(0);
    for (const item of workspace) {
      await openNavigation(phone, item.group, item.title);
      await expect(phone).toHaveURL(
        (url) => url.pathname === item.path && url.searchParams.get("site_id") === active.id,
      );
      await expect(phone.locator('[data-mobile="true"][data-sidebar="sidebar"]')).toHaveCount(0);
      expect(await phone.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
      await ready(phone, item.path);
      if (output)
        await phone.screenshot({
          path: join(output, `mobile-${item.file}.png`),
          fullPage: true,
          animations: "disabled",
        });
    }
    await phone.reload();
    await expect(phone.getByRole("heading", { name: "大模型服务商", level: 1 })).toBeVisible();
    await expect(phone.locator("html")).toHaveClass(/\bdark\b/u);
  } finally {
    await mobile.close();
  }
  expect(errors).toEqual([]);
});

test("every registered SEO page opens directly on the production web build", async ({ page, request }) => {
  test.setTimeout(140_000);
  const { api, token } = testCredentials();
  const provisioned = await request.post(`${api}/api/v1/sites`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name: "UI 2.0 路由验收站点",
      base_url: "https://ui2-route-matrix.example.test",
      cms: "generic",
      country_code: "JP",
      language_tag: "ja-JP",
    },
  });
  expect(provisioned.status()).toBe(201);
  const { site } = await provisioned.json();
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await signIn(page, token);
  const paths = new Set(seoPages.map(({ path }) => path));
  expect(paths.size).toBe(56);
  for (const entry of seoPages) {
    const response = await page.goto(`${entry.path}?site_id=${site.id}`, { waitUntil: "domcontentloaded" });
    expect(response?.status(), entry.path).toBe(200);
    await expect(page.getByRole("heading", { name: entry.title, exact: true, level: 1 })).toBeVisible();
    if (entry.planned) await expect(page.getByText("尚未开放", { exact: true })).toBeVisible();
  }
  expect(errors).toEqual([]);
});
