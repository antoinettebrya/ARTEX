# ARTEX SEO OS 暂存部署手册

暂存版由 Next.js、Go API、Go Worker、一次性迁移和 PostgreSQL 组成。保留 `seoos serve` 作为开发和兼容入口。部署使用 `seoos migrate` 初始化组织及迁移，`seoos api` 只提供 HTTP，`seoos worker` 只领取数据库任务。API 和 Worker 在启动时验证完整的迁移版本及校验和。Worker 只需数据库连接和 Provider 加密密钥。

## 目录与入口

| 路径 | 用途 |
| --- | --- |
| `/opt/artex-seoos/releases/<完整 SHA>` | 审查过的 `seoos/` 和 `web/` 源码树，以及非机密的镜像标签 |
| `/opt/artex-seoos/current` | 当前已通过接口与页面健康检查的版本 |
| `/etc/artex-seoos/staging.env` | 仅 root 可读的数据库口令、组织 ID、应用登录令牌和 Provider 密钥 |
| `/etc/artex-seoos/nginx.htpasswd` | Nginx 暂存入口的认证口令哈希 |
| `/etc/artex-seoos/backup-key` | 独立保管的 PostgreSQL 加密备份口令 |
| `/var/backups/artex-seoos` | PostgreSQL 加密备份、校验和及站点配置回退副本 |

Compose 服务使用独立项目名。PostgreSQL 在内部网络上，Go API 仅供容器使用。Nginx 通过 `127.0.0.1:18889` 访问 Web。HTTP 跳转 HTTPS，ACME 验证路径保持可访问。暂存入口使用 HTTP Basic Auth 和独立的 ARTEX 应用登录。Nginx 阻止嵌入、限制请求频率，并在访问日志中省略查询字符串。 [Compose 健康检查](https://docs.docker.com/compose/how-tos/startup-order/) · [ACME 验证](https://letsencrypt.org/docs/challenge-types/)

`nginx.staging.conf` 是模板。指向由自己管理的域名之前备份生效的 Nginx 配置。为该域名签发并验证证书后，将 `@STAGING_HOST@` 和 `@WEB_PORT@` 替换为实际值。只有独立 Web 容器健康后才启用该站点。用 `nginx -t` 校验配置，再执行平滑重载。

## 发布

从经核对的 Git 提交导出 `seoos/` 与 `web/`，放入对应 SHA 目录。每个版本的镜像以完整提交 SHA 标记，并带有同值的 OCI revision 标签。环境文件中的 `POSTGRES_PASSWORD` 使用 URL 安全的随机字符，`SEOOS_PROVIDER_KEY` 使用标准 Base64 编码的 32 个随机字节。

```bash
cd "/opt/artex-seoos/releases/$RELEASE_SHA"
bash seoos/deploy/release.sh "$RELEASE_SHA"
bash seoos/deploy/compose.sh ps
```

发布脚本构建 API、Worker 和 Web 所需镜像，验证 Compose 配置，执行加密迁移前备份，运行一次性迁移，并等待 API 和 Web 通过健康检查。全部通过后更新 `current`。带浏览器的 Worker 使用 `ARTEX_STAGE_BROWSER=1`，需事先将校验过的 Playwright seccomp profile 放在 `/etc/artex-seoos/playwright-seccomp.json`。浏览器在非 root 和 Chromium 沙箱下运行。调试宿主机兼容性时保留 Chromium 沙箱。 [沙箱配置与测试](../../seoos/scripts/test_extraction.sh)

上线后验证 HTTPS 域名、未认证访问拦截、应用登录、任务成功状态、应用重启后的记录、源站证书续期和已有虚拟主机的响应。Go API 的 `/readyz` 位于容器网络。公开网络的 `/api/` 路径由 SEO Web 门面拒绝。

## 备份、恢复和回退

`bash seoos/deploy/backup.sh` 通过 `pg_dump -Fc` 生成数据库备份，流式加密后保存校验和。可安装同目录的 systemd 服务和定时器，在北京时间 03:30 执行。首次开启前手动运行并检查退出状态。恢复时，先检查加密文件的 SHA-256，再使用独立数据库运行 `pg_restore --exit-on-error`，核对迁移表与业务记录。将加密备份和恢复密钥分别保管在可独立恢复的存储位置。 [PostgreSQL 备份和恢复](https://www.postgresql.org/docs/16/backup.html)

同一迁移版本回退时，先暂停当前 Web、API 和 Worker，再使用上一 SHA 目录的 Compose 文件启动相同数据库上的上一版本。数据库版本发生变化时，先将迁移前备份恢复到独立 PostgreSQL 卷。验证上一版本和已恢复数据后再切换入口。首次上线回退时临时停用 ARTEX 的 Nginx 站点链接并停止 ARTEX 容器，保留数据库卷和证书。镜像和数据库备份保留至恢复验收完毕。

正式推广前的授权外部 Provider 测试、监控告警、自动异地备份、负载、稳定性与许可审查以 [远程部署计划](REMOTE_DEPLOYMENT_PLAN.md) 为准。
