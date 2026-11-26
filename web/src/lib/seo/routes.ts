import { FileText, Globe, LayoutDashboard, Network, Search, Settings2 } from "lucide-react";

import type { NavGroup } from "@/navigation/sidebar/sidebar-items";

import { isID, type Role } from "./client";

type RouteDefinition = { path: string; title: string; ownerOnly?: boolean; planned?: string };

export const seoRoutes = {
  dashboard: {
    overview: { path: "/seo/dashboard", title: "运营总览" },
    health: { path: "/seo/dashboard/health", title: "站群健康" },
    inbox: { path: "/seo/dashboard/inbox", title: "待处理事项" },
    activity: { path: "/seo/dashboard/activity", title: "最近活动" },
  },
  sites: {
    list: { path: "/seo/sites", title: "全部站点" },
    groups: { path: "/seo/sites/groups", title: "站群分组", planned: "站点分组与分组管理将在后续阶段开放。" },
    connections: { path: "/seo/sites/connections", title: "CMS 连接" },
    settings: { path: "/seo/sites/settings", title: "站点配置" },
  },
  research: {
    seeds: { path: "/seo/research", title: "关键词研究" },
    keywords: { path: "/seo/research/keywords", title: "关键词探索" },
    clusters: { path: "/seo/research/clusters", title: "关键词聚类" },
    serps: { path: "/seo/research/serp", title: "SERP 竞品分析" },
    competitors: { path: "/seo/research/competitors", title: "竞争页面" },
    intents: { path: "/seo/research/intents", title: "搜索意图" },
    mappings: { path: "/seo/research/mapping", title: "Keyword-to-URL" },
    opportunities: { path: "/seo/research/opportunities", title: "关键词机会" },
    jobs: { path: "/seo/research/history", title: "研究历史" },
    usage: { path: "/seo/research/usage", title: "数据用量", ownerOnly: true },
  },
  content: {
    overview: { path: "/seo/content", title: "内容项目" },
    strategy: { path: "/seo/content/strategy", title: "内容策略" },
    briefs: { path: "/seo/content/briefs", title: "内容简报" },
    sources: { path: "/seo/content/sources", title: "研究来源" },
    article: { path: "/seo/content/article", title: "文章编辑" },
    "fact-check": { path: "/seo/content/fact-check", title: "事实核验" },
    quality: { path: "/seo/content/quality", title: "质量审核" },
    history: { path: "/seo/content/history", title: "版本历史" },
    refresh: { path: "/seo/content/refresh", title: "旧文优化" },
    inventory: { path: "/seo/content/inventory", title: "站点内容清单" },
  },
  execution: {
    cms: { path: "/seo/execution/cms", title: "CMS 管理" },
    publishing: { path: "/seo/execution/publishing", title: "发布中心" },
    gsc: { path: "/seo/execution/analytics", title: "搜索分析" },
    ga4: { path: "/seo/execution/ga4", title: "GA4" },
    bing: { path: "/seo/execution/bing", title: "Bing Webmaster" },
    index: { path: "/seo/execution/indexing", title: "索引管理" },
    sitemaps: { path: "/seo/execution/sitemap", title: "Sitemap" },
    indexnow: { path: "/seo/execution/indexnow", title: "IndexNow" },
    "internal-links": {
      path: "/seo/execution/internal-links",
      title: "站内链接",
      planned: "站内链接分析、建议和审核尚未开放。",
    },
    network: { path: "/seo/execution/site-network", title: "站群互链", planned: "跨站引用分析和编辑审核尚未开放。" },
    technical: { path: "/seo/execution/technical", title: "Technical SEO" },
    jobs: { path: "/seo/execution/monitoring", title: "执行监控" },
    feedback: { path: "/seo/execution/feedback", title: "优化反馈" },
    usage: { path: "/seo/execution/usage", title: "执行用量", ownerOnly: true },
  },
  system: {
    image: { path: "/seo/system/image-studio", title: "AI 图片工作室", planned: "图片生成和资产工作室尚未开放。" },
    themes: {
      path: "/seo/system/theme-factory",
      title: "WordPress 主题工厂",
      planned: "主题生成、测试和部署管理尚未开放。",
    },
    providers: { path: "/seo/system/providers", title: "AI Provider" },
    agents: { path: "/seo/system/agents", title: "Agent 管理", planned: "Agent 的配置与运行管理界面尚未开放。" },
    jobs: { path: "/seo/system/jobs", title: "任务中心" },
    members: { path: "/seo/system/members", title: "成员管理", ownerOnly: true },
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
export type ResearchView = View<"research">;
export type ContentView = View<"content">;
export type ExecutionView = View<"execution">;
export type DashboardView = View<"dashboard">;
type RouteSelection = { [S in Section]: { section: S; view: View<S> } }[Section];
export type SeoRoute = RouteSelection & RouteDefinition;

const sections = {
  dashboard: { title: "Dashboard", icon: LayoutDashboard },
  sites: { title: "Sites", icon: Globe },
  research: { title: "Research", icon: Search },
  content: { title: "Content", icon: FileText },
  execution: { title: "Execution", icon: Network },
  system: { title: "AI & System", icon: Settings2 },
} as const;

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
const researchFilters = ["country_code", "location_code", "language_code", "search_engine", "device", "q"];

function inResearch(path: string) {
  return path === "/seo/research" || path.startsWith("/seo/research/");
}

export function seoHref(path: string, currentPath: string, current: SearchParams, overrides: SeoQuery = {}) {
  const [pathname, query] = path.split("?", 2);
  const search = new URLSearchParams(query);
  for (const key of ["site", "project"]) {
    const value = current.get(key);
    if (isID(value) && !search.has(key)) search.set(key, value);
  }
  if (inResearch(pathname) && inResearch(currentPath)) {
    for (const filter of researchFilters) {
      const value = current.get(filter);
      if (value && !search.has(filter)) search.set(filter, value);
    }
  }
  for (const [key, value] of Object.entries(overrides)) {
    if (value === null) search.delete(key);
    else if (value !== undefined) search.set(key, value);
  }
  return search.size ? `${pathname}?${search}` : pathname;
}

export function seoGroups(role: Role, currentPath: string, query: SearchParams): NavGroup[] {
  return [
    {
      id: 1,
      items: (Object.keys(sections) as Section[]).map((section) => ({
        id: `seo-${section}`,
        ...sections[section],
        subItems: seoPages
          .filter((route) => route.section === section && (!route.ownerOnly || role === "owner"))
          .map((route) => ({
            id: `seo-${section}-${route.view}`,
            title: route.planned ? `${route.title}（规划）` : route.title,
            url: seoHref(route.path, currentPath, query),
          })),
      })),
    },
  ];
}

const returnFilters = ["site", "project", "package", "job", "new", "keyword", "view", ...researchFilters];

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
