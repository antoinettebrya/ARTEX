# Phase 4 站点接入与 SEO 执行

`/seo/execution` 使用 ARTEX 的 App Shell。工作区按站点显示连接、发布、报告、索引、Sitemap、提取、优化反馈、任务与用量。内容工作室的已审批发布包可直接进入发布中心。

## CMS 接入、发布和恢复

组织所有者为站点登记 HTTPS 连接。WordPress 使用具备目标内容权限的专用账号和 Application Password。先执行连接检测，确认当前账号可读取或修改文章、页面和媒体。分类与标签从实际 REST 接口读取。通用 CMS 可在注册站点下实现受 Bearer Token 保护的 HTTP 适配接口。WordPress 字段和鉴权仅在 WordPress 适配器内处理。[WordPress 鉴权](https://developer.wordpress.org/rest-api/using-the-rest-api/authentication/) · [通用 CMS HTTP 契约与 WordPress 兼容规则](../../seoos/internal/executioncms/README.md)

新建 WordPress 连接时，后台预填已登记的 HTTPS 站点地址。可修改为该站点范围内的安装子目录，例如 `https://example.com/blog`。适配器自动拼接 REST 路径。更换域名请先在「站点管理」修改登记地址。更改连接地址后必须重新填写对应的用户名和应用程序密码。应用程序密码可在 WordPress 的「用户 → 个人资料 → 应用程序密码」生成。启用并保存连接后，在列表中点击「测试连接」。

CMS 写入需要启用连接写权限，并由所有者单独操作。审批包必须属于该组织和站点，且仍对应通过事实核查、独立审核和质量门禁的当前文章版本。创建发布记录后先保存草稿。修改已有文章时另建审阅草稿，原页面在所有者授权前继续提供旧内容。正式发布或预约发布需要填写独立授权理由。预约用于新建内容，依赖 CMS 的计划发布能力。WordPress 应配置有效的前端定时任务或系统 Cron。

`cms_publication` 成功后自动排队 `cms_publication_verify`。即时发布核验可立即运行，预约核验到点后运行。核验重新读取 CMS 记录及匿名访问的最终页面，检查 HTTP、标题、正文、Canonical 及实际可验证的 SEO 字段。支持手动复核。Yoast 或 Rank Math 的 REST 命名空间和编辑 schema 分别检测，只有实际暴露的对应编辑字段可写。发布状态和前后记录包含真实目标 ID 和 URL，CMS JSON 回执加密保留，审计只保存安全标识和状态。[Yoast REST API](https://developer.yoast.com/customization/apis/rest-api/) · [Rank Math REST](https://rankmath.com/kb/headless-cms-support/)

任务在提交远程写入前持久化写入意图和快照。Worker 恢复时用目标 ID 或草稿标记确认已应用操作，遇到未知结果保留待恢复状态。连接配置、站点地址变化和发布包过期会阻止未执行的写入。出现核验差异时，所有者可审阅发布历史后执行回滚。已有内容恢复发布前字段，新页面回到草稿，操作不会删除远程记录。远端页面在 ARTEX 发布后已被编辑时，回滚会记录差异并保留远端内容，需要先由编辑人员核对。提交回滚时会撤销仍在排队的核验。

## 搜索报告与索引证据

| 数据源 | 连接与产出 |
| --- | --- |
| Search Console | 启用 Search Console API，使用 OAuth Web 客户端、只读 scope 和已验证资源。支持域名或 URL-prefix property。持久化日、查询、页面、国家、设备、点击、展示、CTR 和平均排名。支持 Sitemap 状态与 URL Inspection。 |
| GA4 | 启用 Analytics Admin API 与 Data API。使用只读 OAuth 和数字 Property ID。按 `Organic Search`、站点 hostname 与路径约束同步会话、活跃用户、落地页、互动、关键事件及会话关键事件率。保留国家、语言、设备与日期。 |
| Bing Webmaster | 使用已验证站点的 Bing Webmaster API Key。读取站点、Sitemap、站点日趋势、查询与页面数据，保留各报告粒度。开启写权限后支持检查提交额度和提交 URL。 |
| IndexNow | 所有者为站点登记同域 HTTPS key 文件。Worker 读取并校验文件后提交目标 URL，保存 HTTP 和平台状态。HTTP 202 记录为等待平台验证 key。 |

Google 授权回调使用后台的 HTTPS `/seo/execution`。OAuth state 与组织、站点、连接、版本绑定，10 分钟内只可消费一次，授权码由 S256 PKCE 保护。Access Token 过期时使用加密 Refresh Token 刷新。应用先把数据写入站点与连接隔离的日快照，支持按日期、页面和国家筛选。GSC 国家使用三字代码，GA4 使用 API 返回的国家显示名。默认同步最近 14 个可查日期，后续利用最近的日快照并重叠两天来吸收更新。手动单次同步范围最多 31 天。GSC 和 GA4 每页 1,000 行。达到单任务读取上限或 GA4 告知部分数据时，会在结果标记 `truncated`。可按较短日期范围重取报告。GSC `position` 是 Google 返回的平均排名，Bing 的站点、查询和页面数据应分别读取。 [Search Analytics](https://developers.google.com/webmaster-tools/v1/searchanalytics/query) · [URL Inspection](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect) · [GA4 Data API](https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/properties/runReport) · [Bing API](https://learn.microsoft.com/en-us/bingwebmaster/api-protocols)

同时有近期和此前两个七天窗口的足量页面数据时，系统将同站目标页的 CTR、按展示加权的平均排名变化或自然会话变化写回 Phase 3 项目，供编辑人员评估更新。

Sitemap 可从同站 `robots.txt`、配置地址或 CMS 取得。解析索引、子文件、gzip、`lastmod` 和 `hreflang`，去重后以每卷最多 1,000 URL 导出 XML。每次采集最多读取 20 个 Sitemap、保留 10,000 个已授权 URL；单个 XML 限 8 MiB。只访问登记站点范围内的子 Sitemap。跨域 hreflang 可保存为引用。生成的卷和索引可从后台下载，部署 XML 到站点目录由站点发布流程处理。报告同时显示站点发现和 GSC、Bing 返回的处理状态。[Sitemaps 协议](https://www.sitemaps.org/protocol.html) · [Google hreflang](https://developers.google.com/search/docs/specialty/international/localized-versions)

索引记录带有来源、证据和观察时间。`DISCOVERED` 来自站点地图或服务商发现证据，`SUBMITTED` 来自平台接受回执，`CRAWLED` 表示已有抓取证据，`INDEXED` 与 `NOT_INDEXED` 根据官方检索结果记录。Bing 官方接口返回抓取时间时记录抓取证据，缺乏索引判定时使用 `UNKNOWN` 或已有发现证据。 [IndexNow 协议与 HTTP 响应](https://www.indexnow.org/documentation)

## 采集与预算

先在内容工作室为站点启用采集授权。PDF 和 JavaScript 提取任务只接收该授权范围内的 HTTPS URL。公网站点网络请求经 DNS 地址检查与固定连接，拒绝内网、代理和重定向。PDF 使用 Poppler，限制为 8 MiB、最多提取前 100 页文字，回传文件页数、可读元数据和最多 120,000 字节文本。纯扫描件可能没有文本层，OCR 可作为后续扩展。

JavaScript 由隔离的 Playwright/Chromium 进程渲染。每次使用临时浏览器上下文和拒绝直连的代理，只把经 Worker 核验的同源且同站点路径 GET 请求交给 Go。仅允许文档、脚本、样式与同站点 XHR/fetch，最多 16 次网络请求、单资源 1 MiB、总计 5 MiB、总体 24 秒。输出为渲染后的可见正文、标题与可见 H1–H6。运行环境必须支持 Chromium 沙箱。[Playwright 容器沙箱](https://playwright.dev/docs/docker#crawling-and-scraping)

连接默认超时 12 秒，限每分钟 30 次和每 UTC 日 500 次请求。每次通过租约和授权校验的 HTTP 请求登记一条计量，包括 OAuth、只读回查和重试。执行任务复用 PostgreSQL 租约、优先级、幂等、退避、取消、审计和恢复。返回给运营人员的费用为可计量的每次 API 调用费用，Phase 4 已接适配器按零单次费用记录。WordPress 托管、基础设施及第三方平台相关费用仍由其账户或账单管理。加密凭据、OAuth Token 与 CMS 原始回执共用部署的 `SEOOS_PROVIDER_KEY`，不会通过后台读接口返回。

## API

以下路径位于 `/api/v1/execution`。沿用 `Authorization: Bearer` 和组织角色。返回对象以 `connection`、`publication`、`job` 或 `items` 包装，任务与发布记录的幂等重放返回原记录。所有列表带 `site_id`，支持 `limit` 和 `offset`。

| 方法与路径 | 权限与字段 |
| --- | --- |
| GET `/connections` | 查看站点连接、版本、启用和能力状态。CMS 检测后提供 `site_info`，含站点信息及读回的分类和标签。 |
| POST `/connections`，PUT `/connections/{id}` | 所有者。传 `site_id`、`kind`、`name`、`enabled`、`allow_writes` 等设置。修改需 `version`，只在创建或轮换时送 `credential`。 |
| POST `/oauth/start` | 所有者。传 `site_id` 和 `connection_id`，读取 `authorization_url` 与 `state`。 |
| POST `/oauth/exchange` | 所有者。送同一连接的 `state`、`code` 和两个 ID。 |
| GET `/packages` | 读取本站点当前可发布的 Phase 3 包。 |
| POST `/publications` | 所有者。传 `site_id`、`connection_id`、`package_id`、`resource`（`post` 或 `page`）、`idempotency_key`，可选 `remote_id` 更新已有条目。 |
| GET `/publications`，GET `/publications/{id}` | 发布列表或带事件的详情。待核验时带 `verification_job_id`。 |
| POST `/jobs` | 提交任务。需 `site_id`、`kind`、`idempotency_key`，按类型补连接、发布 ID、URL、日期或媒体。支持 `priority`（0–100）。CMS 写入和 URL 提交限所有者。 |
| GET `/jobs`，GET `/pages`，GET `/media`，GET `/properties` | 查任务、内容、媒体与分析资源。`pages`、`media`、`properties` 可按 `connection_id` 筛选。 |
| GET `/metrics` | 可选 `connection_id`、`kind`、`from`、`to`、`page`、`country`。 |
| GET `/index`，GET `/submissions`，GET `/sitemaps`，GET `/sitemaps/{job_id}` | 查证据、提交回执、Sitemap 与导出 XML。Sitemap 详情带 `site_id`。 |
| GET `/extractions`，GET `/feedback`，GET `/usage` | 查提取产物和内容反馈，所有者可查计量。 |

任务类型有 `cms_connection_test`、`cms_page_sync`、`cms_media_upload`、`cms_publication`、`cms_publication_verify`、`gsc_sync`、`ga4_sync`、`bing_sync`、`sitemap_refresh`、`indexnow_submit`、`url_inspection`、`pdf_extraction`、`browser_extraction`。读取状态和取消沿用 `GET/DELETE /api/v1/jobs/{job_id}`。发布任务附 `publication_id`，且 `operation` 为 `draft`、`publish` 或 `rollback`，后两者还需 `note`。定时发布附 RFC 3339 `scheduled_at`。具有写权限的 Bing 连接也可用 `indexnow_submit` 任务发起 Bing 官方直接提交，回执标明实际服务商。媒体上传接受 PNG、JPEG、WebP、GIF，传 `filename`、`media_type` 与 `media_base64`，原文件上限 4 MiB。

## 部署和回滚

1. 暂停生产入口的写入和 Worker。在迁移前获取 PostgreSQL 一致性备份并验证能在隔离库恢复，同时保留 Phase 3 镜像、前端构建和原 `SEOOS_PROVIDER_KEY`。外部 CMS 应保留其站点备份。
2. 使用新 Go 服务执行 `seoos migrate`，再启动 API 和 Worker。新迁移为 `0006_execution.sql`。后台使用同一版本的 `next start`，沿用现有 `SEOOS_API_ORIGIN` 和 HTTPS 代理。验收生产环境前先用[本地测试命令](../../seoos/VALIDATION.md)。
3. 默认 Docker 镜像内置 Poppler。启用 JavaScript 使用 `docker build --target browser -t artex-seoos:browser .`。以非 root 用户和启用用户命名空间的 Chromium 沙箱运行，在容器配置中使用匹配的 Playwright seccomp profile、`cap_drop: [ALL]`、`cap_add: [SYS_CHROOT]`、`no-new-privileges`、`init: true` 和私有的 256 MiB `/dev/shm`。测试脚本固定 profile release 和校验和，并在禁用容器外网时验证浏览器和 PDF。生产保留对授权站点和官方 Provider 的网络连接。[Playwright seccomp profile](https://github.com/microsoft/playwright/blob/v1.65.1/utils/docker/seccomp_profile.json) · [沙箱验证脚本](../../seoos/scripts/test_extraction.sh)
4. 回退服务版本时先停止新 Worker，单独保存迁移后数据库和外部写入记录。使用迁移前备份启动 Phase 3 镜像及前端，读回原站点与内容记录。旧二进制会拒绝较新的数据库迁移版本。已经发生的站外写入由所有者按发布记录回滚，或使用 CMS 快照恢复。

真实本地 WordPress 已跑通 HTTPS、草稿、发布、匿名回读、同 ID 恢复和定时发布。OAuth、GSC、GA4、Bing、IndexNow 与通用 CMS 的自动化验收使用本地 HTTP 夹具。生产验收还需授权测试 WordPress、安装对应插件的环境，以及有数据访问权限的 Google 和 Bing 账号。 [测试与用量实测](../../seoos/VALIDATION.md)
