import { FileText, Globe, LayoutDashboard, Network, Search, Settings2 } from "lucide-react";

import type { NavGroup } from "@/navigation/sidebar/sidebar-items";

import { isID, type Role } from "./client";

type RouteDefinition = { path: string; title: string; ownerOnly?: boolean; planned?: string };

export type ResearchView =
  | "seeds"
  | "expansion"
  | "keywords"
  | "clusters"
  | "serps"
  | "competitors"
  | "intents"
  | "mappings"
  | "opportunities"
  | "jobs"
  | "usage";

export const seoRoutes = {
  dashboard: {
    overview: { path: "/seo/dashboard", title: "运营仪表盘" },
    health: { path: "/seo/dashboard/health", title: "站群健康" },
    inbox: { path: "/seo/dashboard/inbox", title: "待处理事项" },
    activity: { path: "/seo/dashboard/activity", title: "最近活动" },
  },
  sites: {
    list: { path: "/seo/sites", title: "全部站点" },
    groups: { path: "/seo/sites/groups", title: "站群分组" },
    connections: { path: "/seo/sites/connections", title: "CMS 连接" },
    settings: { path: "/seo/sites/settings", title: "站点配置" },
  },
  research: {
    seeds: { path: "/seo/research", title: "关键词研究" },
    expansion: { path: "/seo/research/expansion", title: "关键词拓展" },
    keywords: { path: "/seo/research/keywords", title: "关键词探索" },
    clusters: { path: "/seo/research/clusters", title: "关键词聚类" },
    serps: { path: "/seo/research/serp", title: "SERP 竞争分析" },
    competitors: { path: "/seo/research/competitors", title: "竞争页面" },
    intents: { path: "/seo/research/intents", title: "搜索意图分析" },
    mappings: { path: "/seo/research/mapping", title: "关键词与页面映射" },
    opportunities: { path: "/seo/research/opportunities", title: "关键词机会评分" },
    jobs: { path: "/seo/research/history", title: "研究历史" },
    usage: { path: "/seo/research/usage", title: "数据费用", ownerOnly: true },
  } satisfies Record<ResearchView, RouteDefinition>,
  content: {
    overview: { path: "/seo/content", title: "内容项目" },
    project: { path: "/seo/content/project", title: "内容项目详情" },
    strategy: { path: "/seo/content/strategy", title: "内容策略" },
    briefs: { path: "/seo/content/briefs", title: "内容简报" },
    sources: { path: "/seo/content/sources", title: "研究与资料来源" },
    article: { path: "/seo/content/article", title: "文章编辑" },
    "fact-check": { path: "/seo/content/fact-check", title: "事实核验" },
    quality: { path: "/seo/content/quality", title: "质量审核" },
    history: { path: "/seo/content/history", title: "版本管理" },
    refresh: { path: "/seo/content/refresh", title: "旧文优化" },
    inventory: { path: "/seo/content/inventory", title: "内容资产清单" },
  },
  execution: {
    cms: { path: "/seo/execution/cms", title: "内容与媒体" },
    publishing: { path: "/seo/execution/publishing", title: "发布中心" },
    publications: { path: "/seo/execution/publications", title: "发布记录" },
    verification: { path: "/seo/execution/verification", title: "页面验证" },
    gsc: { path: "/seo/execution/analytics", title: "谷歌搜索数据" },
    ga4: { path: "/seo/execution/ga4", title: "谷歌流量分析" },
    bing: { path: "/seo/execution/bing", title: "必应站长工具" },
    index: { path: "/seo/execution/indexing", title: "索引管理" },
    sitemaps: { path: "/seo/execution/sitemap", title: "站点地图" },
    indexnow: { path: "/seo/execution/indexnow", title: "索引提交" },
    "internal-links": {
      path: "/seo/execution/internal-links",
      title: "站内链接",
      planned: "站内链接分析、建议和审核尚未开放。",
    },
    network: { path: "/seo/execution/site-network", title: "站群互链", planned: "跨站引用分析和编辑审核尚未开放。" },
    technical: { path: "/seo/execution/technical", title: "技术 SEO" },
    jobs: { path: "/seo/execution/monitoring", title: "执行任务" },
    logs: { path: "/seo/execution/logs", title: "执行日志", ownerOnly: true },
    feedback: { path: "/seo/execution/feedback", title: "优化反馈" },
    usage: { path: "/seo/execution/usage", title: "数据服务用量", ownerOnly: true },
  },
  system: {
    image: { path: "/seo/system/image-studio", title: "AI 图片工作室", planned: "图片生成和资产工作室尚未开放。" },
    themes: {
      path: "/seo/system/theme-factory",
      title: "WordPress 主题工厂",
      planned: "主题生成、测试和部署管理尚未开放。",
    },
    providers: { path: "/seo/system/providers", title: "大模型服务商" },
    agents: { path: "/seo/system/agents", title: "AI Agent 管理", planned: "Agent 的配置与运行管理界面尚未开放。" },
    workflows: {
      path: "/seo/system/workflows",
      title: "自动化工作流",
      planned: "可视化编排、流程模板和定时策略将在后续阶段开放。现有执行任务可在任务中心运行。",
    },
    jobs: { path: "/seo/system/jobs", title: "任务中心" },
    members: { path: "/seo/system/members", title: "成员与权限", ownerOnly: true },
    audit: { path: "/seo/system/audit", title: "审计日志", ownerOnly: true },
    settings: {
      path: "/seo/system/settings",
      title: "系统设置",
      planned: "组织级系统设置尚未开放。个人外观和布局可在页头调整。",
      ownerOnly: true,
    },
  },
} satisfies Record<string, Record<string, RouteDefinition>>;

