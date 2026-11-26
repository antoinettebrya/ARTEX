"use client";

import Link from "next/link";

import { ArrowRight, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { TableCell, TableRow } from "@/components/ui/table";
import type { Job, Site } from "@/lib/seo/client";
import { type DashboardView, seoRoutes } from "@/lib/seo/routes";

import { SiteSelector, useSeoLocation, useSiteScope } from "./context";
import {
  loadContentProgress,
  loadDailyCost,
  loadDailyJobs,
  loadPublished,
  loadRemainingSites,
  loadSearchReports,
  utcDay,
} from "./dashboard-data";
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
  useSeoList,
} from "./shared";
import { siteStates } from "./sites";

const descriptions = {
  overview: "查看组织运营指标和当前站点的内容、发布与搜索数据。",
  health: "查看站点登记状态与最近 200 条队列任务中各站的最新结果。",
  inbox: "处理今日失败任务和当前站点通过质量检查后待人工审核的文章。",
  activity: "按创建时间浏览当前组织的任务活动。",
};

const integer = new Intl.NumberFormat("zh-CN");
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
      {!scope.site && !scope.loading && scope.error === undefined && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>暂无登记站点</EmptyTitle>
          </EmptyHeader>
        </Empty>
      )}
    </>
  );
}

function PortfolioMetrics({ scope }: { scope: ReturnType<typeof useSiteScope> }) {
  const day = utcDay();
  const { principal } = useSeoSession();
  const { href } = useSeoLocation();
  const owner = principal.role === "owner";
  const { sites } = scope;
  const needsMore = sites.data?.sites.length === 200;
  const remaining = useSeoQuery(needsMore ? "/sites" : null, loadRemainingSites);
  const jobs = useSeoQuery(day, loadDailyJobs);
  const cost = useSeoQuery(owner ? day : null, loadDailyCost);
  const pending = sites.loading || remaining.loading || jobs.loading || cost.loading;
  const totalSites =
    sites.data && (!needsMore || remaining.data) ? [...sites.data.sites, ...(remaining.data?.rows ?? [])] : undefined;
  const totalsPartial = remaining.data?.complete === false;
  const jobCoverage = jobs.data?.complete === false ? "已读取部分今日记录" : "按 UTC 今日创建";
  const costCoverage = cost.data?.complete === false ? " · 已读取部分记录" : "";

  return (
    <section className="grid gap-3" aria-label="组织指标">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="font-semibold">组织运营</h2>
          <p className="text-muted-foreground text-sm">{day} UTC</p>
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            sites.refresh();
            if (needsMore) remaining.refresh();
            jobs.refresh();
            if (owner) cost.refresh();
          }}
          disabled={pending}
        >
          <RefreshCw /> 刷新组织指标
        </Button>
      </div>
      {Object.entries({ sites, remaining, jobs, cost }).map(
        ([key, source]) =>
          source.error !== undefined && <LoadError key={key} error={source.error} retry={source.refresh} />,
      )}
      {pending ? (
        <PageSkeleton />
      ) : (
        <MetricGrid
          label="组织运营指标"
          metrics={[
            {
              label: "管理站点数量",
              value: totalSites?.length,
              detail: totalsPartial ? "已读取的登记站点" : undefined,
            },
            {
              label: "活跃站点数量",
              value: totalSites?.filter(({ status }) => status === "active").length,
              detail: totalsPartial ? "已读取的启用站点" : "按登记状态",
            },
            { label: "今日执行任务", value: jobs.data?.attempted, detail: `${jobCoverage}且已尝试执行` },
            {
              label: "任务成功率",
              value: jobs.data?.rate === undefined ? undefined : percent.format(jobs.data.rate),
              detail: `${jobCoverage}，仅成功与失败任务`,
            },
            { label: "异常任务", value: jobs.data?.failures.length, detail: `${jobCoverage}的失败任务` },
            ...(owner
              ? [
                  {
                    label: "Provider 成本",
                    value: cost.data?.amount === undefined ? undefined : usd.format(cost.data.amount),
                    detail: cost.data ? `厂商回执及估算 · ${cost.data.unknown} 笔待确认${costCoverage}` : undefined,
                  },
                ]
              : []),
          ]}
        />
      )}
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
  );
}

