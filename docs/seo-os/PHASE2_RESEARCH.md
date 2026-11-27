# Phase 2 关键词与 SERP 研究

## 后台研究流程

在 **Provider** 新建 DataForSEO。填写 DataForSEO 控制台给出的 API 登录名和密码，配置请求上限、每次超时和 UTC 每日费用上限。Go 服务使用 `SEOOS_PROVIDER_KEY` 加密保存两项凭据。记录启用后，编辑者可以在 `/seo/research` 为其组织的活动站点提交研究。

1. **关键词研究** 输入种子词和市场，提交拓展或建议任务。在 **关键词探索** 查看关键词、搜索量、CPC、广告竞争指标、自然难度和采集时间。需要刷新单个关键词时提交指标任务。
2. **SERP 分析** 采集同一市场、同一设备的前十自然结果。选中自然结果中的外部站点，可以启动 **竞争页面** 分析并查看标题、描述、H1–H6、正文区块计数、主题、实体、FAQ、Schema、更新时间和相对本站样本的内容缺口。字段取决于上游可观测证据。
3. **搜索意图** 使用关键词特征、已观测页面类型以及 DataForSEO 的意图字段。可选用组织已有 OpenAI Provider，让模型按排名标题和页面类型补充证据。输出主次意图、用户需求、推荐页面类型、置信度和依据。结果覆盖 informational、commercial、transactional、navigational、local。
4. 为相关关键词分别采集 SERP，再运行 **关键词聚类**。聚类结合词项和概念重合、自然 URL 的 Jaccard 重合、意图、实体。合并要求成员两两至少共享两个自然 URL 且 URL Jaccard 不低于 0.20，另需通过综合阈值。证据不足的关键词保留单条聚类。
5. 在 **Keyword-to-URL** 登记本站已授权的页面元数据，运行映射。分析使用登记的页面和本站在该市场 SERP 中出现的 URL。会记录现有目标、候选 Cannibalization、Canonical 核对证据，以及新建、更新、合并或补充清单的建议。全站自动发现需要已有页面清单的上游接入。
6. 运行 **机会评分**。搜索需求、竞争难度、意图匹配、网站相关性、当前排名、商业价值的权重分别为 20、20、20、15、15、10。API 保存原始指标、来源、版本和六项解释。证据缺项时显示已覆盖权重及已测得分，完整分数留空。评分用于排定研究优先级。

**研究历史** 显示排队、取消、失败原因、完成和可用快照。**Provider 用量** 显示本 UTC 日的实付与尚未确认的预留。组织所有者可以查看原始 Provider 回执。竞品原文及声明的联系字段可能存在于受限原始回执，普通研究界面使用结构和短标签作为证据。

## 市场与数据来源

