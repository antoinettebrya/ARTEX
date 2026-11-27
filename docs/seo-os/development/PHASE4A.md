# Phase 4A 生产化加固

基线为 `f2872c0`。本次继续使用 Phase 4 的连接授权、预算、请求诊断、内容审核及发布租约，保留六个中文导航和 ARTEX App Shell。新增迁移 `0008_production_readiness.sql`。

## 搜索同步

- GSC 任务第一次执行时固定日期范围。每个日期先查询按已登记站点范围过滤的独立总量，再分页读取查询明细。两类请求各自拥有回执。Search Console 的报告日期采用 Pacific Time。
- 每次确认页保存游标和暂存明细。进程中断或滚动请求配额耗尽后继续读取未确认页。完成时事务替换同范围的旧明细，空的完整快照同样替换旧行。重复页、达到 Google 明细上限分别归入截断状态，后续日期可继续。
- `GET /api/v1/execution/snapshots?site_id=…&connection_id=…&from=…&to=…` 提供每日总量、明细状态、行数、来源、错误和采集时间。显式零和缺失总量分别返回数字与 null。旧导入明细可在报告查询，未形成每日总量前首页显示未知。失败日期的上一批明细仍保存在库中供恢复，常规读接口按最新完成或失败状态显示。任务详情返回 `gsc_progress`。
- 首页通过 `GET /api/v1/dashboard` 按站点、国家和语言服务端聚合。GSC CTR 为总点击除以总展示，平均排名按展示加权。每个站点使用最新启用、已配置的对应连接，显示站点日覆盖。GA4 使用已导入的自然会话。Google 检查、Bing、Sitemap、IndexNow 的索引观测按来源计数与筛选。AI 金额和预算仅所有者可见。

[按日读取 Search Console](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data) · [Search Analytics 查询契约](https://developers.google.com/webmaster-tools/v1/searchanalytics/query)

## 模型、任务与内容

- Responses、兼容 Chat Completions 和原生 Claude Messages 共用连接、错误分类、诊断和结算管道。Claude 保留已有兼容协议选项。原生 Messages 使用专用认证 Header、版本头及结构化输出参数。
- 站点简报、补充意图和编辑任务使用同一模型适配层。配置费率时，报价和实际发送使用相同序列化规则与提示词。输入预留按请求字节数加协议余量，实际 Token 和配置费率用于结算。费用账单无法判明的调用保留预留，自动重试仅用于明确未计费的错误。Claude 缓存写入量计入输入 Token，账单可能含单独缓存写入费率，应以厂商导出核对。
- 任务列表支持 `site_id`、`site_country`、`site_language`、`state`、`kind`、UTC `day` 和 `limit/offset`，查询先限定组织再分页。内容编辑要求明确的项目 ID。未保存修改保存在当前浏览器标签页，版本变更后须明确确认合并，最终提交继续使用服务端乐观并发校验。
- CMS 发布、恢复写入和修改已有目标的审阅操作会核对备份目标 ID、资源类型、可恢复状态和 Slug。写前快照、意图与授权在事务内验证，未知的远程写入以回读核对。明确未写入的发布失败保留可重新审阅的草稿。

[Claude Messages](https://platform.claude.com/docs/en/api/messages/create) · [Gemini 推理参数](https://ai.google.dev/gemini-api/docs/openai) · [DeepSeek Thinking](https://api-docs.deepseek.com/guides/thinking_mode/)

## 独立测试与对账

- 标准二进制配独立 PostgreSQL 和测试组织。运行 `python3 seoos/scripts/verify_live_providers.py /安全路径/测试配置.json`。配置文件需为私有权限，含 `api_origin`（标准 Go API 的回环 HTTP 地址）、`api_token`、`site_id`、`run_id`、`maximum_cost_micros` 和 `models`。每条模型记录包含 `kind`、`model`、`api_key`、`budget_micros`、`input_micros_per_million`、`output_micros_per_million`，可选 `model_options`、`max_output_tokens`。每个模型最多保留一次推理请求，请求按 run ID 幂等。
- 厂商同账户 UTC 日账单可通过 `vendor_micros`、可选 `vendor_day` 提供。结果包含请求回执、实际 Token、已确认费用、估算和差异。缺少厂商账单显示待核对。隔离 HTTP 夹具和真实接口分别报告。
- GSC、GA4、Bing 业务验收使用已授权的测试资源。公共端点可达性和未授权响应不能作为业务验收。IndexNow 的外部提交须使用已登记并可验证所有权的测试站点。

## Docker 升级、回退与监测

- 复用独立 Compose 项目，Next.js、Go API、Worker 为三个容器，PostgreSQL 用独立命名卷。Web 绑定回环，由 HTTPS 反向代理接入。互联网入口应为明确配置并持有可信证书的子域名。
- 将固定提交 SHA 放入 `releases/<sha>`，以 `bash seoos/deploy/release.sh <sha>` 运行。脚本先完成镜像构建，再切断当前写入，生成加密备份，用校验和验证后实际恢复为一个新数据库，再只对候选数据库迁移。API、Worker、Web 健康检查通过才移交 `current`。如设置 `ARTEX_STAGE_HEALTH_URL`，还核验指定 HTTPS 登录入口。失败会停止候选服务并重新启动上一版及原数据库。
- `bash seoos/deploy/monitor.sh` 检查 API readiness、Worker 真实领取循环心跳以及 Web。Worker 心跳过期或进程失效时检查失败，可接入外部监控。最长工作租约下心跳容许 150 秒。
- 已验收版本可从当前版本目录运行 `bash seoos/deploy/rollback.sh`。回退前会为当前数据库生成新的加密备份，上一版仍连接其对应数据库。新版期间的写入继续保存在新数据库和备份中，重新上线时需核对是否补录。单独恢复使用 `restore.sh <archive> <new_database>`。
- 可由现有 systemd 定时器运行 `backup.sh`。配置 `ARTEX_BACKUP_REMOTE=SSH别名:/绝对目录` 时，把加密包和校验和传送到独立存储，再在该存储核对 SHA-256。SSH 别名、授权及可信主机密钥应先配置。备份密钥、应用 Provider 加密密钥应分别保管。
