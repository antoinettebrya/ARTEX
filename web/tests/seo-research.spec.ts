import { expect, type Locator, type Page, test } from "@playwright/test";

import { mkdir } from "node:fs/promises";
import { join } from "node:path";

test.skip(process.env.SEOOS_E2E_RESEARCH !== "1", "Requires the real SEO service and its loopback DataForSEO fixture");
test.use({ viewport: { width: 1440, height: 900 } });

const keyword = "robot vacuum";
const related = "best robot vacuum";
const siteName = "研究验收站点";
const providerName = "本地研究 Provider";
const ownPage = "https://research.example.test/reviews/robot-vacuums";
const competitorPage = "https://competitor.example.com/reviews/robot-vacuums";

async function signIn(page: Page, token: string) {
  await page.goto("/seo/login");
  await page.getByLabel("访问凭据", { exact: true }).fill(token);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "运营仪表盘", level: 1 })).toBeVisible();
}

async function openNavigation(page: Page, group: string, name: string) {
  const sidebar = page.locator('[data-sidebar="sidebar"]:visible');
  const groups: Record<string, { title: string; parent: string } | undefined> = {
    关键词探索: { title: "研究工作流", parent: "关键词研究" },
    研究历史: { title: "研究工作流", parent: "关键词研究" },
    数据费用: { title: "研究工作流", parent: "关键词研究" },
    竞争页面: { title: "竞争研究", parent: "SERP 竞争分析" },
  };
  if (name === "关键词探索") {
    const explorer = page
      .getByRole("navigation", { name: "竞争研究", exact: true })
      .getByRole("link", { name, exact: true });
    if (await explorer.isVisible()) {
      await explorer.click();
      return;
    }
  }
  const relatedGroup = groups[name];
  if (relatedGroup) {
    const related = page.getByRole("navigation", { name: relatedGroup.title, exact: true });
    if (!(await related.isVisible())) await openNavigation(page, "SEO 研究", relatedGroup.parent);
    await related.getByRole("link", { name, exact: true }).click();
    return;
  }
  const target = sidebar.getByRole("link", { name, exact: true });
  if (!(await target.isVisible())) await sidebar.getByRole("button", { name: group, exact: true }).click();
  await target.click();
}

async function market(dialog: Locator, device = "desktop") {
  await dialog.getByLabel("国家或地区").fill("US");
  await dialog.getByLabel("语言代码").fill("en");
  await dialog.getByLabel("DataForSEO 地区代码").fill("2840");
  await dialog.getByLabel("搜索引擎").selectOption("google");
  await dialog.getByLabel("设备").selectOption(device);
}

async function screenshot(page: Page, filename: string) {
  const directory = process.env.SEOOS_E2E_RESEARCH_SCREENSHOTS;
  if (!directory) return;
  await mkdir(directory, { recursive: true });
  await expect(page.locator("[data-sonner-toast]").filter({ visible: true })).toHaveCount(0, { timeout: 8000 });
  await page.screenshot({ path: join(directory, filename), animations: "disabled" });
}

