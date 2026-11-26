import { expect, test, type Page } from "@playwright/test";

async function signIn(page: Page, token: string) {
  await page.getByLabel("访问凭据", { exact: true }).fill(token);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "已登记站点" })).toBeVisible();
}

test("dashboard uses the real API for site, job, provider, membership and audit", async ({ page, browser, request }) => {
  const token = process.env.SEOOS_E2E_TOKEN;
  const api = process.env.SEOOS_API_ORIGIN;
  expect(token && api, "run scripts/test_dashboard.py with a disposable PostgreSQL database").toBeTruthy();

  await page.goto("/");
  await expect(page.getByRole("heading", { name: "登录运营后台" })).toBeVisible();
  await page.getByLabel("访问凭据", { exact: true }).fill("invalid-for-this-test-account-opaque-key");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "访问凭据无效或已撤销。" })).toHaveText("访问凭据无效或已撤销。");
  await signIn(page, token!);
  const cookie = (await page.context().cookies()).find(c => c.name === "seoos_session");
  expect(cookie?.httpOnly).toBe(true);
  expect(cookie?.sameSite).toBe("Lax");

  const name = "后台验收站点";
  await page.getByLabel("站点名称").fill(name);
  await page.getByLabel("站点 URL").fill("https://dashboard.example.test");
  await page.getByLabel("国家或地区").fill("JP");
  await page.getByLabel("语言标签").fill("ja-JP");
  await page.getByRole("button", { name: "添加站点" }).click();
  await expect(page.getByRole("row").filter({ hasText: name })).toBeVisible();
  const screenshot = process.env.SEOOS_E2E_SCREENSHOT;
  if (screenshot) await page.screenshot({ path: screenshot });

  await page.getByText("编辑 " + name, { exact: true }).click();
  const editing = page.getByRole("row").filter({ hasText: name });
  await editing.getByLabel("站点名称").fill(name + "（已更新）");
  await editing.getByRole("button", { name: "保存站点" }).click();
  const updated = page.getByRole("row").filter({ hasText: name + "（已更新）" });
  await expect(updated).toBeVisible();
  await updated.getByRole("link", { name: "创建任务" }).click();
  await page.getByRole("button", { name: "提交任务" }).click();
  const auth = { Authorization: "Bearer " + token };
  await expect.poll(async () => {
    const reply = await request.get(api + "/api/v1/jobs", { headers: auth });
    const { jobs } = await reply.json();
    return jobs[0]?.state;
  }).toBe("succeeded");
  await page.getByRole("link", { name: "刷新" }).click();
  await expect(page.getByRole("row").filter({ hasText: "站点资料快照" })).toContainText("完成");
  await page.getByText("查看结果", { exact: true }).click();
  await expect(page.locator("pre")).toContainText("dashboard.example.test");

  await page.getByRole("link", { name: "Provider", exact: true }).click();
  await page.getByLabel("名称", { exact: true }).fill("后台测试配置");
  await page.getByLabel("OpenAI 模型").fill("test-fixture-model");
  await page.getByLabel("API 密钥").fill("dashboard-no-network-test-key");
  await page.getByLabel("启用", { exact: true }).uncheck();
  await page.getByRole("button", { name: "添加 Provider" }).click();
  await expect(page.getByText("后台测试配置", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "成员", exact: true }).click();
  await page.getByLabel("邮箱").fill("dashboard-viewer@example.test");
  await page.getByRole("button", { name: "添加成员" }).click();
  const credential = page.locator('code[aria-label="访问凭据"]');
  await expect(credential).toBeVisible();
  const viewerToken = (await credential.textContent())!;
  expect(viewerToken.startsWith("seo_")).toBe(true);
  await page.getByRole("link", { name: "审计日志" }).click();
  await expect(page.getByText("site.update", { exact: true })).toBeVisible();
  await expect(page.getByText("provider.create", { exact: true })).toBeVisible();

  const viewer = await browser.newPage({ baseURL: page.url() });
  try {
    await viewer.goto("/");
    await signIn(viewer, viewerToken);
    await expect(viewer.getByText(name + "（已更新）", { exact: true })).toBeVisible();
    await expect(viewer.getByRole("button", { name: "添加站点" })).toHaveCount(0);
    await expect(viewer.getByRole("link", { name: "成员", exact: true })).toHaveCount(0);
    const denied = await request.post(api + "/api/v1/sites", {
      headers: { Authorization: "Bearer " + viewerToken }, data: {},
    });
    expect(denied.status()).toBe(403);
    await viewer.getByRole("button", { name: "退出登录" }).click();
    await expect(viewer.getByRole("heading", { name: "登录运营后台" })).toBeVisible();
  } finally {
    await viewer.close();
  }
});
