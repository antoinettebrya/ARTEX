# ARTEX SEO OS 模型与外部数据接口审计

本次工作基于 `seo-os-v1`。用户指定的 `7a6a624` 是祖先提交。实际开发保留了 `c63d46a` 的 Provider 分区和 `0ccd88d` 的 WordPress 地址编辑。迁移 `0007_provider_integrations.sql` 在本地 PostgreSQL 和测试环境验收。线上独立发布的是 WordPress 热修复。

## 问题和修复

| 范围 | 修复前 | 修复后 | 代码 |
| --- | --- | --- | --- |
| OpenAI Responses 与推理模型 | 内容调用固定发送 `temperature`。模型能力没有独立配置。 | 根据厂商、模型系列和显式配置决定采样与推理参数。已知不支持采样的推理模型省略该字段。Responses 使用 `reasoning.effort`、`max_output_tokens` 和 `text.format`。 | [请求构建](../../seoos/internal/contentmodel/payload.go) |
| Claude、Gemini 和 DeepSeek | Chat 适配器沿用同一套参数，Claude 缺少 Workspace 选项。 | 各自使用官方兼容端点。Claude 支持 Workspace ID 和严格 JSON Schema。Gemini 默认 JSON Object。DeepSeek 按配置发送 thinking 参数，结构化模式用 JSON Object。 | [模型能力](../../seoos/internal/domain/model_options.go) · [厂商适配](../../seoos/internal/contentmodel/payload.go) |
| OpenAI 兼容 API | 地址和认证方式固定。 | 独立 Provider 支持 HTTPS Base URL、两种协议、自定义认证 Header、原始密钥或 Bearer。改路由或认证头须重输密钥。生产传输使用公共地址校验、固定 DNS 结果与禁止重定向。 | [配置约束](../../seoos/internal/domain/model_options.go) · [存储](../../seoos/internal/store/providers.go) · [HTTP 客户端](../../seoos/internal/contentmodel/model.go) |
| 返回值与诊断 | 认证、权限、超时和部分厂商错误被合并。生成失败、拒答、截断及缺少用量的解析有限。 | 保留 HTTP 状态、请求 ID、白名单错误代码/参数、失败分类、耗时和计费状态。按 Responses/Chat 解析实际用量和缓存 Token。对配置严格 Schema 的输出校验字段和类型。 | [响应解析](../../seoos/internal/contentmodel/model.go) · [安全诊断](../../seoos/internal/providerhttp/receipt.go) |
| 超时 | 内容调用被固定截到 15 秒。正文写作的 Worker 限时不足以覆盖四次最长配置调用。 | 单请求使用配置的 1–30 秒。正文任务有 130 秒工作窗口与 150 秒租约。已建立连接后超时或断开要保留可能计费状态。 | [HTTP 超时](../../seoos/internal/contentmodel/model.go) · [Worker 限时](../../seoos/internal/worker/worker.go) · [租约](../../seoos/internal/store/jobs.go) |
| Token 与价格 | 预留估算未覆盖实际请求封装和 Schema。旧简报路径只记录调用次数，未使用已配置的内容模型价格。 | 根据序列化请求及协议余量预留输入与输出 Token。支持可选缓存输入费率。已设费率档案的站点简报、意图和内容任务共用每日预算。实际 Token 按回执结算，美元结果标注为估算。 | [预留与结算](../../seoos/internal/store/content_models.go) · [旧路径结算](../../seoos/internal/store/providers.go) |
| 模型重复请求 | 旧简报与意图遇到断线、408 或 5xx 可能自动再次发送。 | 先持久化预留，已结算的成功结果可在租约恢复时复用。账单或回执未确认时保留预留。明确的未计费限流可按策略重试。 | [模型去重](../../seoos/internal/store/providers.go) · [Worker 结算](../../seoos/internal/worker/worker.go) |
| DataForSEO | 诊断缺 HTTP 状态/请求 ID。未确认回执只拦同一任务 ID。收到正费用后某些失败仍会重试。 | 记录安全 HTTP 与任务回执。组织内 24 小时按请求指纹拦截结果不明的重复采集。已记正费用的失败不自动重复请求。继续复用有效快照。 | [DataForSEO HTTP](../../seoos/internal/researchprovider/dataforseo.go) · [研究账本](../../seoos/internal/store/research_budget.go) · [重试决策](../../seoos/internal/worker/research.go) |
| GSC 数据 | 整个时间窗统一分页，聚合方式由厂商默认确定。同一维度的重复行未在导入前标记。 | 按日翻页，显式使用 `byPage`，校验聚合类型。重复行、分页上限或滚动配额耗尽时标记截断，保留已取得的部分。CTR 按点击数与展现数计算，排名按展现量加权。 | [Search Analytics](../../seoos/internal/executionsearch/gsc.go#L62) · [反馈聚合](../../seoos/internal/store/execution_feedback.go) |
| GA4 与 Bing | 资源目录读取后未确认所选资源在已授权列表中。GA4 分页总行数变化或重复日期/维度行缺少标记。 | 核对所选 GA4 Property 和 Bing 站点。GA4 保持自然流量、域名与路径过滤。异常分页标记截断。相同活跃或近期完成的分析同步任务合并。 | [GA4](../../seoos/internal/executionsearch/ga4.go) · [Bing](../../seoos/internal/executionsearch/bing.go) · [同步去重](../../seoos/internal/store/execution_jobs.go) |
| IndexNow 与 Bing 提交 | 进程若在发送后、保存回执前中断，恢复时可能重复提交。 | 发送前持久化提交意图。相同 URL 的近期成功或待确认提交拦截重复发送。明确拒绝保留失败记录，429 可按限流重试。HTTP 202 记录为等待验证。 | [提交意图](../../seoos/internal/store/execution_results.go) · [提交 Worker](../../seoos/internal/worker/execution.go) |
| 账单与观察 | 多个日志接口分别记录局部信息，缺统一回执和日对账。 | 请求诊断读取真实持久化账本。按资源和 UTC 日分别汇总厂商已回报金额、模型估算、实际 Token 与未结算预留。 | [迁移](../../seoos/internal/store/migrations/0007_provider_integrations.sql) · [账单查询](../../seoos/internal/store/provider_receipts.go) · [原后台 Provider 页面](../../web/src/app/%28main%29/seo/_components/providers.tsx) |

执行传输同时记录 Google OAuth、GSC、GA4、Bing、IndexNow 和 CMS 的 HTTP 请求回执。运营诊断中没有完整请求或响应正文、Authorization Header、API Key 或 OAuth Token。遗留调用的可用字段来自原账本。故障发生在数据库结算前时，预留行可辨识为未结算。

## 操作与接口

- 「Provider → 大模型服务」选择厂商。「接口和模型能力」设置推理和输出；兼容 API 的 HTTPS Base URL 应含厂商的版本路径，例如 `https://api.example.com/v1`。应用追加 `/responses`、`/chat/completions` 或只读测试所用的 `/models`。默认 Authorization 使用 Bearer，专用 Header 默认直接发送密钥。
- 「配置内容模型」设置每百万 Token 的输入、可选缓存输入和输出价格，以及每日 Token 和美元预算。首次请求按完整未缓存费率预留；有厂商缓存用量时再折算。价格取决于已配置费率，Provider 对账前应同步合同或项目的实际费率。
- 「Provider → 请求诊断」显示脱敏回执。字段 `retryable` 表示错误具备重试性质；自动重试还受计费确定性、任务租约、次数和配额约束。
- `GET /api/v1/providers/receipts?provider_id=UUID&limit=25&offset=0` 仅组织所有者可用。可省略资源 ID。每条包含 `diagnostic`、时间、关联任务、来源、状态和预留金额。
- `GET /api/v1/providers/{id}/reconcile?day=2026-11-26&vendor_micros=1450` 比较同一账号、同一 UTC 日的 USD 账单金额。返回 Token、已回报费用、估算费用、未结算调用数和差额。有未确定费用时差额为空。请求为只读，比较方提供的金额未作为已核实账单入账。
- 未配置费率的旧站点简报、意图调用仍受原请求数和输出限制，回执显示实际 Token 与待确认金额。GSC API 省略部分匿名或低量查询，详细行可做明细聚合。厂商的全量站点指标需用厂商级汇总。
- 自定义服务可能允许推理而缺少 `/models` 查询。连接测试使用只读目录，诊断界面会显示其真实状态。

## 官方端点可达性

2026-11-26 执行真实 HTTPS 请求。未携带账号密钥，也没有提交已授权资源。这一组记录是公网握手和未授权/无参数响应，费用为 0 美元。

| 端点 | 实测 HTTP | 已授权业务验收 |
| --- | ---: | --- |
| OpenAI Responses | 401 | 待独立测试项目 |
| Anthropic OpenAI Compatible | 401 | 待独立测试 Workspace |
| Gemini OpenAI Compatible | 400 | 待独立测试项目 |
| DeepSeek Chat Completions | 401 | 待独立测试账户 |
| DataForSEO User Data | 401 | 待独立 API Access |
| Google OAuth Token | 400 | 待独立 OAuth 应用与授权回调 |
| GSC sites | 401 | 待已授权的测试 Property |
| GA4 accountSummaries | 401 | 待已授权的 GA4 测试 Property |
| Bing GetUserSites | 400 | 待已验证测试站点和 API Key |
| IndexNow | 400 | 待测试站点和所有权文件 |

真实计费、账号授权、GSC/GA4/Bing 真实数据覆盖和 IndexNow 的真实提交回执仍待独立账户验收。实际账单未取得。测试时没有读取暂存后台已保存的外部厂商凭据。自动导入厂商发票还需要相应账单权限，例如 OpenAI Organization Costs 管理权限。

## 隔离验证

- `go test -race -count=1 ./...` 使用真实 PostgreSQL。涵盖 400、401、403、408、429、500、502、503、本地 HTTP 断线/超时、严格 Schema、安全 Header、重启后复用回执和阻止重复发送。
- `go vet ./...` 和正常、回环夹具二进制均通过。 `python3 scripts/smoke_test.py` 验证真实 HTTP 与 PostgreSQL 的组织隔离、幂等及重启持久化。
- `python3 scripts/smoke_execution.py` 共 25 次回环请求，包含 Google OAuth、GSC 1,001 行分页写入、GA4 自然流量、Bing、IndexNow 和恢复。明确拒绝的 IndexNow 429 收到 `Retry-After: 8`，重试后写入接受状态。
- `SEOOS_E2E_RESEARCH=1 SEOOS_E2E_CONTENT=1 SEOOS_E2E_EXECUTION=1 SEOOS_E2E_BROWSER_CHANNEL=chrome python3 scripts/test_dashboard.py` 通过 7 项浏览器测试。验证兼容模型表单、密钥回显防护、请求诊断权限、WordPress 编辑及 ARTEX 主题。固定编辑数据回执记录 1,374 Token，估算 1,768 USD micros。
- `npm run build`、`tsc --noEmit` 和改动 Web 文件的 Biome 检查通过。
- 临时 PostgreSQL 16 中实测迁移 `0006 → 0007` 保留了原 Provider。旧 API 拒绝读取 `0007`。将备份恢复到独立 `0006` 副本后，旧 API 读取了同一个 Provider 及其原始配额。
- 模型费率算术对账夹具包含输入 800 Token、缓存命中 300、输出 200。按测试费率计算为 1,500 USD micros，测试账单 1,450 USD micros，差额 −50 USD micros。再人为中断一笔调用，账本保留一项待确认并暂缓给出差额。以上金额来自测试数据。

## 发布回退

部署 `0007` 前保留加密 PostgreSQL 快照与对应版本。运行时会拒绝版本不匹配的迁移库。需要切回 `0ccd88d` 时，先停止 ARTEX API 与 Worker，在独立 PostgreSQL 副本恢复迁移前的快照，使用对应旧镜像验证缓存、站点和 Provider 配置，再按部署记录切换。应保留升级数据库以备核对升级期新增的请求和费用记录。

## 官方契约

- [OpenAI Responses](https://developers.openai.com/api/reference/resources/responses/methods/create) · [OpenAI 推理模型](https://developers.openai.com/api/docs/guides/reasoning) · [OpenAI 结构化输出](https://developers.openai.com/api/docs/guides/structured-outputs?api-mode=responses) · [OpenAI Organization Costs](https://developers.openai.com/api/reference/resources/admin/subresources/organization/subresources/usage/methods/costs)
- [Claude OpenAI SDK 兼容](https://platform.claude.com/docs/en/cli-sdks-libraries/libraries/openai-sdk) · [Gemini OpenAI 兼容](https://ai.google.dev/gemini-api/docs/openai) · [DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/) · [DeepSeek Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode/)
- [DataForSEO 账号接口](https://docs.dataforseo.com/v3/appendix-user-data/) · [Google OAuth](https://developers.google.com/identity/protocols/oauth2/web-server) · [GSC 查询分页](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data) · [GA4 属性](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1beta/accountSummaries/list) · [GA4 报告](https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/properties/runReport) · [Bing Webmaster](https://learn.microsoft.com/en-us/bingwebmaster/) · [IndexNow](https://www.indexnow.org/documentation)