type Section = keyof typeof seoRoutes;
type View<S extends Section> = keyof (typeof seoRoutes)[S];
export type ContentView = View<"content">;
export type ExecutionView = View<"execution">;
export type DashboardView = View<"dashboard">;
type RouteSelection =
  | { section: "dashboard"; view: DashboardView }
  | { section: "sites"; view: View<"sites"> }
  | { section: "research"; view: ResearchView }
  | { section: "content"; view: ContentView }
  | { section: "execution"; view: ExecutionView }
  | { section: "system"; view: View<"system"> };
export type SeoRoute = RouteSelection & RouteDefinition;

const sections = {
  dashboard: { title: "运营总览", icon: LayoutDashboard },
  sites: { title: "站点管理", icon: Globe },
  research: { title: "SEO 研究", icon: Search },
  content: { title: "内容中心", icon: FileText },
  execution: { title: "SEO 执行", icon: Network },
  system: { title: "AI 与系统", icon: Settings2 },
} as const;

const { dashboard, sites, research, content, execution, system } = seoRoutes;
const menu: Record<Section, readonly RouteDefinition[]> = {
  dashboard: [dashboard.overview, dashboard.health, dashboard.inbox, dashboard.activity],
  sites: [sites.list, sites.groups, sites.connections, sites.settings, content.inventory],
  research: [
    research.seeds,
    research.expansion,
    research.clusters,
    research.intents,
    research.serps,
    research.mappings,
    research.opportunities,
  ],
  content: [
    content.overview,
    content.strategy,
    content.briefs,
    content.sources,
    content.article,
    content["fact-check"],
    content.quality,
    content.history,
    content.refresh,
  ],
  execution: [
    execution.publishing,
    execution["internal-links"],
    execution.network,
    execution.technical,
    execution.index,
    execution.sitemaps,
    execution.indexnow,
    execution.gsc,
    execution.ga4,
    execution.bing,
    execution.jobs,
  ],
  system: [
    system.image,
    system.themes,
    system.providers,
    system.agents,
    system.workflows,
    system.jobs,
    system.members,
    system.audit,
    system.settings,
  ],
};

const sidebarPaths = new Set(
  Object.values(menu)
    .flat()
    .map(({ path }) => path),
);

export const researchNavigation = [
  research.seeds,
  research.keywords,
  research.expansion,
  research.jobs,
  research.usage,
];
export const serpNavigation = [research.serps, research.competitors, research.keywords];
export const executionNavigation: readonly (readonly RouteDefinition[])[] = [
  [sites.connections, execution.cms, content.inventory],
  [execution.publishing, execution.publications, execution.verification],
  [execution.gsc, execution.ga4, execution.bing],
  [execution.index, execution.sitemaps, execution.indexnow],
  [execution.technical, execution["internal-links"], execution.network, execution.feedback],
  [execution.jobs, execution.usage, execution.logs],
];

export const SEO_HOME = seoRoutes.dashboard.overview.path;

export const seoPages = (Object.keys(seoRoutes) as Section[]).flatMap((section) =>
  Object.entries(seoRoutes[section]).map(([view, page]) => ({ section, view, ...page }) as SeoRoute),
);

const pagesByPath = new Map(seoPages.map((page) => [page.path, page]));

export function getSeoPage(path: string) {
  return pagesByPath.get(path.replace(/\/+$/, ""));
}