function SiteMetrics({ site }: { site: Site }) {
  const progress = useSeoQuery(site.id, loadContentProgress);
  const published = useSeoQuery(site.id, loadPublished);
  const reports = useSeoQuery(site.id, loadSearchReports);
  const { href } = useSeoLocation();
  const pending = progress.loading || published.loading || reports.loading;
  const range = reports.data;
  const projectScope =
    progress.data && (progress.data.complete ? "本站内容项目" : `覆盖最近 ${progress.data.reviewedProjects} 个项目`);
  const gscScope = range?.gsc
    ? `${range.gsc.source} · ${range.gsc.days}/28 天${range.gsc.complete ? "" : " · 部分明细"}`
    : "暂无同步的 GSC 数据";
  const ga4Scope = range?.ga4
    ? `${range.ga4.source} · ${range.ga4.days}/28 天${range.ga4.complete ? "" : " · 部分明细"}`
    : "暂无同步的 GA4 自然搜索数据";
  return (
    <section className="grid gap-3" aria-label="站点指标">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div className="min-w-0">
          <h2 className="break-words font-semibold">{site.name} · 站点运营</h2>
          {range && (
            <p className="text-muted-foreground text-sm">
              搜索报告 {range.from} 至 {range.to}，按已同步明细统计
            </p>
          )}
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={pending}
          onClick={() => {
            progress.refresh();
            published.refresh();
            reports.refresh();
          }}
        >
          <RefreshCw /> 刷新站点指标
        </Button>
      </div>
      {progress.error !== undefined && <LoadError error={progress.error} retry={progress.refresh} />}
      {published.error !== undefined && <LoadError error={published.error} retry={published.refresh} />}
      {reports.error !== undefined && <LoadError error={reports.error} retry={reports.refresh} />}
      {pending ? (
        <PageSkeleton />
      ) : (
        <MetricGrid
          label="站点运营指标"
          metrics={[
            { label: "内容生产数量", value: progress.data?.count, detail: projectScope },
            { label: "待审核文章", value: progress.data?.pending.length, detail: projectScope },
            {
              label: "发布成功数量",
              value: published.data?.count,
              detail: published.data?.complete === false ? "已读取的发布记录" : "已发布或已核验",
            },
            { label: "GSC 点击", value: range?.gsc?.totals[0], detail: gscScope },
            { label: "GSC 展示", value: range?.gsc?.totals[1], detail: gscScope },
            { label: "自然搜索流量", value: range?.ga4?.totals[0], detail: ga4Scope },
          ]}
        />
      )}
      <div className="flex flex-wrap gap-2">
        <Button asChild variant="outline" size="sm">
          <Link href={href(seoRoutes.content.overview.path, { site: site.id })}>
            内容项目 <ArrowRight />
          </Link>
        </Button>
        <Button asChild variant="outline" size="sm">
          <Link href={href(seoRoutes.execution.publishing.path, { site: site.id })}>
            发布中心 <ArrowRight />
          </Link>
        </Button>
        <Button asChild variant="outline" size="sm">
          <Link href={href(seoRoutes.execution.gsc.path, { site: site.id })}>
            搜索分析 <ArrowRight />
          </Link>
        </Button>
      </div>
    </section>
  );
}

function Overview() {
  const scope = useSiteScope();
  return (
    <>
      <PortfolioMetrics scope={scope} />
      <ScopedSelection scope={scope} />
      {scope.site && <SiteMetrics site={scope.site} />}
    </>
  );
}

function Health() {
  const sites = useSeoList<Site>("sites");
  const jobs = useSeoQuery<{ jobs: Job[] }>("/jobs?limit=200");
  const { href, siteID } = useSeoLocation();
  const latest = new Map<string, Job>();
  let noJob = "暂无近期任务";
  if (jobs.loading) noJob = "正在读取";
  if (jobs.error !== undefined) noJob = "读取失败";
  for (const job of jobs.data?.jobs ?? []) {
    if (!latest.has(job.site_id)) latest.set(job.site_id, job);
  }
  return (
    <>
      {jobs.error !== undefined && <LoadError error={jobs.error} retry={jobs.refresh} />}
      <RecordsTable
        label="站群健康"
        columns={["站点", "市场", "登记状态", "最近任务"]}
        empty="暂无登记站点"
        list={sites}
      >
        {sites.records.map((site) => {
          const job = latest.get(site.id);
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
            </TableRow>
          );
        })}
      </RecordsTable>
    </>
  );
}

function Inbox() {
  const scope = useSiteScope();
  const day = utcDay();
  const jobs = useSeoQuery(day, loadDailyJobs);
  const progress = useSeoQuery(scope.site?.id ?? null, loadContentProgress);
  const { href } = useSeoLocation();
  const failures = jobs.data?.failures.slice(0, 25) ?? [];
  const pending = progress.data?.pending ?? [];
  return (
    <>
      <ScopedSelection scope={scope} />
      <section aria-label="失败任务" className="grid gap-3">
        <h2 className="font-semibold">今日失败任务（UTC）</h2>
        {jobs.data && (!jobs.data.complete || jobs.data.failures.length > failures.length) && (
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
  const list = useSeoList<Job>("jobs");
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
