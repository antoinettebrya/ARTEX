# 统一搜索服务集成中心

从「AI 与系统 → 系统设置」进入。应用、外部账户、站点资源、IndexNow 密钥、连接健康与操作历史使用原有 ARTEX App Shell。统计报告继续在「SEO 执行」查看。

## Google OAuth

1. 在 Google Cloud 启用 Search Console API、Google Analytics Admin API 和 Google Analytics Data API。使用 Web 类型 OAuth 客户端，为测试中的应用配置明确授权的测试用户。
2. 在 Google Cloud 登记 `https://<ARTEX 域名>/seo/api/integrations/oauth/callback/google`，并在系统设置填写同一回调地址和客户端凭据。
3. 添加外部账户，勾选实际需要的只读范围。Search Console 使用 `https://www.googleapis.com/auth/webmasters.readonly`。GA4 使用 `https://www.googleapis.com/auth/analytics.readonly`。两个范围可以共用一个已授权的 Google 账户。
4. 在站点资源绑定中发现资源并逐站确认。GSC 支持已授权的域名和 URL 前缀资源。GA4 使用 `properties/{id}`，保存时读取资源时区并验证 Web 数据流 Host。站点范围内的 GA4 报表同时过滤 Host 和 `Organic Search`。

Google 增量授权需要由已有 Google 账户继续完成。更换 Google 身份时先撤销该账户；重新授权后逐站复核并启用绑定。外部测试应用的刷新令牌按 Google 的测试模式规则过期。

