# First site-management milestone — validation record

Validated on 2026-11-26 against the new `seoos/` module. Baseline branch head:
`9b039e47d09989f1c233bb60659fc1cb3f773bcf`.

## Executed successfully

| Check | Observed result |
| --- | --- |
| Go toolchain | Go 1.26.3, linux/amd64; official download checksum verified |
| Database | Disposable local PostgreSQL 17.12 |
| `go test -race -count=1 ./...` with `SEOOS_TEST_DATABASE_URL` set | API, configuration, domain and storage packages passed; database tests were executed |
| `go vet ./...` | Passed |
| `go build -trimpath -o bin/seoos ./cmd/seoos` | Passed |
| `python3 scripts/smoke_test.py` | Real HTTP and PostgreSQL lifecycle passed, including restart and graceful shutdown |
| `go list -mod=readonly -deps ./cmd/seoos` | 236 packages; no legacy ARTEX packages outside `seoos/`, no Norma dependency |
| `git diff --check` | Passed before commit |

The suite contains 20 top-level Go tests with additional table-driven cases.
Five tests exercise PostgreSQL: migration startup concurrency/repetition,
organization isolation and lifecycle, concurrent optimistic updates,
conflicting/cancelled writes and pagination, and non-overwriting organization
bootstrap. Another store test checks invalid-DSN credential redaction.

The smoke script launches two configured organizations against the same local
database, verifies authentication and cross-organization read/update isolation,
creates and updates real records, checks duplicate URL/version conflicts, and
restarts the service to verify persisted state.

The first test run caught an unused cancellation-context variable in an
integration test. That test now passes the cancelled context to the write, and
the full suite was rerun successfully. Review also led to regression coverage
for unreserved URL encodings, preserved escaped slashes, IPv6 normalization and
canonical URL length.

## Verification boundaries

- Docker was not available in the local execution environment. Container build
  validation is configured in `.github/workflows/seoos-ci.yml`; this record does
  not claim that GitHub CI or Docker has already passed.
- CI is configured for PostgreSQL 16; local integration evidence is PostgreSQL
  17.12. The CI result is the PostgreSQL 16 compatibility check.
- Existing root Go/Next.js applications were not changed or started, and their
  full regression suites were not run. The Phase 0 audit is still incomplete.
- No WordPress website, external model provider, image-generation service or SEO
  data provider was called. Those integrations and the new frontend remain to be
  implemented.
- This is a tested site-management foundation, not evidence of production
  readiness or 150-site load capacity.

## Phase 1 基础内核验证 · 2026-11-26

基于 `cf39a8467af9f1ee93b762672e9a76bc4484fcec` 开发。本地环境为 macOS arm64、Go 1.26.3、临时 PostgreSQL 16.16、Node.js 24.22.1 和 Chrome。

| 命令或场景 | 实测结果 |
| --- | --- |
| `go test -race -count=1 ./...` | 28 项顶层测试通过，PostgreSQL 集成测试全部运行 |
| `go mod verify`、`go vet ./...`、`go build -trimpath -o bin/seoos ./cmd/seoos` | 全部通过 |
| `go list -mod=readonly -deps ./cmd/seoos` | 236 个依赖包，无旧 ARTEX 或 Norma 运行依赖 |
| `python3 scripts/smoke_test.py` | HTTP、兼容凭据、多组织隔离、CLI 签发、角色变更、撤销、审计、重启持久化和优雅退出通过 |
| `python3 scripts/load_test.py` | 12 路并发登记 160 站、提交 160 个任务，零失败。登记 P95 为 7.8 ms，提交 P95 为 8.3 ms，首个任务提交至全部完成用时 1.02 s |
| `npm ci`、`npm run build`、`npm run typecheck`、`npm audit --audit-level=high` | 后台安装、生产构建和类型检查通过，审计结果为 0 个已知漏洞 |
| `python3 scripts/test_dashboard.py` | Chrome 生产构建 E2E 通过。验证站点保存、提交任务、Provider 保存、成员登录、写入权限与审计 |
| 从 `cf39a846` 升级临时数据库 | 老版本登记并将站点更新到 v2，新版本升级后保留 v2 字段和状态，更新到 v3 后审计保留 v2 快照 |

