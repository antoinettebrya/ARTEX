# 统一搜索服务集成中心验收

## FIXTURE

- Go HTTP 契约覆盖 Google PKCE 和刷新、GSC 日级报告与 URL Inspection、GA4 报表条件和元数据、Bing JSON API Key 与 OAuth Bearer、IndexNow 根目录和自定义目录以及 200、202、400、403、422、429。
- Bing OAuth Bearer 已在本地 HTTP 契约验证。真实 Bing 验收采用 API Key。
- Phase 4.1 增加由本地测试密钥签发的 JWT 和本地 JWKS，覆盖签发者、受众、有效期、nonce、签名、不同账号、既有令牌扩权、新令牌替换与权限不足。GA4 多站点契约测试验证分页过程中按协议、Host 和路径返回本站数据流。Bing 诊断测试用回显的 Key 和 Bearer 验证脱敏。

## LOCAL_INTEGRATION

在 PostgreSQL 16.16 独立数据库执行 `go test -count=1 ./...`。提交版本包含账户版本约束、组织隔离、跨站绑定、撤销、配额、租约恢复、旧连接迁移、IndexNow 轮换和 GSC 每日快照测试。

迁移演练先用 `5c34f66` 启动已有迁移版本 `0008`，建立加密旧连接并生成 PostgreSQL 备份。用新版升级 `0009`，依次验证 `READY`、`MIGRATED`、`ALREADY_MIGRATED`。另外恢复备份，确认上一版可启动并读取旧连接。演练使用独立测试数据库。

Phase 4.1 使用 PostgreSQL 16.16 的独立数据库，从 `b988e93` 和 `0009` 建立四条旧绑定后备份并升级 `0010`。预检发现一个 Google 配对和一个待审批的 Bing 配对，应用后四条绑定指向两个有效账户，保留四份旧连接密文与两个变更回执。重复应用无新回执。将备份恢复到另一数据库后，旧版 CLI 仍可读取原四个绑定。隔离测试另覆盖不同 Client Secret、不同 Google 授权、跨组织隔离、过期审批回滚、账户撤销、同一身份重新授权、重新绑定、同站同服务冲突与 GA4 逐站数据流元数据。

Web 执行 Next.js 生产构建、静态导出、TypeScript 和 Biome 定向检查。Chromium 测试使用真实 Go API、真实 PostgreSQL 及本地 HTTP 服务商契约，覆盖桌面与 390px 移动端、浅色和暗色主题、外部授权回调、两个站点绑定与单站解除、两种 IndexNow 密钥位置、Viewer 权限及旧版 Google 回调。

Phase 4.1 的 Chromium 测试 10 项全部通过，包含已验证身份显示、IndexNow 字母与数字密钥、后台接受 1,000 URL 回执、拒绝 1,001 URL 和最近提交的重复保护。另一个隔离库的 Worker 冒烟测试使用本地 HTTP 服务，记录了 429 后按 8 秒 `Retry-After` 重试成功，验证了服务重启后提交回执及 GSC 未完成分页恢复。Go 竞态检测、Next.js 两种构建、TypeScript 与定向 Biome 检查均通过。

```sh
cd seoos
go test -race -count=1 ./...
go build -trimpath -tags seoos_testfixture -o bin/seoos-fixture ./cmd/seoos
python3 scripts/smoke_execution.py
cd ../web
npm run build
npx tsc --noEmit
cd ../seoos
SEOOS_E2E_EXECUTION=1 SEOOS_E2E_CONTENT=1 SEOOS_E2E_RESEARCH=1 python3 scripts/test_dashboard.py
cd ../web
npm run build:static
```

数据库测试需要指向隔离库的 `SEOOS_TEST_DATABASE_URL`。并行执行时为每套测试分配不同的库，避免争用任务队列。静态导出演练验证页面生成。生产 OAuth 由 Next.js 服务处理。

## LIVE_PROVIDER · Phase 4.1

Google Cloud 控制台曾两次拒绝创建 OAuth 配置，连接恢复后已完成隔离项目的品牌配置。Web OAuth 客户端和测试用户访问仍待批准。首次 GSC 授权、GA4 增量授权、旧刷新令牌是否扩权和新令牌实际签发情况尚无本阶段的真实回执。不同 Google 账号的拒绝、撤销后重新授权与逐站恢复已在本地用签名 JWT、HTTP 和 PostgreSQL 覆盖。Bing 与 IndexNow 本阶段未再次向外部服务提交 URL。

真实联调使用隔离的 Google Cloud 项目、Web OAuth 客户端、已授权的测试用户，以及已启用的 Search Console API 和 Analytics Admin API。登记回调 `http://127.0.0.1:42821/seo/api/integrations/oauth/callback/google`。用仅当前用户可读的文件保存 `client_id`、`client_secret` 和 `isolated_project: true`，将文件路径提供给下列测试。测试会在 GSC 和 GA4 两次授权时分别输出可在 Chrome 打开的本地入口。它检验真实 Google 签发的身份、增量后的两个读取范围、是否签发和替换刷新令牌，并探测旧刷新令牌。

```sh
cd seoos
SEOOS_LIVE_GOOGLE_CONFIG=/secure/path/google-oauth.json \
  go test -v -count=1 ./internal/executionsearch -run '^TestLiveGoogleIncrementalOAuth$'
```

此测试要求首轮使用该项目尚无 GA4 授权的测试账号。Google 的合并授权以 Cloud 项目为单位。使用另一客户端 ID 仍可能继承同项目既有权限。 [Google 增量授权说明](https://developers.google.com/identity/protocols/oauth2/web-server#incrementalAuth)

## LIVE_PROVIDER · 0009 历史验收

以下结果来自 0009 阶段的用户授权测试资源。

| 服务 | 真实结果 |
|---|---|
| Google Search Console | 同一测试用户的 OAuth、资源发现及 Search Analytics 返回成功。部署中的既有 ARTEX 版本通过 URL Inspection 取得首页已索引证据。 |
| Google Analytics 4 | 创建独立媒体资源及 Web 数据流。Admin API 读取到资源和时区，Data API 的 `runReport` 返回 HTTP 200、0 行。 |
| Bing Webmaster | 用已验证账户的 API Key 完成 `GetUserSites`、`GetRankAndTrafficStats`、额度检查和一次 `SubmitUrlBatch`。 |
| IndexNow | 复用已有 WordPress IndexNow 插件发布的根目录 TXT。请求收到 HTTP 200 且正文精确匹配。一次授权 URL 提交返回 HTTP 200。 |

真实外部验收使用既有线上版本或独立验证程序。新集成中心的端到端操作见本地集成结果。GA4 新资源等待网站安装对应的采集代码。
