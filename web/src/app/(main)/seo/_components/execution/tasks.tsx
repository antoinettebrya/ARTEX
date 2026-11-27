"use client";

import { useEffect, useState } from "react";

import { Ban, FileText, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { isID, type Job, SeoAPIError, type Site, seoRequest } from "@/lib/seo/client";
import {
  type Connection,
  type ExecutionJobInput,
  type ExecutionJobKind,
  type ExecutionJobRecord,
  type ExecutionPublication,
  executionJobLabels,
  type PublicationOperation,
  searchDataStates,
  searchTypes,
} from "@/lib/seo/execution";

import { useSeoQuery } from "../query";
import {
  ConfirmChange,
  Field,
  FormDialog,
  field,
  formatTime,
  LoadError,
  RecordDetails,
  RecordsTable,
  StateBadge,
} from "../shared";
import { useExecutionRecords } from "./records";

const jobStates = { queued: "排队中", running: "运行中", succeeded: "完成", failed: "失败", cancelled: "已取消" };
const operations = { draft: "保存草稿", publish: "批准正式发布", rollback: "恢复发布前版本" } as const;
const analyticsJobs = new Set<ExecutionJobKind>(["gsc_sync", "ga4_sync", "bing_sync"]);
const requiredURLs = new Set<ExecutionJobKind>([
  "url_inspection",
  "indexnow_submit",
  "pdf_extraction",
  "browser_extraction",
]);
const maxUploadSize = 4 * 1024 * 1024;

export type TaskSelection = {
  kind: ExecutionJobKind;
  connection?: Connection;
  publication?: ExecutionPublication;
  operation?: PublicationOperation;
  url?: string;
  from?: string;
  to?: string;
};

function selectionLabel(selection: TaskSelection) {
  return selection.operation ? operations[selection.operation] : executionJobLabels[selection.kind];
}

async function mediaFields(data: FormData) {
  const file = data.get("media");
  if (!(file instanceof File) || !file.size || file.size > maxUploadSize) {
    throw new SeoAPIError(400, "execution_media_too_large");
  }
  const dataURL = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new SeoAPIError(400, "execution_media_read"));
    reader.readAsDataURL(file);
  });
  return { filename: file.name, media_type: file.type, media_base64: dataURL.slice(dataURL.indexOf(",") + 1) };
}

