# ARTEX SEO OS UI 2.0 路由映射

普通页面由静态参数枚举。可分享链接使用 `site_id`、`project_id` 和必要市场查询参数。旧查询字段 `site`、`project` 继续可读。下表列出正式页面和承载入口。

| 一级组 | 页面 | 路径 | 入口 |
| --- | --- | --- | --- |
| 运营总览 | 运营仪表盘 | `/seo/dashboard` | 侧栏 |
| 运营总览 | 站群健康 | `/seo/dashboard/health` | 侧栏 |
| 运营总览 | 待处理事项 | `/seo/dashboard/inbox` | 侧栏 |
| 运营总览 | 最近活动 | `/seo/dashboard/activity` | 侧栏 |
| 站点管理 | 全部站点 | `/seo/sites` | 侧栏 |
| 站点管理 | 站群分组 | `/seo/sites/groups` | 侧栏 |
| 站点管理 | CMS 连接 | `/seo/sites/connections` | 侧栏 |
| 站点管理 | 站点配置 | `/seo/sites/settings` | 侧栏 |
| SEO 研究 | 关键词研究 | `/seo/research` | 侧栏 |
| SEO 研究 | 关键词拓展 | `/seo/research/expansion` | 侧栏 |
| SEO 研究 | 关键词探索 | `/seo/research/keywords` | 研究工作流 |
| SEO 研究 | 关键词聚类 | `/seo/research/clusters` | 侧栏 |
| SEO 研究 | SERP 竞争分析 | `/seo/research/serp` | 侧栏 |
| SEO 研究 | 竞争页面 | `/seo/research/competitors` | 竞争研究 |
| SEO 研究 | 搜索意图分析 | `/seo/research/intents` | 侧栏 |
| SEO 研究 | 关键词与页面映射 | `/seo/research/mapping` | 侧栏 |
| SEO 研究 | 关键词机会评分 | `/seo/research/opportunities` | 侧栏 |
| SEO 研究 | 研究历史 | `/seo/research/history` | 研究工作流 |
| SEO 研究 | 数据费用 | `/seo/research/usage` | 研究工作流，Owner |
| 内容中心 | 内容项目 | `/seo/content` | 侧栏 |
| 内容中心 | 内容项目详情 | `/seo/content/project` | 项目列表与项目快捷导航 |
| 内容中心 | 内容策略 | `/seo/content/strategy` | 侧栏 |
| 内容中心 | 内容简报 | `/seo/content/briefs` | 侧栏 |
| 内容中心 | 研究与资料来源 | `/seo/content/sources` | 侧栏 |
| 内容中心 | 文章编辑 | `/seo/content/article` | 侧栏 |
| 内容中心 | 事实核验 | `/seo/content/fact-check` | 侧栏 |
| 内容中心 | 质量审核 | `/seo/content/quality` | 侧栏 |
| 内容中心 | 版本管理 | `/seo/content/history` | 侧栏 |
| 内容中心 | 旧文优化 | `/seo/content/refresh` | 侧栏 |
| 站点管理 | 内容资产清单 | `/seo/content/inventory` | 侧栏 |
| SEO 执行 | 内容与媒体 | `/seo/execution/cms` | CMS 与站点资产相关链接 |
| SEO 执行 | 发布中心 | `/seo/execution/publishing` | 侧栏 |
| SEO 执行 | 发布记录 | `/seo/execution/publications` | 发布相关工作流 |
| SEO 执行 | 页面验证 | `/seo/execution/verification` | 发布相关工作流 |
| SEO 执行 | 谷歌搜索数据 | `/seo/execution/analytics` | 侧栏 |
| SEO 执行 | 谷歌流量分析 | `/seo/execution/ga4` | 侧栏 |
| SEO 执行 | 必应站长工具 | `/seo/execution/bing` | 侧栏 |
| SEO 执行 | 索引管理 | `/seo/execution/indexing` | 侧栏 |
| SEO 执行 | 站点地图 | `/seo/execution/sitemap` | 侧栏 |
| SEO 执行 | 索引提交 | `/seo/execution/indexnow` | 侧栏 |
| SEO 执行 | 站内链接 | `/seo/execution/internal-links` | 侧栏，规划 |
| SEO 执行 | 站群互链 | `/seo/execution/site-network` | 侧栏，规划 |
| SEO 执行 | 技术 SEO | `/seo/execution/technical` | 侧栏 |
| SEO 执行 | 执行任务 | `/seo/execution/monitoring` | 侧栏 |
| SEO 执行 | 执行日志 | `/seo/execution/logs` | 执行任务相关链接，Owner |
| SEO 执行 | 优化反馈 | `/seo/execution/feedback` | 技术 SEO 相关链接 |
| SEO 执行 | 数据服务用量 | `/seo/execution/usage` | 执行任务相关链接，Owner |
| AI 与系统 | AI 图片工作室 | `/seo/system/image-studio` | 侧栏，规划 |
| AI 与系统 | WordPress 主题工厂 | `/seo/system/theme-factory` | 侧栏，规划 |
| AI 与系统 | 大模型服务商 | `/seo/system/providers` | 侧栏 |
| AI 与系统 | AI Agent 管理 | `/seo/system/agents` | 侧栏，规划 |
| AI 与系统 | 自动化工作流 | `/seo/system/workflows` | 侧栏，规划 |
| AI 与系统 | 任务中心 | `/seo/system/jobs` | 侧栏 |
| AI 与系统 | 成员与权限 | `/seo/system/members` | 侧栏，Owner |
| AI 与系统 | 审计日志 | `/seo/system/audit` | 侧栏，Owner |
| AI 与系统 | 系统设置 | `/seo/system/settings` | 侧栏，Owner，规划 |

## 旧入口

| 旧入口 | 目标 |
| --- | --- |
| `/seo` | `/seo/dashboard` |
| `/seo/execution` | `/seo/sites/connections`，带发布包时进入 `/seo/execution/publishing`。含 Google 授权回调参数时先完成原回调，再恢复站点范围。 |
| `/seo/content?view=refresh` | `/seo/content/refresh` |
| `/seo/content?site=…&project=…` | `/seo/content/project?site_id=…&project_id=…` |
| `/seo/jobs` | `/seo/system/jobs`，旧站点创建任务入口仍有效。 |
| `/seo/system`、`/seo/providers` | `/seo/system/providers` |
| `/seo/members` | `/seo/system/members` |
| `/seo/audit` | `/seo/system/audit` |

`/seo/login` 保留。登录返回目标只接受已登记 SEO 页面和允许的业务查询参数。
