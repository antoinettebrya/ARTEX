# ARTEX SEO OS

`seoos/` 是独立运行的 Go 服务。它使用独立的 PostgreSQL 表、迁移、入口和 Next.js 后台。参见[产品范围](../docs/seo-os/PRODUCT_SCOPE.md)和 [Phase 2 关键词与 SERP 研究](../docs/seo-os/PHASE2_RESEARCH.md)。

## 启动

在 `seoos/` 目录执行。配置文件应由当前用户独占读写。

```bash
umask 077
python3 - <<'PY'
import base64, secrets, uuid
with open('.env', 'x') as f:
    f.write('POSTGRES_PASSWORD=' + secrets.token_hex(32) + '\n')
    f.write('SEOOS_API_TOKEN=' + secrets.token_hex(32) + '\n')
    f.write('SEOOS_ORGANIZATION_ID=' + str(uuid.uuid4()) + '\n')
    f.write('SEOOS_ORGANIZATION_NAME="SEO OS"\n')
    f.write('SEOOS_PROVIDER_KEY=' + base64.b64encode(secrets.token_bytes(32)).decode() + '\n')
PY
docker compose up -d --build
curl --fail http://127.0.0.1:8789/readyz
```

Compose 包含 PostgreSQL 16，并将 API 绑定到 `127.0.0.1:8789`。数据库使用专用命名卷。保留组织 ID 和 Provider 加密密钥。远程访问 API 和后台应经由 HTTPS 反向代理。

本地运行 Go 程序需要 Go 1.26.3 或更高版本。可执行 `go run ./cmd/seoos serve`，由程序在启动时应用迁移。仅迁移可执行 `go run ./cmd/seoos migrate`。环境变量可从秘密管理服务或当前 shell 提供。

| 环境变量 | 说明 |
| --- | --- |
| `SEOOS_DATABASE_URL` | 必填，专用 PostgreSQL DSN |
| `SEOOS_ORGANIZATION_ID` | 必填，配置组织的规范小写 UUID |
| `SEOOS_API_TOKEN` | 必填，配置组织的操作员凭据，使用随机值 |
| `SEOOS_ORGANIZATION_NAME` | 首次建立配置组织时的名称，默认 `SEO OS` |
| `SEOOS_ADDR` | HTTP 地址，默认 `127.0.0.1:8789` |
| `SEOOS_PROVIDER_KEY` | 32 个随机字节的标准 Base64 编码，配置 OpenAI 或 DataForSEO 凭据时必需 |

### Next.js 后台

后台从仓库根目录的 `web/` 启动，需要 Node.js 22 或更高版本。运行前确认上面的 SEO Go API 已就绪。

```bash
cd web
npm ci
npm run build
export SEOOS_API_ORIGIN=http://127.0.0.1:8789
npm start -- --hostname 127.0.0.1
```

访问 `http://127.0.0.1:3000/seo/sites`，使用成员凭据或配置组织的操作员凭据登录。设置 `SEOOS_API_ORIGIN` 会启用 SEO 专用运行模式，后台使用 ARTEX 共用的 App Shell、主题、字体和偏好控件。站点、任务、Provider、成员、审计地址分别为 `/seo/sites`、`/seo/jobs`、`/seo/providers`、`/seo/members`、`/seo/audit`。

`/seo/api/` 在 Next.js 服务端把同源请求转给 SEO Go API。会话凭据保存在有效期 8 小时的 HttpOnly、Secure、SameSite=Lax Cookie 中，作用域是 `/seo`。远程使用请配置 HTTPS，并让反向代理保留请求 Host 和原始协议。同源服务端转发需要 `next start` 或 `next dev`。原应用的静态导出仍可单独构建。

在 `/seo/research` 可以提交和查看关键词拓展、指标、SERP、竞品页面、意图、聚类、Keyword-to-URL 与机会评分。站点详情可直接进入该站点的研究面板。组织所有者先在 Provider 页登记 DataForSEO 登录名、API 密码、请求限额和每日 USD 预算。市场代码、API 契约和回退流程见 [Phase 2 文档](../docs/seo-os/PHASE2_RESEARCH.md)。


## 组织、成员和权限

配置操作员凭据始终绑定 `SEOOS_ORGANIZATION_ID`。数据库运营人员可通过以下命令新建组织并签发首位所有者的凭据。

```bash
go run ./cmd/seoos provision-org "Editorial team" owner@example.test
```

Compose 部署可在 `seoos/` 执行 `docker compose exec -T api /usr/local/bin/seoos provision-org "Editorial team" owner@example.test`。凭据仅在创建或轮换响应中显示。数据库保存随机凭据的 SHA-256 摘要。每次 API 请求从凭据解析组织与角色，组织由服务端确定。角色更改、轮换和撤销从下一次授权检查生效。后台登录是访问凭据登录，邮箱用于标识组织成员。

| 角色 | 权限 |
| --- | --- |
| `owner` | 编辑站点和任务，管理 Provider、成员、凭据，读取组织审计 |
| `editor` | 读取组织内站点、任务和 Provider，编辑站点、提交和取消任务 |
| `viewer` | 读取组织内站点、任务和 Provider |

同一个邮箱可以加入多个组织，每个组织的凭据独立。事务内检查最后一名持有有效凭据的所有者，防止将组织锁死。配置组织可使用配置操作员凭据进行管理。

## API

