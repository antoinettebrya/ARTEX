"use client";

import { useEffect, useState } from "react";

import { Ban, FileText, RefreshCw, Settings2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { TableCell, TableRow } from "@/components/ui/table";
import { type Job, type Site, seoRequest } from "@/lib/seo/client";
import {
  type ContentJobRecord,
  type CrawlPolicy,
  contentJobLabels,
  contentLabel,
  type SitePage,
} from "@/lib/seo/content";

import { useSeoQuery } from "../query";
import {
  ConfirmChange,
  formatTime,
  LoadError,
  PageSkeleton,
  RecordDetails,
  RecordsTable,
  StateBadge,
  useSeoRecords,
} from "../shared";
import { EvidenceLink, TextList } from "./article";
import { CrawlEditor } from "./forms";
import { ContentPanel, DataFact, StageButton, type StartStage } from "./views";

const taskTones: Record<Job["state"], "destructive" | "secondary" | "outline"> = {
  queued: "outline",
  running: "outline",
  succeeded: "secondary",
  failed: "destructive",
  cancelled: "outline",
};

function InventoryDetails({ siteID, pageID, onClose }: { siteID: string; pageID: string; onClose: () => void }) {
  const query = useSeoQuery<{ page: SitePage }>(`/content/inventory/${pageID}?site_id=${siteID}`);
  const page = query.data?.page;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{page?.title ?? "站点页面详情"}</DialogTitle>
          <DialogDescription>已保存的采集证据与页面快照。</DialogDescription>
        </DialogHeader>
        {query.loading && <PageSkeleton />}
        {query.error !== undefined && <LoadError error={query.error} retry={query.refresh} />}
        {page && (
          <div className="grid gap-3 text-sm">
            <dl className="grid gap-3 sm:grid-cols-2">
              <DataFact label="页面 URL">
                <EvidenceLink url={page.url} />
              </DataFact>
              <DataFact label="Canonical">
                <EvidenceLink url={page.canonical_url} />
              </DataFact>
              <DataFact label="HTTP 状态">{page.http_status}</DataFact>
              <DataFact label="采集状态">{contentLabel(page.state)}</DataFact>
              <DataFact label="已核实的索引状态">{contentLabel(page.index_status || "unknown")}</DataFact>
              <DataFact label="关键词映射">{page.mapped ? "已映射" : "暂无映射"}</DataFact>
              <DataFact label="语言">{page.language}</DataFact>
              <DataFact label="页面类型">{page.page_type || page.kind}</DataFact>
              <DataFact label="Robots">{page.robots}</DataFact>
              <DataFact label="更新时间（UTC）">
                {page.last_modified ? formatTime(page.last_modified) : undefined}
              </DataFact>
              <DataFact label="采集时间（UTC）">{page.checked_at ? formatTime(page.checked_at) : undefined}</DataFact>
              <DataFact label="发现时间（UTC）">
                {page.discovered_at ? formatTime(page.discovered_at) : undefined}
              </DataFact>
              <DataFact label="内容指纹">
                <code className="break-all font-mono text-xs">{page.fingerprint ?? "—"}</code>
              </DataFact>
              <DataFact label="快照 ID">
                <code className="break-all font-mono text-xs">{page.snapshot_id ?? "—"}</code>
              </DataFact>
            </dl>
            {page.description && <p className="break-words text-muted-foreground">{page.description}</p>}
            <TextList label="标题结构" values={page.headings?.map((heading) => `H${heading.level} ${heading.text}`)} />
            <TextList label="Schema" values={page.schema} />
            <TextList label="分类" values={page.categories} />
            <TextList label="标签" values={page.tags} />
            {!!page.images?.length && (
              <div className="space-y-1">
                <h3 className="font-medium">图片信息</h3>
                <ul className="list-disc space-y-1 ps-5">
                  {[...new Map(page.images.map((image) => [image.url, image])).values()].map((image) => (
                    <li key={image.url}>
                      <EvidenceLink url={image.url} text={image.alt} />
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {page.body && (
              <div className="space-y-1">
                <h3 className="font-medium">采集正文</h3>
                <p className="max-h-72 overflow-y-auto whitespace-pre-wrap break-words rounded-md border p-2">
                  {page.body}
                </p>
              </div>
            )}
          </div>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">关闭</Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function Inventory({
  site,
  owner,
  canWrite,
  onStage,
}: {
  site: Site;
  owner: boolean;
  canWrite: boolean;
  onStage: StartStage;
}) {
  const crawl = useSeoQuery<{ policy: CrawlPolicy | null }>(`/content/crawl?site_id=${site.id}`);
  const list = useSeoRecords<SitePage>(`/content/inventory?site_id=${site.id}`, "items");
  const [configuring, setConfiguring] = useState(false);
  const [pageID, selectPage] = useState<string>();
  const policy = crawl.data?.policy ?? null;
  return (
    <>
      <ContentPanel
        title="站点清单"
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                list.refresh();
                crawl.refresh();
              }}
            >
              <RefreshCw /> 刷新清单
            </Button>
            {owner && (
              <Button variant="outline" size="sm" onClick={() => setConfiguring(true)} disabled={crawl.loading}>
                <Settings2 /> 配置只读采集
              </Button>
            )}
            {canWrite && policy?.enabled && <StageButton kind="site_inventory" onStage={onStage} primary />}
          </>
        }
      >
        {crawl.error !== undefined && <LoadError error={crawl.error} retry={crawl.refresh} />}
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <StateBadge
            label={policy?.enabled ? "已授权只读访问" : "采集未启用"}
            tone={policy?.enabled ? "secondary" : "outline"}
          />
          {policy?.enabled && (
            <span className="text-muted-foreground">
              {policy.connector} · 每次最多 {policy.max_pages} 页
            </span>
          )}
          {policy?.sitemap_url && <EvidenceLink url={policy.sitemap_url} text="Sitemap" />}
        </div>
      </ContentPanel>
      <RecordsTable
        label="已发现页面"
        columns={["页面", "采集状态", "索引状态", "映射状态", "最近检查（UTC）", "操作"]}
        empty="暂无页面"
        list={list}
      >
        {list.records.map((page) => (
          <TableRow key={page.id}>
            <TableCell className="max-w-80 whitespace-normal px-3 py-2">
              <Button
                type="button"
                size="sm"
                variant="link"
                className="h-auto justify-start whitespace-normal break-all px-0 text-left"
                onClick={() => selectPage(page.id)}
              >
                {page.title || page.url}
              </Button>
              {page.title && <p className="break-all text-muted-foreground text-xs">{page.url}</p>}
            </TableCell>
            <TableCell className="px-3">
              <StateBadge label={contentLabel(page.state)} /> {page.http_status}
            </TableCell>
            <TableCell className="px-3">{contentLabel(page.index_status || "unknown")}</TableCell>
            <TableCell className="px-3">{page.mapped ? "已映射" : "暂无映射"}</TableCell>
            <TableCell className="px-3 tabular-nums">{page.checked_at ? formatTime(page.checked_at) : "—"}</TableCell>
            <TableCell className="px-3">
              {canWrite && policy?.enabled && (
                <Button variant="ghost" size="sm" onClick={() => onStage({ kind: "page_extraction", url: page.url })}>
                  <RefreshCw /> 重新采集
                </Button>
              )}
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {configuring && owner && (
        <CrawlEditor site={site} policy={policy} onClose={() => setConfiguring(false)} onSaved={crawl.refresh} />
      )}
      {pageID && <InventoryDetails siteID={site.id} pageID={pageID} onClose={() => selectPage(undefined)} />}
    </>
  );
}

export function TaskRecords({
  siteID,
  projectID,
  canWrite,
  inventory = false,
  onRefresh,
}: {
  siteID: string;
  projectID?: string;
  canWrite: boolean;
  inventory?: boolean;
  onRefresh?: () => void;
}) {
  const list = useSeoRecords<ContentJobRecord>(
    `/content/jobs?site_id=${siteID}${projectID ? `&project_id=${projectID}` : ""}`,
    "items",
  );
  const [details, setDetails] = useState<ContentJobRecord>();
  const [cancel, setCancel] = useState<ContentJobRecord>();
  const records = inventory ? list.records.filter(({ request }) => !request.project_id) : list.records;
  const pending = records.some(({ job }) => job.state === "running" || job.state === "queued");
  const { refresh } = list;
  useEffect(() => {
    if (!pending) return;
    const timer = window.setInterval(refresh, 2500);
    return () => window.clearInterval(timer);
  }, [pending, refresh]);

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-medium text-sm">任务执行记录</h2>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => {
            refresh();
            onRefresh?.();
          }}
        >
          <RefreshCw /> 刷新任务与结果
        </Button>
      </div>
      <RecordsTable
        label="内容任务执行记录"
        columns={["任务", "状态", "尝试次数", "调度时间（UTC）", "操作"]}
        empty="暂无执行记录"
        list={inventory ? { ...list, count: records.length } : list}
      >
        {records.map((record) => {
          const { job, request } = record;
          const active = job.state === "queued" || job.state === "running";
          return (
            <TableRow key={job.id}>
              <TableCell className="max-w-72 whitespace-normal px-3 py-2">
                <div className="font-medium">{contentJobLabels[request.kind] ?? request.kind}</div>
                {request.url && <p className="break-all text-muted-foreground text-xs">{request.url}</p>}
                <p className="break-all font-mono text-muted-foreground text-xs" title={job.id}>
                  {job.id.slice(0, 12)}
                </p>
              </TableCell>
              <TableCell className="px-3">
                <StateBadge label={contentLabel(job.state)} tone={taskTones[job.state]} />
                {job.error_code && <p className="mt-1 font-mono text-muted-foreground text-xs">{job.error_code}</p>}
              </TableCell>
              <TableCell className="px-3 tabular-nums">{job.attempts}</TableCell>
              <TableCell className="px-3 tabular-nums">
                {job.available_at ? formatTime(job.available_at) : "—"}
              </TableCell>
              <TableCell className="px-3">
                {job.result != null && (
                  <Button size="sm" variant="ghost" onClick={() => setDetails(record)}>
                    <FileText /> 查看结果
                  </Button>
                )}
                {canWrite && active && (
                  <Button size="sm" variant="ghost" onClick={() => setCancel(record)}>
                    <Ban /> 取消任务
                  </Button>
                )}
              </TableCell>
            </TableRow>
          );
        })}
      </RecordsTable>
      {details && (
        <RecordDetails
          title={contentJobLabels[details.request.kind]}
          value={details.job.result}
          onClose={() => setDetails(undefined)}
        />
      )}
      {cancel && (
        <ConfirmChange
          title="取消内容任务"
          description="取消后续执行。已发送的外部请求可能计入用量。"
          onClose={() => setCancel(undefined)}
          onConfirm={async () => {
            await seoRequest(`/jobs/${cancel.job.id}`, { method: "DELETE" });
            refresh();
            toast.success("内容任务已取消");
          }}
        />
      )}
    </div>
  );
}