市场由 ISO 两位国家代码、DataForSEO 地区代码、语言代码、搜索引擎和设备共同标识。服务用地区代码向 Provider 请求，返回明确的异市场响应会被拒绝。配置时使用官方地区与语言列表确认组合，如 `US / 2840 / en` 和 `JP / 2392 / ja`。 [Google 地区](https://docs.dataforseo.com/v3/serp/google/locations/)、[Google 语言](https://docs.dataforseo.com/v3/serp/google/languages/)、[Bing 地区](https://docs.dataforseo.com/v3/serp/bing/locations/)、[Bing 语言](https://docs.dataforseo.com/v3/serp/bing/languages/)。

| 研究数据 | 来源和覆盖范围 |
| --- | --- |
| Google 拓展和关键词指标 | Google Labs Related、Suggestions、Overview 与 Google Ads Search Volume。上游无设备维度，返回指标标记 `device_scope=all`，同市场桌面与移动共享原始回执 |
| Bing 拓展和关键词指标 | Bing Ads Suggestions 与 Search Volume 处理 en/fr/de 和设备。Bing Labs Related 与 Bulk Difficulty 只在 US 2840/en 启用，返回全部设备口径。其余 Bing 市场按上游返回展示 |
| Google 和 Bing SERP | Advanced Live，按自然结果的 `rank_group` 取实际返回的 1–10 名，桌面和移动分别保存 |
| 竞品页面 | DataForSEO Instant Pages、Content Parsing Live、Microdata。只对当前关键词和市场上一次已观测的前十外部 URL 请求。按明确来源提取短结构证据，无法验证的 FAQ、Schema、实体、更新时间保持为空 |

每个独立指标有 `metric_sources`，包含 `source`、`snapshot_id`、`collected_at` 和 `device_scope`。同一市场中从多个真实端点得到的互补指标分别记录来源，缺失的值保持空。CPC 以 USD 表示，广告竞争和自然难度独立。Provider 的精确请求字段、费用预留和官方文档见[连接器契约](../../seoos/internal/researchprovider/README.md)。

## HTTP API

沿用 `/api/v1` Bearer 凭据和原有 RBAC。查看研究列表需要组织内站点访问权限，提交研究和登记页面需要 `owner` 或 `editor`。原始快照与费用限 `owner`。分页参数沿用 `limit` 和 `offset`。

| 方法与路径 | 响应或操作 |
| --- | --- |
| `POST /api/v1/research/jobs` | `{job}` 及 `Location`。首次提交 `201`，同键同参数重放 `200`，同键异参 `409` |
| `GET /api/v1/research/jobs?site_id=<uuid>` | `{items: [{job, request}]}`，按创建时间列出该站点的研究历史 |
| `GET /api/v1/jobs/<uuid>`、`DELETE /api/v1/jobs/<uuid>` | 查询任务或取消排队、执行中的任务 |
| `GET /api/v1/research/{seeds,keywords,suggestions,serps,competitors,intents,clusters,mappings,opportunities,pages}?site_id=<uuid>` | `{items: [...]}`。可同时附加 `country_code`、`location_code`、`language_code`、`search_engine`、`device` 精确过滤。`keywords` 的 `q` 匹配关键词，`suggestions` 的 `q` 匹配种子 |
| `POST /api/v1/research/pages` | 登记站点页面元数据，返回 `{id}`。请求含 `site_id`、完整 `market`、`page` 对象。至少提供同源且属于站点路径的 `page.url`。可带 `title`、`description`、绝对 `canonical_url`、`page_type`、`headings`、`topics` 和 `entities` |
| `GET /api/v1/research/usage` | `{items: [...]}`，组织下 DataForSEO Provider 本 UTC 日用量，金额使用 USD 微单位 |
| `GET /api/v1/research/snapshots/<uuid>` | 原始 JSON，组织级校验。回执缺失或不属于组织时返回 `404` |

提交示例，两个 UUID 需替换成组织内真实记录。`priority` 可设 0–100，缺省为 0。`target_url` 使用 `competitor_analysis` 的已观测结果 URL。`intent_provider_id` 只用于可选 OpenAI 意图分析。`business_value` 只用于机会评分，可填 0–10。

```json
{
  "site_id": "11111111-1111-4111-8111-111111111111",
  "provider_id": "22222222-2222-4222-8222-222222222222",
  "kind": "serp_analysis",
  "keyword": "robot vacuum",
  "market": {"country_code": "US", "location_code": 2840, "language_code": "en", "search_engine": "google", "device": "desktop"},
  "idempotency_key": "research-serp-us-desktop-001",
  "priority": 25
}
```

| 任务 | 所需输入或现有证据 |
| --- | --- |
| `keyword_discovery`、`keyword_suggestions`、`keyword_metrics` | DataForSEO Provider、市场、种子或单个关键词 |
| `serp_analysis` | DataForSEO Provider、查询词、市场和设备 |
| `competitor_analysis` | DataForSEO Provider、同市场最近 SERP 保存的外部 URL |
| `keyword_intent` | 市场、关键词及已有证据，可选 OpenAI Provider |
| `keyword_clustering` | 同站同市场关联的关键词，跨词合并需要足够的 SERP 重合 |
| `keyword_url_mapping` | 市场、关键词、已有本站页面证据或自然结果 |
| `opportunity_scoring` | 指标、意图和映射证据，商业价值由编辑者输入 |

## 持久化、费用与恢复

版本化迁移 `0004_research.sql` 追加研究市场、组织级关键词、站点关联和队列扩展。`seo_keyword_seeds`、`seo_keywords`、`seo_keyword_metrics`、`seo_keyword_suggestions`、`seo_keyword_clusters`、`seo_keyword_cluster_members`、`seo_search_intents`、`seo_keyword_opportunities`、`seo_keyword_url_mappings`、`seo_serp_snapshots`、`seo_serp_results`、`seo_competitor_pages`、`seo_competitor_page_snapshots`、`seo_research_jobs` 的租户与适用市场由组合外键或父记录约束。站点专属的页面、机会、映射和聚类持有 `site_id`。可重用关键词指标及 SERP 证据在组织内共享。时间字段在采集行或其父快照记录。

现有队列优先取较高 `priority`，持有 90 秒租约，研究任务执行限时 70 秒。外部单次请求上限 30 秒。DataForSEO 各 Provider 记录最多 12 次/分钟，并受每日请求数和费用上限控制。事务在请求前预留费用，并按 Provider 实际回执结算。正常缓存有效期分别为关键词 7 天、SERP 24 小时、OnPage 3 天。缓存命中沿用快照并跳过扣费。429 遵循 `Retry-After` 与现有有界重试。暂时受预算限制的任务保留队列位置，等待下一可用窗口。

任务本地幂等键和组织内请求指纹防止并发重复付费。DataForSEO `tag` 用于关联回执。Worker 租约过期后可接管。已有成功回执用于恢复落库。缺失的账单回执保留预留并标记待核对，同一个任务停止自动重复收费。取消会拒绝旧租约写入研究证据，仍保存已经发生的 Provider 账单。队列、请求预留、结算和证据写入均记录审计事件。

## 升级、回退和验收

升级前暂停写入，备份专用 SEO PostgreSQL 数据库及匹配的 Provider 加密密钥。在临时数据库验证备份可恢复，随后用 Phase 2 二进制运行 `seoos migrate`。对本地研究验收使用 `seoos_testfixture` 构建，加载正式凭据前使用标准构建和正式 HTTPS API。

需要退回 Phase 1 时，先暂停 SEO Go 服务和后台写入。保留当前数据库的独立备份，随后将迁移前备份恢复到新数据库，确认 `0001`–`0003` 状态后，把 Phase 1 API 和前端切换到该库。旧二进制会拒绝包含 `0004` 的数据库。已采集的 Phase 2 证据保留在单独的升级后备份中。

实验室验收使用真实 Go API、Worker、PostgreSQL、Next.js 和 Chrome，外部 HTTP 使用固定本地响应。真实付费账号、市场可用性与账单对账需要单独实测。自动页面清单接入及内容生产以这套研究证据为输入。参见[验证记录](../../seoos/VALIDATION.md)。