负载测试覆盖 4 个市场和本地站点资料快照，数据经过实际 HTTP、PostgreSQL 和 Worker。Provider HTTP 契约用本地测试服务验证，测试环境未使用真实付费凭据。全站抓取、发布、周期性任务和外部 Provider 的长期负载属于后续验证范围。本机 Docker 守护进程未启动，容器构建由 GitHub Actions 验证。

## ARTEX 后台界面接入验证 · 2026-11-26

设计规范基线为 `4df79121b5434c9de83766d611b654f06b0208c7`。同一份临时 PostgreSQL 数据用于修改前后的应用内浏览器截图。原 ARTEX 的 LLM 页面用仓库自带的前端演示模式作对照。SEO 预览与 E2E 使用实际 Go API 和 PostgreSQL。

- 登录继承原登录框架。站点、任务、Provider、成员、审计使用同一个 ARTEX App Shell 和现有界面组件。
- 表格接入现有 Card、Table 和 Pagination。站点 API 分页无总条数，界面请求多一条来判断能否翻页。表单用共享 Dialog、Input、NativeSelect、Switch，反馈由现有 Toaster 显示。
- 浏览器检查 1440 像素桌面与 390 × 844 像素窄屏。检查 Default 浅色和暗色、Brutalist、Soft Pop、Tangerine、Geist Mono 及刷新后的偏好恢复，也检查键盘打开和关闭对话框、移动侧栏自动收起。
- Go 业务、数据库迁移、原主题样式、字体注册、主题启动脚本及偏好存储的文件差异为空。

| 命令 | 实测结果 |
| --- | --- |
| `go test -race -count=1 ./...`、`go vet ./...`、`go build` | 通过，PostgreSQL 集成测试实际运行 |
| `npm run build:static`、`npm run build`、`npx tsc --noEmit` | 静态导出、生产构建、类型检查通过 |
| `python3 scripts/test_dashboard.py` | 两条 Chrome E2E 通过，覆盖真实数据读写、同源会话、跨源写入拒绝、角色隔离、导航与主题 |
| Biome 定向检查 | 21 个新增或抽出的文件通过 |
| `npm run lint` | 全量前后均报告 84 个错误，仍需单独处理 |

## Phase 2 关键词与 SERP 研究 · 2026-11-26

基线为 `dbe21d18e26b7df6ad1c127f7dfc9f79ee332781`。本地使用 macOS arm64、Go 1.26.3、临时 PostgreSQL 16.16、Node.js 24.22.1 和 Chrome。

| 命令或场景 | 实测结果 |
| --- | --- |
| `go test -race -count=1 ./...` | 57 项顶层测试通过，全部 PostgreSQL 集成测试实际运行。覆盖加密与租户隔离、不同市场数据、SERP 过滤、聚类、页面映射、费用预留、限流与 Worker 租约恢复 |
| `go mod verify`、`go vet ./...`、`go build -trimpath -o bin/seoos ./cmd/seoos` | 通过。依赖检查确认 238 个包，无旧 ARTEX 或 Norma 运行依赖 |
| `go test -race ./internal/researchprovider` | Google、Bing 和 OnPage 使用固定本地 HTTP 夹具，覆盖实际请求结构、不同语言、429、超时、账单、缺失数据和快照 |
| `python3 scripts/smoke_test.py` | 实际 HTTP 与 PostgreSQL 启动、鉴权、跨组织隔离、重启持久化和退出通过 |
| `python3 scripts/load_test.py` | 12 路并发登记 160 站、提交 160 个本地站点库存任务，零失败。负载在本地 API、PostgreSQL 和 Worker 上实测 |
| 从 `dbe21d18` 启动、应用 `0004`、切回迁移前副本 | 旧站点和 OpenAI Provider 保留。旧二进制拒绝升级库，可从迁移前 PostgreSQL 副本启动并读回原有记录 |
| `npm run build:static`、`npm run build`、`npx tsc --noEmit` | 静态导出、生产构建和 TypeScript 通过。变更文件的 Biome 定向检查通过，全量 lint 仍报告原有 84 个问题 |
| `SEOOS_E2E_RESEARCH=1 python3 scripts/test_dashboard.py` | Chrome 3 条通过。覆盖导航、桌面与移动 SERP、十条自然结果、竞品正文区块和结构化证据、两关键词聚类、URL 映射、六项评分、缓存和角色访问 |

