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
  latestJobBySite,
  loadContentProgress,
  loadDailyAI,
  loadDailyJobs,
  loadIndexStatus,
  loadPublished,
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
  useLocalSeoRecords,
} from "./shared";
import { siteStates } from "./sites";

const descriptions = {
  overview: "查看组织运营指标和当前站点的内容、发布与搜索数据。",
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

function PortfolioMetrics({ scope }: { scope: ReturnType<typeof useSiteScope> }) {
  const jobs = useRangeJobs(scope);
  const { href } = useSeoLocation();
  const outcomes = latestJobBySite(jobs.recent, undefined, true);
  const cms = latestJobBySite(jobs.recent, "cms_connection_test", true);
  const failed = jobs.today.filter(({ state }) => state === "failed").length;
  const succeeded = jobs.today.filter(({ state }) => state === "succeeded").length;
  const outcomesDetail = outcomes.size
    ? `最近 200 条组织任务覆盖 ${outcomes.size}/${scope.rows.length} 站`
    : "暂无已完成任务观测";
  const cmsDetail = cms.size ? `最近 CMS 验证覆盖 ${cms.size}/${scope.rows.length} 站` : "暂无 CMS 验证观测";
  const dayDetail = jobs.data?.complete === false ? "UTC 今日 · 部分任务记录" : "UTC 今日创建";
  const hasSites = !scope.loading && scope.error === undefined;
  const hasJobs = !jobs.loading && jobs.error === undefined && jobs.data !== undefined;

  return (
    <section className="grid gap-3" aria-label="组织指标">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="font-semibold">站群状态</h2>
          <p className="text-muted-foreground text-sm">{utcDay()} UTC · 当前站点范围</p>
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            scope.refresh();
            jobs.refresh();
          }}
          disabled={jobs.loading}
        >
          <RefreshCw /> 刷新站群状态
        </Button>
      </div>
      {jobs.error !== undefined && (
        <LoadError
          error={jobs.error}
          retry={() => {
            scope.refresh();
            jobs.refresh();
          }}
        />
      )}
      {jobs.loading ? (
        <PageSkeleton />
      ) : (
        <MetricGrid
          label="组织运营指标"
          metrics={[
            {
              label: "网站总数",
              value: hasSites ? scope.rows.length : undefined,
              detail: scope.complete === false ? "仅已读取的站点" : "当前登记范围",
            },
            {
              label: "活跃网站",
              value: hasSites ? scope.rows.filter(({ status }) => status === "active").length : undefined,
              detail: "按登记状态",
            },
            {
              label: "异常网站",
              value:
                hasJobs && outcomes.size
                  ? [...outcomes.values()].filter(({ state }) => state === "failed").length
                  : undefined,
              detail: outcomesDetail,
            },
            {
              label: "CMS 连接失败",
              value:
                hasJobs && cms.size ? [...cms.values()].filter(({ state }) => state === "failed").length : undefined,
              detail: cmsDetail,
            },
            {
              label: "今日执行任务",
              value: hasJobs ? jobs.today.filter(({ attempts }) => attempts > 0).length : undefined,
              detail: `${dayDetail}且已尝试执行`,
            },
            {
              label: "任务成功率",
              value: hasJobs && failed + succeeded > 0 ? percent.format(succeeded / (failed + succeeded)) : undefined,
              detail: `${dayDetail}，仅成功与失败任务`,
            },
            { label: "失败任务", value: hasJobs ? failed : undefined, detail: dayDetail },
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

function SearchMetrics({ site }: { site: Site }) {
  const reports = useSeoQuery(site.id, loadSearchReports);
  const index = useSeoQuery(site.id, loadIndexStatus);
  const { href } = useSeoLocation();
  const report = reports.data;
  const gsc = report?.gsc;
  const ga4 = report?.ga4;
  const missing = report?.connectionsComplete === false ? "连接列表覆盖不完整" : "未连接";
  const detail = (source?: { source: string; days: number; complete: boolean }) =>
    source ? `${source.source} · ${source.days}/28 天${source.complete ? "" : " · 部分明细"}` : undefined;
  const scope = index.data?.observed
    ? `${index.data.observed} 个已观测 URL${index.data.complete ? "" : " · 部分记录"}`
    : "暂无索引观测";
  const waiting = reports.loading || index.loading;
  const gscEmpty = gsc ? "待同步" : missing;
  const gaEmpty = ga4 ? "待同步" : missing;
  function searchValue(value: Metric["value"], empty: string) {
    return value ?? (report ? empty : undefined);
  }
  return (
    <section className="grid gap-3" aria-label="SEO 表现">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="font-semibold">SEO 表现</h2>
          {report && (
            <p className="text-muted-foreground text-sm">
              {report.from} 至 {report.to} · 已同步明细
            </p>
          )}
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            reports.refresh();
            index.refresh();
          }}
          disabled={waiting}
        >
          <RefreshCw /> 刷新搜索与索引
        </Button>
      </div>
      {reports.error !== undefined && <LoadError error={reports.error} retry={reports.refresh} />}
      {index.error !== undefined && <LoadError error={index.error} retry={index.refresh} />}
      {waiting ? (
        <PageSkeleton />
      ) : (
        <MetricGrid
          label="站点搜索指标"
          metrics={[
            { label: "GSC 点击", value: searchValue(gsc?.clicks, gscEmpty), detail: detail(gsc) },
            { label: "GSC 展示", value: searchValue(gsc?.impressions, gscEmpty), detail: detail(gsc) },
            {
              label: "GSC 点击率",
              value: searchValue(
                gsc?.ctr === undefined ? undefined : percent.format(gsc.ctr),
                gsc?.days ? "暂无有效展示" : gscEmpty,
              ),
              detail: detail(gsc),
            },
            {
              label: "GSC 平均排名",
              value: searchValue(
                gsc?.position === undefined ? undefined : decimal.format(gsc.position),
                gsc?.days ? "暂无有效排名" : gscEmpty,
              ),
              detail: detail(gsc),
            },
            { label: "自然搜索流量", value: searchValue(ga4?.sessions, gaEmpty), detail: detail(ga4) },
            { label: "已索引 URL", value: index.data?.observed ? index.data.indexed : undefined, detail: scope },
            { label: "未索引 URL", value: index.data?.observed ? index.data.notIndexed : undefined, detail: scope },
            { label: "待核实索引", value: index.data?.observed ? index.data.pending : undefined, detail: scope },
          ]}
        />
      )}
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
  );
}

function ContentMetrics({ site }: { site: Site }) {
  const progress = useSeoQuery(site.id, loadContentProgress);
  const published = useSeoQuery(site.id, loadPublished);
  const { href } = useSeoLocation();
  const pending = progress.loading || published.loading;
  const coverage =
    progress.data?.complete === false ? `覆盖最近 ${progress.data.reviewedProjects} 个项目` : "本站内容项目";
  return (
    <section className="grid gap-3" aria-label="内容运营">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <h2 className="font-semibold">内容运营</h2>
        <Button
          size="sm"
          variant="outline"
          disabled={pending}
          onClick={() => {
            progress.refresh();
            published.refresh();
          }}
        >
          <RefreshCw /> 刷新内容指标
        </Button>
      </div>
      {progress.error !== undefined && <LoadError error={progress.error} retry={progress.refresh} />}
      {published.error !== undefined && <LoadError error={published.error} retry={published.refresh} />}
      {pending ? (
        <PageSkeleton />
      ) : (
        <MetricGrid
          label="站点运营指标"
          metrics={[
            {
              label: "内容项目",
              value: progress.data?.total,
              detail: progress.data?.totalComplete === false ? "仅已读取的项目" : "本站全部项目",
            },
            { label: "待生成文章", value: progress.data?.generation, detail: coverage },
            { label: "内容生产数量", value: progress.data?.count, detail: coverage },
            { label: "待审核文章", value: progress.data?.pending.length, detail: coverage },
            {
              label: "已发布文章",
              value: published.data?.count,
              detail: published.data?.complete === false ? "仅已读取的发布记录" : "已发布或核验的 CMS 内容，按目标去重",
            },
          ]}
        />
      )}
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
  );
}

function AIMetrics() {
  const ai = useSeoQuery(utcDay(), loadDailyAI);
  const { href } = useSeoLocation();
  const { model, data } = ai.data ?? {};
  function money(summary?: { amount?: number; unknown: number }) {
    return summary?.amount === undefined ? undefined : usd.format(summary.amount);
  }
  return (
    <section className="grid gap-3" aria-label="AI 运营">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="font-semibold">AI 运营</h2>
          <p className="text-muted-foreground text-sm">
            组织范围 · UTC 今日{ai.data?.complete === false ? " · 部分回执" : ""}
          </p>
        </div>
        <Button size="sm" variant="outline" onClick={ai.refresh} disabled={ai.loading}>
          <RefreshCw /> 刷新 AI 指标
        </Button>
      </div>
      {ai.error !== undefined && <LoadError error={ai.error} retry={ai.refresh} />}
      {ai.loading ? (
        <PageSkeleton />
      ) : (
        <MetricGrid
          label="AI 运营指标"
          metrics={[
            { label: "模型调用量", value: model?.count, detail: "已记录回执，排除测试和纯预留" },
            {
              label: "Token 使用量",
              value: model?.tokens,
              detail: model ? `${model.tokenCoverage}/${model.count} 条调用有完整 Token` : undefined,
            },
            {
              label: "模型费用",
              value: money(model),
              detail: model ? `含估算 · ${model.unknown} 笔待确认` : undefined,
            },
            { label: "DataForSEO 费用", value: money(data), detail: data ? `${data.unknown} 笔待确认` : undefined },
            {
              label: "模型每日预算使用率",
              value: ai.data?.modelBudget === undefined ? undefined : budgetPercent.format(ai.data.modelBudget),
              detail: "内容模型费用与预留 ÷ 已设美元预算",
            },
            {
              label: "数据每日预算使用率",
              value: ai.data?.dataBudget === undefined ? undefined : budgetPercent.format(ai.data.dataBudget),
              detail:
                ai.data?.budgetComplete === false
                  ? "仅已读取的 DataForSEO 预算"
                  : "DataForSEO 费用与预留 ÷ 已设美元预算",
            },
          ]}
        />
      )}
      <Button asChild variant="outline" size="sm" className="justify-self-start">
        <Link href={href(seoRoutes.system.providers.path)}>
          大模型服务商与请求诊断 <ArrowRight />
        </Link>
      </Button>
    </section>
  );
}

function Overview() {
  const scope = useSiteScope();
  const { principal } = useSeoSession();
  return (
    <>
      <ScopedSelection scope={scope} />
      <PortfolioMetrics scope={scope} />
      {scope.site ? (
        <>
          <SearchMetrics site={scope.site} />
          <ContentMetrics site={scope.site} />
        </>
      ) : (
        scope.rows.length > 0 &&
        !scope.error && (
          <p className="text-muted-foreground text-sm">选择单个站点可查看该站点的 SEO 表现与内容运营数据。</p>
        )
      )}
      {principal.role === "owner" && <AIMetrics />}
    </>
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
