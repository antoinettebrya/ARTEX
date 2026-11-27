"use client";

import Link from "next/link";

import { ArrowRight, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { TableCell, TableRow } from "@/components/ui/table";
import type { Job } from "@/lib/seo/client";
import { type DashboardView, seoRoutes } from "@/lib/seo/routes";

import { SiteSelector, siteScopeParams, useSeoLocation, useSiteScope } from "./context";
import { latestJobBySite, loadContentProgress, loadDailyJobs, utcDay } from "./dashboard-data";
import { jobNames, jobStates, jobTones } from "./jobs";
import { useSeoQuery } from "./query";
import { useSeoSession } from "./session";
import {
  FilterBar,
  formatTime,
  LoadError,
  PageFrame,
  PageSkeleton,
  RecordsTable,
  StateBadge,
  useLocalSeoRecords,
} from "./shared";
import { siteStates } from "./sites";

const descriptions = {
  overview: "查看当前站点范围内的任务、内容、发布与搜索表现，以及组织的 AI 使用量。",
  health: "查看站点登记状态与最近 200 条队列任务中各站的最新结果。",
  inbox: "处理今日失败任务和当前站点通过质量检查后待人工审核的文章。",
  activity: "在当前范围内查看最近 200 条组织任务中的活动。",
};

const integer = new Intl.NumberFormat("zh-CN");
const decimal = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2 });
const budgetPercent = new Intl.NumberFormat("zh-CN", { style: "percent", maximumFractionDigits: 4 });
const percent = new Intl.NumberFormat("zh-CN", { style: "percent", maximumFractionDigits: 1 });
const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 6 });

type Metric = { label: string; value?: string | number; detail?: string };

function MetricGrid({ label, metrics }: { label: string; metrics: Metric[] }) {
  return (
    <dl aria-label={label} className="grid grid-cols-2 gap-3 lg:grid-cols-3">
      {metrics.map(({ label, value, detail }) => (
        <Card key={label} className="grid min-w-0 gap-1 p-3">
          <dt className="text-muted-foreground text-sm">{label}</dt>
          <dd className="break-words font-semibold text-lg tabular-nums">
            {typeof value === "number" ? integer.format(value) : (value ?? "暂无数据")}
          </dd>
          {detail && <dd className="break-words text-muted-foreground text-xs">{detail}</dd>}
        </Card>
      ))}
    </dl>
  );
}

function JobStatus({ job }: { job: Job }) {
  return <StateBadge label={jobStates[job.state]} tone={jobTones[job.state]} />;
}

function JobLink({ job }: { job: Job }) {
  const { href, siteID } = useSeoLocation();
  return (
    <Link
      className="font-medium hover:underline"
      href={href(seoRoutes.system.jobs.path, {
        job: job.id,
        site: job.site_id,
        project: siteID === job.site_id ? undefined : null,
      })}
    >
      {jobNames[job.kind]}
    </Link>
  );
}

function ScopedSelection({ scope }: { scope: ReturnType<typeof useSiteScope> }) {
  return (
    <>
      {scope.options.length > 0 && (
        <FilterBar>
          <SiteSelector id="dashboard-site" label="统计站点" scope={scope} />
        </FilterBar>
      )}
      {scope.loading && <PageSkeleton />}
      {scope.error !== undefined && <LoadError error={scope.error} retry={scope.refresh} />}
      {!scope.rows.length && !scope.loading && scope.error === undefined && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>当前范围没有站点</EmptyTitle>
          </EmptyHeader>
        </Empty>
      )}
    </>
  );
}

function useRangeJobs(scope: ReturnType<typeof useSiteScope>) {
  const jobs = useSeoQuery(`${utcDay()}/${scope.key}`, loadDailyJobs);
  const siteIDs = new Set(scope.rows.map(({ id }) => id));
  return {
    ...jobs,
    recent: jobs.data?.recent.filter(({ site_id }) => siteIDs.has(site_id)) ?? [],
    today: jobs.data?.today.filter(({ site_id }) => siteIDs.has(site_id)) ?? [],
    loading: jobs.loading || scope.loading,
    error: jobs.error ?? scope.error,
  };
}

type SourceIndex = { observed: number; indexed: number; not_indexed: number; pending: number };
type DailyUsage = {
  calls: number | null;
  metered: number | null;
  tokens: number | null;
  cost_micros: number | null;
  unknown: number | null;
  budget_ratio: number | null;
};
type DashboardData = {
  date_utc: string;
  from: string;
  to: string;
  sites: { total: number; active: number; observed: number; failed: number; cms_observed: number; cms_failed: number };
  jobs: { executed: number; succeeded: number; failed: number };
  content: { total: number; generation: number; produced: number; pending: number; published: number };
  gsc: {
    connected_sites: number;
    covered_days: number;
    truncated_days: number;
    failed_days: number;
    pending_days: number;
    clicks: number | null;
    impressions: number | null;
    ctr: number | null;
    position: number | null;
    observed_at: string | null;
  };
  ga4: { connected_sites: number; covered_days: number; sessions: number | null; observed_at: string | null };
  index: Record<string, SourceIndex>;
  ai: { model: DailyUsage; data: DailyUsage } | null;
};

