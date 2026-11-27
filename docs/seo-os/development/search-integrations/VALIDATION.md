# 统一搜索服务集成中心验收

## FIXTURE

- Go HTTP 契约覆盖 Google PKCE 和刷新、GSC 日级报告与 URL Inspection、GA4 报表条件和元数据、Bing JSON API Key 与 OAuth Bearer、IndexNow 根目录和自定义目录以及 200、202、400、403、422、429。
- Bing OAuth Bearer 已在本地 HTTP 契约验证。真实 Bing 验收采用 API Key。

## LOCAL_INTEGRATION

在 PostgreSQL 16.16 独立数据库执行 `go test -count=1 ./...`。提交版本包含账户版本约束、组织隔离、跨站绑定、撤销、配额、租约恢复、旧连接迁移、IndexNow 轮换和 GSC 每日快照测试。

迁移演练先用 `5c34f66` 启动已有迁移版本 `0008`，建立加密旧连接并生成 PostgreSQL 备份。用新版升级 `0009`，依次验证 `READY`、`MIGRATED`、`ALREADY_MIGRATED`。另外恢复备份，确认上一版可启动并读取旧连接。演练使用独立测试数据库。

Web 执行 Next.js 生产构建、静态导出、TypeScript 和 Biome 定向检查。Chromium 测试使用真实 Go API、真实 PostgreSQL 及本地 HTTP 服务商契约，覆盖桌面与 390px 移动端、浅色和暗色主题、外部授权回调、两个站点绑定与单站解除、两种 IndexNow 密钥位置、Viewer 权限及旧版 Google 回调。

```sh
cd seoos
go test -count=1 ./...
go build -trimpath -tags seoos_testfixture -o bin/seoos-fixture ./cmd/seoos
cd ../web
npm run build
npx tsc --noEmit
cd ../seoos
SEOOS_E2E_EXECUTION=1 SEOOS_E2E_CONTENT=1 SEOOS_E2E_RESEARCH=1 python3 scripts/test_dashboard.py
cd ../web
npm run build:static
```

上面两个数据库验证命令使用独立的 `SEOOS_TEST_DATABASE_URL`。静态导出演练验证页面生成。生产 OAuth 由 Next.js 服务处理。

## LIVE_PROVIDER

使用用户授权的测试账户和测试站点，服务商的请求结果独立记录于交付附件。

| 服务 | 真实结果 |
|---|---|
| Google Search Console | 同一测试用户的 OAuth、资源发现及 Search Analytics 返回成功。部署中的既有 ARTEX 版本通过 URL Inspection 取得首页已索引证据。 |
| Google Analytics 4 | 创建独立媒体资源及 Web 数据流。Admin API 读取到资源和时区，Data API 的 `runReport` 返回 HTTP 200、0 行。 |
| Bing Webmaster | 用已验证账户的 API Key 完成 `GetUserSites`、`GetRankAndTrafficStats`、额度检查和一次 `SubmitUrlBatch`。 |
| IndexNow | 复用已有 WordPress IndexNow 插件发布的根目录 TXT。请求收到 HTTP 200 且正文精确匹配。一次授权 URL 提交返回 HTTP 200。 |

真实外部验收使用既有线上版本或独立验证程序。新集成中心的端到端操作见本地集成结果。GA4 新资源等待网站安装对应的采集代码。