研究浏览器流程使用回环 HTTP Provider、真实 Go Worker 与临时 PostgreSQL。真实 DataForSEO 调用为 0 次，真实费用为 0。真账号的地区覆盖、数据可用性和费用对账待验。150 站以上实测范围为站点库存任务，付费研究任务的生产负载仍需采集实测数据。

数据库迁移、研究 API、缓存有效期、预算和回退步骤见 [Phase 2 研究契约](../docs/seo-os/PHASE2_RESEARCH.md)。

## Phase 3 内容智能与质量门禁 · 2026-11-26

基线 `a76367c3a55b433604d33f333bfca91e1f59020f`。本地验收使用 macOS arm64、Go 1.26.3、PostgreSQL 16.16、Node.js 24 和 Chrome。Go 服务、队列、数据库和 Next.js 均使用实际运行版本。DataForSEO、Model Provider、授权站点和一手资料使用本地固定 HTTP 夹具。

| 检查 | 实测结果 |
| --- | --- |
| `go test -race -count=1 ./...` | 82 项顶层测试通过。覆盖 6 种语言的简报与语言检查、事实冲突和过时、重要断言引用、重复和蚕食、12 项门禁、模型契约和采集边界 |
| PostgreSQL Phase 3 集成 | 覆盖同站市场证据、跨组织与站点隔离、版本恢复、审批权限、增量快照、旧任务接管和模型请求预留去重 |
| `go mod verify`、`go vet ./...`、标准与夹具 Go 构建 | 通过。256 个运行依赖包均与旧 ARTEX 业务和 Norma 隔离 |
| `python3 scripts/smoke_test.py` 与 `python3 scripts/load_test.py` | 原 HTTP、组织、审计和持久化流程通过。160 站库存基线零失败 |
| 旧二进制实测 `a76367c → 0005` | 保留 Phase 2 SERP 快照 ID，新项目读取同一证据。旧二进制从迁移前数据库副本启动并读回该快照 |
| `npm run build:static`、`npm run build`、`npx tsc --noEmit` | 通过。15 个变更前端文件的定向 Biome 检查通过。全量 lint 仍报告基线 84 项 |
| `SEOOS_E2E_RESEARCH=1 SEOOS_E2E_CONTENT=1 python3 scripts/test_dashboard.py` | 4 条 Chrome 回归通过。内容用例覆盖 Phase 2 证据、站点发现与采集、来源事实、分章节写作、独立质量审核、版本比较、单独审批和导出。旧 HTML 核验失败，夹具 CMS 呈现导出文章后核验成功 |

内容 E2E 加上 Phase 2 回归共发出 13 次回环 DataForSEO 请求。模型合同共 5 次回环请求，分别为大纲 1 次、章节 3 次、独立审核 1 次。固定回执给出 1374 个输入与输出 Token，按测试配置的单价结算估算费用。真实 DataForSEO、模型和正式 CMS 调用均为 0 次。正式账号的输出质量、费率与账单仍需单独验收。站点清单和更新信号要达到生产覆盖，还需实际站点、可靠索引源与分析数据接入。

实测视觉范围为 1440 × 900 的浅色桌面、390 × 844 的暗色 Tangerine，以及 ARTEX 原有模式偏好回归。API、采集授权、模型预算和回退见 [Phase 3 运行手册](../docs/seo-os/PHASE3_CONTENT.md)。
