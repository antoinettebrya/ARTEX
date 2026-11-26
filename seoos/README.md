# ARTEX SEO OS — 第一个可运行开发版本

本目录是独立 Go 模块，提供站点管理 API 与 PostgreSQL 存储。它有自己的入口、数据库迁移和构建方式，不依赖根目录原产品的服务、代理、工具或流量模块。

## 已实现的范围

- 通过服务配置建立组织，API Token 固定绑定这个组织。
- 登记、分页查询、读取与更新站点，记录站点地址、CMS、国家、语言、状态。
- 所有站点查询按组织隔离；同组织内规范化后的站点地址唯一。
- 状态支持 `active`、`paused`、`archived`；归档保留记录。
- 使用 `version` 处理并发更新；过期版本返回 `409`，不会覆盖新数据。
- 独立的 `public.seo_*` 表；事务迁移、迁移锁、版本和校验和检查。
- 健康检查、数据库就绪检查、请求期限和优雅退出。
- 单元测试、真实 PostgreSQL 集成测试、HTTP 启动与重启持久化验证、独立 CI。

当前每个服务实例只有一个配置组织和一个操作员 Token。数据库支持多个组织的数据隔离，但用户登录、组织成员、RBAC 与凭据轮换界面仍待实现。现有 Next.js 前端尚未连接此 API。

图片生成、WordPress 内容连接器、主题生成及安装启用、站点互链、任务队列和 SEO 数据提供商仍在后续开发范围内。本版本登记站点时不会连接网站；状态字段目前只表示登记状态，不会暂停尚未实现的任务。整个仓库仍包含待迁移的旧产品，SEO 入口本身不加载这些模块。

## Docker 启动

在本目录执行；不要使用仓库根目录的旧产品 Compose 文件。

```bash
cd seoos
umask 077
python3 - <<'PY'
import secrets, uuid
with open('.env', 'x') as f:
    f.write('POSTGRES_PASSWORD=' + secrets.token_hex(32) + '\n')
    f.write('SEOOS_API_TOKEN=' + secrets.token_hex(32) + '\n')
    f.write('SEOOS_ORGANIZATION_ID=' + str(uuid.uuid4()) + '\n')
    f.write('SEOOS_ORGANIZATION_NAME="SEO OS"\n')
PY
docker compose up -d --build
curl --fail http://127.0.0.1:8789/readyz
```

生成命令不会覆盖现有 `.env`。保留组织 ID，重启时才能使用原组织。Compose 将 API 绑定到本机 `127.0.0.1:8789`，不映射数据库端口，数据库保存在独立命名卷中。跨主机访问应通过提供 HTTPS 的反向代理；服务本身不终止 TLS。

Docker 配置采用 PostgreSQL 16。不要修改已经应用的 SQL 迁移；后续变更新增连续编号的迁移文件。

## API 示例

先读取本地配置中的 Token，然后创建站点：

```bash
. ./.env
curl --fail-with-body http://127.0.0.1:8789/api/v1/sites \
  -H "Authorization: Bearer $SEOOS_API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"name":"示例站点","base_url":"https://example.com","cms":"wordpress","country_code":"US","language_tag":"en-US"}'
```

创建返回 `201`、`Location` 和 `{"site": {...}}`，其中包含 `id`、`organization_id`、`version`、创建时间和更新时间。`cms` 默认 `wordpress`，也支持 `generic`；`status` 默认 `active`。

| 方法与路径 | 行为 |
| --- | --- |
| `GET /healthz` | 进程存活，不检查数据库 |
| `GET /readyz` | 数据库可达才返回 `200` |
| `GET /api/v1/sites?limit=50&offset=0` | 按创建时间及 ID 倒序列出本组织站点 |
| `POST /api/v1/sites` | 创建站点 |
| `GET /api/v1/sites/{id}` | 读取本组织的指定站点 |
| `PUT /api/v1/sites/{id}` | 完整替换可编辑字段，必须带当前 `version` |

更新请求示例：

```json
{
  "name": "示例站点",
  "base_url": "https://example.com",
  "cms": "wordpress",
  "country_code": "US",
  "language_tag": "en-US",
  "status": "paused",
  "version": 1
}
```

成功后版本变为 `2`。若出现 `409 version_conflict`，重新读取并确认最新值再修改。PUT 是完整替换，省略可选字段会使用其默认值。当前没有硬删除接口。

所有 `/api/` 请求必须提供 Bearer Token。接口不接受来自正文、查询参数的组织覆盖，不使用 Cookie 或 URL Token。JSON 正文限制 64 KiB，拒绝未知字段和多个文档。分页限制 `limit=1..200`、`offset=0..100000`。

站点地址必须为 HTTP(S)，不包含凭据、查询参数或片段。规范化主机名、IP 表示、默认端口、末尾斜线与路径中非保留字符的编码；支持 WordPress 子目录路径并保留大小写及转义斜线语义。国际化域名使用 punycode。登记 URL 不代表已验证所有权或已允许后续网络连接。

## 本地 Go 开发

需要 Go 1.26.3 或更高版本，以及独立的 PostgreSQL 数据库。环境变量如下：

| 环境变量 | 要求 |
| --- | --- |
| `SEOOS_DATABASE_URL` | 必填，PostgreSQL DSN；不读取旧 `ARTEX_PG_DSN` |
| `SEOOS_ORGANIZATION_ID` | 必填，非零、规范小写 UUID |
| `SEOOS_API_TOKEN` | 必填，32–512 个无空白 ASCII 字符；使用随机值 |
| `SEOOS_ORGANIZATION_NAME` | 可选，首次建立组织时的名称，默认 `SEO OS` |
| `SEOOS_ADDR` | 可选，默认 `127.0.0.1:8789` |

配置后执行：

```bash
go mod download
go run ./cmd/seoos migrate
go run ./cmd/seoos serve
```

`serve` 同样会先完成迁移和组织初始化。所有配置需显式提供；`migrate` 与 `serve` 使用同一套配置校验。启动失败不会将数据库连接字符串写入日志。修改组织显示名配置不会覆盖已经存在的组织名称。

## 验证

```bash
# 未设置测试数据库时，数据库集成测试会明确跳过。
go test -race ./...

# 使用专门的临时/测试数据库；不要使用生产数据库。
export SEOOS_TEST_DATABASE_URL='postgres://test_user:test_password@127.0.0.1:5432/seoos_test?sslmode=disable'
go test -race -count=1 ./...
go vet ./...
go build -trimpath -o bin/seoos ./cmd/seoos
python3 scripts/smoke_test.py
```

存储测试只清理自身建立的组织记录；HTTP smoke 脚本留下两个随机测试组织和测试站点，由临时数据库的销毁流程统一清理。CI 使用临时 PostgreSQL 16 服务，随后测试、构建 Go 程序及 Docker 镜像，不发布或部署。

## 后续接入顺序

1. Next.js 站点管理页面、用户与组织权限。
2. WordPress 连接验证、内容及媒体同步、密钥管理。
3. 持久任务队列、提供商适配器、图片生成与媒体资产。
4. 主题生成、测试环境验证、安装启用与回滚。
5. 跨站相关内容候选、审核和可撤销发布记录。

这些能力尚未实现，也没有调用生产站点或付费模型。项目范围以 `../docs/seo-os/PRODUCT_SCOPE.md` 为准，仓库原许可证及声明保持原样。
