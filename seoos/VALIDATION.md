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