function Overview() {
  const scope = useSiteScope();
  const { principal } = useSeoSession();
  const { href } = useSeoLocation();
  const params = siteScopeParams(scope);
  const dashboard = useSeoQuery<DashboardData>(params ? `/dashboard?${params}` : null);
  const data = dashboard.data;
  const terminal = data ? data.jobs.failed + data.jobs.succeeded : 0;
  const gscCoverage = data
    ? `GSC · ${data.gsc.covered_days}/${data.gsc.connected_sites * 28} 站点日 · 明细截断 ${data.gsc.truncated_days} · 失败 ${data.gsc.failed_days} · 待处理 ${data.gsc.pending_days}`
    : undefined;

  return (
    <>
      <ScopedSelection scope={scope} />
      <div className="flex flex-wrap items-center justify-between gap-2">
        {data && (
          <p className="text-muted-foreground text-sm">
            {data.from} 至 {data.to} · 搜索观测 · UTC 任务日 {data.date_utc}
          </p>
        )}
        <Button size="sm" variant="outline" onClick={dashboard.refresh} disabled={dashboard.loading}>
          <RefreshCw /> 刷新运营指标
        </Button>
      </div>
      {dashboard.error !== undefined && <LoadError error={dashboard.error} retry={dashboard.refresh} />}
      {dashboard.loading && <PageSkeleton />}
      {data && (
        <>
          <section className="grid gap-3" aria-label="组织指标">
            <h2 className="font-semibold">站群状态</h2>
            <MetricGrid
              label="组织运营指标"
              metrics={[
                { label: "网站总数", value: data.sites.total },
                { label: "活跃网站", value: data.sites.active },
                {
                  label: "异常网站",
                  value: data.sites.observed ? data.sites.failed : undefined,
                  detail: `有任务观测的站点 ${data.sites.observed}`,
                },
                {
                  label: "CMS 连接失败",
                  value: data.sites.cms_observed ? data.sites.cms_failed : undefined,
                  detail: `有连接验证的站点 ${data.sites.cms_observed}`,
                },
                { label: "今日执行任务", value: data.jobs.executed, detail: "UTC 今日创建且已尝试执行" },
                { label: "任务成功率", value: terminal ? percent.format(data.jobs.succeeded / terminal) : undefined },
                { label: "失败任务", value: data.jobs.failed },
              ]}
            />
            <div className="flex flex-wrap gap-2">
              <Button asChild variant="outline" size="sm">
                <Link href={href(seoRoutes.dashboard.health.path)}>
                  站群健康 <ArrowRight />
                </Link>
              </Button>
              <Button asChild variant="outline" size="sm">
                <Link href={href(seoRoutes.dashboard.inbox.path)}>
                  待处理事项 <ArrowRight />
                </Link>
              </Button>
              <Button asChild variant="outline" size="sm">
                <Link href={href(seoRoutes.dashboard.activity.path)}>
                  最近活动 <ArrowRight />
                </Link>
              </Button>
            </div>
          </section>
          <section className="grid gap-3" aria-label="SEO 表现">
            <h2 className="font-semibold">SEO 表现</h2>
            <MetricGrid
              label="站点搜索指标"
              metrics={[
                {
                  label: "GSC 点击",
                  value: data.gsc.clicks ?? (data.gsc.connected_sites ? "未知" : "未连接"),
                  detail: gscCoverage,
                },
                {
                  label: "GSC 展示",
                  value: data.gsc.impressions ?? (data.gsc.connected_sites ? "未知" : "未连接"),
                  detail: gscCoverage,
                },
                {
                  label: "GSC 点击率",
                  value: data.gsc.ctr === null ? undefined : percent.format(data.gsc.ctr),
                  detail: gscCoverage,
                },
                {
                  label: "GSC 平均排名",
                  value: data.gsc.position === null ? undefined : decimal.format(data.gsc.position),
                  detail: gscCoverage,
                },
                {
                  label: "自然搜索流量",
                  value: data.ga4.sessions ?? (data.ga4.connected_sites ? "未知" : "未连接"),
                  detail: `GA4 · ${data.ga4.covered_days} 个站点日`,
                },
                { label: "已索引 URL", value: data.index.gsc?.indexed, detail: "仅 Google URL 检查" },
                { label: "未索引 URL", value: data.index.gsc?.not_indexed, detail: "仅 Google URL 检查" },
                { label: "待核实索引", value: data.index.gsc?.pending, detail: "仅 Google URL 检查" },
                { label: "必应已索引 URL", value: data.index.bing?.indexed, detail: "Bing 独立观测" },
                { label: "Sitemap 已发现 URL", value: data.index.sitemap?.observed, detail: "站点地图发现记录" },
                { label: "IndexNow 已观测 URL", value: data.index.indexnow?.observed, detail: "提交回执" },
              ]}
            />
            <div className="flex flex-wrap gap-2">
              <Button asChild variant="outline" size="sm">
                <Link href={href(seoRoutes.execution.gsc.path)}>
                  谷歌搜索数据 <ArrowRight />
                </Link>
              </Button>
              <Button asChild variant="outline" size="sm">
                <Link href={href(seoRoutes.execution.index.path)}>
                  索引管理 <ArrowRight />
                </Link>
              </Button>
            </div>
          </section>
          <section className="grid gap-3" aria-label="内容运营">
            <h2 className="font-semibold">内容运营</h2>
            <MetricGrid
              label="站点运营指标"
              metrics={[
                { label: "内容项目", value: data.content.total },
                { label: "待生成文章", value: data.content.generation },
                { label: "内容生产数量", value: data.content.produced },
                { label: "待审核文章", value: data.content.pending },
                { label: "已发布文章", value: data.content.published, detail: "已发布或核验的 CMS 内容，按目标去重" },
              ]}
            />
            <div className="flex flex-wrap gap-2">
              <Button asChild variant="outline" size="sm">
                <Link href={href(seoRoutes.content.overview.path)}>
                  内容项目 <ArrowRight />
                </Link>
              </Button>
              <Button asChild variant="outline" size="sm">
                <Link href={href(seoRoutes.execution.publishing.path)}>
                  发布中心 <ArrowRight />
                </Link>
              </Button>
            </div>
          </section>
          {principal.role === "owner" && data.ai && <AIMetrics model={data.ai.model} research={data.ai.data} />}
        </>
      )}
    </>
  );
}

