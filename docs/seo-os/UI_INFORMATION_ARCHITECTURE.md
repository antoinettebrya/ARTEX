# ARTEX SEO OS 后台信息架构

审计基线为 `7a6a624`，以 `seo-os-v1` 的 `6bb12b7` 作为实现起点。路由改造保留此版本的 Provider 请求诊断、WordPress 安装地址编辑和现有 SEO API。

## 当前页面与容器

SEO 和原 ARTEX 分别通过现有组织会话和原登录守卫进入同一个 `AppShell`。实际导航使用 `AppSidebar`、`NavMain` 和全局搜索，页头由 `MainContent` 提供。原 ARTEX 的 LLM 页面复用 Card、Table、Sheet、表单和 Toast。SEO 页面沿用 `shared.tsx` 的页面框架、列表、分页、状态、加载、报错和确认框。根布局继续从 `ThemeBootScript`、偏好 store、字体注册及 CSS Token 应用主题、字体、密度和响应式设置。

| 当前入口 | 当前内容 | 新位置 |
| --- | --- | --- |
| `/seo`、登录后的默认页 | 默认跳转站点列表 | Dashboard 的运营总览 |
| `/seo/sites` | 站点列表、登记、编辑、创建任务、前往研究 | Sites 的全部站点和独立站点配置 |
| `/seo/research` | 研究概览、种子与建议、关键词探索、意图、聚类、SERP、竞争页面、映射与站点页面、机会、历史、用量 | Research 独立页面 |
| `/seo/content` | 项目概览、策略、简报、证据、编辑、事实、审核、历史、更新、清单 | Content 独立页面和少量项目内快捷入口 |
| `/seo/execution` | CMS 与搜索连接、内容和媒体、发布、GSC、GA4、Bing、URL 检查与索引提交、Sitemap、页面提取、反馈、任务、用量 | Sites 的 CMS 连接及 Execution 独立页面 |
| `/seo/jobs` | 任务列表、登记、详情及取消 | AI & System 的任务中心 |
| `/seo/providers` | 大模型、DataForSEO、请求诊断、内容模型配置 | AI & System 的 AI Provider |
| `/seo/members`、`/seo/audit` | 组织成员与审计 | AI & System 的成员管理与审计日志 |

## 导航与页面路由

六组运用原 Sidebar 的可展开菜单。组内页面是普通导航链接，支持键盘、直接访问、刷新及浏览器历史。在页面内保留少量同一工作流的快捷链接，例如内容项目与编辑/审核、搜索报告不同来源。规划项目有「规划」文字标识和说明，页面没有执行按钮。

| 一级导航 | 子页面和正式 URL |
| --- | --- |
| Dashboard | 运营总览 `/seo/dashboard`，站群健康 `/seo/dashboard/health`，待处理事项 `/seo/dashboard/inbox`，最近活动 `/seo/dashboard/activity` |
| Sites | 全部站点 `/seo/sites`，站群分组（规划） `/seo/sites/groups`，CMS 连接 `/seo/sites/connections`，站点配置 `/seo/sites/settings` |
| Research | 关键词研究 `/seo/research`，关键词探索 `/seo/research/keywords`，关键词聚类 `/seo/research/clusters`，SERP 竞品分析 `/seo/research/serp`，竞争页面 `/seo/research/competitors`，搜索意图 `/seo/research/intents`，Keyword-to-URL `/seo/research/mapping`，关键词机会 `/seo/research/opportunities`，研究历史 `/seo/research/history`，数据用量 `/seo/research/usage` |
| Content | 内容项目 `/seo/content`，内容策略 `/seo/content/strategy`，内容简报 `/seo/content/briefs`，研究来源 `/seo/content/sources`，文章编辑 `/seo/content/article`，事实核验 `/seo/content/fact-check`，质量审核 `/seo/content/quality`，版本历史 `/seo/content/history`，旧文优化 `/seo/content/refresh`，站点内容清单 `/seo/content/inventory` |
| Execution | CMS 管理 `/seo/execution/cms`，发布中心 `/seo/execution/publishing`，搜索分析 `/seo/execution/analytics`，GA4 `/seo/execution/ga4`，Bing Webmaster `/seo/execution/bing`，索引管理 `/seo/execution/indexing`，Sitemap `/seo/execution/sitemap`，IndexNow `/seo/execution/indexnow`，站内链接（规划） `/seo/execution/internal-links`，站群互链（规划） `/seo/execution/site-network`，Technical SEO `/seo/execution/technical`，执行监控 `/seo/execution/monitoring`，优化反馈 `/seo/execution/feedback`，执行用量 `/seo/execution/usage` |
| AI & System | AI 图片工作室（规划） `/seo/system/image-studio`，WordPress 主题工厂（规划） `/seo/system/theme-factory`，AI Provider `/seo/system/providers`，Agent 管理（规划） `/seo/system/agents`，任务中心 `/seo/system/jobs`，成员管理 `/seo/system/members`，审计日志 `/seo/system/audit`，系统设置（规划） `/seo/system/settings` |

