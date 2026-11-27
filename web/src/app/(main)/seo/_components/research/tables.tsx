"use client";

import type { ReactNode } from "react";

import Link from "next/link";

import { Ban, FileText, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { marketFilterChanges } from "@/lib/seo/market";
import type {
  Cluster,
  Competitor,
  IntentRow,
  KeywordRow,
  MappingRow,
  Market,
  OpportunityRow,
  ProviderUsage,
  ResearchJob,
  ResearchKind,
  SERP,
  Seed,
  SitePage,
  Suggestion,
} from "@/lib/seo/research";
import { marketLabel, marketQuery, researchJobLabels, usd } from "@/lib/seo/research";
import { type ResearchView, seoRoutes } from "@/lib/seo/routes";

import { useSeoLocation } from "../context";
import { formatTime, RecordsTable, StateBadge, useSeoRecords } from "../shared";

export type Detail =
  | { kind: "serp"; row: SERP }
  | { kind: "competitor"; row: Competitor }
  | { kind: "cluster"; row: Cluster }
  | { kind: "intent"; row: IntentRow }
  | { kind: "mapping"; row: MappingRow }
  | { kind: "opportunity"; row: OpportunityRow }
  | { kind: "page"; row: SitePage }
  | { kind: "snapshot"; id: string }
  | { kind: "job"; row: ResearchJob };

type Column<T> = { name: string; value: (row: T) => ReactNode };

function ResearchTable<T>({
  title,
  path,
  columns,
  identify,
  children,
}: {
  title: string;
  path: string;
  columns: Column<T>[];
  identify: (row: T) => string;
  children?: ReactNode;
}) {
  const list = useSeoRecords<T>(path, "items");
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-end gap-2">
        {children}
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-label={`刷新${title}`}
          onClick={list.refresh}
          disabled={list.refreshing}
        >
          <RefreshCw /> 刷新
        </Button>
      </div>
      <RecordsTable label={title} columns={columns.map((column) => column.name)} empty={`暂无${title}数据`} list={list}>
        {list.records.map((row) => (
          <TableRow key={identify(row)}>
            {columns.map((column) => (
              <TableCell className="px-3 py-2 tabular-nums" key={column.name}>
                {column.value(row)}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </RecordsTable>
    </div>
  );
}

const intentLabels: Record<string, string> = {
  informational: "信息型",
  commercial: "商业调查型",
  transactional: "交易型",
  navigational: "导航型",
  local: "本地型",
  unknown: "证据不足",
};

export function intentLabel(value?: string) {
  return value ? (intentLabels[value] ?? value) : "—";
}

const actionLabels: Record<string, string> = {
  keep: "保留",
  update: "更新已有页面",
  create: "创建新页面",
  merge: "核对合并",
  consolidate: "核对合并",
  inventory_missing: "补充本站页面证据",
  insufficient_evidence: "证据不足",
};

export function mappingLabel(value: string) {
  return actionLabels[value] ?? value;
}

const states = { queued: "排队中", running: "运行中", succeeded: "完成", failed: "失败", cancelled: "已取消" } as const;

export function time(value?: string | null) {
  return value && !Number.isNaN(Date.parse(value)) ? formatTime(value) : "—";
}

export function percent(value: number) {
  return `${Math.round(value * 100)}%`;
}

export function URLText({ value }: { value?: string }) {
  return (
    <span className="block max-w-120 whitespace-normal break-all font-mono text-muted-foreground text-xs">
      {value ? value : "—"}
    </span>
  );
}

function MarketText({ market }: { market: Market }) {
  return <span className="text-muted-foreground text-xs">{marketLabel(market)}</span>;
}

function KeywordText({ keyword }: { keyword: string }) {
  return <span className="block min-w-30 max-w-80 whitespace-normal break-words font-medium">{keyword}</span>;
}

const sourceLabels: Record<string, string> = {
  analysis: "研究分析",
  site_inventory_import: "本站页面登记",
  keyword_overview: "关键词指标",
  related_keywords: "相关关键词",
  keyword_suggestions: "关键词建议",
  keywords_for_keywords: "关键词建议",
  search_volume: "搜索量",
  instant_pages: "页面证据",
  bulk_keyword_difficulty: "关键词难度",
  advanced: "自然搜索结果",
  content_parsing: "正文分块",
  microdata: "结构化数据",
};

export function sourceLabel(source: string) {
  if (!source.startsWith("dataforseo/")) return sourceLabels[source] ?? source;
  const parts = source.split("/");
  const operation = parts.at(-1) === "live" ? parts.at(-2) : parts.at(-1);
  const label = operation ? sourceLabels[operation] : undefined;
  return label ? `DataForSEO · ${label}` : "DataForSEO";
}

function Scope({
  market,
  source,
  collected_at,
}: {
  market: Market;
  source?: string | null;
  collected_at?: string | null;
}) {
  return (
    <>
      <MarketText market={market} />
      <div className="max-w-56 whitespace-normal break-words text-muted-foreground text-xs">
        {source && (
          <>
            <span title={source}>{sourceLabel(source)}</span>
            {" · "}
          </>
        )}
        <span className="whitespace-nowrap">{time(collected_at)}</span>
      </div>
    </>
  );
}

function action(title: string, onClick: () => void) {
  return (
    <Button size="sm" variant="ghost" type="button" onClick={onClick}>
      <FileText /> {title}
    </Button>
  );
}

function keyed(row: {
  keyword?: string;
  id?: string;
  market: Market;
  collected_at?: string | null;
  source?: string | null;
}) {
  return `${row.id ?? row.keyword ?? ""}/${marketLabel(row.market)}/${row.collected_at ?? ""}/${row.source ?? ""}`;
}

function researchPath(section: ResearchView | "pages" | "suggestions", site: string, market?: Market, query?: string) {
  if (section === "usage") return "/research/usage";
  const params = new URLSearchParams({ site_id: site });
  if (market && section !== "jobs") marketQuery(params, market);
  if (query && section === "keywords") params.set("q", query);
  return `/research/${section}?${params}`;
}

export function ResearchResults({
  view,
  site,
  market,
  search,
  writable,
  canStart,
  owner,
  onDetail,
  onJob,
  onCancel,
  onAddPage,
}: {
  view: ResearchView;
  site: string;
  market?: Market;
  search?: string;
  writable: boolean;
  canStart: boolean;
  owner: boolean;
  onDetail: (detail: Detail) => void;
  onJob: (kind: ResearchKind, keyword: string, market: Market) => void;
  onCancel: (job: ResearchJob) => void;
  onAddPage: () => void;
}) {
  const { href } = useSeoLocation();
  const path = researchPath(view === "expansion" ? "suggestions" : view, site, market, search);
  const title = view === "seeds" ? "种子关键词" : seoRoutes.research[view].title;

  function dataSource(row: { snapshot_id?: string | null }) {
    const id = row.snapshot_id;
    return owner && id ? action("原始响应", () => onDetail({ kind: "snapshot", id })) : null;
  }

  switch (view) {
    case "seeds":
      return (
        <ResearchTable<Seed>
          title={title}
          path={path}
          identify={keyed}
          columns={[
            { name: "种子词", value: (r) => <KeywordText keyword={r.keyword} /> },
            { name: "市场", value: (r) => <MarketText market={r.market} /> },
            { name: "最近研究（UTC）", value: (r) => time(r.collected_at) },
            ...(canStart
              ? [
                  {
                    name: "操作",
                    value: (r: Seed) => action("收集指标", () => onJob("keyword_metrics", r.keyword, r.market)),
                  },
                ]
              : []),
          ]}
        />
      );
    case "expansion":
      return (
        <ResearchTable<Suggestion>
          title="关键词建议"
          path={path}
          identify={(r) => `${keyed(r)}/${r.seed}/${r.keyword}/${r.operation}`}
          columns={[
            { name: "种子词", value: (r) => <KeywordText keyword={r.seed} /> },
            { name: "建议关键词", value: (r) => <KeywordText keyword={r.keyword} /> },
            { name: "拓展方式", value: (r) => sourceLabel(r.operation) },
            { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
            {
              name: "操作",
              value: (r) => (
                <div className="flex flex-wrap gap-1">
                  {canStart && action("收集指标", () => onJob("keyword_metrics", r.keyword, r.market))}
                  {dataSource(r)}
                </div>
              ),
            },
          ]}
        />
      );
    case "keywords":
      return (
        <ResearchTable<KeywordRow>
          title={title}
          path={path}
          identify={keyed}
          columns={[
            { name: "关键词", value: (r) => <KeywordText keyword={r.keyword} /> },
            { name: "搜索量", value: (r) => r.metrics?.search_volume ?? "—" },
            { name: "CPC · USD", value: (r) => (r.metrics?.cpc_usd == null ? "—" : usd.format(r.metrics.cpc_usd)) },
            {
              name: "广告竞争",
              value: ({ metrics }) => {
                if (metrics?.competition_index != null) return `${metrics.competition_index} / 100`;
                return metrics?.competition == null ? "—" : `${metrics.competition} / 1`;
              },
            },
            { name: "关键词难度", value: (r) => r.metrics?.keyword_difficulty ?? "—" },
            { name: "主要意图", value: (r) => intentLabel(r.intent?.primary) },
            {
              name: "市场与来源（UTC）",
              value: (r) => (
                <>
                  <Scope {...r} />
                  {r.metrics?.device_scope === "all" && (
                    <div className="text-muted-foreground text-xs">指标覆盖全部设备</div>
                  )}
                </>
              ),
            },
            {
              name: "操作",
              value: (r) => {
                const intent = r.intent;
                return (
                  <div className="flex flex-wrap gap-1">
                    {canStart && action("研究 SERP", () => onJob("serp_analysis", r.keyword, r.market))}
                    {writable && (
                      <Button asChild size="sm" variant="ghost">
                        <Link
                          href={href(seoRoutes.content.overview.path, {
                            site_id: site,
                            project_id: null,
                            keyword: r.keyword,
                            ...marketFilterChanges(r.market),
                          })}
                        >
                          用于内容项目
                        </Link>
                      </Button>
                    )}
                    {intent && action("意图依据", () => onDetail({ kind: "intent", row: { ...r, intent } }))}
                    {dataSource(r)}
                  </div>
                );
              },
            },
          ]}
        />
      );
    case "clusters":
      return (
        <ResearchTable<Cluster>
          title={title}
          path={path}
          identify={keyed}
          columns={[
            { name: "主关键词", value: (r) => <KeywordText keyword={r.primary_keyword} /> },
            { name: "成员", value: (r) => r.member_ids?.length ?? 0 },
            { name: "推荐 URL", value: (r) => <URLText value={r.recommended_url} /> },
            { name: "页面类型", value: (r) => r.page_type || "—" },
            { name: "聚类置信度", value: (r) => percent(r.confidence) },
            { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
            { name: "操作", value: (r) => action("聚类依据", () => onDetail({ kind: "cluster", row: r })) },
          ]}
        />
      );
    case "serps":
      return (
        <ResearchTable<SERP>
          title={title}
          path={path}
          identify={keyed}
          columns={[
            { name: "关键词", value: (r) => <KeywordText keyword={r.keyword} /> },
            { name: "自然结果", value: (r) => r.results?.length ?? 0 },
            { name: "第一名", value: (r) => <URLText value={r.results?.[0]?.url} /> },
            { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
            {
              name: "操作",
              value: (r) => (
                <div className="flex flex-wrap gap-1">
                  {action("查看自然结果", () => onDetail({ kind: "serp", row: r }))}
                  {dataSource(r)}
                </div>
              ),
            },
          ]}
        />
      );
    case "competitors":
      return (
        <ResearchTable<Competitor>
          title={title}
          path={path}
          identify={(r) => `${keyed(r)}/${r.page.url}`}
          columns={[
            {
              name: "竞争页面",
              value: (r) => (
                <>
                  <KeywordText keyword={r.page.title || r.keyword} />
                  <URLText value={r.page.url} />
                </>
              ),
            },
            { name: "关键词", value: (r) => r.keyword },
            { name: "页面类型", value: (r) => r.page.page_type || "—" },
            { name: "更新时间（UTC）", value: (r) => time(r.page.last_modified) },
            { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
            {
              name: "操作",
              value: (r) => (
                <div className="flex flex-wrap gap-1">
                  {action("查看页面证据", () => onDetail({ kind: "competitor", row: r }))}
                  {dataSource(r)}
                </div>
              ),
            },
          ]}
        />
      );
    case "intents":
      return (
        <ResearchTable<IntentRow>
          title={title}
          path={path}
          identify={keyed}
          columns={[
            { name: "关键词", value: (r) => <KeywordText keyword={r.keyword} /> },
            { name: "主意图", value: (r) => intentLabel(r.intent.primary) },
            { name: "次意图", value: (r) => intentLabel(r.intent.secondary) },
            { name: "推荐页面类型", value: (r) => (r.intent.page_type ? r.intent.page_type : "—") },
            { name: "分类置信度", value: (r) => percent(r.intent.confidence) },
            { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
            { name: "操作", value: (r) => action("意图依据", () => onDetail({ kind: "intent", row: r })) },
          ]}
        />
      );
    case "mappings":
      return (
        <div className="space-y-6">
          <ResearchTable<MappingRow>
            title={title}
            path={path}
            identify={keyed}
            columns={[
              { name: "关键词", value: (r) => <KeywordText keyword={r.keyword} /> },
              { name: "目标 URL", value: (r) => <URLText value={r.mapping.target_url} /> },
              { name: "建议", value: (r) => mappingLabel(r.mapping.action) },
              { name: "蚕食 URL", value: (r) => r.mapping.cannibalizing_urls?.length ?? 0 },
              { name: "Canonical", value: (r) => (r.mapping.canonical_issue ? <StateBadge label="需要核对" /> : "—") },
              { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
              { name: "操作", value: (r) => action("映射依据", () => onDetail({ kind: "mapping", row: r })) },
            ]}
          />
          <div className="space-y-2">
            <h2 className="font-medium text-sm">本站页面证据</h2>
            <ResearchTable<SitePage>
              title="本站页面证据"
              path={researchPath("pages", site, market)}
              identify={(r) => `${keyed(r)}/${r.page.url}`}
              columns={[
                {
                  name: "页面",
                  value: (r) => (
                    <>
                      <KeywordText keyword={r.page.title || r.page.url} />
                      <URLText value={r.page.url} />
                    </>
                  ),
                },
                { name: "Canonical URL", value: (r) => <URLText value={r.page.canonical_url} /> },
                { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
                { name: "操作", value: (r) => action("查看页面证据", () => onDetail({ kind: "page", row: r })) },
              ]}
            >
              {writable && (
                <Button size="sm" variant="outline" type="button" onClick={onAddPage}>
                  登记本站页面
                </Button>
              )}
            </ResearchTable>
          </div>
        </div>
      );
    case "opportunities":
      return (
        <ResearchTable<OpportunityRow>
          title={title}
          path={path}
          identify={keyed}
          columns={[
            { name: "关键词", value: (r) => <KeywordText keyword={r.keyword} /> },
            { name: "机会分／100", value: (r) => r.opportunity.score?.toFixed(1) ?? "证据不足" },
            { name: "已知分", value: (r) => r.opportunity.known_points.toFixed(1) },
            { name: "覆盖权重", value: (r) => `${r.opportunity.coverage} / 100` },
            { name: "算法版本", value: (r) => r.opportunity.version },
            { name: "市场与来源（UTC）", value: (r) => <Scope {...r} /> },
            { name: "操作", value: (r) => action("评分依据", () => onDetail({ kind: "opportunity", row: r })) },
          ]}
        />
      );
    case "jobs":
      return (
        <ResearchTable<ResearchJob>
          title={title}
          path={path}
          identify={(r) => r.job.id}
          columns={[
            { name: "关键词", value: (r) => <KeywordText keyword={r.request.keyword} /> },
            { name: "任务", value: (r) => researchJobLabels[r.request.kind] },
            { name: "市场", value: (r) => <MarketText market={r.request.market} /> },
            {
              name: "状态",
              value: (r) => (
                <>
                  <StateBadge label={states[r.job.state]} tone={r.job.state === "failed" ? "destructive" : "outline"} />
                  {r.job.error_code && (
                    <div className="mt-1 font-mono text-muted-foreground text-xs">{r.job.error_code}</div>
                  )}
                </>
              ),
            },
            { name: "优先级", value: (r) => r.request.priority },
            { name: "尝试次数", value: (r) => r.job.attempts },
            { name: "调度时间（UTC）", value: (r) => time(r.job.available_at) },
            {
              name: "操作",
              value: (r) => (
                <div className="flex flex-wrap gap-1">
                  {action("任务详情", () => onDetail({ kind: "job", row: r }))}
                  {writable && (r.job.state === "queued" || r.job.state === "running") && (
                    <Button type="button" variant="ghost" size="sm" onClick={() => onCancel(r)}>
                      <Ban /> 取消
                    </Button>
                  )}
                </div>
              ),
            },
          ]}
        />
      );
    case "usage":
      return (
        owner && (
          <ResearchTable<ProviderUsage>
            title={title}
            path={path}
            identify={(r) => `${r.provider_id}/${r.date_utc}`}
            columns={[
              { name: "Provider", value: (r) => r.name },
              { name: "预算日（UTC）", value: (r) => r.date_utc },
              { name: "请求数", value: (r) => r.requests },
              { name: "已结算 USD", value: (r) => usd.format(r.spent_micros / 1_000_000) },
              { name: "待结算预留 USD", value: (r) => usd.format(r.reserved_micros / 1_000_000) },
              { name: "日预算 USD", value: (r) => usd.format(r.day_limit_micros / 1_000_000) },
            ]}
          />
        )
      );
  }
}