export function ExecutionTaskEditor({
  selection,
  site,
  onClose,
  onSaved,
}: {
  selection: TaskSelection;
  site: Site;
  onClose: () => void;
  onSaved: (job: Job) => void;
}) {
  const { kind, connection, publication, operation } = selection;
  const [key] = useState(() => crypto.randomUUID());
  const requiresNote = operation === "publish" || operation === "rollback" || kind === "indexnow_submit";
  const title = selectionLabel(selection);
  const instructions = {
    draft: `${site.name} · ${connection?.name ?? "CMS"}`,
    publish: "确认要将此版本正式发布到已授权的 CMS。填写排期可安排未来发布时间。",
    rollback: "从本次发布的快照恢复 CMS 内容和状态。",
  };
  const submitLabels = { draft: "提交任务", publish: "确认发布", rollback: "确认恢复" };
  const description = operation ? instructions[operation] : `${site.name}${connection ? ` · ${connection.name}` : ""}`;
  return (
    <FormDialog
      title={title}
      description={description}
      submitLabel={operation ? submitLabels[operation] : "提交任务"}
      onClose={onClose}
      onSubmit={async (data) => {
        const local = field(data, "scheduled_at");
        const url = field(data, "url").trim();
        const body: ExecutionJobInput = {
          site_id: site.id,
          kind,
          connection_id: connection?.id ?? publication?.connection_id,
          publication_id: publication?.id,
          operation,
          url: url || undefined,
          search_type: field(data, "search_type") || undefined,
          data_state: field(data, "data_state") || undefined,
          from: field(data, "from") || undefined,
          to: field(data, "to") || undefined,
          note: requiresNote ? field(data, "note").trim() : undefined,
          scheduled_at: local ? new Date(local).toISOString() : undefined,
          priority: Number(field(data, "priority")),
          idempotency_key: key,
          ...(kind === "cms_media_upload" ? await mediaFields(data) : {}),
        };
        const { job } = await seoRequest<{ job: Job }>("/execution/jobs", { method: "POST", body });
        onSaved(job);
        toast.success("执行任务已提交");
      }}
    >
      {publication && (
        <div className="sm:col-span-2">
          <p className="font-medium">{publication.title}</p>
          <p className="text-muted-foreground text-xs">已批准内容 · 第 {publication.article_version} 版</p>
        </div>
      )}
      {requiresNote && (
        <div className="sm:col-span-2">
          <Field id="execution-task-note" label={kind === "indexnow_submit" ? "提交说明" : "授权说明"}>
            <Textarea id="execution-task-note" name="note" rows={3} maxLength={500} required />
          </Field>
        </div>
      )}
      {operation === "publish" && connection?.capabilities.schedule && !publication?.remote_id && (
        <div className="sm:col-span-2">
          <Field id="execution-task-schedule" label="发布时间（本地时间，可选）">
            <Input type="datetime-local" id="execution-task-schedule" name="scheduled_at" />
          </Field>
        </div>
      )}
      {(requiredURLs.has(kind) || kind === "sitemap_refresh") && (
        <div className="sm:col-span-2">
          <Field id="execution-task-url" label={kind === "sitemap_refresh" ? "Sitemap 地址（可选）" : "目标 URL"}>
            <Input
              id="execution-task-url"
              type="url"
              name="url"
              defaultValue={selection.url}
              placeholder={
                kind === "sitemap_refresh" ? `${site.base_url.replace(/\/$/u, "")}/sitemap.xml` : site.base_url
              }
              maxLength={4096}
              required={requiredURLs.has(kind)}
            />
          </Field>
        </div>
      )}
      {analyticsJobs.has(kind) && (
        <>
          <Field id="execution-task-from" label="开始日期">
            <Input id="execution-task-from" type="date" name="from" defaultValue={selection.from} />
          </Field>
          <Field id="execution-task-to" label="结束日期">
            <Input id="execution-task-to" type="date" name="to" defaultValue={selection.to} />
          </Field>
        </>
      )}
      {kind === "gsc_sync" && (
        <>
          <Field label="搜索类型" id="gsc-sync-type">
            <NativeSelect id="gsc-sync-type" name="search_type" defaultValue="web">
              {Object.entries(searchTypes).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
          <Field label="数据范围" id="gsc-sync-state">
            <NativeSelect id="gsc-sync-state" name="data_state" defaultValue="final">
              {Object.entries(searchDataStates).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
        </>
      )}
      {kind === "cms_media_upload" && (
        <div className="sm:col-span-2">
          <Field id="execution-task-media" label="媒体文件（最大 4 MiB）">
            <Input
              id="execution-task-media"
              type="file"
              name="media"
              accept="image/png,image/jpeg,image/gif,image/webp"
              required
            />
          </Field>
        </div>
      )}
      <Field id="execution-task-priority" label="优先级（0–100）">
        <Input id="execution-task-priority" name="priority" type="number" defaultValue={0} min={0} max={100} required />
      </Field>
    </FormDialog>
  );
}

function state(job: Job) {
  const tones = {
    queued: "outline",
    running: "outline",
    succeeded: "secondary",
    failed: "destructive",
    cancelled: "outline",
  } as const;
  const tone = tones[job.state];
  return <StateBadge label={jobStates[job.state]} tone={tone} />;
}

function followupVerification(job: Job) {
  const result = job.result;
  if (
    job.kind === "cms_publication" &&
    job.state === "succeeded" &&
    result &&
    typeof result === "object" &&
    "verification_job_id" in result
  ) {
    const id = result.verification_job_id;
    if (typeof id === "string" && isID(id)) return id;
  }
}

export function LatestExecutionJob({ initial, onSettled }: { initial: Job; onSettled: () => void }) {
  const [job, setJob] = useState(initial);
  const [failure, setFailure] = useState<unknown>();
  const [details, setDetails] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const followup = followupVerification(initial);
    if (!followup && initial.state !== "queued" && initial.state !== "running") return;
    let jobID = followup ?? initial.id;
    let timer: ReturnType<typeof setTimeout>;
    const controller = new AbortController();
    const until = Date.now() + 60000;
    const load = async () => {
      try {
        const response = await seoRequest<{ job: Job }>(`/jobs/${jobID}`, { signal: controller.signal });
        if (controller.signal.aborted) return;
        setJob(response.job);
        setFailure(undefined);
        const pending = response.job.state === "queued" || response.job.state === "running";
        if (!pending) {
          onSettled();
          const verificationID = followupVerification(response.job);
          if (verificationID && verificationID !== jobID) {
            jobID = verificationID;
            timer = setTimeout(load, 900);
          }
          return;
        }
        const due = Date.parse(response.job.available_at);
        if (response.job.state === "queued" && due >= until) return;
        const delay = Math.max(1500, Math.min(10000, due > Date.now() ? due - Date.now() : 2000));
        if (Date.now() + delay < until) timer = setTimeout(load, delay);
      } catch (error) {
        if (!controller.signal.aborted) setFailure(error);
      }
    };
    timer = setTimeout(load, retry > 0 ? 0 : 900);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [initial, onSettled, retry]);

  return (
    <>
      {failure !== undefined && <LoadError error={failure} retry={() => setRetry((count) => count + 1)} />}
      <Card className="py-3" aria-label="最近的执行任务">
        <CardContent className="flex flex-wrap items-center justify-between gap-2 px-3">
          <div className="flex flex-wrap items-center gap-2" aria-live="polite">
            <span className="text-muted-foreground text-xs">最近任务</span>
            <span className="font-medium text-sm">{executionJobLabels[job.kind as ExecutionJobKind]}</span>
            {state(job)}
            {job.error_code && <code className="break-all text-muted-foreground text-xs">{job.error_code}</code>}
          </div>
          <div className="flex gap-1">
            {(job.state === "queued" || job.state === "running") && (
              <Button type="button" variant="ghost" size="sm" onClick={() => setRetry((count) => count + 1)}>
                <RefreshCw /> 刷新状态
              </Button>
            )}
            <Button type="button" variant="ghost" size="sm" onClick={() => setDetails(true)}>
              <FileText /> 任务详情
            </Button>
          </div>
        </CardContent>
      </Card>
      {details && <RecordDetails title="执行任务详情" value={job} onClose={() => setDetails(false)} />}
    </>
  );
}

export function ExecutionJobHistory({
  site,
  owner,
  writable,
  revision,
}: {
  site: Site;
  owner: boolean;
  writable: boolean;
  revision: number;
}) {
  const list = useExecutionRecords<ExecutionJobRecord>("jobs", site.id, revision);
  const [open, setOpen] = useState<string>();
  const [cancel, setCancel] = useState<Job>();
  const detail = useSeoQuery<{ job: Job }>(open ? `/jobs/${open}` : null);
  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-semibold">执行任务</h2>
        <Button type="button" variant="outline" size="sm" onClick={list.refresh} disabled={list.refreshing}>
          <RefreshCw /> 刷新任务
        </Button>
      </div>
      <RecordsTable
        label="站点执行任务"
        columns={["任务", "状态", "尝试次数", "调度时间（UTC）", "操作"]}
        empty="暂无执行任务"
        list={list}
      >
        {list.records.map(({ job, request }) => (
          <TableRow key={job.id}>
            <TableCell className="px-3">
              <div className="font-medium">{selectionLabel({ kind: request.kind, operation: request.operation })}</div>
              <code className="text-muted-foreground text-xs" title={job.id}>
                {job.id.slice(0, 12)}
              </code>
            </TableCell>
            <TableCell className="px-3">
              {state(job)}
              {job.error_code && (
                <div className="max-w-64 break-all font-mono text-muted-foreground text-xs">{job.error_code}</div>
              )}
            </TableCell>
            <TableCell className="px-3 tabular-nums">{job.attempts}</TableCell>
            <TableCell className="px-3 tabular-nums">{formatTime(job.available_at)}</TableCell>
            <TableCell className="px-3">
              <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(job.id)}>
                <FileText /> 查看详情
              </Button>
              {writable &&
                (owner || !["cms_publication", "cms_media_upload", "indexnow_submit"].includes(job.kind)) &&
                (job.state === "queued" || job.state === "running") && (
                  <Button type="button" variant="ghost" size="sm" onClick={() => setCancel(job)}>
                    <Ban /> 取消任务
                  </Button>
                )}
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {detail.error !== undefined && <LoadError error={detail.error} retry={detail.refresh} />}
      {open && detail.data && (
        <RecordDetails title="执行任务详情" value={detail.data.job} onClose={() => setOpen(undefined)} />
      )}
      {cancel && (
        <ConfirmChange
          title="取消执行任务"
          description="取消会停止后续重试。已经发送的 CMS 请求仍可能完成。"
          onClose={() => setCancel(undefined)}
          onConfirm={async () => {
            await seoRequest(`/jobs/${cancel.id}`, { method: "DELETE" });
            list.refresh();
            toast.success("执行任务已取消");
          }}
        />
      )}
    </div>
  );
}

export function ConnectionSelect({
  id,
  label,
  connections,
  value,
  onChange,
}: {
  id: string;
  label: string;
  connections: Connection[];
  value: string;
  onChange: (id: string) => void;
}) {
  return (
    <Field id={id} label={label}>
      <NativeSelect id={id} className="w-full" value={value} onChange={(event) => onChange(event.target.value)}>
        <NativeSelectOption value="" disabled>
          {connections.length ? "选择连接" : "暂无连接"}
        </NativeSelectOption>
        {connections.map((connection) => (
          <NativeSelectOption key={connection.id} value={connection.id}>
            {connection.name}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </Field>
  );
}
