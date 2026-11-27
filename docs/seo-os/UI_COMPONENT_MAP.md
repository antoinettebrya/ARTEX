# ARTEX SEO OS UI 2.0 组件映射

| 体验 | 实现 | 数据与行为 |
| --- | --- | --- |
| 应用容器和主题 | `(main)/layout.tsx`、`AppShell`、`MainContent`、根布局和现有主题脚本 | SEO 会话复用原页头与侧栏，在可选页头插槽放置站点范围。CSS Variables、主题预设、字体和原 ARTEX 路由继续由原配置驱动。 |
| 六组菜单和命令查找 | `lib/seo/routes.ts`、`NavMain`、`NavGroup`、`NavMainParentItem` | 中央路由表决定标题、排列、规划状态和 Owner 可见性。展开和折叠继续使用原 Collapsible 与 Dropdown，移动端使用既有侧栏 Sheet。 |
| 全局站点、区域与市场 | `SeoSiteProvider`、`SeoScopeControl`、`SiteSelector`、`WorkspaceSelector`、`MarketDialog` | 通过当前会话读取站点目录，按国家、语言派生筛选。原 Popover、NativeSelect、Field 和 FormDialog 用于交互。业务页中的选择器使用同一个 URL 范围。 |
| 项目上下文与相关流程 | `ProjectSelector`、`useSeoLocation`、`WorkflowNav` 和普通 Next Link | 保留可刷新、可分享的站点、项目和市场。详情与列表各有 URL，切换站点时清除上一个详情上下文。 |
| 页面标题、操作和筛选 | `PageFrame`、`FilterBar`、原 Button/Card、Field/Input | 页面标题和主要操作同一排，窄屏折行。相邻功能使用少量可换行的链接。 |
| 表格、排序与分页 | `RecordsTable`、`useSeoRecords`、`useLocalSeoRecords`、原 Table/Pagination | 使用 API 的稳定顺序。全部站点沿用服务端分页，区域目录筛选复用相同表格。范围变化重置分页。 |
| 运营指标及状态 | `dashboard.tsx`、`dashboard-data.ts`、原 Card、Badge、Empty 和 Skeleton | 统计分站群、SEO、内容和 AI。缺失、加载、权限和覆盖范围由实际查询状态决定。 |
| 风险操作和错误恢复 | `FormDialog`、`ConfirmChange`、`LoadError`、`PageSkeleton`、原 AlertDialog/Toast | 沿用现有发布、版本恢复、取消和凭据撤销检查，继续由业务 API 判定授权。 |

Web 层通过 `useSeoQuery` 和原同源代理访问业务 API。分页读取有上限，站点切换会取消旧请求并隐藏不匹配的详情。Owner 的财务、成员和审计入口由中央路由权限和业务 API 双重校验。