function AIMetrics({ model, research }: { model: DailyUsage; research: DailyUsage }) {
  const { href } = useSeoLocation();
  const money = (amount: number | null) => (amount === null ? undefined : usd.format(amount / 1_000_000));
  return (
    <section className="grid gap-3" aria-label="AI 运营">
      <div>
        <h2 className="font-semibold">AI 运营</h2>
        <p className="text-muted-foreground text-sm">组织范围 · UTC 今日</p>
      </div>
      <MetricGrid
        label="AI 运营指标"
        metrics={[
          { label: "模型调用量", value: model.calls ?? undefined, detail: "已记录回执" },
          {
            label: "Token 使用量",
            value: model.tokens ?? undefined,
            detail: `${model.metered ?? 0} 条调用有完整 Token`,
          },
          { label: "模型费用", value: money(model.cost_micros), detail: `含估算 · ${model.unknown ?? 0} 笔待确认` },
          { label: "DataForSEO 费用", value: money(research.cost_micros), detail: `${research.unknown ?? 0} 笔待确认` },
          {
            label: "模型每日预算使用率",
            value: model.budget_ratio === null ? undefined : budgetPercent.format(model.budget_ratio),
          },
          {
            label: "数据每日预算使用率",
            value: research.budget_ratio === null ? undefined : budgetPercent.format(research.budget_ratio),
          },
        ]}
      />
      <Button asChild variant="outline" size="sm" className="justify-self-start">
        <Link href={href(seoRoutes.system.providers.path)}>
          大模型服务商与请求诊断 <ArrowRight />
        </Link>
      </Button>
    </section>
  );
}

function Health() {
  const scope = useSiteScope();
  const sites = useLocalSeoRecords(scope.rows, scope.key, scope);
  const jobs = useRangeJobs(scope);
  const { href, siteID } = useSeoLocation();
  const latest = latestJobBySite(jobs.recent);
  const cms = latestJobBySite(jobs.recent, "cms_connection_test", true);
  let noJob = "暂无近期任务";
  if (jobs.loading) noJob = "正在读取";
  if (jobs.error !== undefined) noJob = "读取失败";
  return (
    <>
      {jobs.error !== undefined && <LoadError error={jobs.error} retry={jobs.refresh} />}
      <RecordsTable
        label="站群健康"
        columns={["站点", "市场", "登记状态", "最近任务", "最近 CMS 验证"]}
        empty="当前范围没有站点"
        list={sites}
      >
        {sites.records.map((site) => {
          const job = latest.get(site.id);
          const check = cms.get(site.id);
          return (
            <TableRow key={site.id}>
              <TableCell className="px-3 py-2">
                <Link
                  className="font-medium hover:underline"
                  href={href(seoRoutes.sites.settings.path, {
                    site: site.id,
                    project: siteID === site.id ? undefined : null,
                  })}
                >
                  {site.name}
                </Link>
              </TableCell>
              <TableCell className="px-3">
                {site.country_code} · {site.language_tag}
              </TableCell>
              <TableCell className="px-3">
                <StateBadge label={siteStates[site.status]} />
              </TableCell>
              <TableCell className="px-3">
                {job ? (
                  <div className="flex items-center gap-2">
                    <JobLink job={job} />
                    <JobStatus job={job} />
                  </div>
                ) : (
                  noJob
                )}
              </TableCell>
              <TableCell className="px-3">{check ? <JobStatus job={check} /> : "暂无观测"}</TableCell>
            </TableRow>
          );
        })}
      </RecordsTable>
    </>
  );
}

