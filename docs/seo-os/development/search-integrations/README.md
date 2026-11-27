# 统一搜索服务集成中心

从「AI 与系统 → 系统设置」进入。应用、外部账户、站点资源、IndexNow 密钥、连接健康与操作历史使用原有 ARTEX App Shell。统计报告继续在「SEO 执行」查看。

## Google OAuth

1. 在 Google Cloud 启用 Search Console API、Google Analytics Admin API 和 Google Analytics Data API。使用 Web 类型 OAuth 客户端，为测试中的应用配置明确授权的测试用户。
2. 在 Google Cloud 登记 `https://<ARTEX 域名>/seo/api/integrations/oauth/callback/google`，并在系统设置填写同一回调地址和客户端凭据。
3. 添加外部账户，勾选实际需要的只读范围。Search Console 使用 `https://www.googleapis.com/auth/webmasters.readonly`。GA4 使用 `https://www.googleapis.com/auth/analytics.readonly`。同一个 Google 账户可以增量授权这两个范围。
4. 在站点资源绑定中发现资源并逐站确认。GSC 支持已授权的域名和 URL 前缀资源。GA4 使用 `properties/{id}`，保存时读取资源时区，并匹配 Web 数据流的协议、主机和路径。新版验证记录保存本站数据流 ID。其后运行的 GA4 报表使用 Host、适用的路径、已验证的 `streamId` 和 `Organic Search` 过滤。旧绑定可逐站重新验证以保存数据流 ID。

Google 回调通过官方公开密钥验证 ID Token 的签名、签发者、受众、有效期和本次授权的 nonce。服务端保存 Google `sub` 和验证时间。邮箱仅用于 Google 登录，API 只返回身份验证状态和时间。

增量授权使用同一个 Google 账户。ARTEX 先验证回调身份和完整权限，再使用 Google 发回的新刷新令牌，或在省略新令牌时使用原令牌实际刷新。只有服务商证实刷新可用且覆盖所需权限，授权才保存。失败会保留先前的账户状态。旧令牌没有稳定身份时，需从同一个原始授权证明身份，或者撤销后重新授权并逐站核对。已有验证身份的账户撤销后仍绑定原 Google 身份，更换身份需要创建新账户。撤销会暂停绑定，同一身份重新授权后须逐站验证并启用。外部测试应用的刷新令牌受 Google 测试模式的过期规则限制。

