"use client";

import { type FormEvent, type ReactNode, useState } from "react";

import Link from "next/link";

import { ArrowRight, CloudDownload, FileText, RefreshCw, Search, Send, Upload } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { TableCell, TableRow } from "@/components/ui/table";
import type { Site } from "@/lib/seo/client";
import {
  type AnalyticsKind,
  type CMSMedia,
  type CMSPage,
  type Connection,
  connectionLabels,
  type ExecutionMetric,
  type ExecutionProperty,
  type ExtractionRecord,
  type IndexRecord,
  type IndexSubmission,
  indexingStates,
  isCMS,
  type OptimizationFeedback,
  type SitemapRecord,
} from "@/lib/seo/execution";
import { seoRoutes } from "@/lib/seo/routes";

import { Field, FilterBar, formatTime, RecordDetails, RecordsTable, StateBadge } from "../shared";
import { WebAddress } from "./publishing";
import { useExecutionRecords } from "./records";
import { SitemapDetails } from "./sitemap";
import { ConnectionSelect, type TaskSelection } from "./tasks";

export type PanelScope = {
  site: Site;
  connections: Connection[];
  writable: boolean;
  owner: boolean;
  onTask: (selection: TaskSelection) => void;
  revision: number;
};

function PanelTitle({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="font-semibold">{title}</h2>
      {children && <div className="flex flex-wrap gap-2">{children}</div>}
    </div>
  );
}

function Reload({ reload, disabled, children }: { reload: () => void; disabled?: boolean; children: ReactNode }) {
  return (
    <Button type="button" variant="outline" size="sm" onClick={reload} disabled={disabled}>
      <RefreshCw /> {children}
    </Button>
  );
}

function useSelectedConnection(connections: Connection[]) {
  const [selected, setSelected] = useState("");
  const connection = connections.find((item) => item.id === selected) ?? connections[0];
  return { connection, setSelected };
}

const searchTitles = { gsc: "Search Console 指标", ga4: "GA4 自然搜索指标", bing: "Bing 搜索指标" };
const metricFields: Record<AnalyticsKind, { key: string; label: string; format?: "percent" | "seconds" }[]> = {
  gsc: [
    { key: "clicks", label: "点击" },
    { key: "impressions", label: "展示" },
    { key: "ctr", label: "CTR", format: "percent" },
    { key: "position", label: "平均排名" },
  ],
  ga4: [
    { key: "sessions", label: "自然搜索会话" },
    { key: "active_users", label: "活跃用户" },
    { key: "engaged_sessions", label: "互动会话" },
    { key: "engagement_rate", label: "互动率", format: "percent" },
    { key: "user_engagement_seconds", label: "活跃互动时长", format: "seconds" },
    { key: "key_events", label: "关键事件" },
    { key: "session_key_event_rate", label: "会话关键事件率", format: "percent" },
  ],
  bing: [
    { key: "clicks", label: "点击" },
    { key: "impressions", label: "展示" },
    { key: "ctr", label: "CTR", format: "percent" },
    { key: "avg_click_position", label: "点击平均排名" },
    { key: "avg_impression_position", label: "展示平均排名" },
  ],
};

function metricValue(number: number | undefined, format: "percent" | "seconds" | undefined) {
  if (number === undefined || !Number.isFinite(number)) return "—";
  const formatted = new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 2,
    style: format === "percent" ? "percent" : "decimal",
  }).format(number);
  return format === "seconds" ? `${formatted} 秒` : formatted;
}