test("keyword research uses the ARTEX shell and persists actual worker results", async ({ page, request, browser }) => {
  test.setTimeout(150_000);
  const token = process.env.SEOOS_E2E_TOKEN;
  const api = process.env.SEOOS_API_ORIGIN;
  if (!token || !api) throw new Error("Run the SEO E2E driver with an isolated PostgreSQL database");
  const auth = { Authorization: `Bearer ${token}` };
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));

  async function view(name: string, table = name) {
    await openNavigation(page, "SEO 研究", name);
    await expect(page.getByRole("heading", { name, level: 1, exact: true })).toBeVisible();
    return page.getByRole("table", { name: table, exact: true });
  }

  async function createTask(
    kind: string,
    paid: boolean,
    options: { device?: string; value?: string; term?: string } = {},
  ) {
    await page.getByRole("button", { name: "创建研究任务", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "创建研究任务" });
    await dialog.getByLabel("研究任务类型").selectOption(kind);
    await dialog.getByLabel("关键词", { exact: true }).fill(options.term ?? keyword);
    await market(dialog, options.device);
    if (paid) await dialog.getByLabel("DataForSEO Provider").selectOption({ label: providerName });
    if (options.value) await dialog.getByLabel("商业价值（0–10，可选）").fill(options.value);
    return submit(dialog);
  }

  async function submit(dialog: Locator) {
    const created = page.waitForResponse(
      (reply) => new URL(reply.url()).pathname === "/seo/api/research/jobs" && reply.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: "提交研究任务" }).click();
    const reply = await created;
    const body = await reply.json();
    expect(reply.ok(), body.error?.code).toBe(true);
    let completed: { state: string; error_code?: string; result?: { cache_hits?: number } } | undefined;
    await expect
      .poll(
        async () => {
          const result = await request.get(`${api}/api/v1/jobs/${body.job.id}`, { headers: auth });
          completed = (await result.json()).job;
          return completed?.state ?? "unavailable";
        },
        { timeout: 12_000 },
      )
      .toMatch(/^(succeeded|failed|cancelled)$/);
    expect(completed?.state, completed?.error_code).toBe("succeeded");
    await expect(page).toHaveURL((url) => url.pathname === "/seo/research/history" && url.searchParams.has("site_id"));
    return completed;
  }

  await signIn(page, token);
  await openNavigation(page, "站点管理", "全部站点");
  await page.getByRole("button", { name: "添加站点" }).click();
  const site = page.getByRole("dialog", { name: "添加站点" });
  await site.getByLabel("站点名称").fill(siteName);
  await site.getByLabel("站点 URL").fill("https://research.example.test");
  await site.getByLabel("国家或地区").fill("US");
  await site.getByLabel("语言标签").fill("en-US");
  await site.getByRole("button", { name: "保存站点" }).click();
  await expect(page.getByRole("link", { name: siteName, exact: true })).toBeVisible();

  await openNavigation(page, "AI 与系统", "大模型服务商");
  await page.getByRole("tab", { name: "DataForSEO 数据源" }).click();
  await page.getByRole("button", { name: "添加DataForSEO" }).click();
  const provider = page.getByRole("dialog", { name: "添加DataForSEO" });
  await expect(provider.getByLabel("模型厂商")).toHaveCount(0);
  await provider.getByLabel("名称").fill(providerName);
  await provider.getByLabel("DataForSEO 登录名").fill("phase-two-fixture@example.test");
  await provider.getByLabel("DataForSEO API 密码").fill("test-fixture-password-never-used-live");
  await provider.getByLabel("每日费用上限（USD）").fill("10");
  await provider.getByLabel("每分钟请求上限").fill("12");
  await provider.getByRole("button", { name: "保存配置" }).click();
  const providerRecord = page.getByRole("row").filter({ hasText: providerName });
  await expect(providerRecord).toContainText("DataForSEO");
  await expect(providerRecord).not.toContainText("test-fixture-password");
  await providerRecord.getByRole("button", { name: "测试连接", exact: true }).click();
  await expect(providerRecord.getByRole("status")).toHaveText(/连接成功（\d+ ms）/);
  await screenshot(page, "phase2-dataforseo-provider.png");

  await openNavigation(page, "SEO 研究", "关键词研究");
  await page.getByLabel("研究站点").selectOption({ label: siteName });
  const siteID = await page.getByLabel("研究站点").inputValue();
  await createTask("keyword_discovery", true);
  const suggestions = await createTask("keyword_suggestions", true);
  expect(suggestions?.result?.cache_hits).toBe(1);
  await createTask("keyword_metrics", true);
  await expect(await view("关键词研究", "种子关键词")).toContainText(keyword);
  await expect(await view("关键词拓展", "关键词建议")).toContainText(related);
  await screenshot(page, "phase2-research-dashboard.png");
  const explorer = await view("关键词探索");
  await page.getByRole("searchbox", { name: "筛选关键词" }).fill(keyword);
  await page.getByRole("button", { name: "筛选关键词", exact: true }).click();
  await expect(page).toHaveURL(
    (url) => url.pathname === "/seo/research/keywords" && url.searchParams.get("q") === keyword,
  );
  await page.reload();
  await expect(page.getByRole("searchbox", { name: "筛选关键词" })).toHaveValue(keyword);
  const measured = explorer
    .getByRole("row")
    .filter({ has: page.getByText(keyword, { exact: true }) })
    .first();
  await expect(measured).toContainText("指标覆盖全部设备");
  await measured.getByRole("button", { name: "原始响应" }).click();
  const snapshot = page.getByRole("dialog", { name: "Provider 原始响应" });
  await expect(snapshot.locator("pre")).toContainText(keyword);
  await expect(snapshot.locator("pre")).not.toContainText("test-fixture-password");
  await snapshot.getByRole("button", { name: "关闭", exact: true }).click();
  expect(await explorer.evaluate((table) => table.scrollWidth <= (table.parentElement?.clientWidth ?? 0) + 1)).toBe(
    true,
  );
  await screenshot(page, "phase2-keyword-explorer.png");
  const resumeResearch = page.url();
  await measured.getByRole("link", { name: "用于内容项目", exact: true }).click();
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === "/seo/content" &&
      url.searchParams.get("site_id") === siteID &&
      url.searchParams.get("keyword") === keyword &&
      url.searchParams.get("location_code") === "2840",
  );
  await page.getByRole("button", { name: "创建内容项目", exact: true }).click();
  const newProject = page.getByRole("dialog", { name: "创建内容项目", exact: true });
  await expect(newProject.getByLabel("主关键词", { exact: true })).toHaveValue(keyword);
  await expect(newProject.getByLabel("DataForSEO 地区代码", { exact: true })).toHaveValue("2840");
  await newProject.getByRole("button", { name: "创建项目", exact: true }).click();
  await expect(page).toHaveURL((url) => url.pathname === "/seo/content/project" && url.searchParams.has("project_id"));
  const firstProject = new URL(page.url()).searchParams.get("project_id");
  const second = await request.post(`${api}/api/v1/content/projects`, {
    headers: auth,
    data: {
      site_id: siteID,
      keyword: related,
      market: {
        country_code: "US",
        language_code: "en",
        location_code: 2840,
        search_engine: "google",
        device: "desktop",
      },
    },
  });
  expect(second.status()).toBe(201);
  const { project: alternative } = await second.json();
  await page.getByRole("button", { name: "刷新项目", exact: true }).click();
  const projects = page.getByLabel("内容项目", { exact: true });
  await projects.selectOption(alternative.id);
  await expect(page).toHaveURL(
    (url) => url.searchParams.get("project_id") === alternative.id && url.searchParams.get("keyword") === related,
  );
  await page.reload();
  await expect(projects).toHaveValue(alternative.id);
  await page.goBack();
  await expect(projects).toHaveValue(firstProject ?? "");
  await page.goForward();
  await expect(projects).toHaveValue(alternative.id);
  await page.goto(resumeResearch);

  await createTask("serp_analysis", true);
  await createTask("serp_analysis", true, { device: "mobile" });
  await createTask("serp_analysis", true, { term: related });
  const serp = await view("SERP 竞争分析");
  await expect(serp.getByRole("row").filter({ hasText: "移动" })).toContainText(keyword);
  await page.getByRole("button", { name: "筛选市场", exact: true }).click();
  const filter = page.getByRole("dialog", { name: "筛选市场" });
  await market(filter, "mobile");
  await filter.getByRole("button", { name: "应用筛选" }).click();
  await expect(page).toHaveURL(
    (url) => url.pathname === "/seo/research/serp" && url.searchParams.get("device") === "mobile",
  );
  await expect(serp.getByRole("row").filter({ hasText: "移动" })).toContainText(keyword);
  await expect(serp.getByRole("row").filter({ hasText: "桌面" })).toHaveCount(0);
  await view("关键词探索");
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === "/seo/research/keywords" &&
      url.searchParams.get("site_id") === siteID &&
      url.searchParams.get("device") === "mobile" &&
      url.searchParams.get("q") === keyword,
  );
  await page.goBack();
  await expect(page.getByRole("heading", { name: "SERP 竞争分析", level: 1 })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("searchbox", { name: "筛选关键词" })).toHaveValue(keyword);
  await page.goBack();
  await page.reload();
  await expect(serp.getByRole("row").filter({ hasText: "移动" })).toContainText(keyword);
  await expect(serp.getByRole("row").filter({ hasText: "桌面" })).toHaveCount(0);
  await page.getByRole("button", { name: "清除市场筛选", exact: true }).click();
  await expect(page).toHaveURL((url) => !url.searchParams.has("device"));
  await expect(serp.getByRole("row").filter({ hasText: "桌面" }).first()).toBeVisible();
  await serp
    .getByRole("row")
    .filter({ has: page.getByText(keyword, { exact: true }) })
    .filter({ hasText: "桌面" })
    .getByRole("button", { name: "查看自然结果" })
    .click();
  const top = page.getByRole("dialog", { name: `${keyword} 的自然搜索结果` });
  await expect(top.getByRole("table", { name: "自然搜索结果" })).toContainText(ownPage);
  await expect(top.getByRole("table", { name: "自然搜索结果" }).locator("tbody tr")).toHaveCount(10);
  await expect(top.getByRole("row").filter({ hasText: ownPage }).getByRole("button", { name: "分析页面" })).toHaveCount(
    0,
  );
  const competitorResult = top.getByRole("row").filter({ hasText: competitorPage });
  await expect(competitorResult).toBeVisible();
  await screenshot(page, "phase2-serp-analysis.png");
  await competitorResult.getByRole("button", { name: "分析页面" }).click();
  const competitorTask = page.getByRole("dialog", { name: "创建研究任务" });
  await expect(competitorTask.getByLabel("已观测 SERP URL")).toHaveValue(competitorPage);
  await competitorTask.getByLabel("DataForSEO Provider").selectOption({ label: providerName });
  await submit(competitorTask);
  const competitor = await view("竞争页面");
  await expect(competitor).toContainText(competitorPage);
  await competitor.getByRole("button", { name: "查看页面证据" }).click();
  const analyzed = page.getByRole("dialog", { name: "Robot vacuum reviews" });
  await expect(analyzed.getByRole("table", { name: "正文分块" })).toContainText("Robot vacuum battery life");
  await expect(analyzed).toContainText("FAQPage");
  await expect(analyzed).toContainText("comparison");
  await expect(analyzed.getByRole("list", { name: "FAQ" })).toContainText("How long does it last?");
  await expect(analyzed.getByRole("button", { name: /^原始响应/ })).toHaveCount(3);
  await screenshot(page, "phase2-competitor-evidence.png");
  await analyzed.getByRole("button", { name: /原始响应.*结构化数据/ }).click();
  const schemaSnapshot = page.getByRole("dialog", { name: "Provider 原始响应" });
  await expect(schemaSnapshot.locator("pre")).toContainText("FAQPage");
  await schemaSnapshot.getByRole("button", { name: "关闭", exact: true }).click();
  await analyzed.getByRole("button", { name: "关闭", exact: true }).click();

  await createTask("keyword_intent", false);
  await expect(await view("搜索意图分析")).toContainText(keyword);
  await createTask("keyword_clustering", false);
  const clusters = await view("关键词聚类");
  await clusters
    .getByRole("row")
    .filter({ has: page.getByText(keyword, { exact: true }) })
    .getByRole("button", { name: "聚类依据" })
    .click();
  const clusterDetails = page.getByRole("dialog", { name: `${keyword} · 聚类依据` });
  await expect(clusterDetails).toContainText(related);
  await clusterDetails.getByRole("button", { name: "关闭", exact: true }).click();

  await view("关键词与页面映射");
  await page.getByRole("button", { name: "登记本站页面" }).click();
  const evidence = page.getByRole("dialog", { name: "登记本站页面" });
  await evidence.getByLabel("页面 URL", { exact: true }).fill(ownPage);
  await evidence.getByLabel("页面标题").fill("Best robot vacuum comparison");
  await evidence.getByLabel("页面类型").selectOption("comparison");
  await evidence.getByLabel("H1（可选）").fill("Compare robot vacuums");
  await market(evidence);
  await evidence.getByRole("button", { name: "保存页面证据" }).click();
  await expect(page.getByRole("table", { name: "本站页面证据" })).toContainText(ownPage);
  await createTask("keyword_url_mapping", false);
  const mapped = await view("关键词与页面映射");
  await expect(mapped).toContainText(ownPage);
  await createTask("keyword_url_mapping", false);
  const savedMappings = await request.get(`${api}/api/v1/research/mappings?site_id=${siteID}`, { headers: auth });
  const { items: mappings } = await savedMappings.json();
  expect(mappings[0]?.mapping?.previous_target_url).toBe(ownPage);
  await view("关键词与页面映射");
  await screenshot(page, "phase2-keyword-mapping.png");

  await createTask("opportunity_scoring", false, { value: "8" });
  await page.getByRole("button", { name: "刷新研究历史", exact: true }).click();
  await expect(
    page.getByRole("table", { name: "研究历史" }).getByRole("row").filter({ hasText: "机会评分" }),
  ).toContainText("完成");
  const scoring = await view("关键词机会评分");
  await expect(scoring).toContainText(keyword);
  await scoring.getByRole("button", { name: "评分依据" }).click();
  await expect(page.getByRole("table", { name: "机会评分分项" }).locator("tbody tr")).toHaveCount(6);
  await page.getByRole("dialog").getByRole("button", { name: "关闭", exact: true }).click();
  await expect(await view("数据费用")).toContainText(providerName);

  const createdMember = await request.post(`${api}/api/v1/members`, {
    headers: auth,
    data: { email: "phase-two-viewer@example.test", role: "viewer" },
  });
  const viewerToken = (await createdMember.json()).token;
  if (typeof viewerToken !== "string") throw new Error("Expected a local viewer credential");
  const viewer = await browser.newPage({ baseURL: page.url() });
  try {
    await signIn(viewer, viewerToken);
    await viewer.goto(`/seo/research?site_id=${siteID}`);
    await expect(viewer.getByRole("heading", { name: "关键词研究", level: 1 })).toBeVisible();
    await expect(viewer.getByRole("button", { name: "创建研究任务" })).toHaveCount(0);
    await expect(viewer.getByRole("link", { name: "数据费用", exact: true })).toHaveCount(0);
    await openNavigation(viewer, "SEO 研究", "关键词探索");
    const viewerKeywords = viewer.getByRole("table", { name: "关键词探索" });
    await expect(viewerKeywords).toContainText(keyword);
    await expect(viewerKeywords.getByRole("button", { name: "原始响应" })).toHaveCount(0);
    await viewer.goto(`/seo/research/usage?site_id=${siteID}`);
    await expect(viewer.getByText("数据费用需要所有者权限", { exact: true })).toBeVisible();
  } finally {
    await viewer.close();
  }

  await page.getByRole("button", { name: /^Current theme: light/ }).click();
  await expect(page.locator("html")).toHaveClass(/\bdark\b/);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Toggle Sidebar", exact: true }).click();
  await view("关键词探索");
  await expect(page.getByRole("table", { name: "关键词探索" })).toContainText(keyword);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await screenshot(page, "phase2-research-mobile-dark.png");
  expect(pageErrors).toEqual([]);
});
