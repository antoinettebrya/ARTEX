import { type APIResponse, expect, type Page, test } from "@playwright/test";

import { integrationOAuthCallbackPath } from "../src/lib/seo/google-oauth";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";

const settings = "/seo/system/settings";
const sites = [
  {
    name: "集成验收站点一",
    host: "center-one.example.test",
    property: "properties/65431",
    directory: "/",
    key: "GZgz0123456789ab",
  },
  {
    name: "集成验收站点二",
    host: "center-two.example.test",
    property: "properties/65432",
    directory: "/updates/",
    key: "ZzgG9876543210xy",
  },
];

function resourceID(provider: "gsc" | "ga4" | "bing", site: (typeof sites)[number]) {
  if (provider === "gsc") return `sc-domain:${site.host}`;
  if (provider === "ga4") return site.property;
  return `https://${site.host}/`;
}

async function checked(response: APIResponse) {
  const body = await response.json();
  expect(response.ok(), `${response.status()} ${body.error?.code ?? ""}`).toBe(true);
  return body;
}

async function signIn(page: Page, token: string) {
  await page.goto("/seo/login");
  await page.getByLabel("访问凭据", { exact: true }).fill(token);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "运营仪表盘", level: 1 })).toBeVisible();
}

function tab(page: Page, name: string) {
  return page.getByRole("navigation", { name: "搜索集成设置" }).getByRole("link", { name, exact: true });
}

async function screenshot(page: Page, filename: string) {
  const output = process.env.SEOOS_E2E_INTEGRATION_SCREENSHOTS;
  if (!output) return;
  await mkdir(output, { recursive: true });
  await expect(page.locator("[data-sonner-toast]:visible")).toHaveCount(0, { timeout: 8000 });
  await page.screenshot({ path: join(output, filename), animations: "disabled" });
}