export function AnalyticsPanel({
  kind,
  site,
  connections,
  writable,
  onTask,
  revision,
}: PanelScope & { kind: AnalyticsKind }) {
  const available = connections.filter((item) => item.kind === kind);
  const { connection, setSelected } = useSelectedConnection(available);
  const [range, setRange] = useState<{ from?: string; to?: string; page?: string; country?: string }>({});
  const filters = { kind, connection_id: connection?.id };
  const metrics = useExecutionRecords<ExecutionMetric>("metrics", site.id, revision, { ...filters, ...range });
  const properties = useExecutionRecords<ExecutionProperty>("properties", site.id, revision, filters);
  const fields = metricFields[kind];
  const grain = {
    gsc: ["页面", "搜索词", "国家", "设备"],
    ga4: ["落地页", "国家", "设备", "语言"],
    bing: ["页面", "搜索词"],
  }[kind];
  const sync = { gsc: "gsc_sync", ga4: "ga4_sync", bing: "bing_sync" } as const;

  function updateFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const from = String(data.get("from") ?? "");
    const to = String(data.get("to") ?? "");
    const page = String(data.get("page") ?? "").trim();
    const country = String(data.get("country") ?? "").trim();
    setRange({ from: from || undefined, to: to || undefined, page: page || undefined, country: country || undefined });
  }

  return (
    <div className="grid gap-3">
      <PanelTitle title={connectionLabels[kind]}>
        <Reload
          reload={() => {
            metrics.refresh();
            properties.refresh();
          }}
          disabled={metrics.refreshing}
        >
          刷新数据
        </Reload>
        {writable && connection?.enabled && connection.configured && (
          <Button type="button" size="sm" onClick={() => onTask({ kind: sync[kind], connection, ...range })}>
            <CloudDownload /> 同步报告
          </Button>
        )}
      </PanelTitle>
      <Card className="py-3">
        <CardContent className="grid gap-3 px-3 lg:grid-cols-[minmax(12rem,1fr)_2fr]">
          <ConnectionSelect
            id="execution-analytics-connection"
            label="数据连接"
            connections={available}
            value={connection?.id ?? ""}
            onChange={setSelected}
          />
          <form onSubmit={updateFilters} className="flex flex-wrap items-end gap-2">
            <Field id="execution-metrics-from" label="开始日期">
              <Input id="execution-metrics-from" name="from" type="date" />
            </Field>
            <Field id="execution-metrics-to" label="结束日期">
              <Input id="execution-metrics-to" name="to" type="date" />
            </Field>
            <div className="min-w-0 flex-1 basis-48">
              <Field id="execution-metrics-page" label="页面 URL">
                <Input id="execution-metrics-page" name="page" type="url" maxLength={2048} />
              </Field>
            </div>
            {kind !== "bing" && (
              <div className={kind === "gsc" ? "w-24" : "w-40"}>
                <Field id="execution-metrics-country" label={kind === "gsc" ? "国家代码" : "国家（全名）"}>
                  <Input
                    id="execution-metrics-country"
                    name="country"
                    maxLength={kind === "gsc" ? 3 : 100}
                    placeholder={kind === "gsc" ? "USA" : "United States"}
                    pattern={kind === "gsc" ? "[A-Za-z]{3}" : undefined}
                  />
                </Field>
              </div>
            )}
            <Button type="submit" size="sm" variant="outline">
              筛选指标
            </Button>
          </form>
        </CardContent>
      </Card>
      {kind === "gsc" && (
        <p className="text-muted-foreground text-xs">平均排名采用 Search Console 报告中的展示位置平均值。</p>
      )}
      <RecordsTable
        label={searchTitles[kind]}
        columns={["日期", ...grain, ...fields.map(({ label }) => label)]}
        empty="暂无已同步指标"
        list={metrics}
      >
        {metrics.records.map((row) => (
          <TableRow
            key={`${row.connection_id}:${row.date}:${row.page}:${row.query}:${row.country}:${row.device}:${row.language}`}
          >
            <TableCell className="px-3 tabular-nums">{row.date}</TableCell>
            <TableCell className="max-w-48 whitespace-normal px-3">
              <WebAddress url={row.page} />
            </TableCell>
            {kind !== "ga4" && (
              <TableCell className="max-w-44 whitespace-normal break-words px-3">{row.query || "—"}</TableCell>
            )}
            {kind !== "bing" && (
              <>
                <TableCell className="px-3">{row.country || "—"}</TableCell>
                <TableCell className="px-3">{row.device || "—"}</TableCell>
              </>
            )}
            {kind === "ga4" && <TableCell className="px-3">{row.language || "—"}</TableCell>}
            {fields.map(({ key, format }) => (
              <TableCell key={key} className="px-3 tabular-nums">
                {metricValue(row.values?.[key], format)}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </RecordsTable>
      <RecordsTable label="数据资源" columns={["资源", "名称", "权限"]} empty="暂无已发现资源" list={properties}>
        {properties.records.map((item) => (
          <TableRow key={`${item.connection_id}:${item.id}`}>
            <TableCell className="max-w-72 whitespace-normal break-all px-3">{item.id}</TableCell>
            <TableCell className="px-3">{item.name || "—"}</TableCell>
            <TableCell className="px-3">{item.permission || "—"}</TableCell>
          </TableRow>
        ))}
      </RecordsTable>
    </div>
  );
}

const cmsState: Record<string, string> = {
  draft: "草稿",
  publish: "已发布",
  published: "已发布",
  future: "已排期",
  pending: "待审核",
  private: "私有",
};

export function CMSContentPanel({ site, connections, owner, writable, onTask, revision }: PanelScope) {
  const available = connections.filter((item) => isCMS(item.kind));
  const { connection, setSelected } = useSelectedConnection(available);
  const filters = { connection_id: connection?.id };
  const pages = useExecutionRecords<CMSPage>("pages", site.id, revision, filters);
  const media = useExecutionRecords<CMSMedia>("media", site.id, revision, filters);
  const [detail, setDetail] = useState<CMSPage>();
  return (
    <div className="grid gap-3">
      <PanelTitle title="CMS 内容及媒体">
        <Reload
          reload={() => {
            pages.refresh();
            media.refresh();
          }}
          disabled={pages.refreshing}
        >
          刷新清单
        </Reload>
        {writable && connection?.enabled && connection.configured && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => onTask({ kind: "cms_page_sync", connection })}
          >
            <CloudDownload /> 同步 CMS 内容
          </Button>
        )}
        {owner &&
          writable &&
          connection?.enabled &&
          connection.allow_writes &&
          connection.capabilities?.write_media && (
            <Button type="button" size="sm" onClick={() => onTask({ kind: "cms_media_upload", connection })}>
              <Upload /> 上传媒体
            </Button>
          )}
      </PanelTitle>
      <div className="max-w-lg">
        <ConnectionSelect
          id="execution-cms-filter"
          label="CMS 连接"
          connections={available}
          value={connection?.id ?? ""}
          onChange={setSelected}
        />
      </div>
      <RecordsTable
        label="CMS 页面与文章"
        columns={["名称", "类型", "状态", "目标地址", "更新时间（UTC）", "操作"]}
        empty="暂无同步内容"
        list={pages}
      >
        {pages.records.map((item) => (
          <TableRow key={`${item.connection_id}:${item.resource}:${item.id}`}>
            <TableCell className="max-w-56 whitespace-normal break-words px-3">
              <div className="font-medium">{item.title}</div>
              <code className="text-muted-foreground text-xs">{item.id}</code>
            </TableCell>
            <TableCell className="px-3">{item.resource === "post" ? "文章" : "页面"}</TableCell>
            <TableCell className="px-3">
              <StateBadge label={cmsState[item.status] ?? item.status} />
            </TableCell>
            <TableCell className="max-w-64 whitespace-normal px-3">
              <WebAddress url={item.url} />
            </TableCell>
            <TableCell className="px-3">{item.modified_at ? formatTime(item.modified_at) : "—"}</TableCell>
            <TableCell className="px-3">
              <Button type="button" variant="ghost" size="sm" onClick={() => setDetail(item)}>
                <FileText /> 查看内容
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      <RecordsTable label="CMS 媒体库" columns={["媒体", "类型", "地址"]} empty="暂无同步媒体" list={media}>
        {media.records.map((item) => (
          <TableRow key={`${item.connection_id}:${item.id}`}>
            <TableCell className="max-w-56 whitespace-normal break-words px-3">{item.title || item.id}</TableCell>
            <TableCell className="px-3">{item.media_type || "—"}</TableCell>
            <TableCell className="max-w-72 whitespace-normal px-3">
              <WebAddress url={item.url} />
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {detail && <RecordDetails title="CMS 内容详情" value={detail} onClose={() => setDetail(undefined)} />}
    </div>
  );
}

export function IndexPanel({ site, connections, writable, onTask, revision }: PanelScope) {
  const inspectors = connections.filter(
    (item) => item.enabled && item.configured && (item.kind === "gsc" || item.kind === "bing"),
  );
  const inspect = useSelectedConnection(inspectors);
  const list = useExecutionRecords<IndexRecord>("index", site.id, revision);
  return (
    <div className="grid gap-3">
      <PanelTitle title="索引记录">
        <Reload reload={list.refresh} disabled={list.refreshing}>
          刷新记录
        </Reload>
      </PanelTitle>
      {writable && (
        <FilterBar>
          <ConnectionSelect
            id="execution-inspection-connection"
            label="URL 检查连接"
            connections={inspectors}
            value={inspect.connection?.id ?? ""}
            onChange={inspect.setSelected}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!inspect.connection}
            onClick={() => onTask({ kind: "url_inspection", connection: inspect.connection })}
          >
            <Search /> 检查 URL
          </Button>
        </FilterBar>
      )}
      <RecordsTable
        label="索引状态"
        columns={["URL", "状态", "来源和证据", "观测时间（UTC）", "最近抓取（UTC）"]}
        empty="暂无索引记录"
        list={list}
      >
        {list.records.map((item) => (
          <TableRow key={`${item.provider}:${item.url}:${item.observed_at}`}>
            <TableCell className="max-w-64 whitespace-normal px-3">
              <WebAddress url={item.url} />
            </TableCell>
            <TableCell className="px-3">
              <StateBadge
                label={indexingStates[item.status] ?? item.status}
                tone={item.status === "INDEXED" ? "secondary" : "outline"}
              />
            </TableCell>
            <TableCell className="max-w-60 whitespace-normal break-words px-3">
              <div>{item.provider}</div>
              <p className="text-muted-foreground text-xs">{item.evidence}</p>
            </TableCell>
            <TableCell className="px-3">{item.observed_at ? formatTime(item.observed_at) : "—"}</TableCell>
            <TableCell className="px-3">{item.last_crawled ? formatTime(item.last_crawled) : "—"}</TableCell>
          </TableRow>
        ))}
      </RecordsTable>
    </div>
  );
}

export function IndexSubmissionPanel({ site, connections, owner, writable, onTask, revision }: PanelScope) {
  const submitters = connections.filter(
    (item) =>
      item.enabled && item.configured && item.allow_writes && (item.kind === "indexnow" || item.kind === "bing"),
  );
  const submit = useSelectedConnection(submitters);
  const submissions = useExecutionRecords<IndexSubmission>("submissions", site.id, revision);
  return (
    <div className="grid gap-3">
      <PanelTitle title="提交记录">
        <Reload reload={submissions.refresh} disabled={submissions.refreshing}>
          刷新记录
        </Reload>
      </PanelTitle>
      {owner && writable && (
        <FilterBar>
          <ConnectionSelect
            id="execution-indexnow-connection"
            label="提交连接"
            connections={submitters}
            value={submit.connection?.id ?? ""}
            onChange={submit.setSelected}
          />
          <Button
            type="button"
            size="sm"
            disabled={!submit.connection}
            onClick={() => onTask({ kind: "indexnow_submit", connection: submit.connection })}
          >
            <Send /> 提交索引请求
          </Button>
        </FilterBar>
      )}
      <RecordsTable
        label="索引提交记录"
        columns={["URL", "平台", "提交结果", "HTTP 状态", "提交时间（UTC）"]}
        empty="暂无索引提交"
        list={submissions}
      >
        {submissions.records.map((item) => (
          <TableRow key={`${item.job_id}/${item.provider}/${item.url}`}>
            <TableCell className="max-w-64 whitespace-normal px-3">
              <WebAddress url={item.url} />
            </TableCell>
            <TableCell className="px-3">
              {item.provider === "indexnow" || item.provider === "bing"
                ? connectionLabels[item.provider]
                : item.provider}
            </TableCell>
            <TableCell className="max-w-52 whitespace-normal break-words px-3">
              <StateBadge
                label={
                  { accepted: "已接收", pending_key_validation: "等待密钥验证", failed: "提交失败" }[item.status] ??
                  item.status
                }
                tone={item.status === "failed" ? "outline" : "secondary"}
              />
              {item.error && <p className="mt-1 text-muted-foreground text-xs">{item.error}</p>}
            </TableCell>
            <TableCell className="px-3 tabular-nums">{item.http_status || "—"}</TableCell>
            <TableCell className="px-3">{formatTime(item.created_at)}</TableCell>
          </TableRow>
        ))}
      </RecordsTable>
    </div>
  );
}

export function SitemapPanel({ site, writable, onTask, revision }: PanelScope) {
  const list = useExecutionRecords<SitemapRecord>("sitemaps", site.id, revision);
  const [detail, setDetail] = useState<string>();
  return (
    <div className="grid gap-3">
      <PanelTitle title="Sitemap 管理">
        <Reload reload={list.refresh} disabled={list.refreshing}>
          刷新状态
        </Reload>
        {writable && (
          <Button type="button" size="sm" onClick={() => onTask({ kind: "sitemap_refresh" })}>
            <CloudDownload /> 刷新 Sitemap
          </Button>
        )}
      </PanelTitle>
      <RecordsTable
        label="Sitemap 记录"
        columns={["Sitemap 地址", "来源", "状态", "URL 数", "分卷", "最近读取（UTC）", "警告", "错误", "操作"]}
        empty="暂无 Sitemap 记录"
        list={list}
      >
        {list.records.map((item) => (
          <TableRow key={`${item.provider}:${item.url}`}>
            <TableCell className="max-w-72 whitespace-normal px-3">
              <WebAddress url={item.url} />
            </TableCell>
            <TableCell className="px-3">{item.provider}</TableCell>
            <TableCell className="px-3">{item.status || "—"}</TableCell>
            <TableCell className="px-3 tabular-nums">{item.entries ?? "—"}</TableCell>
            <TableCell className="px-3 tabular-nums">{item.parts ?? "—"}</TableCell>
            <TableCell className="px-3">{item.last_read ? formatTime(item.last_read) : "—"}</TableCell>
            <TableCell className="px-3 tabular-nums">{item.warnings ?? "—"}</TableCell>
            <TableCell className="px-3 tabular-nums">{item.errors ?? "—"}</TableCell>
            <TableCell className="px-3">
              {item.job_id && (
                <Button type="button" variant="ghost" size="sm" onClick={() => setDetail(item.job_id)}>
                  查看 Sitemap
                </Button>
              )}
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {detail && <SitemapDetails siteID={site.id} jobID={detail} onClose={() => setDetail(undefined)} />}
    </div>
  );
}

export function ExtractionPanel({ site, writable, onTask, revision }: PanelScope) {
  const list = useExecutionRecords<ExtractionRecord>("extractions", site.id, revision);
  const [detail, setDetail] = useState<ExtractionRecord>();
  return (
    <div className="grid gap-3">
      <PanelTitle title="提取监控">
        <Reload reload={list.refresh} disabled={list.refreshing}>
          刷新结果
        </Reload>
        {writable && (
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => onTask({ kind: "pdf_extraction" })}>
              <FileText /> 提取 PDF
            </Button>
            <Button type="button" size="sm" onClick={() => onTask({ kind: "browser_extraction" })}>
              <CloudDownload /> 渲染网页
            </Button>
          </>
        )}
      </PanelTitle>
      <RecordsTable
        label="提取结果"
        columns={["文档", "提取方式", "页数", "请求", "操作"]}
        empty="暂无已保存的提取结果"
        list={list}
      >
        {list.records.map((item) => (
          <TableRow key={item.job_id ?? `${item.kind}:${item.url}`}>
            <TableCell className="max-w-72 whitespace-normal break-words px-3">
              {item.title && <div className="font-medium">{item.title}</div>}
              <WebAddress url={item.url} />
            </TableCell>
            <TableCell className="px-3">
              <div>{item.kind.includes("pdf") ? "PDF" : "浏览器渲染"}</div>
              {item.truncated && <StateBadge label="已截断" />}
            </TableCell>
            <TableCell className="px-3">{item.pages ?? "—"}</TableCell>
            <TableCell className="px-3">{item.requests ?? "—"}</TableCell>
            <TableCell className="px-3">
              <Button type="button" variant="ghost" size="sm" onClick={() => setDetail(item)}>
                查看详情
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {detail && (
        <RecordDetails title={detail.title ?? "提取结果"} value={detail} onClose={() => setDetail(undefined)} />
      )}
    </div>
  );
}

function signalDelta(value: number | undefined, unit: string, maximumFractionDigits = 2) {
  if (value === undefined || value === null || !Number.isFinite(value)) return "—";
  const formatted = new Intl.NumberFormat("zh-CN", { signDisplay: "exceptZero", maximumFractionDigits }).format(value);
  return `${formatted}${unit}`;
}

export function FeedbackPanel({ site, revision }: { site: Site; revision: number }) {
  const list = useExecutionRecords<OptimizationFeedback>("feedback", site.id, revision);
  return (
    <div className="grid gap-3">
      <PanelTitle title="优化观测记录">
        <Reload reload={list.refresh} disabled={list.refreshing}>
          刷新反馈
        </Reload>
      </PanelTitle>
      <RecordsTable
        label="内容优化反馈"
        columns={[
          "内容项目",
          "当前页面",
          "自然搜索流量变化",
          "平均排名变化",
          "CTR 变化",
          "更新（UTC）",
          "数据来源",
          "操作",
        ]}
        empty="暂无满足对比条件的优化反馈"
        list={list}
      >
        {list.records.map((item) => (
          <TableRow key={item.project_id}>
            <TableCell className="max-w-52 whitespace-normal break-words px-3">{item.keyword}</TableCell>
            <TableCell className="max-w-60 whitespace-normal px-3">
              <WebAddress url={item.target_url} />
            </TableCell>
            <TableCell className="px-3 tabular-nums">{signalDelta(item.signals?.traffic_change, "%")}</TableCell>
            <TableCell className="px-3 tabular-nums">{signalDelta(item.signals?.rank_change, " 位")}</TableCell>
            <TableCell className="px-3 tabular-nums">{signalDelta(item.signals?.ctr_change, " 个百分点")}</TableCell>
            <TableCell className="px-3">{formatTime(item.updated_at)}</TableCell>
            <TableCell className="max-w-40 whitespace-normal px-3">
              <WebAddress url={item.signals?.evidence_url} />
            </TableCell>
            <TableCell className="px-3">
              <Button asChild size="sm" variant="ghost">
                <Link href={`${seoRoutes.content.refresh.path}?site=${site.id}&project=${item.project_id}`}>
                  查看更新建议 <ArrowRight />
                </Link>
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
    </div>
  );
}

type Usage = {
  connection_id?: string;
  name: string;
  date_utc: string;
  requests: number;
  requests_per_day: number;
  cost_micros: number;
};
const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 6 });

export function UsagePanel({ site, revision }: { site: Site; revision: number }) {
  const list = useExecutionRecords<Usage>("usage", site.id, revision);
  return (
    <div className="grid gap-3">
      <PanelTitle title="连接与采集额度">
        <Reload reload={list.refresh} disabled={list.refreshing}>
          刷新用量
        </Reload>
      </PanelTitle>
      <RecordsTable
        label="站点执行用量"
        columns={["连接", "日期（UTC）", "请求量", "每日上限", "已记录费用（USD）"]}
        empty="暂无用量记录"
        list={list}
      >
        {list.records.map((item) => (
          <TableRow key={`${item.connection_id ?? item.name}:${item.date_utc}`}>
            <TableCell className="px-3">{item.name}</TableCell>
            <TableCell className="px-3 tabular-nums">{item.date_utc}</TableCell>
            <TableCell className="px-3 tabular-nums">{item.requests}</TableCell>
            <TableCell className="px-3 tabular-nums">{item.requests_per_day}</TableCell>
            <TableCell className="px-3 tabular-nums">{usd.format(item.cost_micros / 1000000)}</TableCell>
          </TableRow>
        ))}
      </RecordsTable>
    </div>
  );
}
