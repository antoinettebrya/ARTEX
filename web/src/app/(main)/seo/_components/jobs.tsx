"use client";

import { useState } from "react";

import Link from "next/link";

import { Ban, FileText, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";
import { isID, type Job, type Provider, type Site, seoRequest } from "@/lib/seo/client";
import { contentJobLabels } from "@/lib/seo/content";
import { executionJobLabels } from "@/lib/seo/execution";
import { researchJobLabels } from "@/lib/seo/research";
import { seoRoutes } from "@/lib/seo/routes";

import { siteScopeParams, useSeoLocation, useSiteScope } from "./context";
import { useSeoQuery } from "./query";
import { useSeoSession } from "./session";
import {
  ConfirmChange,
  Field,
  FilterBar,
  FormDialog,
  field,
  formatTime,
  LoadError,
  PageFrame,
  PageSkeleton,
  RecordDetails,
  RecordsTable,
  StateBadge,
  useSeoRecords,
} from "./shared";

export const jobNames = {
  ...contentJobLabels,
  ...executionJobLabels,
  ...researchJobLabels,
  site_inventory: "站点资料快照",
  site_brief: "SEO 启动简报",
} as const;
export const jobStates = {
  queued: "排队中",
  running: "运行中",
  succeeded: "完成",
  failed: "失败",
  cancelled: "已取消",
} as const;

export const jobTones = {
  queued: "outline",
  running: "outline",
  succeeded: "secondary",
  failed: "destructive",
  cancelled: "outline",
} as const;

function JobEditor({
  site,
  providers,
  onClose,
  refresh,
}: {
  site: Site;
  providers: Provider[];
  onClose: () => void;
  refresh: () => void;
}) {
  const [kind, setKind] = useState<"site_inventory" | "site_brief">("site_inventory");
  const [key] = useState(() => crypto.randomUUID());
  return (
    <FormDialog
      title="创建任务"
      description={`选择 ${site.name} 的任务类型。`}
      submitLabel="提交任务"
      onClose={onClose}
      onSubmit={async (data) => {
        await seoRequest("/jobs", {
          method: "POST",
          body: {
            site_id: site.id,
            kind,
            provider_id: kind === "site_brief" ? field(data, "provider_id") : undefined,
            idempotency_key: key,
          },
        });
        refresh();
        toast.success("任务已提交");
      }}
    >
      <Field id="job-kind" label="任务类型">
        <NativeSelect
          id="job-kind"
          name="kind"
          className="w-full"
          value={kind}
          onChange={(event) => setKind(event.target.value === "site_brief" ? "site_brief" : "site_inventory")}
        >
          <NativeSelectOption value="site_inventory">{jobNames.site_inventory}</NativeSelectOption>
          {providers.length > 0 && <NativeSelectOption value="site_brief">{jobNames.site_brief}</NativeSelectOption>}
        </NativeSelect>
      </Field>
      {kind === "site_brief" && (
        <Field id="job-provider" label="Provider">
          <NativeSelect id="job-provider" name="provider_id" className="w-full" required defaultValue="">
            <NativeSelectOption value="" disabled>
              选择 Provider
            </NativeSelectOption>
            {providers.map((provider) => (
              <NativeSelectOption key={provider.id} value={provider.id}>
                {provider.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
      )}
    </FormDialog>
  );
}

export function Jobs() {
  const { principal } = useSeoSession();
  const writable = principal.role !== "viewer";
  const { params, siteID: site, href, update } = useSeoLocation();
  const scope = useSiteScope();
  const editing = writable && !!scope.site && params.get("new") === "1";
  const selectedJob = params.get("job");
  const details = useSeoQuery<{ job: Job }>(isID(selectedJob) ? `/jobs/${selectedJob}` : null);
  const providers = useSeoQuery<{ providers: Provider[] }>(editing ? "/providers?limit=200" : null);
  const filters = siteScopeParams(scope);
  const state = params.get("job_state") ?? "";
  const kind = params.get("job_kind") ?? "";
  if (state) filters?.set("state", state);
  if (kind) filters?.set("kind", kind);
  const list = useSeoRecords<Job>(filters ? `/jobs?${filters}` : null, "jobs");
  const siteIDs = new Set(scope.rows.map(({ id }) => id));
  const [result, setResult] = useState<Job>();
  const [cancel, setCancel] = useState<Job>();

  const close = () => update({ new: null }, { replace: true });

  return (
    <PageFrame
      title={seoRoutes.system.jobs.title}
      description="查看排队、执行结果和重试状态。"
      actions={
        <>
          <Button variant="outline" size="sm" onClick={list.refresh} disabled={list.refreshing}>
            <RefreshCw /> 刷新
          </Button>
          {writable && (
            <Button asChild variant="outline" size="sm">
              <Link href={href(seoRoutes.sites.list.path)}>
                <Plus /> 从站点创建
              </Link>
            </Button>
          )}
        </>
      }
    >
      {editing && providers.loading && <PageSkeleton />}
      <FilterBar>
        <Field id="jobs-state" label="任务状态">
          <NativeSelect
            id="jobs-state"
            value={state}
            onChange={(event) => update({ job_state: event.target.value || null })}
          >
            <NativeSelectOption value="">全部状态</NativeSelectOption>
            {Object.entries(jobStates).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field id="jobs-kind" label="任务类型筛选">
          <NativeSelect
            id="jobs-kind"
            value={kind}
            onChange={(event) => update({ job_kind: event.target.value || null })}
          >
            <NativeSelectOption value="">全部类型</NativeSelectOption>
            {Object.entries(jobNames).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
      </FilterBar>
      {editing && providers.error !== undefined && <LoadError error={providers.error} retry={providers.refresh} />}
      {details.loading && <PageSkeleton />}
      {details.error !== undefined && <LoadError error={details.error} retry={details.refresh} />}
      {details.data && (!scope.restricted || siteIDs.has(details.data.job.site_id)) && (
        <RecordDetails
          title={jobNames[details.data.job.kind]}
          value={{
            state: details.data.job.state,
            attempts: details.data.job.attempts,
            error: details.data.job.error_code,
            result: details.data.job.result,
            gsc_progress: details.data.job.gsc_progress,
          }}
          onClose={() => update({ job: null }, { replace: true })}
        />
      )}
      {details.data && scope.restricted && !siteIDs.has(details.data.job.site_id) && (
        <p className="text-muted-foreground text-sm">该任务不属于当前选择的站点。</p>
      )}
      <RecordsTable
        label="任务队列"
        columns={["任务", "状态", "尝试次数", "调度时间（UTC）", "操作"]}
        empty="暂无任务"
        list={list}
      >
        {list.records.map((job) => (
          <TableRow key={job.id}>
            <TableCell className="px-3 py-2">
              <div className="font-medium">{jobNames[job.kind]}</div>
              <code className="font-mono text-muted-foreground text-xs" title={job.id}>
                {job.id.slice(0, 12)}
              </code>
            </TableCell>
            <TableCell className="px-3">
              <StateBadge label={jobStates[job.state]} tone={jobTones[job.state]} />
              {job.error_code && <div className="mt-0.5 font-mono text-muted-foreground text-xs">{job.error_code}</div>}
            </TableCell>
            <TableCell className="px-3 tabular-nums">{job.attempts}</TableCell>
            <TableCell className="px-3 tabular-nums">{formatTime(job.available_at)}</TableCell>
            <TableCell className="px-3">
              {job.result !== undefined && (
                <Button variant="ghost" size="sm" onClick={() => setResult(job)}>
                  <FileText /> 查看结果
                </Button>
              )}
              {writable && (job.state === "queued" || job.state === "running") && (
                <Button variant="ghost" size="sm" onClick={() => setCancel(job)}>
                  <Ban /> 取消任务
                </Button>
              )}
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {editing && scope.site?.status === "active" && providers.data && (
        <JobEditor
          key={site}
          site={scope.site}
          providers={providers.data.providers.filter((provider) => provider.enabled && provider.kind !== "dataforseo")}
          refresh={list.refresh}
          onClose={close}
        />
      )}
      {result && (
        <RecordDetails title={jobNames[result.kind]} value={result.result} onClose={() => setResult(undefined)} />
      )}
      {cancel && (
        <ConfirmChange
          title="取消任务"
          description="这会停止后续重试。已发送给 Provider 的请求可能已计费。"
          onClose={() => setCancel(undefined)}
          onConfirm={async () => {
            await seoRequest(`/jobs/${cancel.id}`, { method: "DELETE" });
            list.refresh();
            toast.success("任务已取消");
          }}
        />
      )}
    </PageFrame>
  );
}
