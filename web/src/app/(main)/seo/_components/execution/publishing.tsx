"use client";

import { useState } from "react";

import Link from "next/link";

import { ClipboardCheck, FileText, History, Plus, RefreshCw, RotateCcw, Send } from "lucide-react";
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
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { SeoAPIError, type Site, seoRequest } from "@/lib/seo/client";
import {
  type Connection,
  type ExecutionPublication,
  executionPath,
  type PreparedPackage,
  type PublicationEvent,
  type PublicationStatus,
  publicationStates,
} from "@/lib/seo/execution";
import { seoRoutes } from "@/lib/seo/routes";

import { useSeoLocation } from "../context";
import { useSeoQuery } from "../query";
import { Field, FormDialog, field, formatTime, LoadError, PageSkeleton, RecordsTable, StateBadge } from "../shared";
import { useExecutionRecords } from "./records";
import type { TaskSelection } from "./tasks";

const draftStates = new Set<PublicationStatus>(["PENDING", "FAILED", "CANCELLED"]);
const verifyStates = new Set<PublicationStatus>(["SCHEDULED", "PUBLISHED", "VERIFIED", "ROLLBACK_REQUIRED"]);
const rollbackStates = new Set<PublicationStatus>(["SCHEDULED", "PUBLISHED", "VERIFIED", "ROLLBACK_REQUIRED"]);

export function WebAddress({ url }: { url?: string }) {
  if (!url) return <span>—</span>;
  try {
    const address = new URL(url);
    if (["https:", "http:"].includes(address.protocol) && !address.username && !address.password) {
      return (
        <a
          href={address.href}
          target="_blank"
          rel="noopener noreferrer"
          className="break-all underline decoration-muted-foreground/50 underline-offset-2 hover:text-primary"
        >
          {url}
        </a>
      );
    }
  } catch {
    // Page paths and property keys can be displayed without navigation.
  }
  return <span className="break-all">{url}</span>;
}

function publicationStatus(status: PublicationStatus) {
  let tone: "destructive" | "secondary" | "outline" = "outline";
  if (["FAILED", "ROLLBACK_REQUIRED"].includes(status)) tone = "destructive";
  else if (["VERIFIED", "DRAFT_CREATED", "ROLLED_BACK"].includes(status)) tone = "secondary";
  return <StateBadge label={publicationStates[status] ?? status} tone={tone} />;
}