function Inbox() {
  const scope = useSiteScope();
  const jobs = useRangeJobs(scope);
  const progress = useSeoQuery(scope.site?.id ?? null, loadContentProgress);
  const { href } = useSeoLocation();
  const failed = jobs.today.filter(({ state }) => state === "failed");
  const failures = failed.slice(0, 25);
  const pending = progress.data?.pending ?? [];
  return (
    <>
      <ScopedSelection scope={scope} />
      <section aria-label="失败任务" className="grid gap-3">
        <h2 className="font-semibold">今日失败任务（UTC）</h2>
        {jobs.data && (!jobs.data.complete || failed.length > failures.length) && (
          <p className="text-muted-foreground text-sm">
            已显示 {failures.length} 条近期失败任务。
            <Link className="underline" href={href(seoRoutes.system.jobs.path)}>
              打开任务中心
            </Link>
          </p>
        )}
        <RecordsTable
          label="失败任务"
          columns={["任务", "错误", "更新时间（UTC）"]}
          empty="今日暂无失败任务"
          list={{ ...jobs, count: failures.length }}
        >
          {failures.map((job) => (
            <TableRow key={job.id}>
              <TableCell className="px-3 py-2">
                <JobLink job={job} />
              </TableCell>
              <TableCell className="px-3">{job.error_code ?? "暂无错误代码"}</TableCell>
              <TableCell className="px-3">{formatTime(job.updated_at)}</TableCell>
            </TableRow>
          ))}
        </RecordsTable>
      </section>
      {scope.site && (
        <section aria-label="待审核文章" className="grid gap-3">
          <h2 className="font-semibold">{scope.site.name} · 待审核文章</h2>
          {progress.data?.complete === false && (
            <p className="text-muted-foreground text-sm">覆盖最近 {progress.data.reviewedProjects} 个项目。</p>
          )}
          <RecordsTable
            label="待审核文章"
            columns={["项目", "更新时间（UTC）", "操作"]}
            empty="当前范围暂无待审核文章"
            list={{ ...progress, count: pending.length }}
          >
            {pending.map((project) => (
              <TableRow key={project.id}>
                <TableCell className="px-3 py-2">{project.keyword}</TableCell>
                <TableCell className="px-3">{formatTime(project.updated_at)}</TableCell>
                <TableCell className="px-3">
                  <Link
                    className="font-medium hover:underline"
                    href={href(seoRoutes.content.quality.path, { site: project.site_id, project: project.id })}
                  >
                    打开质量审核
                  </Link>
                </TableCell>
              </TableRow>
            ))}
          </RecordsTable>
        </section>
      )}
    </>
  );
}

function Activity() {
  const scope = useSiteScope();
  const jobs = useRangeJobs(scope);
  const list = useLocalSeoRecords(jobs.recent, scope.key, jobs);
  return (
    <RecordsTable
      label="最近任务活动"
      columns={["任务", "状态", "创建时间（UTC）", "更新时间（UTC）"]}
      empty="暂无任务活动"
      list={list}
    >
      {list.records.map((job) => (
        <TableRow key={job.id}>
          <TableCell className="px-3 py-2">
            <JobLink job={job} />
          </TableCell>
          <TableCell className="px-3">
            <JobStatus job={job} />
          </TableCell>
          <TableCell className="px-3">{formatTime(job.created_at)}</TableCell>
          <TableCell className="px-3">{formatTime(job.updated_at)}</TableCell>
        </TableRow>
      ))}
    </RecordsTable>
  );
}

export function Dashboard({ view }: { view: DashboardView }) {
  return (
    <PageFrame title={seoRoutes.dashboard[view].title} description={descriptions[view]}>
      {view === "overview" && <Overview />}
      {view === "health" && <Health />}
      {view === "inbox" && <Inbox />}
      {view === "activity" && <Activity />}
    </PageFrame>
  );
}
