# Phase 3 内容智能与质量门禁

在 ARTEX 的 **内容工作室**（`/seo/content`）选择站点和内容项目。项目以站点、现有 Phase 2 关键词和完整市场为单位。关键词、SERP、Page Mapping、聚类、竞品结构和机会分数均来自同一站点可访问的 Phase 2 数据。

## 从站点清单到发布包

1. 所有者在 **站点清单** 保存只读授权。授权绑定当前登记的 origin 和路径。选择 Sitemap、WordPress REST 或通用 CMS 清单，并列出可以作为一手证据的精确来源 origin。站点登记和授权是两个操作。
2. 提交 **发现站点页面**。发现任务将候选 URL 和采集子任务写入现有队列。采集记录包括状态、Title、Description、H1–H6、Canonical、Robots、Schema、正文、图片及 ALT、语言、页面类型、公开更新时间、分类和标签、采集时间和指纹。条件请求与指纹复用现有快照。
3. 为已有 Phase 2 关键词与市场创建内容项目。提交 **生成内容策略**，检查新建、更新、合并、调整页面类型、Topic Cluster、蚕食或暂停原因。策略需要可用的该市场自然 SERP 和站点证据。
4. 生成 **内容简报**。简报包括关键词、意图、本地读者需求、页面类型、推荐 URL、元数据、标题结构、问题、实体、主题、来源、竞品缺口、图片、内链及事实要求。简报可编辑和重新生成，每次写入留版本。英文、中文、日文、泰文、缅甸文、高棉文分别从本市场证据与语言规则构建。
5. 在 **研究来源** 拉取已批准的一手资料。系统在 Phase 2 SERP 中筛选许可 origin，支持追加官方文档、监管部门、原始研究和授权资料。竞品输入保留已采集结构与缺口。每条可用事实关联采集来源、片段、市场、可信度和冲突状态。
6. 在 **文章编辑** 生成大纲并分章节写作。大纲由一次模型请求生成，正文按大纲进行 2–4 次章节请求。编辑器保存结构化稿件和引用。SEO 编辑生成 Title、Description、层级、Slug、Canonical、站内链接、ALT 和 Schema 建议。内容版本可以比较、编辑和恢复。
7. 运行 **核验文章事实** 和 **独立质量审核**。事实核验检查渲染文本与真实证据的关系，并识别未引用的可检测重要断言。质量门禁将规则、来源、近似重复、市场语言和独立模型审核组合打分，存储 12 项细分指标、理由、建议和算法版本。
8. 所有者阅读当前稿件和门禁结果，提交独立的人工审批意见。通过审批后可打开最终预览，生成含文章和引用来源的 CMS JSON 包。修改稿件或简报、重新采集证据或重新执行门禁后需要为有效版本重新审批。
9. 外部 CMS 上线后，所有者可对授权站点的发布 URL 提交核验。核验通过要求实际读回标题及各正文段落。Phase 3 提供准备、下载和只读核验接口。正式 CMS 写入由后续 Connector 阶段接入。

更新旧文章前，工作室记录库存快照和变更信号。**内容更新** 支持登记可追溯的排名、流量、CTR、SERP、意图和来源变化，生成保留、局部更新、重写、合并或检查 noindex 的建议。更新稿沿同一简报、写作、门禁和审批流程生成。

## 只读采集与证据限制

| 功能 | 实现 |
| --- | --- |
| Sitemap | 支持 Sitemap 索引和 gzip，默认最多 100 个 URL，可配置 1–1000 |
| WordPress | 授权范围的公开 REST 页面、文章、分类、标签，再对相应公开 URL 观察 HTTP 和正文 |
| 通用 CMS | `connector: "json"`，同范围接口返回 `{"items":[{"url":"https://example.com/article","kind":"article","categories":["Guides"],"tags":["Maintenance"],"modified_at":"2026-08-01T09:00:00Z"}],"next":null}`。可提供同范围分页 URL |
| 独立站点 | 首选 Sitemap，通用站可回退至已授权入口中发现的同范围链接，结果标记为截断 |
| 提取 | 有界 HTTP GET、条件请求、读回 HTTP 状态和 HTML 元数据。WordPress 正式认证与写入、PDF 与需要浏览器运行 JavaScript 的正文提取属于后续能力 |
| 索引状态 | 当前 `unverified`。采集成功、发现、robots 指令、已有关键词映射分别记录。真实索引状态接入可靠索引来源后更新 |
| 事实与重复 | 保守匹配已捕获片段，登记过时和相互冲突的证据。站内重复比较基于近期同站采集样本和词项、连续短语。模型复核补充可读性、意图与独立读者价值 |