`/api/v1/` 下的端点使用 `Authorization: Bearer <凭据>`。JSON 正文上限 64 KiB，拒绝未知字段。列表端点接受 `limit=1..200` 和 `offset=0..100000`。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /healthz`、`GET /readyz` | 检查进程、检查数据库就绪 |
| `GET /api/v1/me` | 当前组织、成员和角色 |
| `GET /api/v1/sites`、`POST /api/v1/sites` | 列表、登记站点 |
| `GET /api/v1/sites/{id}`、`PUT /api/v1/sites/{id}` | 读取、按版本更新站点 |
| `GET /api/v1/members`、`POST /api/v1/members` | 列表、添加成员，新增返回凭据 |
| `PUT /api/v1/members/{id}/role` | 更新角色 |
| `POST /api/v1/members/{id}/token`、`DELETE /api/v1/members/{id}/token` | 轮换、撤销成员凭据 |
| `GET /api/v1/audit` | 查询本组织写入与任务状态审计 |
| `GET /api/v1/providers`、`POST /api/v1/providers` | 列出配置、创建 OpenAI Provider |
| `PUT /api/v1/providers/{id}` | 按版本更新 Provider，可保留或轮换密钥 |
| `GET /api/v1/jobs`、`POST /api/v1/jobs` | 列表、提交任务 |
| `GET /api/v1/jobs/{id}`、`DELETE /api/v1/jobs/{id}` | 读取、取消任务 |

### 登记站点

```bash
. ./.env
curl --fail-with-body http://127.0.0.1:8789/api/v1/sites \
  -H "Authorization: Bearer $SEOOS_API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"name":"示例站点","base_url":"https://example.com","cms":"wordpress","country_code":"US","language_tag":"en-US"}'
```

`PUT /api/v1/sites/{id}` 完整替换可编辑字段，并接收读取时的 `version`。冲突返回 `409 version_conflict`，应重新读取后再提交。站点状态为 `active`、`paused` 或 `archived`。登记只保存规范化 URL 和地域信息。请使用国际化域名的 ASCII 表示。

### 完整任务流程

1. 选择启用站点，向 `POST /api/v1/jobs` 发送 `site_id`、`kind`、`idempotency_key`。
2. `site_inventory` 将站点登记数据的快照持久化。它不访问站点。
3. `site_brief` 还需要同组织启用的 `provider_id`。它将站点名称、CMS、国家和语言发送给 OpenAI Responses API，生成后续研究简报。关键词、排名、网页内容和性能需要后续模块提供证据。[OpenAI Responses API](https://developers.openai.com/api/reference/resources/responses/methods/create)
4. 通过任务详情读取 `state`、`attempts`、`available_at` 和成功结果。用同一个幂等键和相同参数重试提交，会得到同一任务。
5. Worker 以行锁认领任务，租约为 90 秒，执行时限 40 秒。过期租约可以续跑，同一任务最多 3 次执行尝试。临时失败按尝试次数退避，旧租约无法覆盖已取消或已重新认领的结果。[PostgreSQL 行锁](https://www.postgresql.org/docs/16/sql-select.html#SQL-FOR-UPDATE-SHARE)

Provider 创建接收 `name`、`model`、`api_key`、`timeout_seconds`、`requests_per_minute`、`requests_per_day`、`max_output_tokens` 和 `enabled`。更新附带当前 `version`，留空 `api_key` 可保留原密钥。密钥用 AES-256-GCM 加密并绑定组织和 Provider，普通 API 响应与审计记录不带密钥。当前预算按 Provider 记录滚动一分钟和 UTC 日请求数，并限制单次输出 Token。预算不足推迟任务且不消耗任务尝试次数。已预留的请求即使失败或在服务重启时结果未知，也计入预算。

业务写入和审计事件在同一事务提交。站点和 Provider 更新的审计包含前后值。取消任务会阻止后续尝试和结果提交。已经发送至 Provider 的请求可能已经被计费。

## 升级和回滚

升级前暂停写入并保留 `cf39a846` 的部署产物、环境配置及数据库备份。用 `pg_dump --format=custom --file=seoos-before-phase1.dump` 备份目标 SEO 数据库，按运维配置提供连接参数。Provider 加密密钥应与数据库备份分开保管。先在临时数据库用现有版本和新版本验证旧站点、组织和版本号，再切换生产实例。迁移 `0002_access.sql` 和 `0003_runtime.sql` 为追加式，原迁移校验和保持有效。

服务级回滚时停止新实例，使用 `pg_restore --exit-on-error` 将升级前备份恢复到独立数据库，配置旧实例指向恢复库，验证原 API 后再切换流量。升级后写入的数据需先导出，再决定如何补录。旧服务会拒绝包含未知迁移的数据库。单条站点或 Provider 配置可参考审计的旧值，使用当前 `version` 重新提交。Provider 旧密钥需从密钥保管处取得。

## 测试

以下命令在 `seoos/` 执行。测试会写入独立的临时 PostgreSQL 数据库，应先通过 `SEOOS_TEST_DATABASE_URL` 指定该库。

```bash
go test -race -count=1 ./...
go vet ./...
go build -trimpath -o bin/seoos ./cmd/seoos
python3 scripts/smoke_test.py
python3 scripts/load_test.py
(cd ../web && npm ci && npm run build && npx tsc --noEmit && npx playwright install chromium)
python3 scripts/test_dashboard.py
go build -trimpath -tags seoos_testfixture -o bin/seoos-fixture ./cmd/seoos
SEOOS_E2E_RESEARCH=1 python3 scripts/test_dashboard.py
```

测试包含角色与跨组织隔离、站点 API 兼容、审计事务、队列幂等与接管、Provider 加密和配额、后台到真实 API 与 PostgreSQL 的浏览器流程。OpenAI 和 DataForSEO HTTP 契约测试使用本地固定响应。实测结果见[验证记录](VALIDATION.md)。

## 下一阶段

内容生产和 Quality Gate 使用 Phase 2 的意图、聚类、竞品结构、页面映射与评分依据。周期调度、WordPress、媒体和分析模块见产品范围。Phase 0 源码来源和许可审查仍需完成。
