import { type APIResponse, expect, type Page, test } from "@playwright/test";

import { executionEnabled, executionScreenshot, runExecutionFlow } from "./execution-flow";
import { randomUUID } from "node:crypto";
import { mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";

test.skip(
  process.env.SEOOS_E2E_CONTENT !== "1" && !executionEnabled,
  "Requires the Go API, PostgreSQL, and authorized loopback content fixtures.",
);
test.use({ viewport: { width: 1440, height: 900 } });

const siteURL = "https://phase3.example.com";
const siteName = "内容验收站点";
const keyword = "robot vacuum maintenance";
const source = "https://official.example.com/robot-vacuum-guide";
const market = {
  country_code: "US",
  location_code: 2840,
  language_code: "en",
  search_engine: "google",
  device: "desktop",
};
const writer = "内容验收写作模型";
const reviewer = "内容验收审核模型";

async function signIn(page: Page, token: string) {
  await page.goto("/seo/login");
  await page.getByLabel("访问凭据", { exact: true }).fill(token);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "运营仪表盘", exact: true })).toBeVisible();
}

async function openSection(page: Page, group: string, name: string) {
  const menu = page.locator('[data-slot="sidebar-content"]');
  const groupButton = menu.getByRole("button", { name: group, exact: true });
  if (!(await groupButton.isVisible())) {
    await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  }
  if ((await groupButton.getAttribute("aria-expanded")) !== "true") await groupButton.click();
  await menu.getByRole("link", { name, exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name, exact: true })).toBeVisible();
}

async function screenshot(page: Page, name: string) {
  const output = process.env.SEOOS_E2E_CONTENT_SCREENSHOTS;
  if (!output) return;
  await mkdir(output, { recursive: true });
  await expect(page.locator("[data-sonner-toast]").filter({ visible: true })).toHaveCount(0, { timeout: 8000 });
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await page.screenshot({ path: join(output, name), animations: "disabled" });
}

async function read(response: APIResponse, label: string) {
  const value = await response.json();
  expect(response.ok(), `${label}: ${value.error?.code ?? response.status()}`).toBe(true);
  return value;
}