export const seoAliases = {
  execution: seoRoutes.sites.connections.path,
  system: seoRoutes.system.providers.path,
  jobs: seoRoutes.system.jobs.path,
  providers: seoRoutes.system.providers.path,
  members: seoRoutes.system.members.path,
  audit: seoRoutes.system.audit.path,
};

type SearchParams = Pick<URLSearchParams, "get">;
export type SeoQuery = Record<string, string | null | undefined>;
export const marketKeys = ["country_code", "location_code", "language_code", "search_engine", "device"] as const;
const scopeKeys = ["site_group", "site_country", "site_language"] as const;
const transferKeys = [...scopeKeys, ...marketKeys, "keyword"] as const;
export const ALL_SITES = "all";

function inResearch(path: string) {
  return path === "/seo/research" || path.startsWith("/seo/research/");
}

function inContent(path: string) {
  return path === "/seo/content" || path.startsWith("/seo/content/");
}

export function seoSelection(params: SearchParams) {
  const site = params.get("site_id") ?? params.get("site");
  const project = params.get("project_id") ?? params.get("project");
  return { site: isID(site) || site === ALL_SITES ? site : undefined, project: isID(project) ? project : undefined };
}

const legacyToCanonical: Record<string, string> = { site: "site_id", project: "project_id" };

export function changeSeoQuery(query: URLSearchParams, changes: SeoQuery) {
  for (const [rawKey, value] of Object.entries(changes)) {
    if (value === undefined) continue;
    const key = legacyToCanonical[rawKey] ?? rawKey;
    if (key === "site_id") query.delete("site");
    if (key === "project_id") query.delete("project");
    if (value === null) query.delete(key);
    else query.set(key, value);
  }
}

export function seoHref(path: string, currentPath: string, current: SearchParams, overrides: SeoQuery = {}) {
  const [pathname, query] = path.split("?", 2);
  const search = new URLSearchParams(query);
  const previous = seoSelection(current);
  const target = seoSelection(search);
  const site = overrides.site_id ?? overrides.site ?? target.site ?? previous.site;
  const sameSite = site === previous.site;
  const nextKeyword =
    search.get("keyword") ??
    (sameSite ? current.get("keyword") : null) ??
    (sameSite && inResearch(currentPath) && inContent(pathname) ? current.get("q") : null);

  changeSeoQuery(search, {
    site_id: site,
    project_id: target.project ?? (sameSite ? previous.project : null),
    keyword: nextKeyword?.slice(0, 400),
  });
  for (const key of transferKeys) {
    const value = current.get(key);
    if (key !== "keyword" && value && !search.has(key)) search.set(key, value);
  }
  if (sameSite && inResearch(pathname) && inResearch(currentPath)) {
    const q = current.get("q");
    if (q && !search.has("q")) search.set("q", q.slice(0, 400));
  }
  changeSeoQuery(search, overrides);
  return search.size ? `${pathname}?${search}` : pathname;
}

export function seoGroups(role: Role, currentPath: string, query: SearchParams): NavGroup[] {
  return [
    {
      id: 1,
      items: (Object.keys(sections) as Section[]).map((section) => ({
        id: `seo-${section}`,
        ...sections[section],
        activePaths: seoPages
          .filter((page) => page.section === section && !sidebarPaths.has(page.path))
          .map((page) => page.path),
        subItems: menu[section]
          .filter((route) => !route.ownerOnly || role === "owner")
          .map((route) => ({
            id: route.path,
            title: route.planned ? `${route.title}（规划）` : route.title,
            url: seoHref(route.path, currentPath, query),
          })),
      })),
    },
  ];
}

const returnFilters = [
  "site",
  "site_id",
  "project",
  "project_id",
  "package",
  "job",
  "new",
  "keyword",
  "view",
  "q",
  ...scopeKeys,
  ...marketKeys,
];

export function safeSeoReturn(value: string | null) {
  if (!value?.startsWith("/seo/")) return;
  const base = "https://seo.local";
  const url = new URL(value, base);
  const pathname = url.pathname.replace(/\/+$/, "");
  const alias = pathname.slice("/seo/".length);
  if (url.origin !== base || (!getSeoPage(pathname) && !Object.hasOwn(seoAliases, alias))) return;
  const search = new URLSearchParams();
  for (const key of returnFilters) {
    const entry = url.searchParams.get(key);
    if (entry) search.set(key, entry);
  }
  return search.size ? `${pathname}?${search}` : pathname;
}