function ReleaseEditor({
  site,
  connections,
  packages,
  requestedPackage,
  onClose,
  onSaved,
}: {
  site: Site;
  connections: Connection[];
  packages: PreparedPackage[];
  requestedPackage?: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [key] = useState(() => crypto.randomUUID());
  const [selected, setSelected] = useState(() => {
    if (!requestedPackage) return packages[0]?.id ?? "";
    return packages.some((entry) => entry.id === requestedPackage) ? requestedPackage : "";
  });
  const source = packages.find((entry) => entry.id === selected);

  return (
    <FormDialog
      title="创建发布记录"
      description="选择经过内容审核的版本和已授权的 CMS。发布记录用于准备草稿和单独批准正式发布。"
      submitLabel="创建记录"
      onClose={onClose}
      onSubmit={async (data) => {
        if (!source) throw new SeoAPIError(400, "content_gate_blocked");
        await seoRequest<{ publication: ExecutionPublication }>("/execution/publications", {
          method: "POST",
          body: {
            site_id: site.id,
            package_id: source.id,
            connection_id: field(data, "connection_id"),
            resource: field(data, "resource"),
            remote_id: field(data, "remote_id").trim() || undefined,
            idempotency_key: key,
          },
        });
        onSaved();
        toast.success("发布记录已创建");
      }}
    >
      <div className="sm:col-span-2">
        <Field id="execution-release-package" label="已批准的发布包">
          <NativeSelect
            id="execution-release-package"
            className="w-full"
            value={selected}
            required
            onChange={(event) => setSelected(event.target.value)}
          >
            {!selected && (
              <NativeSelectOption value="" disabled>
                请选择发布包
              </NativeSelectOption>
            )}
            {packages.map((entry) => (
              <NativeSelectOption key={entry.id} value={entry.id}>
                {entry.article.title} · 第 {entry.version} 版
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        {source?.article.canonical && (
          <p className="mt-1 break-all text-muted-foreground text-xs">Canonical · {source.article.canonical}</p>
        )}
      </div>
      <Field id="execution-release-cms" label="目标 CMS">
        <NativeSelect id="execution-release-cms" name="connection_id" className="w-full" required>
          {connections.map((item) => (
            <NativeSelectOption key={item.id} value={item.id}>
              {item.name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Field>
      <Field id="execution-release-resource" label="内容类型">
        <NativeSelect id="execution-release-resource" name="resource" className="w-full" defaultValue="post">
          <NativeSelectOption value="post">文章</NativeSelectOption>
          <NativeSelectOption value="page">页面</NativeSelectOption>
        </NativeSelect>
      </Field>
      <div className="sm:col-span-2">
        <Field id="execution-release-existing" label="要更新的 CMS 内容 ID（可选）">
          <Input id="execution-release-existing" name="remote_id" maxLength={100} />
        </Field>
      </div>
    </FormDialog>
  );
}

function PublicationDetails({ id, onClose }: { id: string; onClose: () => void }) {
  const detail = useSeoQuery<{ publication: ExecutionPublication; events: PublicationEvent[] }>(
    `/execution/publications/${id}`,
  );
  const record = detail.data?.publication;
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader className="pr-7">
          <DialogTitle>发布历史</DialogTitle>
          <DialogDescription>{record?.title ?? "读取发布状态和操作记录。"}</DialogDescription>
        </DialogHeader>
        {detail.loading && <PageSkeleton />}
        {detail.error !== undefined && <LoadError error={detail.error} retry={detail.refresh} />}
        {record && (
          <>
            <dl className="grid gap-2 text-sm sm:grid-cols-3">
              <div>
                <dt className="text-muted-foreground">状态</dt>
                <dd>{publicationStatus(record.status)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">文章版本</dt>
                <dd>{record.article_version}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">CMS 内容 ID</dt>
                <dd>{record.target_id ?? "—"}</dd>
              </div>
              <div className="sm:col-span-3">
                <dt className="text-muted-foreground">目标 URL</dt>
                <dd>
                  <WebAddress url={record.url} />
                </dd>
              </div>
              {record.error && (
                <div className="sm:col-span-3">
                  <dt className="text-muted-foreground">错误</dt>
                  <dd className="break-all">{record.error}</dd>
                </div>
              )}
            </dl>
            <Table aria-label="发布事件">
              <TableHeader>
                <TableRow>
                  <TableHead scope="col">时间（UTC）</TableHead>
                  <TableHead scope="col">状态</TableHead>
                  <TableHead scope="col">操作和说明</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(detail.data?.events ?? []).map((event) => (
                  <TableRow key={`${event.created_at}:${event.action}:${event.status}`}>
                    <TableCell>{formatTime(event.created_at)}</TableCell>
                    <TableCell>{publicationStatus(event.status)}</TableCell>
                    <TableCell className="max-w-80 whitespace-normal break-words">
                      <div>{event.action}</div>
                      {event.note && <p className="text-muted-foreground text-xs">{event.note}</p>}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="outline">
              关闭
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function PublishingPanel({
  site,
  connections,
  owner,
  writable,
  onTask,
  revision,
  requestedPackage,
  mode,
}: {
  site: Site;
  connections: Connection[];
  owner: boolean;
  writable: boolean;
  onTask: (selection: TaskSelection) => void;
  revision: number;
  requestedPackage?: string;
  mode: "publishing" | "publications" | "verification";
}) {
  const list = useExecutionRecords<ExecutionPublication>("publications", site.id, revision);
  const authoring = owner && writable && mode === "publishing";
  const verifying = mode === "verification";
  const { href } = useSeoLocation();
  const packages = useSeoQuery<{ items: PreparedPackage[] }>(authoring ? executionPath("packages", site.id) : null);
  const { refresh } = list;
  const { refresh: refreshPackages } = packages;
  const [editor, setEditor] = useState(false);
  const [details, setDetails] = useState<string>();
  const publishConnections = connections.filter(
    (item) => item.enabled && item.configured && item.allow_writes && item.capabilities?.write_pages,
  );

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-semibold">{verifying ? "发布核验状态" : "CMS 发布记录"}</h2>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              refresh();
              refreshPackages();
            }}
            disabled={list.refreshing}
          >
            <RefreshCw /> 刷新发布记录
          </Button>
          {authoring && publishConnections.length > 0 && !!packages.data?.items?.length && (
            <Button type="button" size="sm" onClick={() => setEditor(true)}>
              <Plus /> 创建发布记录
            </Button>
          )}
        </div>
      </div>
      {authoring && packages.error !== undefined && <LoadError error={packages.error} retry={refreshPackages} />}
      {authoring && !packages.loading && !packages.error && !packages.data?.items?.length && (
        <p className="text-muted-foreground text-sm">
          <Link
            href={href(seoRoutes.content.overview.path, { site_id: site.id })}
            className="underline underline-offset-2"
          >
            在内容工作室准备通过审核的发布包
          </Link>
        </p>
      )}
      {authoring && !!packages.data?.items?.length && !publishConnections.length && (
        <p className="text-muted-foreground text-sm">请在 CMS 连接启用并测试具备草稿写入权限的服务。</p>
      )}
      <RecordsTable
        label="CMS 发布记录"
        columns={["内容版本", "CMS", "状态", "目标 URL", "操作"]}
        empty="暂无发布记录"
        list={list}
      >
        {list.records.map((entry) => {
          const connection = connections.find((item) => item.id === entry.connection_id);
          const allowed =
            !verifying && owner && writable && connection?.enabled && connection.configured && connection.allow_writes;

          return (
            <TableRow key={entry.id}>
              <TableCell className="max-w-72 whitespace-normal px-3">
                <div className="font-medium">
                  <span className="break-words">{entry.title}</span>
                </div>
                <div className="text-muted-foreground text-xs">
                  第 {entry.article_version} 版 · {entry.resource === "page" ? "页面" : "文章"}
                </div>
              </TableCell>
              <TableCell className="px-3">
                <div>{connection?.name ?? "—"}</div>
                {entry.target_id && <code className="text-muted-foreground text-xs">{entry.target_id}</code>}
              </TableCell>
              <TableCell className="px-3">
                {publicationStatus(entry.status)}
                {entry.verification_job_id && entry.status !== "VERIFYING" && (
                  <p className="mt-1 text-muted-foreground text-xs">等待核验</p>
                )}
                {entry.error && (
                  <p className="max-w-48 whitespace-normal break-all text-muted-foreground text-xs">{entry.error}</p>
                )}
              </TableCell>
              <TableCell className="max-w-64 whitespace-normal px-3 text-xs">
                <WebAddress url={entry.url} />
              </TableCell>
              <TableCell className="px-3">
                <div className="flex max-w-sm flex-wrap">
                  {allowed && connection?.capabilities.write_pages && draftStates.has(entry.status) && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        onTask({ kind: "cms_publication", operation: "draft", publication: entry, connection })
                      }
                    >
                      <FileText /> 保存草稿
                    </Button>
                  )}
                  {allowed && connection?.capabilities.publish && entry.status === "DRAFT_CREATED" && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        onTask({ kind: "cms_publication", operation: "publish", publication: entry, connection })
                      }
                    >
                      <Send /> 批准正式发布
                    </Button>
                  )}
                  {writable &&
                    connection?.enabled &&
                    connection.configured &&
                    entry.target_id &&
                    !entry.verification_job_id &&
                    verifyStates.has(entry.status) && (
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => onTask({ kind: "cms_publication_verify", publication: entry, connection })}
                      >
                        <ClipboardCheck /> 核验发布
                      </Button>
                    )}
                  {allowed && rollbackStates.has(entry.status) && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        onTask({ kind: "cms_publication", operation: "rollback", publication: entry, connection })
                      }
                    >
                      <RotateCcw /> 恢复版本
                    </Button>
                  )}
                  <Button type="button" size="sm" variant="ghost" onClick={() => setDetails(entry.id)}>
                    <History /> 发布历史
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          );
        })}
      </RecordsTable>
      {editor && (
        <ReleaseEditor
          site={site}
          connections={publishConnections}
          packages={packages.data?.items ?? []}
          requestedPackage={requestedPackage}
          onClose={() => setEditor(false)}
          onSaved={refresh}
        />
      )}
      {details && <PublicationDetails id={details} onClose={() => setDetails(undefined)} />}
    </div>
  );
}