test("content studio produces an evidenced, versioned and separately approved CMS package", async ({
  page,
  request,
  browser,
}) => {
  test.setTimeout(executionEnabled ? 240_000 : 180_000);
  const api = process.env.SEOOS_API_ORIGIN;
  const token = process.env.SEOOS_E2E_TOKEN;
  if (!api || !token)
    throw new Error("Run the content acceptance suite with the real local SEO API and fixture driver.");
  const base = `${api}/api/v1`;
  const headers = { Authorization: `Bearer ${token}` };
  const errors: string[] = [];
  page.on("pageerror", (failure) => errors.push(failure.message));

  async function finish(id: string, label: string, expectedError?: string) {
    let job: { state: string; error_code?: string } | undefined;
    await expect
      .poll(
        async () => {
          const response = await request.get(`${base}/jobs/${id}`, { headers });
          ({ job } = await read(response, label));
          return job?.state;
        },
        { timeout: 25_000 },
      )
      .toMatch(/^(succeeded|failed|cancelled)$/);
    expect(job?.state, `${label}: ${job?.error_code ?? "no error code"}`).toBe(expectedError ? "failed" : "succeeded");
    if (expectedError) expect(job?.error_code).toBe(expectedError);
  }

  const created = await read(
    await request.post(`${base}/sites`, {
      headers,
      data: { name: siteName, base_url: siteURL, cms: "generic", country_code: "US", language_tag: "en-US" },
    }),
    "content test site",
  );
  const siteID: string = created.site.id;
  const secondSite = await read(
    await request.post(`${base}/sites`, {
      headers,
      data: {
        name: "内容上下文验收站点",
        base_url: "https://content-context.example.com",
        cms: "generic",
        country_code: "US",
        language_tag: "en-US",
      },
    }),
    "second site for project isolation",
  );
  const secondSiteID: string = secondSite.site.id;
  const provider = await read(
    await request.post(`${base}/providers`, {
      headers,
      data: {
        kind: "dataforseo",
        name: "内容验收研究数据",
        enabled: true,
        requests_per_day: 200,
        requests_per_minute: 12,
        timeout_seconds: 5,
        login: "phase-two-fixture@example.test",
        password: "test-fixture-password-never-used-live",
        daily_cost_micros: 5_000_000,
      },
    }),
    "local research provider",
  );
  for (const [kind, name, model] of [
    ["openai", writer, "fixture-writer"],
    ["gemini", reviewer, "fixture-reviewer"],
  ]) {
    await read(
      await request.post(`${base}/providers`, {
        headers,
        data: {
          kind,
          name,
          model,
          api_key: "phase3-model-fixture-token",
          timeout_seconds: 5,
          requests_per_minute: 12,
          requests_per_day: 200,
          max_output_tokens: 2048,
          enabled: true,
        },
      }),
      name,
    );
  }
  for (const kind of ["keyword_metrics", "serp_analysis", "keyword_intent", "keyword_url_mapping"]) {
    const reply = await read(
      await request.post(`${base}/research/jobs`, {
        headers,
        data: {
          site_id: siteID,
          keyword,
          market,
          kind,
          priority: 10,
          idempotency_key: randomUUID(),
          provider_id: ["keyword_metrics", "serp_analysis"].includes(kind) ? provider.provider.id : undefined,
        },
      }),
      kind,
    );
    await finish(reply.job.id, kind);
  }

  await signIn(page, token);
  await openSection(page, "SEO 研究", "关键词研究");
  await screenshot(page, "phase3-research-entry.png");
  await openSection(page, "AI 与系统", "大模型服务商");
  for (const [name, role] of [
    [writer, "writer"],
    [reviewer, "reviewer"],
  ]) {
    await page.getByRole("button", { name: `配置 ${name} 内容模型`, exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "配置内容模型", exact: true });
    await dialog.getByLabel("模型角色", { exact: true }).selectOption(role);
    await dialog.getByRole("button", { name: "保存模型配置", exact: true }).click();
    await expect(dialog).toBeHidden();
  }

  const openContent = (name: string) => openSection(page, "内容中心", name);
  await openContent("内容项目");
  await page.getByLabel("内容站点", { exact: true }).selectOption(siteID);
  await expect(page).toHaveURL((url) => url.searchParams.get("site_id") === siteID);

  async function runStage(label: string, options: { model?: string; url?: string; expectedError?: string } = {}) {
    await page.getByRole("button", { name: label, exact: true }).click();
    const dialog = page.getByRole("dialog", { name: label, exact: true });
    if (options.model) await dialog.getByLabel("模型 Provider", { exact: true }).selectOption({ label: options.model });
    if (options.url) await dialog.getByLabel("页面 URL", { exact: true }).fill(options.url);
    const submit = page.waitForResponse(
      (reply) => new URL(reply.url()).pathname === "/seo/api/content/jobs" && reply.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: "提交任务", exact: true }).click();
    const accepted = await submit;
    const payload = await accepted.json();
    expect(accepted.ok(), `${label}: ${payload.error?.code ?? accepted.status()}`).toBe(true);
    await finish(payload.job.id, label, options.expectedError);
    await expect(dialog).toBeHidden();
  }

  await openSection(page, "站点管理", "内容资产清单");
  await page.getByRole("button", { name: "配置只读采集", exact: true }).click();
  const crawl = page.getByRole("dialog", { name: "配置只读采集", exact: true });
  await crawl.getByRole("switch", { name: "我授权只读访问本站和下列来源" }).click();
  await crawl.getByLabel("Sitemap URL").fill(`${siteURL}/sitemap.xml`);
  await crawl.getByLabel("一手来源域名（每行一个源）").fill("https://official.example.com");
  await crawl.getByRole("button", { name: "保存采集授权" }).click();
  await expect(page.getByText("已授权只读访问", { exact: true })).toBeVisible();
  await runStage("发现站点页面");
  await expect
    .poll(
      async () => {
        const rows = await read(
          await request.get(`${base}/content/inventory?site_id=${siteID}`, { headers }),
          "site inventory",
        );
        const page = rows.items.find((item: { url: string }) => item.url === `${siteURL}/existing`);
        if (page?.http_status === 200) expect(page.index_status).not.toBe("indexed");
        return page?.http_status;
      },
      { timeout: 20_000 },
    )
    .toBe(200);
  await page.getByRole("button", { name: "刷新清单", exact: true }).click();
  await expect(page.getByRole("table", { name: "已发现页面" })).toContainText(`${siteURL}/existing`);
  await screenshot(page, "phase3-site-inventory.png");

  await page.getByRole("button", { name: "创建内容项目", exact: true }).click();
  const project = page.getByRole("dialog", { name: "创建内容项目", exact: true });
  await project.getByLabel("主关键词").fill(keyword);
  await project.getByLabel("国家或地区").fill("US");
  await project.getByLabel("语言代码").fill("en");
  await project.getByLabel("DataForSEO 地区代码").fill("2840");
  await project.getByRole("button", { name: "创建项目", exact: true }).click();
  await expect(project).toBeHidden();
  await expect(page.getByLabel("内容项目", { exact: true })).toBeVisible();
  const projectID = await page.getByLabel("内容项目", { exact: true }).inputValue();
  const atProject = (path: string) => (url: URL) =>
    url.pathname === path &&
    url.searchParams.get("site_id") === siteID &&
    url.searchParams.get("project_id") === projectID;
  await expect(page).toHaveURL(atProject("/seo/content/project"));
  await openContent("内容项目");
  const projectList = page.getByRole("table", { name: "内容项目列表", exact: true });
  await expect(projectList).toContainText(keyword);
  await screenshot(page, "phase3-content-projects.png");
  await projectList.getByRole("link", { name: keyword, exact: true }).click();
  await expect(page).toHaveURL(atProject("/seo/content/project"));
  const fetchWorkspace = async () =>
    (await read(await request.get(`${base}/content/projects/${projectID}`, { headers }), "content workspace"))
      .workspace;

  await openContent("内容策略");
  await expect(page).toHaveURL(atProject("/seo/content/strategy"));
  await page.goBack();
  await expect(page).toHaveURL(atProject("/seo/content/project"));
  await page.goForward();
  await expect(page).toHaveURL(atProject("/seo/content/strategy"));
  await page.reload();
  await expect(page.getByLabel("内容站点", { exact: true })).toHaveValue(siteID);
  await expect(page.getByLabel("内容项目", { exact: true })).toHaveValue(projectID);
  await runStage("生成内容策略");
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByText("决策理由", { exact: true })).toBeVisible();
  expect((await fetchWorkspace()).evidence.serp_snapshot_id).toBeTruthy();
  await screenshot(page, "phase3-content-strategy.png");

  await openContent("内容简报");
  await runStage("生成内容简报");
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByLabel("主关键词", { exact: true })).toHaveValue(keyword);
  await openContent("研究与资料来源");
  await page.getByRole("button", { name: "登记一手来源", exact: true }).click();
  const sourceDialog = page.getByRole("dialog", { name: "登记一手来源", exact: true });
  await sourceDialog.getByLabel("来源 URL").fill(source);
  await sourceDialog.getByRole("button", { name: "登记来源", exact: true }).click();
  await expect(sourceDialog).toBeHidden();
  await runStage("研究一手资料");
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByText(/every 90 days/i).first()).toBeVisible();
  await screenshot(page, "phase3-source-evidence.png");

  await openContent("文章编辑");
  await runStage("生成文章大纲", { model: `${writer} · fixture-writer` });
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByRole("list", { name: "文章大纲" })).toBeVisible();
  await runStage("撰写文章章节", { model: `${writer} · fixture-writer` });
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByLabel("章节 1 正文", { exact: true })).toBeVisible();
  expect((await fetchWorkspace()).article.sections.length).toBeGreaterThan(1);
  await page.getByLabel("作者", { exact: true }).fill("ARTEX Editorial Desk");
  await page.getByRole("button", { name: "保存文章版本", exact: true }).click();
  await expect.poll(async () => (await fetchWorkspace()).article.author).toBe("ARTEX Editorial Desk");
  await runStage("优化文章 SEO");
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await screenshot(page, "phase3-article-editor.png");

  await openContent("事实核验");
  await runStage("核验文章事实");
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByText("事实核验通过", { exact: true })).toBeVisible();
  await expect(page.getByRole("table", { name: "事实核验结果", exact: true })).toContainText(/90 days/i);

  await openContent("质量审核");
  await runStage("独立质量审核", { model: `${reviewer} · fixture-reviewer` });
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByText("已通过质量门禁", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "预览 CMS 发布包", exact: true })).toHaveCount(0);
  expect((await fetchWorkspace()).publications ?? []).toHaveLength(0);
  await screenshot(page, "phase3-quality-review.png");
  await page.goto(`/seo/dashboard/inbox?site_id=${siteID}&project_id=${projectID}`);
  const pending = page.getByRole("table", { name: "待审核文章", exact: true });
  await expect(pending).toContainText(keyword);
  await pending.getByRole("link", { name: "打开质量审核", exact: true }).click();
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === "/seo/content/quality" &&
      url.searchParams.get("site_id") === siteID &&
      url.searchParams.get("project_id") === projectID,
  );
  await page.getByRole("button", { name: "批准当前文章", exact: true }).click();
  const approval = page.getByRole("dialog", { name: "批准当前文章", exact: true });
  await approval.getByLabel("审批意见", { exact: true }).fill("已核对本地采集证据与文章版本。");
  await approval.getByRole("button", { name: "批准文章", exact: true }).click();
  await expect(approval).toBeHidden();

  await page.getByRole("button", { name: "预览 CMS 发布包", exact: true }).click();
  const publication = page.getByRole("dialog", { name: "发布前最终预览", exact: true });
  await expect(publication.getByRole("article")).toBeVisible();
  await publication.getByRole("button", { name: "准备 CMS 发布包", exact: true }).click();
  await expect(publication).toBeHidden();
  await openContent("版本管理");
  await expect(page.getByRole("table", { name: "CMS 发布记录" })).toContainText("已准备");
  await page.getByRole("button", { name: "比较版本", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "比较内容版本" })).toBeVisible();
  await page.keyboard.press("Escape");
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "下载 CMS 发布包", exact: true }).click();
  const downloaded = await download;
  expect(downloaded.suggestedFilename()).toMatch(/^content-v\d+\.json$/u);
  const pack = JSON.parse(await readFile(await downloaded.path(), "utf-8"));
  expect(pack.article.language_code).toBe("en");
  expect(pack.sources?.some((item: { url: string }) => item.url === source)).toBe(true);

  if (process.env.SEOOS_E2E_CONTENT_FIXTURE) {
    const fixture = new URL(process.env.SEOOS_E2E_CONTENT_FIXTURE);
    expect(fixture.protocol).toBe("http:");
    expect(fixture.hostname).toBe("127.0.0.1");
    const url = `${siteURL}/existing`;
    await runStage("验证发布页面", { url, expectedError: "publication_page_mismatch" });
    expect((await fetchWorkspace()).publications[0].status).toBe("prepared");
    const published = await request.post(new URL("/test/published", fixture).href, { data: { article: pack.article } });
    expect(published.ok()).toBe(true);
    await runStage("验证发布页面", { url });
    await page.getByRole("button", { name: "刷新项目", exact: true }).click();
    await expect(page.getByRole("table", { name: "CMS 发布记录" })).toContainText("已核实");
    expect((await fetchWorkspace()).publications[0].published_url).toBe(url);
  }
  await screenshot(page, "phase3-version-publication.png");

  await page.goto(`/seo/content?site=${siteID}&project=${projectID}&view=refresh`);
  await expect(page).toHaveURL(atProject("/seo/content/refresh"));
  await page.getByRole("button", { name: "登记更新信号", exact: true }).click();
  const signals = page.getByRole("dialog", { name: "登记更新信号", exact: true });
  await signals.getByLabel("流量变动（%）", { exact: true }).fill("-25");
  await signals.getByLabel("证据 URL", { exact: true }).fill(source);
  await signals.getByRole("button", { name: "保存更新信号", exact: true }).click();
  await expect(signals).toBeHidden();
  await runStage("分析更新机会");
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  await expect(page.getByText("建议依据", { exact: true })).toBeVisible();
  await expect(page.getByRole("table", { name: "内容任务执行记录", exact: true })).toContainText("独立质量审核");

  const usage = await read(await request.get(`${base}/content/usage`, { headers }), "owner model usage");
  expect(usage.items.find((item: { name: string }) => item.name === writer)?.used_tokens).toBeGreaterThan(0);
  expect(usage.items.find((item: { name: string }) => item.name === reviewer)?.spent_micros).toBeGreaterThan(0);

  const member = await read(
    await request.post(`${base}/members`, {
      headers,
      data: { email: "content-viewer@example.test", role: "viewer" },
    }),
    "viewer credential",
  );
  const viewer = await browser.newPage({ baseURL: page.url() });
  try {
    await signIn(viewer, member.token);
    await viewer.goto(`/seo/content?site_id=${siteID}`);
    await expect(viewer.getByRole("heading", { level: 1, name: "内容项目", exact: true })).toBeVisible();
    await expect(viewer.getByRole("button", { name: "创建内容项目", exact: true })).toHaveCount(0);
    await viewer.goto(`/seo/content/article?site_id=${siteID}&project_id=${projectID}`);
    await expect(viewer).toHaveURL(atProject("/seo/content/article"));
    await expect(viewer.getByRole("article")).toBeVisible();
    await expect(viewer.getByRole("button", { name: "保存文章版本", exact: true })).toHaveCount(0);
    const denied = await request.post(`${base}/content/projects/${projectID}/actions`, {
      headers: { Authorization: `Bearer ${member.token}` },
      data: { action: "prepare_publication", revision: (await fetchWorkspace()).project.revision },
    });
    expect(denied.status()).toBe(403);
  } finally {
    await viewer.close();
  }

  if (executionEnabled) {
    const packageID = (await fetchWorkspace()).publications[0].id;
    await runExecutionFlow({ page, request, base, headers, siteID, siteURL, packageID });
  }

  await page.goto(`/seo/content/article?site_id=${secondSiteID}&project_id=${projectID}`);
  await expect(page.getByText("当前站点无法读取所选项目", { exact: true })).toBeVisible();
  await expect(page.getByLabel("章节 1 正文", { exact: true })).toHaveCount(0);
  await page.getByLabel("内容站点", { exact: true }).selectOption(siteID);
  await expect(page).toHaveURL(
    (url) => url.searchParams.get("site_id") === siteID && !url.searchParams.has("project_id"),
  );
  await expect(page.getByLabel("内容项目", { exact: true })).toHaveValue(projectID);
  await expect(page.getByLabel("章节 1 正文", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: /^Current theme: light/u }).click();
  await expect(page.locator("html")).toHaveClass(/\bdark\b/u);
  await page
    .getByRole("button")
    .filter({ has: page.locator("svg.lucide-settings") })
    .click();
  const preferences = page.locator('[data-slot="popover-content"]');
  await preferences.getByRole("combobox").first().click();
  await page.getByRole("option", { name: "Tangerine", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme-preset", "tangerine");
  await page.keyboard.press("Escape");
  await page.setViewportSize({ width: 390, height: 844 });
  await openContent("内容项目");
  await expect(page.locator('[data-slot="sidebar"][data-mobile="true"]')).toBeHidden();
  await expect(page.getByRole("heading", { level: 1, name: "内容项目", exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await screenshot(page, "phase3-mobile-dark.png");
  if (executionEnabled) {
    await page.goto(`/seo/sites/connections?site_id=${siteID}`);
    await expect(page.getByRole("heading", { level: 1, name: "CMS 连接", exact: true })).toBeVisible();
    await expect(page.getByRole("table", { name: "站点连接", exact: true })).toContainText("Phase 4 通用 CMS 验收");
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
    await executionScreenshot(page, "phase4-mobile-dark.png");
  }
  expect(errors).toEqual([]);
});