test("search integrations authorize shared accounts, verify scoped keys and keep other site bindings", async ({
  page,
  browser,
  request,
  baseURL,
}) => {
  test.skip(process.env.SEOOS_E2E_EXECUTION !== "1", "Requires the execution provider HTTP fixture.");
  test.setTimeout(120_000);
  const token = process.env.SEOOS_E2E_TOKEN;
  const api = process.env.SEOOS_API_ORIGIN;
  if (!token || !api || !baseURL)
    throw new Error("Run seoos/scripts/test_dashboard.py using an isolated PostgreSQL database.");
  const base = `${api}/api/v1`;
  const headers = { Authorization: `Bearer ${token}` };
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));

  const registered = await Promise.all(
    sites.map(async (site) => {
      const { site: result } = await checked(
        await request.post(`${base}/sites`, {
          headers,
          data: {
            name: site.name,
            base_url: `https://${site.host}`,
            cms: "generic",
            country_code: "US",
            language_tag: "en-US",
            status: "active",
          },
        }),
      );
      return { ...site, id: result.id as string };
    }),
  );
  async function runJob(data: Record<string, unknown>, expected = "succeeded") {
    const { job } = await checked(
      await request.post(`${base}/execution/jobs`, {
        headers,
        data: { idempotency_key: crypto.randomUUID(), ...data },
      }),
    );
    let state = job;
    await expect
      .poll(
        async () => {
          ({ job: state } = await checked(await request.get(`${base}/jobs/${job.id}`, { headers })));
          return state.state;
        },
        { timeout: 15_000 },
      )
      .toMatch(/^(succeeded|failed|cancelled)$/);
    expect(state.state, state.error_code ?? job.kind).toBe(expected);
    return state;
  }
  await signIn(page, token);
  await page.goto(settings);
  await expect(page.getByRole("heading", { name: "系统设置", exact: true })).toBeVisible();
  await tab(page, "OAuth 应用配置").click();
  await page.getByRole("button", { name: "添加 OAuth 应用" }).click();
  const application = page.getByRole("dialog", { name: "添加 OAuth 应用" });
  await application.getByLabel("应用名称").fill("Google 集成应用");
  await application.getByLabel("Client ID", { exact: true }).fill("integration-shared-client");
  await application.getByLabel("Client Secret").fill("integration-shared-secret");
  const callback = new URL(integrationOAuthCallbackPath("google"), baseURL);
  const registeredCallback = callback.href.replace(/^http:/, "https:");
  await application.getByLabel("回调地址", { exact: true }).fill(registeredCallback);
  const allGoogleScopes = async (dialog: ReturnType<Page["getByRole"]>) => {
    await dialog.getByRole("checkbox", { name: "Search Console 只读" }).check();
    await dialog.getByRole("checkbox", { name: "Google Analytics 只读" }).check();
  };
  await allGoogleScopes(application);
  await application.getByRole("button", { name: "保存应用" }).click();
  await expect(page.getByRole("table", { name: "OAuth 应用" })).toContainText("Google 集成应用");
  await tab(page, "外部账户授权").click();
  await page.getByRole("button", { name: "添加外部账户" }).click();
  const addAccount = page.getByRole("dialog", { name: "添加外部账户" });
  await addAccount.getByLabel("账户名称").fill("共享 Google 账户");
  await addAccount.getByLabel("OAuth 应用", { exact: true }).selectOption({ label: "Google 集成应用" });
  await addAccount.getByRole("button", { name: "保存账户" }).click();

  await page.route(
    "https://accounts.google.com/o/oauth2/v2/auth?*",
    async (route) => {
      const auth = new URL(route.request().url()).searchParams;
      expect(auth.get("redirect_uri")).toBe(registeredCallback);
      expect(auth.get("code_challenge_method")).toBe("S256");
      expect(auth.get("access_type")).toBe("offline");
      expect(auth.get("include_granted_scopes")).toBe("true");
      expect(auth.get("nonce")).toMatch(/^[A-Za-z0-9_-]{43}$/);
      expect(auth.get("scope")?.split(" ").sort()).toEqual([
        "https://www.googleapis.com/auth/analytics.readonly",
        "https://www.googleapis.com/auth/webmasters.readonly",
        "openid",
      ]);
      callback.searchParams.set("state", auth.get("state") ?? "");
      callback.searchParams.set("code", "phase4-shared-code");
      await route.fulfill({ status: 302, headers: { Location: callback.href } });
    },
    { times: 1 },
  );
  const accountTable = page.getByRole("table", { name: "外部账户", exact: true });
  const google = accountTable.getByRole("row").filter({ hasText: "共享 Google 账户" });
  await google.getByRole("button", { name: "授权或增加权限" }).click();
  const consent = page.getByRole("dialog", { name: "授权外部账户" });
  await allGoogleScopes(consent);
  const exchanged = page.waitForResponse((r) => new URL(r.url()).pathname === integrationOAuthCallbackPath("google"));
  await consent.getByRole("button", { name: "前往授权" }).click();
  const redirect = await exchanged;
  expect(redirect.status()).toBe(303);
  expect(redirect.headers().location).toContain("integration_oauth=connected");
  expect(redirect.headers()["referrer-policy"]).toBe("no-referrer");
  await expect(google.getByText("已授权", { exact: true })).toBeVisible();
  await expect(google.getByText("已验证", { exact: true })).toBeVisible();
  await expect(page).toHaveURL((url) => url.search === "?tab=accounts");
  expect((await page.request.get(callback.href, { maxRedirects: 0 })).status()).toBe(403);
  await page.getByRole("button", { name: "添加外部账户" }).click();
  await addAccount.getByLabel("账户名称").fill("共享 Bing 账户");
  await addAccount.getByLabel("认证方式").selectOption("api_key");
  await addAccount.getByLabel("Bing API Key").fill("phase4-bing-key");
  await addAccount.getByRole("button", { name: "保存账户" }).click();
  await expect(accountTable.getByRole("row").filter({ hasText: "共享 Bing 账户" })).toContainText("已授权");
  const accounts = (await checked(await request.get(`${base}/integrations/accounts`, { headers }))).items;
  expect(JSON.stringify(accounts)).not.toMatch(/phase4-|encrypted|access_token|refresh_token|fixture-google-subject/);
  await screenshot(page, "06-external-accounts.png");

  await tab(page, "站点资源绑定").click();
  const bindings = page.getByRole("table", { name: "站点资源绑定", exact: true });
  const siteRow = (name: string) => bindings.getByRole("row").filter({ hasText: name });
  for (const provider of ["gsc", "ga4", "bing"] as const) {
    for (const site of registered) await bindings.getByRole("checkbox", { name: `选择 ${site.name}` }).check();
    await page.getByRole("button", { name: /批量绑定/ }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("搜索服务").selectOption(provider);
    await dialog
      .getByLabel("已授权账户")
      .selectOption({ label: provider === "bing" ? "共享 Bing 账户" : "共享 Google 账户" });
    await dialog.getByRole("button", { name: "查找资源" }).click();
    if (provider === "ga4") await dialog.getByRole("button", { name: "加载下一页资源" }).click();
    for (const site of registered) {
      const resource = resourceID(provider, site);
      const select = dialog.getByLabel(`资源 · ${site.name}`);
      await expect(select.locator("option").filter({ hasText: resource })).toHaveCount(1);
      await select.selectOption(resource);
    }
    await dialog.getByRole("button", { name: "确认绑定" }).click();
    await expect(dialog).toBeHidden();
    for (const site of registered) {
      const resource = resourceID(provider, site);
      await expect(siteRow(site.name).getByText(resource, { exact: true })).toBeVisible();
    }
  }
  await expect(bindings.getByText("America/New_York", { exact: true })).toHaveCount(2);
  const ga = (await checked(await request.get(`${base}/integrations/bindings?provider=ga4`, { headers }))).items;
  expect(ga.map((item: { resource: { time_zone: string } }) => item.resource.time_zone)).toEqual([
    "America/New_York",
    "America/New_York",
  ]);
  await screenshot(page, "02-site-bindings.png");

  const day = new Date(Date.now() - 3 * 86_400_000).toISOString().slice(0, 10);
  const first = registered[0];
  await runJob({ kind: "gsc_sync", site_id: first.id, from: day, to: day });
  await runJob({ kind: "gsc_sync", site_id: first.id, from: day, to: day, search_type: "discover", data_state: "all" });
  await runJob({
    kind: "url_inspection",
    provider: "gsc",
    site_id: first.id,
    url: `https://${first.host}/existing`,
  });
  const report = await runJob({ kind: "ga4_sync", site_id: first.id, from: day, to: day });
  expect(report.result.report_quality).toMatchObject({
    time_zone: "America/New_York",
    empty: true,
    sampled: true,
    thresholded: true,
    data_loss_from_other_row: true,
  });
  await page.goto(`/seo/execution/ga4?site_id=${first.id}`);
  await expect(page.getByRole("heading", { name: "最近报表的数据质量" })).toBeVisible();
  await expect(page.getByText("存在抽样", { exact: true })).toBeVisible();
  await page.goto(`${settings}?tab=bindings`);
  const snapshots = (
    await checked(
      await request.get(
        `${base}/execution/snapshots?site_id=${first.id}&kind=gsc&search_type=discover&data_state=all`,
        { headers },
      ),
    )
  ).items;
  expect(snapshots).toHaveLength(1);
  expect(snapshots[0]).toMatchObject({
    search_type: "discover",
    data_state: "all",
    freshness: "preliminary",
    first_incomplete_date: day,
  });

  await tab(page, "IndexNow 密钥管理").click();
  const keys = page.getByRole("table", { name: "IndexNow 密钥", exact: true });
  for (const site of registered) {
    await page.getByRole("button", { name: "添加站点密钥" }).click();
    const dialog = page.getByRole("dialog", { name: "添加站点密钥" });
    await dialog.getByLabel("站点", { exact: true }).selectOption(site.id);
    await dialog.getByLabel("验证目录").fill(site.directory);
    await dialog.getByLabel("自定义密钥").fill(site.key);
    await dialog.getByRole("checkbox", { name: "允许提交 URL" }).check();
    await dialog.getByRole("button", { name: "生成或保存" }).click();
    await expect(dialog).toBeHidden();
    const row = keys.getByRole("row").filter({ hasText: site.name });
    await expect(row).toBeVisible();
    await expect
      .poll(
        async () => {
          await page.getByRole("button", { name: "刷新状态" }).click();
          return row.textContent();
        },
        { timeout: 15_000 },
      )
      .toContain("已验证");
    const file = await row.getByRole("link", { name: "下载 TXT" }).getAttribute("href");
    if (!file) throw new Error("Missing IndexNow download.");
    const downloaded = await page.request.get(file);
    expect(downloaded.status()).toBe(200);
    expect(downloaded.headers()["content-type"]).toContain("text/plain");
    expect(await downloaded.text()).toBe(site.key);
    const urls = Array.from(
      { length: site.directory === "/" ? 1_000 : 2 },
      (_, index) => `https://${site.host}${site.directory}trial-${index}`,
    );
    const job = await runJob({
      kind: "indexnow_submit",
      site_id: site.id,
      urls,
    });
    const submissions = (
      await checked(await request.get(`${base}/execution/submissions?site_id=${site.id}`, { headers }))
    ).items;
    expect(submissions[0]).toMatchObject({
      batch_id: job.id,
      http_status: site.directory === "/" ? 202 : 200,
      integration_status: site.directory === "/" ? "ACCEPTED_PENDING_VERIFICATION" : "SUBMITTED",
      requested_at: expect.any(String),
    });
    if (site.directory === "/") {
      const tooMany = [...urls, `https://${site.host}/over-limit`];
      await row.getByRole("button", { name: "提交 URL", exact: true }).click();
      const batchDialog = page.getByRole("dialog", { name: "提交 IndexNow URL" });
      await batchDialog.getByLabel("提交 URL").fill(tooMany.join("\n"));
      await batchDialog.getByRole("button", { name: "提交索引请求" }).click();
      await expect(batchDialog.getByRole("alert")).toContainText("提交的 URL 数量超出");
      await batchDialog.getByRole("button", { name: "取消", exact: true }).click();
      const rejected = await request.post(`${base}/execution/jobs`, {
        headers,
        data: {
          idempotency_key: crypto.randomUUID(),
          kind: "indexnow_submit",
          site_id: site.id,
          urls: tooMany,
        },
      });
      expect(rejected.status()).toBe(400);
      const repeated = await runJob({ kind: "indexnow_submit", site_id: site.id, urls: [urls[0]] }, "failed");
      expect(repeated.error_code).toBe("execution_indexing_recently_submitted");
    }
  }
  await screenshot(page, "03-indexnow-verified.png");
  await tab(page, "同步与健康监控").click();
  await expect(page.getByRole("table", { name: "连接健康", exact: true })).toContainText("正常");
  await screenshot(page, "04-health.png");
  await tab(page, "集成操作历史").click();
  await expect(page.getByRole("table", { name: "集成操作历史", exact: true })).toContainText("indexnow_submit");
  await expect(page.getByRole("table", { name: "集成操作历史", exact: true })).toContainText(
    "ACCEPTED_PENDING_VERIFICATION",
  );
  await page.goBack();
  await expect(tab(page, "同步与健康监控")).toHaveAttribute("aria-current", "page");
  await page.reload();
  await expect(tab(page, "同步与健康监控")).toHaveAttribute("aria-current", "page");
  await tab(page, "站点资源绑定").click();
  const firstBing = siteRow(first.name)
    .getByRole("cell")
    .filter({ has: page.getByText(`https://${first.host}/`, { exact: true }) });
  await firstBing.getByRole("button", { name: "解除", exact: true }).click();
  const remove = page.getByRole("alertdialog", { name: "解除站点绑定" });
  await remove.getByRole("button", { name: "确认", exact: true }).click();
  await expect(firstBing).toHaveCount(0);
  await expect(siteRow(registered[1].name).getByText(`https://${registered[1].host}/`, { exact: true })).toBeVisible();
  const remaining = (await checked(await request.get(`${base}/integrations/bindings?provider=bing`, { headers })))
    .items;
  expect(remaining).toHaveLength(1);
  expect(remaining[0].site_id).toBe(registered[1].id);

  await tab(page, "搜索与分析集成").click();
  await expect(tab(page, "搜索与分析集成")).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("heading", { name: "Google Search Console" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme-mode", "light");
  await screenshot(page, "01-desktop-overview.png");
  await page.getByRole("button", { name: /^Current theme: light/ }).click();
  await expect(page.locator("html")).toHaveClass(/\bdark\b/);
  await screenshot(page, "07-desktop-dark.png");
  const mobile = await browser.newContext({
    baseURL,
    viewport: { width: 390, height: 844 },
    isMobile: true,
    colorScheme: "dark",
    storageState: await page.context().storageState(),
  });
  try {
    const device = await mobile.newPage();
    await device.goto(settings);
    await expect(tab(device, "搜索与分析集成")).toHaveAttribute("aria-current", "page");
    await expect(device.getByRole("heading", { name: "Google Search Console" })).toBeVisible();
    if (!(await device.locator("html").getAttribute("class"))?.split(" ").includes("dark")) {
      await device.getByRole("button", { name: /^Current theme: light/ }).click();
    }
    await expect(device.locator("html")).toHaveClass(/\bdark\b/);
    expect(await device.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
    await screenshot(device, "05-mobile-overview.png");
  } finally {
    await mobile.close();
  }
  const { token: viewerToken } = await checked(
    await request.post(`${base}/members`, {
      headers,
      data: { role: "viewer", email: "search-integrations-viewer@example.test" },
    }),
  );
  const viewer = await browser.newPage({ baseURL });
  try {
    await signIn(viewer, viewerToken);
    await viewer.goto(`${settings}?tab=indexnow&site_id=${first.id}`);
    await expect(viewer.getByRole("table", { name: "IndexNow 密钥", exact: true })).toContainText("已验证");
    await expect(tab(viewer, "OAuth 应用配置")).toHaveCount(0);
    await expect(viewer.getByRole("link", { name: "下载 TXT", exact: true })).toHaveCount(0);
    expect(
      (
        await request.get(`${base}/integrations/accounts`, {
          headers: { Authorization: `Bearer ${viewerToken}` },
        })
      ).status(),
    ).toBe(403);
  } finally {
    await viewer.close();
  }
  expect(errors).toEqual([]);
});