[Google Web OAuth](https://developers.google.com/identity/protocols/oauth2/web-server) · [GSC Sites](https://developers.google.com/webmaster-tools/v1/sites/list) · [GA4 Account Summaries](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1beta/accountSummaries/list) · [GA4 Data Streams](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1beta/properties.dataStreams/list)

## Bing Webmaster

- 使用已验证站点所属 Bing Webmaster 账户的 API Key，或登记 Bing OAuth Client 并配置 `https://<ARTEX 域名>/seo/api/integrations/oauth/callback/bing`。新 OAuth 配置使用 `Webmaster.read`。需要 Bing URL 提交时开启 `Webmaster.manage`，并在具体站点绑定上单独允许写入。
- API Key 经 `ssl.bing.com/webmaster/api.svc/json/` 发送。OAuth Bearer 经 `www.bing.com/webmaster/api.svc/json/` 发送。两种方式复用同一个站点列表、搜索报表、URL 检查和提交配额逻辑。提交前查询当前配额。
- 解除一个站点绑定会保留账户和其余绑定。Bing 密钥轮换影响该账户的站点，旧版本任务停止执行。Bing 撤销在 ARTEX 立即生效。需要同时停用服务商凭据时，在 Bing 控制台撤销 OAuth 或轮换 API Key。

[Bing OAuth](https://learn.microsoft.com/en-us/bingwebmaster/getting-access) · [Bing API JSON 协议](https://learn.microsoft.com/en-us/bingwebmaster/using-json)

## IndexNow

为每个站点 Host 创建密钥。下载 TXT，将它部署到与界面显示一致的 HTTPS 地址，再点「重新验证」。根目录为 `/<key>.txt`。自定义目录示例为 `/news/<key>.txt`，对应的提交 URL 须在本 Host 的 `/news/` 下。TXT 内容应与密钥完全一致。新密钥通过随机数生成，也允许符合规格的手工密钥。

文件验证检查 HTTP 200、精确正文与大小，并禁止跳转。根目录模式可省略提交请求的 `keyLocation`。自定义目录模式显式发送。站点授权开启后，ARTEX 单个任务最多接收 1,000 条 URL，校验范围并去重。同一 URL 的最近接受回执或未确认回执会阻止自动重复提交。

| 服务商响应 | ARTEX 状态 |
|---|---|
| 200 | `SUBMITTED` |
| 202 | `ACCEPTED_PENDING_VERIFICATION` |
| 400 或 422 | `REJECTED` |
| 403 | `KEY_VERIFICATION_FAILED` |
| 429 | `RETRY_WAIT`，尊重 `Retry-After` |
| 已派发且响应不明 | `RECEIPT_UNKNOWN`，人工核对后决定是否再次提交 |

索引状态依据相应来源的检查证据。IndexNow 只记录提交回执。

[IndexNow 官方协议](https://www.indexnow.org/documentation)

## API

应用 API 前缀为 `/api/v1/integrations`。登录后的 Web 代理前缀为 `/seo/api/integrations`。列表使用 `limit` 和 `offset`，适用时可选 `site_id`、`provider`、`account_id`、`status`、`q`、`from`、`to`。

| 路径后缀 | 方法 | 请求与响应 |
|---|---|---|
| `/summary` | GET | 四个服务的账户数、绑定数、最近完成的同步与异常 |
| `/applications`、`/applications/{id}` | GET 列表、POST、PUT、DELETE | 应用厂商、标签、Client ID、回调地址、允许范围、启用状态及配置版本 |
| `/accounts`、`/accounts/{id}` | GET、POST、DELETE | OAuth/Bing API Key 账户、授权范围、版本和授权状态 |
| `/accounts/{id}/authorize` | POST | 接收 `scopes` 并返回服务商授权地址 |
| `/oauth/callback/{google\|bing}` | GET | 单次消费 State。浏览器完成后跳转至外部账户页面 |
| `/accounts/{id}/discover` | POST | 接收 `provider` 和可选 `page_token`。返回资源和下一页游标 |
| `/accounts/{id}/refresh`、`revoke`、`rotate-key` | POST | 刷新、撤销或按 `credential_version` 轮换 Bing API Key |
| `/sites`、`/bindings` | GET | 站点资源和绑定状态 |
| `/bindings`、`/bindings/{id}` | POST、PUT、DELETE | 逐站确认 `site_id`、`integration_account_id`、`provider` 和 `external_resource_id`。编辑使用当前 `version` |
| `/bindings/batch`、`/bindings/{id}/verify` | POST | 一次确认最多 20 个绑定并返回逐站结果，或重新验证一个绑定 |
| `/indexnow/keys` | GET、POST | 列表只返回元数据。创建时输入站点、验证目录、可选密钥及写入授权 |
| `/indexnow/keys/{id}/verify`、`rotate` | POST | 产生文件验证任务、轮换密钥 |
| `/indexnow/keys/{id}/reveal`、`file` | GET | Owner 显式查看或下载 TXT，两项操作均审计 |
| `/health`、`/history`、`/usage` | GET | 站点健康、任务/审计历史、请求用量。请求用量仅 Owner 可读 |

现有 `POST /api/v1/execution/jobs` 支持 `gsc_sync`、`ga4_sync`、`bing_sync`、`url_inspection`、`indexnow_submit`。提供 `site_id` 和 `idempotency_key`。可选 `connection_id`，省略时要求该站点和服务只有一个有效默认绑定。URL 检查用 `url` 并可指定 `provider`。批量提交用 `urls`。GSC 支持 `search_type`、`data_state` 和日期窗口。GA4 最新保留的报表质量可通过 `GET /api/v1/execution/report-quality` 查询。

## 数据、额度和权限

- GSC 按 Pacific 日期保存逐日总量和明细。明细分页、租约恢复和覆盖旧快照都在数据库中处理。`web/final` 与其他搜索类型、`all` 分开。包含未最终日期的响应保留 Google 的 `first_incomplete_date`。明细截断、未知总量、零值和失败分别显示。URL Inspection 有单独计数，仍受账户总额度约束。
- GA4 保留资源时区、采样、阈值、其他行、截断、空报表和资源配额。含最近日期的报表会提示数据可能继续更新。关键事件使用 GA4 资源定义。
- OAuth 刷新、发现和绑定验证在交互中完成，使用同一账户的持久化请求额度与诊断。站点同步、索引检查、TXT 验证和提交使用现有持久化任务队列。队列记录绑定版本、账户版本、租约、重试和最终结果。授权变化后，旧任务在下一次请求前停止。
- Owner 管理应用、凭据和绑定。Editor 可触发授权范围内的只读任务。Viewer 只读查看站点状态与报表。角色与现有 RBAC 一致。账户，站点和 Provider 由 API 与 PostgreSQL 双重约束。

[Search Analytics 数据完整性](https://developers.google.com/webmaster-tools/v1/searchanalytics/query) · [GA4 报表元数据](https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/ResponseMetaData) · [GA4 数据处理时效](https://support.google.com/analytics/answer/11198161)

## 迁移和回滚

1. 在升级前生成 PostgreSQL 加密备份并对隔离实例做实际恢复检查。独立保护与备份配套的 `SEOOS_PROVIDER_KEY`。
2. 在从同一份备份恢复的隔离库中，用新镜像执行 `seoos migrate`。这会加上 `0009`，保留旧连接、历史数据及已有密文。
3. 用现有组织的部署配置执行下面的只读预检。每个组织分别执行。检视 `INVALID_LEGACY_SCOPE`、`INVALID_LEGACY_CREDENTIAL`、`BINDING_CONFLICT` 和已变更站点。经确认后应用。每条连接独立提交，重复运行跳过已迁移项。

```sh
seoos integrations migrate-legacy --dry-run
seoos integrations migrate-legacy
seoos integrations migrate-legacy --dry-run
```

迁移在内存解密旧凭据，以新账户或站点密钥上下文重新加密并验证可读。Google 不按 Client ID 合并账户。迁移完成前，旧连接仍可由 Worker 运行，并可从设置页检查迁移前的连接。旧连接密文保留为回滚来源。

升级失败时先停止新版 API 和 Worker，冻结新写入，另存一份失败现场备份。恢复升级前的数据库和配套密钥，在应用流量恢复前，让上一版镜像通过迁移检查、健康检查及旧 Google 回调测试。直接把旧镜像连接到带有 `0009` 的数据库无法完成版本校验。接受过写入后再恢复旧库，会丢失备份后新增数据。先确认回滚窗口并核对可重放的变更。