[Google OpenID Connect](https://developers.google.com/identity/openid-connect/openid-connect) · [Google Web OAuth](https://developers.google.com/identity/protocols/oauth2/web-server) · [GSC Sites](https://developers.google.com/webmaster-tools/v1/sites/list) · [GA4 Data Streams](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1beta/properties.dataStreams/list) · [GA4 报表维度](https://developers.google.com/analytics/devguides/reporting/data/v1/api-schema)

### 资源绑定基数

同一个 GSC Domain Property 可以覆盖同一组织内多个子域站点。URL 前缀资源只能覆盖其范围内的站点。同一 GA4 Property 可以绑定多个站点，每个站点都要有覆盖其地址范围的 Web 数据流。各站保存自身验证结果和同步证据。每站每项服务最多一个有效绑定，Bing 和 IndexNow 的外部资源在组织内仍保持单站绑定。不同组织需要各自的账户、凭据和站点验证。

## Bing Webmaster

- 使用已验证站点所属 Bing Webmaster 账户的 API Key，或登记 Bing OAuth Client 并配置 `https://<ARTEX 域名>/seo/api/integrations/oauth/callback/bing`。新 OAuth 配置使用 `Webmaster.read`。需要 Bing URL 提交时开启 `Webmaster.manage`，并在具体站点绑定上单独允许写入。
- API Key 经 `ssl.bing.com/webmaster/api.svc/json/` 发送。OAuth Bearer 经 `www.bing.com/webmaster/api.svc/json/` 发送。两种方式复用同一个站点列表、搜索报表、URL 检查和提交配额逻辑。提交前查询当前配额。
- Bing 账户使用自定义标签与授权状态。现有官方 Webmaster 接口提供已验证站点能力，当前集成不依赖外部用户 ID。API Key 只进入官方 `apikey` 查询参数，诊断屏蔽服务商回显的密钥和 Bearer。只读与提交权限分别校验，重试依任务状态与服务商回执执行。
- 解除一个站点绑定会保留账户和其余绑定。Bing 密钥轮换影响该账户的站点，旧版本任务停止执行。Bing 撤销在 ARTEX 立即生效。需要同时停用服务商凭据时，在 Bing 控制台撤销 OAuth 或轮换 API Key。

[Bing OAuth](https://learn.microsoft.com/en-us/bingwebmaster/getting-access) · [Bing API JSON 协议](https://learn.microsoft.com/en-us/bingwebmaster/using-json)

## IndexNow

为每个站点 Host 创建密钥。可以自动生成，或输入 8–128 个 ASCII 英文字母、数字和连字符，区分大小写。下载 TXT，将它部署到与界面显示一致的 HTTPS 地址，再点「重新验证」。根目录为 `/<key>.txt`。自定义目录示例为 `/news/<key>.txt`，对应的提交 URL 须在本 Host 的 `/news/` 下。使用与密钥大小写一致的文件名，TXT 正文只写密钥。已迁移的旧文件仍接受既有的首尾空白。

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

[IndexNow 官方协议](https://www.indexnow.org/documentation) · [密钥字符列表](https://www.indexnow.org/faq)

## API

应用 API 前缀为 `/api/v1/integrations`。登录后的 Web 代理前缀为 `/seo/api/integrations`。列表使用 `limit` 和 `offset`，适用时可选 `site_id`、`provider`、`account_id`、`status`、`q`、`from`、`to`。

| 路径后缀 | 方法 | 请求与响应 |
|---|---|---|
| `/summary` | GET | 四个服务的账户数、绑定数、最近完成的同步与异常 |
| `/applications`、`/applications/{id}` | GET 列表、POST、PUT、DELETE | 应用厂商、标签、Client ID、回调地址、允许范围、启用状态及配置版本 |
| `/accounts`、`/accounts/{id}` | GET、POST、DELETE | OAuth/Bing API Key 账户、授权范围、版本、授权状态、`identity_status` 及适用时的 `identity_verified_at` |
| `/accounts/{id}/authorize` | POST | 接收 `scopes` 并返回服务商授权地址 |
| `/oauth/callback/{google\|bing}` | GET | 单次消费 State。浏览器完成后跳转至外部账户页面 |
| `/accounts/{id}/discover` | POST | 接收 `provider` 和可选 `page_token`。返回资源和下一页游标 |
| `/accounts/{id}/refresh`、`revoke`、`rotate-key` | POST | 刷新、撤销或按 `credential_version` 轮换 Bing API Key |
| `/sites`、`/bindings` | GET | 站点资源和绑定状态 |
| `/bindings`、`/bindings/{id}` | POST、PUT、DELETE | 逐站确认 `site_id`、`integration_account_id`、`provider` 和 `external_resource_id`。编辑使用当前 `version` |
| `/bindings/batch`、`/bindings/{id}/verify` | POST | 一次确认最多 20 个绑定并返回逐站 `bound`、`conflict` 或 `not_verified`，或重新验证一个绑定 |
| `/indexnow/keys` | GET、POST | 列表只返回元数据。创建时输入站点、验证目录、可选密钥及写入授权 |
| `/indexnow/keys/{id}/verify`、`rotate` | POST | 产生文件验证任务、轮换密钥 |
| `/indexnow/keys/{id}/reveal`、`file` | GET | Owner 显式查看或下载 TXT，两项操作均审计 |
| `/health`、`/history`、`/usage` | GET | 站点健康、任务/审计历史、请求用量。请求用量仅 Owner 可读 |

单站提交的新绑定与现有活动绑定冲突时，返回 HTTP 409 和 `integration_binding_conflict`。解除后可重新绑定，历史仍引用旧绑定 ID。

现有 `POST /api/v1/execution/jobs` 支持 `gsc_sync`、`ga4_sync`、`bing_sync`、`url_inspection`、`indexnow_submit`。提供 `site_id` 和 `idempotency_key`。可选 `connection_id`，省略时要求该站点和服务只有一个有效默认绑定。URL 检查用 `url` 并可指定 `provider`。批量提交用 `urls`，IndexNow 限 1,000 条，Bing URL 提交另限 500 条并受账户的实时配额约束。超限请求返回 HTTP 400。GSC 支持 `search_type`、`data_state` 和日期窗口。GA4 最新保留的报表质量可通过 `GET /api/v1/execution/report-quality` 查询。

## 数据、额度和权限

- GSC 按 Pacific 日期保存逐日总量和明细。明细分页、租约恢复和覆盖旧快照都在数据库中处理。`web/final` 与其他搜索类型、`all` 分开。包含未最终日期的响应保留 Google 的 `first_incomplete_date`。明细截断、未知总量、零值和失败分别显示。URL Inspection 有单独计数，仍受账户总额度约束。
- GA4 保留资源时区、采样、阈值、其他行、截断、空报表和资源配额。含最近日期的报表会提示数据可能继续更新。关键事件使用 GA4 资源定义。
- OAuth 刷新、发现和绑定验证在交互中完成，使用同一账户的持久化请求额度与诊断。站点同步、索引检查、TXT 验证和提交使用现有持久化任务队列。队列记录绑定版本、账户版本、租约、重试和最终结果。授权变化后，旧任务在下一次请求前停止。
- Owner 管理应用、凭据和绑定。Editor 可触发授权范围内的只读任务。Viewer 只读查看站点状态与报表。角色与现有 RBAC 一致。账户，站点和 Provider 由 API 与 PostgreSQL 双重约束。

[Search Analytics 数据完整性](https://developers.google.com/webmaster-tools/v1/searchanalytics/query) · [GA4 报表元数据](https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/ResponseMetaData) · [GA4 数据处理时效](https://support.google.com/analytics/answer/11198161)

## 迁移和回滚

1. 在升级前生成 PostgreSQL 加密备份并对隔离实例做实际恢复检查。独立保护与备份配套的 `SEOOS_PROVIDER_KEY`。
2. 在从备份恢复的隔离库中，用新版执行 `seoos migrate` 安装 `0010_integration_identity_and_legacy_matches.sql`。该迁移扩展账户身份，调整资源唯一规则并加入迁移回执。账户合并由独立的管理命令执行。
3. 按组织用相同部署配置运行预检和重复报告。查看目标资源、绑定结果和重新授权原因，再根据审核表应用 Bing 配对。没有 Bing 配对时，使用无参数命令处理独立账户和凭据完全一致的 Google 账户。

```sh
seoos integrations migrate-legacy --dry-run
seoos integrations migrate-legacy --show-duplicates
seoos integrations migrate-legacy --apply-approved < approved-bing-pairs.json
seoos integrations migrate-legacy --dry-run
```

审批文件是非空 JSON 数组，每项只含从重复报告选出的连接 ID 和目标账户 ID。示例 ID 仅说明格式。

```json
[{"connection_id":"11111111-1111-4111-8111-111111111111","target_account_id":"22222222-2222-4222-8222-222222222222"}]
```

Google 匹配同时要求 Client ID、Client Secret 的服务端 HMAC 和 Refresh Token 的服务端 HMAC 一致。只匹配 Client ID 的行在完整报告中标记为 `GOOGLE_CLIENT_ID_WITH_DIFFERENT_GRANT`，独立迁移。Bing 相同 API Key 的服务端 HMAC 仅生成合并建议。每次应用都重新校验审批配对。指纹在内存中按组织和字段计算，报告、数据库和日志中均不输出。

报告逐条给出 `connection_id`、`site_id`、`target_account_id`、`previous_account_id`、`match_basis`、`target_property`、`binding_result`、`reauthorization_reason` 和适用时的 `receipt_id`。预检与重复报告会回滚全部临时写入。应用按组织原子提交，过期审批导致本次事务回滚。完成的配对可重复执行而不新增回执。复用旧绑定时保留站点历史和原密文，提升变更绑定的版本以停止持有旧版本的任务。没有剩余绑定的重复账户退出账户选择列表，其加密记录仍可用于恢复。

迁移回执保存在 `seo_integration_legacy_migration_receipts`，记录旧、新账户和迁移前绑定版本、启用状态与受影响的授权范围。回滚演练采用恢复升级前快照的方式。升级或应用失败时冻结写入，保存失败现场备份，恢复升级前的快照和配套密钥，再使用上一版本通过迁移检查与健康检查后恢复流量。旧版本要求其支持的最新迁移为 `0009`。接受新写入后的快照恢复需先核对备份后的数据和可重放的变更。