站点读取不跟随重定向，遵守时间和响应体限制，以及每进程同一 origin 的请求间隔。授权期间变更站点 URL 会使原授权失效。一手资料抓取只选批准的 origin，每次研究最多接收五条成功来源。地区事实的适用性需由该市场来源和审阅人确认。 [Sitemap 协议](https://www.sitemaps.org/protocol.html) · [WordPress REST 文章](https://developer.wordpress.org/rest-api/reference/posts/) · [WordPress REST 页面](https://developer.wordpress.org/rest-api/reference/pages/)

## Model Provider、预算与任务

在原 **Provider** 页面登记 GPT、Claude、Gemini 或 DeepSeek，再保存内容模型档案。模型档案包含角色、System Prompt、温度、输入限制、每日 Token 和 USD 预算、输入与输出计价、重试次数和退避。密钥沿用现有 Provider 加密库。请求超时和最大输出 Token 沿用 Provider 设置，内容请求还有 15 秒上限。

模型连接器使用固定的官方 API，单次请求将上下文和结构化 JSON 任务交给模型，且无工具权限。具体模型需要支持对应 API 和配置参数。写作和独立质量审核需要不同的 Provider 记录和模型标识。 [OpenAI Responses](https://developers.openai.com/api/reference/resources/responses/methods/create) · [Claude 兼容 API](https://platform.claude.com/docs/en/build-with-claude/openai-sdk) · [Gemini 兼容 API](https://ai.google.dev/gemini-api/docs/openai) · [DeepSeek API](https://api-docs.deepseek.com/)

费用预留使用保守的输入估算和输出上限。响应中的实际 Token 用于按配置单价结算估算费用。用量分开显示已计和仍预留的 Token 与 USD 微单位。可确认未计费的限流响应按队列重试策略退避。中断后已记录的章节回执可复用，账单不明的回执保留预留并停止同任务重复提交。实际账单以 Provider 对账结果为准。

现有队列提供 0–100 优先级、幂等键、90 秒租约、取消和有界重试。Phase 3 任务执行限时 70 秒。锁定当前租约和项目修订后再写版本，防止旧任务覆盖新版本。队列和写入过程使用组织审计。老的 `POST /api/v1/jobs` 的 `site_inventory` 继续生成站点资料快照。新的 `POST /api/v1/content/jobs` 的同名任务执行授权发现。

## API

沿用 `Authorization: Bearer <组织凭据>` 和既有同源代理。站点列表使用 `site_id`，分页用 `limit`、`offset`。发布审批、准备、核验任务提交、抓取授权更新和模型预算管理限所有者。编辑者可以生成和编辑稿件，查看者可以读取组织内内容。

| 方法和相对 `/api/v1/content` 路径 | 功能 |
| --- | --- |
| `GET /projects?site_id=<uuid>`、`POST /projects` | 列出项目，或按 `site_id`、`keyword`、完整 `market` 建立项目 |
| `GET /projects/<id>` | `{workspace}`，包含 Phase 2 引用、策略、简报、大纲、稿件、事实、质量、版本、更新和发布记录 |
| `POST /projects/<id>/actions` | 请求含 `action` 和当前 `revision`。可用 `save_brief`、`restore_brief`、`save_article`、`restore_article`、`add_source`、`save_signals`、`approve`、`prepare_publication`。恢复需 `version`，审批需 `note` |
| `GET /jobs?site_id=<uuid>&project_id=<uuid>`、`POST /jobs` | 列执行记录，或提交 `site_id`、`project_id`（项目任务）、`kind`、`provider_id`（模型任务）、`idempotency_key`、可选 `priority` 和提取或核验 `url` |
| `GET /inventory?site_id=<uuid>`、`GET /inventory/<id>?site_id=<uuid>` | 页面清单和采集详情 |
| `GET/PUT /crawl?site_id=<uuid>` | 只读授权、清单来源、页数上限和一手来源 origin |
| `GET/PUT /models`、`GET /usage` | 内容模型档案和 UTC 当日用量 |

所有任务可从原 `GET/DELETE /api/v1/jobs/<id>` 查询或取消。采集任务为 `site_inventory`、`page_extraction`。项目任务为 `content_strategy`、`content_brief`、`content_research`、`content_outline`、`content_draft`、`content_fact_check`、`content_quality_review`、`content_seo_optimization`、`content_refresh` 和 `content_publish_verify`。

## 迁移、回退与验收

追加的 `0005_content.sql` 扩展 Provider 类型、原队列、站点授权和盘点，以及内容项目、来源、事实、简报与文章版本、章节、审核、质量、更新、发布、模型档案和用量台账。各项目引用同组织同站点市场的 Phase 2 关键词，事实证据引用同项目来源，内容发布包引用准确的文章版本。

升级前保留匹配的 Provider 加密密钥和 Phase 2 数据库备份，先在独立 PostgreSQL 中验证。需要回退时暂停写入并保留升级后的备份，恢复迁移前的备份到独立数据库，再用 Phase 2 产物启动。Phase 2 二进制会拒绝含有 `0005` 的库。

验收由真 PostgreSQL、Go Worker 和 Next.js 生产构建执行。站点、模型和 DataForSEO 采用固定本地 HTTP 夹具，记录见 [Phase 3 验收报告](../../seoos/VALIDATION.md)。正式模型、正式站点和收费调用需要单独做生产验收。Cloud 数据、排名与 Search Console 的自动接入、CMS 正式写入和对账属于下一阶段依赖。
