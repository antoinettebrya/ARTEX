import { type APIRequestContext, type APIResponse, expect, type Page } from "@playwright/test";

import { mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";

export const executionEnabled = process.env.SEOOS_E2E_EXECUTION === "1";

async function checked(response: APIResponse, label: string) {
  const body = await response.json();
  expect(response.ok(), `${label}: ${body.error?.code ?? response.status()}`).toBe(true);
  return body;
}

export async function executionScreenshot(page: Page, file: string) {
  const output = process.env.SEOOS_E2E_EXECUTION_SCREENSHOTS;
  if (!output) return;
  await mkdir(output, { recursive: true });
  await expect(page.locator("[data-sonner-toast]").filter({ visible: true })).toHaveCount(0, { timeout: 8000 });
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await page.screenshot({ path: join(output, file), animations: "disabled" });
}

export async function runExecutionFlow({
  page,
  request,
  base,
  headers,
  siteID,
  siteURL,
  packageID,
}: {
  page: Page;
  request: APIRequestContext;
  base: string;
  headers: Record<string, string>;
  siteID: string;
  siteURL: string;
  packageID: string;
}) {
  const title = "Phase 4 通用 CMS 验收";
  const open = (path: string) => page.goto(`${path}?site=${siteID}`);

  async function awaitJob(id: string, name: string, expected = "succeeded") {
    let final: { state: string; error_code?: string; result?: { verification_job_id?: string } } | undefined;
    await expect
      .poll(
        async () => {
          ({ job: final } = await checked(await request.get(`${base}/jobs/${id}`, { headers }), name));
          return final?.state;
        },
        { timeout: 25000 },
      )
      .toMatch(/^(succeeded|failed|cancelled)$/u);
    expect(final?.state, `${name}: ${final?.error_code ?? "no error code"}`).toBe(expected);
    return final?.result;
  }

  async function submitTask(dialogName: string, submitLabel = "提交任务", note?: string) {
    const dialog = page.getByRole("dialog", { name: dialogName, exact: true });
    if (note) await dialog.getByLabel("授权说明", { exact: true }).fill(note);
    const response = page.waitForResponse(
      (res) => new URL(res.url()).pathname === "/seo/api/execution/jobs" && res.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: submitLabel, exact: true }).click();
    const submitted = await response;
    const body = await submitted.json();
    expect(submitted.ok(), `${dialogName}: ${body.error?.code ?? submitted.status()}`).toBe(true);
    const result = await awaitJob(body.job.id, dialogName);
    await expect(dialog).toBeHidden();
    return { id: body.job.id, input: submitted.request().postDataJSON(), result };
  }

  await open("/seo/sites/connections");
  await expect(page.getByRole("heading", { name: "CMS 连接", exact: true })).toBeVisible();
  await expect(page.getByLabel("执行站点", { exact: true })).toHaveValue(siteID);
  async function createConnection(
    kind: "generic" | "gsc" | "ga4" | "indexnow",
    name: string,
    fields: Record<string, string>,
  ) {
    await page.getByRole("button", { name: "新建连接", exact: true }).click();
    const editor = page.getByRole("dialog", { name: "新建连接", exact: true });
    await editor.getByLabel("连接类型", { exact: true }).selectOption(kind);
    await editor.getByLabel("连接名称", { exact: true }).fill(name);
    for (const [label, value] of Object.entries(fields)) {
      await editor.getByLabel(label, { exact: true }).fill(value);
    }
    await editor.getByRole("switch", { name: "启用连接", exact: true }).click();
    if (kind === "generic" || kind === "indexnow") {
      await editor
        .getByRole("switch", {
          name: kind === "generic" ? "授权此连接写入站点" : "授权此连接提交索引请求",
          exact: true,
        })
        .click();
    }
    const connect = page.waitForResponse(
      (res) => new URL(res.url()).pathname === "/seo/api/execution/connections" && res.request().method() === "POST",
    );
    await editor.getByRole("button", { name: "保存连接", exact: true }).click();
    const stored = await connect;
    const body = await stored.json();
    expect(stored.ok(), `save ${kind} connection: ${body.error?.code ?? stored.status()}`).toBe(true);
    const connection = body.connection;
    expect(connection.credential).toBeUndefined();
    await expect(editor).toBeHidden();
    return connection as { id: string };
  }

  const connection = await createConnection("generic", title, { "CMS 访问令牌": "phase4-generic-token" });
  const connectionTable = page.getByRole("table", { name: "站点连接", exact: true });
  const connectionRow = connectionTable.getByRole("row").filter({ hasText: title });
  await connectionRow.getByRole("button", { name: "测试连接", exact: true }).click();
  await submitTask("验证 CMS 连接");
  await page.getByRole("button", { name: "刷新连接", exact: true }).click();
  await expect(connectionRow).toContainText("草稿写入");
  await executionScreenshot(page, "phase4-cms-connection.png");

  await page.goto(`/seo/execution?site=${siteID}&package=${packageID}`);
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === "/seo/execution/publishing" &&
      url.searchParams.get("site") === siteID &&
      url.searchParams.get("package") === packageID,
  );
  await page.reload();
  await page.getByRole("button", { name: "创建发布记录", exact: true }).click();
  const release = page.getByRole("dialog", { name: "创建发布记录", exact: true });
  await expect(release.getByLabel("已批准的发布包", { exact: true })).toHaveValue(packageID);
  await release.getByLabel("目标 CMS", { exact: true }).selectOption(connection.id);
  await release.getByLabel("内容类型", { exact: true }).selectOption("page");
  const creation = page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/seo/api/execution/publications" && res.request().method() === "POST",
  );
  await release.getByRole("button", { name: "创建记录", exact: true }).click();
  const created = await creation;
  const publication = (await created.json()).publication;
  expect(created.ok(), "create immutable publication record").toBe(true);
  const replay = await checked(
    await request.post(`${base}/execution/publications`, { headers, data: created.request().postDataJSON() }),
    "replay publication request",
  );
  expect(replay.publication.id).toBe(publication.id);
  await expect(release).toBeHidden();

  const publications = page.getByRole("table", { name: "CMS 发布记录", exact: true });
  const record = publications.getByRole("row").filter({ hasText: title });
  await expect(record).toContainText("待准备");
  await expect(record.getByRole("button", { name: "批准正式发布", exact: true })).toHaveCount(0);
  await record.getByRole("button", { name: "保存草稿", exact: true }).click();
  const draft = await submitTask("保存草稿");
  await expect(record).toContainText("已保存草稿");
  const replayedDraft = await checked(
    await request.post(`${base}/execution/jobs`, { headers, data: draft.input }),
    "replay draft request",
  );
  expect(replayedDraft.job.id).toBe(draft.id);
  const first = await checked(
    await request.get(`${base}/execution/publications/${publication.id}`, { headers }),
    "draft target",
  );
  expect(first.publication.status).toBe("DRAFT_CREATED");
  expect(first.publication.target_id).toBeTruthy();

  const editorMember = await checked(
    await request.post(`${base}/members`, {
      headers,
      data: { email: "execution-editor@example.test", role: "editor" },
    }),
    "editor credential",
  );
  const unauthorized = await request.post(`${base}/execution/jobs`, {
    headers: { Authorization: `Bearer ${editorMember.token}` },
    data: {
      ...draft.input,
      operation: "publish",
      note: "editor must be denied",
      idempotency_key: "phase4-editor-publish",
    },
  });
  expect(unauthorized.status()).toBe(403);

  await record.getByRole("button", { name: "批准正式发布", exact: true }).click();
  const published = await submitTask("批准正式发布", "确认发布", "批准在本地 CMS 验收本版本。");
  const verificationID = published.result?.verification_job_id;
  expect(verificationID, "publish returns a durable automatic verification job").toMatch(/^[a-f\d-]{36}$/u);
  await awaitJob(verificationID as string, "automatic publication verification");
  await expect(record).toContainText("已核验");
  await record.getByRole("button", { name: "核验发布", exact: true }).click();
  await submitTask("核验发布结果");
  await expect(record).toContainText("已核验");
  const verified = await checked(
    await request.get(`${base}/execution/publications/${publication.id}`, { headers }),
    "publication readback",
  );
  expect(verified.publication.url).toBe(`${siteURL}/existing`);
  expect(verified.publication.target_id).toBe(first.publication.target_id);
  await executionScreenshot(page, "phase4-publishing-verified.png");
  await open("/seo/dashboard");
  const totals = page.locator('[aria-label="站点运营指标"]');
  const metric = (label: string) => totals.getByText(label, { exact: true }).locator("..").locator("dd").first();
  await expect(metric("发布成功数量")).toHaveText("1");
  await expect(metric("内容生产数量")).toHaveText("1");
  await open("/seo/execution/publishing");

  await record.getByRole("button", { name: "发布历史", exact: true }).click();
  const history = page.getByRole("dialog", { name: "发布历史", exact: true });
  await expect(history.getByRole("table", { name: "发布事件", exact: true })).toContainText("已核验");
  await executionScreenshot(page, "phase4-publication-history.png");
  await history.getByRole("button", { name: "关闭", exact: true }).click();
  await record.getByRole("button", { name: "恢复版本", exact: true }).click();
  await submitTask("恢复发布前版本", "确认恢复", "恢复本地验收前的 CMS 内容状态。");
  await expect(record).toContainText("已恢复");

  await open("/seo/execution/cms");
  await page.getByRole("button", { name: "同步 CMS 内容", exact: true }).click();
  await submitTask("同步 CMS 内容及媒体");
  const pages = page.getByRole("table", { name: "CMS 页面与文章", exact: true });
  await expect(pages).toContainText("Maintenance notes");
  await expect(pages.getByRole("row").filter({ hasText: `${siteURL}/existing` })).toContainText("草稿");
  await page.getByRole("button", { name: "上传媒体", exact: true }).click();
  const media = page.getByRole("dialog", { name: "上传 CMS 媒体", exact: true });
  await media.getByLabel("媒体文件（最大 4 MiB）", { exact: true }).setInputFiles({
    name: "phase4-media-fixture.gif",
    mimeType: "image/gif",
    buffer: Buffer.from("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7", "base64"),
  });
  await submitTask("上传 CMS 媒体");
  await expect(page.getByRole("table", { name: "CMS 媒体库", exact: true })).toContainText("phase4-media-fixture.gif");

  await open("/seo/execution/sitemap");
  await page.getByRole("button", { name: "刷新 Sitemap", exact: true }).click();
  const sitemapTask = page.getByRole("dialog", { name: "刷新 Sitemap", exact: true });
  await sitemapTask.getByLabel("Sitemap 地址（可选）", { exact: true }).fill(`${siteURL}/sitemap.xml`);
  await submitTask("刷新 Sitemap");
  const sitemapRow = page
    .getByRole("table", { name: "Sitemap 记录", exact: true })
    .getByRole("row")
    .filter({ hasText: `${siteURL}/sitemap.xml` });
  await sitemapRow.getByRole("button", { name: "查看 Sitemap", exact: true }).click();
  const sitemap = page.getByRole("dialog", { name: "Sitemap 详情", exact: true });
  const sitemapEntries = sitemap.getByRole("table", { name: "Sitemap URL", exact: true });
  await expect(sitemapEntries).toContainText(`${siteURL}/existing`);
  await expect(sitemapEntries).toContainText("en-US");
  const file = page.waitForEvent("download");
  await sitemap
    .getByRole("button", { name: /\.xml$/u })
    .first()
    .click();
  const localXML = await (await file).path();
  expect(await readFile(localXML, "utf8")).toContain(`<loc>${siteURL}/existing</loc>`);
  await executionScreenshot(page, "phase4-sitemap.png");
  await sitemap.getByRole("button", { name: "关闭", exact: true }).click();
  await open("/seo/execution/indexing");
  const index = page.getByRole("table", { name: "索引状态", exact: true });
  await expect(index.getByRole("row").filter({ hasText: `${siteURL}/existing` })).toContainText("已发现");
  await expect(page.getByRole("table", { name: "索引提交记录", exact: true })).toHaveCount(0);
  await executionScreenshot(page, "phase4-indexing.png");

  await open("/seo/sites/connections");
  const indexNowKey = "phase4-indexnow-key";
  const indexNow = await createConnection("indexnow", "Phase 4 IndexNow 验收", {
    "IndexNow 密钥文件地址": `${siteURL}/${indexNowKey}.txt`,
    "API 密钥": indexNowKey,
  });
  await open("/seo/execution/indexnow");
  await expect(page.getByRole("heading", { name: "IndexNow", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "检查 URL", exact: true })).toHaveCount(0);
  await page.getByLabel("提交连接", { exact: true }).selectOption(indexNow.id);
  await page.getByRole("button", { name: "提交索引请求", exact: true }).click();
  const submission = page.getByRole("dialog", { name: "提交索引请求", exact: true });
  await submission.getByLabel("目标 URL", { exact: true }).fill(`${siteURL}/existing`);
  await submission.getByLabel("提交说明", { exact: true }).fill("验证本地 IndexNow 合同与平台回执。");
  await submitTask("提交索引请求");
  await page.reload();
  const receipt = page
    .getByRole("table", { name: "索引提交记录", exact: true })
    .getByRole("row")
    .filter({
      hasText: `${siteURL}/existing`,
    });
  await expect(receipt).toContainText("IndexNow");
  await expect(receipt).toContainText("等待密钥验证");
  await expect(receipt).toContainText("202");
  await executionScreenshot(page, "phase4-indexnow.png");

  await open("/seo/sites/connections");
  const searchConnection = "Phase 4 Search Console 验收";
  const configuredCallback = "https://artex-fixture.example.test/seo/execution";
  await createConnection("gsc", searchConnection, {
    "资源地址或 sc-domain": `sc-domain:${new URL(siteURL).hostname}`,
    "Google 客户端 ID": "phase4-gsc-client",
    "Google 客户端密钥": "phase4-gsc-secret",
    "Google 授权回调地址": configuredCallback,
  });
  await checked(
    await request.post(`${base}/sites`, {
      headers,
      data: {
        name: "OAuth 回调对照站点",
        base_url: "https://oauth-callback-control.example.test",
        cms: "generic",
        country_code: "US",
        language_tag: "en-US",
        status: "active",
      },
    }),
    "unrelated site established before returning from consent",
  );
  async function authorizeGoogle(kind: "gsc" | "ga4", name: string) {
    const callback = new URL("/seo/execution", page.url());
    await page.route(
      (url) => url.origin === "https://accounts.google.com" && url.pathname === "/o/oauth2/v2/auth",
      async (route) => {
        const authorization = new URL(route.request().url());
        expect(authorization.searchParams.get("client_id")).toBe(`phase4-${kind}-client`);
        expect(authorization.searchParams.get("redirect_uri")).toBe(configuredCallback);
        expect(authorization.searchParams.get("code_challenge_method")).toBe("S256");
        callback.searchParams.set("state", authorization.searchParams.get("state") ?? "");
        callback.searchParams.set("code", `phase4-${kind}-code`);
        await route.fulfill({ status: 302, headers: { Location: callback.href } });
      },
      { times: 1 },
    );
    const exchanged = page.waitForResponse(
      (res) => new URL(res.url()).pathname === "/seo/api/execution/oauth/exchange" && res.request().method() === "POST",
    );
    await connectionTable
      .getByRole("row")
      .filter({ hasText: name })
      .getByRole("button", { name: "授权 Google", exact: true })
      .click();
    expect((await exchanged).ok(), "exchange approved fixture Google authorization").toBe(true);
    await expect(page).toHaveURL(
      (url) => url.pathname === "/seo/sites/connections" && url.search === `?site=${siteID}`,
    );
    await expect(page.getByLabel("执行站点", { exact: true })).toHaveValue(siteID);
  }
  await authorizeGoogle("gsc", searchConnection);

  await open("/seo/execution/analytics");
  await page.getByRole("button", { name: "同步报告", exact: true }).click({ timeout: 10000 });
  const reportDay = new Date(Date.now() - 3 * 86_400_000).toISOString().slice(0, 10);
  const gscReport = page.getByRole("dialog", { name: "同步 Search Console", exact: true });
  await gscReport.getByLabel("开始日期", { exact: true }).fill(reportDay);
  await gscReport.getByLabel("结束日期", { exact: true }).fill(reportDay);
  await submitTask("同步 Search Console");
  const metrics = page.getByRole("table", { name: "Search Console 指标", exact: true });
  await expect(metrics).toContainText("phase4 organic query");
  await page.getByLabel("页面 URL", { exact: true }).fill(`${siteURL}/existing`);
  await page.getByLabel("国家代码", { exact: true }).fill("ESP");
  await page.getByRole("button", { name: "筛选指标", exact: true }).click();
  await expect(page.getByText("暂无已同步指标", { exact: true })).toBeVisible();
  await page.getByLabel("国家代码", { exact: true }).fill("USA");
  await page.getByRole("button", { name: "筛选指标", exact: true }).click();
  await expect(metrics).toContainText("6.5");
  await executionScreenshot(page, "phase4-search-console.png");

  const sources = page.getByRole("navigation", { name: "搜索分析来源", exact: true });
  await sources.getByRole("link", { name: "GA4", exact: true }).click();
  await expect(page.getByRole("table", { name: "GA4 自然搜索指标", exact: true })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(
    (url) => url.pathname === "/seo/execution/analytics" && url.searchParams.get("site") === siteID,
  );
  await expect(metrics).toContainText("phase4 organic query");
  await page.goForward();
  await expect(page).toHaveURL(
    (url) => url.pathname === "/seo/execution/ga4" && url.searchParams.get("site") === siteID,
  );
  await expect(page.getByLabel("执行站点", { exact: true })).toHaveValue(siteID);

  await open("/seo/execution/indexing");
  await page.getByRole("button", { name: "检查 URL", exact: true }).click();
  const inspection = page.getByRole("dialog", { name: "检查 URL", exact: true });
  await inspection.getByLabel("目标 URL", { exact: true }).fill(`${siteURL}/existing`);
  await submitTask("检查 URL");
  await expect(index.getByRole("row").filter({ hasText: "Submitted and indexed" })).toContainText("已索引");

  await open("/seo/sites/connections");
  const analyticsConnection = "Phase 4 GA4 验收";
  await createConnection("ga4", analyticsConnection, {
    "GA4 属性 ID": "properties/54321",
    "Google 客户端 ID": "phase4-ga4-client",
    "Google 客户端密钥": "phase4-ga4-secret",
    "Google 授权回调地址": configuredCallback,
  });
  await authorizeGoogle("ga4", analyticsConnection);
  await open("/seo/execution/ga4");
  await page.getByRole("button", { name: "同步报告", exact: true }).click();
  const ga4Report = page.getByRole("dialog", { name: "同步 Google Analytics 4", exact: true });
  await ga4Report.getByLabel("开始日期", { exact: true }).fill(reportDay);
  await ga4Report.getByLabel("结束日期", { exact: true }).fill(reportDay);
  await submitTask("同步 Google Analytics 4");
  await open("/seo/dashboard");
  await expect(metric("GSC 点击")).toHaveText("12,012");
  await expect(metric("GSC 展示")).toHaveText("84,084");
  await expect(metric("自然搜索流量")).toHaveText("17");
  await executionScreenshot(page, "phase4-dashboard-data.png");

  for (const [path, table] of [
    ["/seo/execution/ga4", "GA4 自然搜索指标"],
    ["/seo/execution/bing", "Bing 搜索指标"],
    ["/seo/execution/technical", "提取结果"],
    ["/seo/execution/feedback", "内容优化反馈"],
    ["/seo/execution/monitoring", "站点执行任务"],
    ["/seo/execution/usage", "站点执行用量"],
  ]) {
    await open(path);
    await expect(page.getByRole("table", { name: table, exact: true })).toBeVisible();
  }
  await expect(page.getByRole("table", { name: "站点执行用量", exact: true })).toContainText(title);
  await executionScreenshot(page, "phase4-usage.png");
}