Technical SEO 保留现有授权页面提取和证据监控。整站技术审计及自动修复仍待建设。系统设置指组织级设置，原 App Shell 的个人主题、字体与布局偏好继续通过页头访问。

`/seo/jobs`、`/seo/providers`、`/seo/members`、`/seo/audit` 保留旧书签。旧 `/seo/content?view=refresh` 指向旧文优化。旧 `/seo/execution?package=…` 指向发布中心。Google 已注册的回调保持 `/seo/execution`，由原授权流程消费后返回对应站点的 CMS 连接页。新页面在构建时枚举，保持静态导出和 Next.js 运行方式兼容。

## 上下文与权限

- `site`、`project` 使用 URL 查询参数承载，经现有 UUID 规则验证。站点选择以实际组织站点 API 为准。直接访问的站点不在选择器首批记录时，通过现有单站 API 查找。跨模块链接保留站点及已选项目，切换站点清除旧项目和发布包上下文。跨站点的项目详情不会渲染。
- 项目页面复用站点选择器、项目选择器、布局与筛选栏。工作流本身的业务状态、发布审批、凭据和写权限仍由现有 API 控制。
- 当前 SEO API 和数据库定义 Owner、Editor、Viewer。Owner 可见成员、审计、请求诊断和用量；Editor 及 Viewer 按原授权显示。独立 Admin 角色需要产品权限模型与迁移，当前前端不提供角色映射或新增授权。
- 复用 `PageFrame`、`RecordsTable`、`StateBadge`、`PageSkeleton`、`LoadError` 与原 Empty、AlertDialog 和分页。新增的站点/项目选择器与筛选布局复用 NativeSelect、Field、Card。

## 运营总览的统计口径

| 指标 | 数据源与口径 |
| --- | --- |
| 管理站点、活跃站点 | 组织 `/sites`，活跃为 `status=active`。站群健康显示登记状态和近期任务，区分站点启用与站点可用性。 |
| 今日执行、任务成功率、异常任务 | 组织 `/jobs`，以 UTC 今日创建时间为范围。执行数依据实际执行尝试，成功率分母为已完成的成功与失败，异常为失败。最近活动使用真实任务记录。 |
| 内容生产、待审核 | 当前站点真实内容项目与工作区。文章版本存在时计入内容生产，质量门通过且未批准时进入待人工审核。工作区读取有上限并展示覆盖范围。 |
| 发布成功 | 当前站点 `/execution/publications` 中状态为已发布或核验通过的记录。 |
| GSC 点击和展示、自然搜索流量 | 当前站点已有同步数据，使用最近 28 个完整 UTC 日期中的单一 GSC 连接和单一 GA4 连接，显示来源与观测覆盖。自然流量采用 GA4 Organic Search 的 sessions。 |
| Provider 成本 | Owner 可阅 UTC 今日 `/providers/receipts` 已记录的厂商金额与估算值。排除测试和站点执行回执，待确认金额单列。 |

跨站点数据只通过当前组织的现有 API 读取。组织汇总与选中站点指标分区，站点选切不会触发其它站点的统计查询。无观测与加载失败分别显示空状态和重试。有限样本明确标注范围，未读取的数据不计为零。

## 验证

先取同一套 ARTEX App Shell 的改造前参照，再执行静态导出、Next.js 生产构建、TypeScript 和改动路径的 Biome 检查。Chromium 桌面及移动端验证六组导航、直接访问、刷新、浏览器历史、站点及项目上下文、角色、浅色、暗色与现有主题预设。原研究、内容、发布、Provider 和 WordPress 流程通过实际 Go API 和隔离 PostgreSQL 运行。外部供应商测试夹具只用于流程回归。
