"use client";

import { useState } from "react";

import { FileText } from "lucide-react";

import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import type { AuditEvent } from "@/lib/seo/client";
import { executionNavigation, seoRoutes } from "@/lib/seo/routes";

import { useSiteScope } from "./context";
import { readSeoPages, useSeoQuery } from "./query";
import { formatTime, PageFrame, RecordDetails, RecordsTable, useLocalSeoRecords, useSeoList } from "./shared";
import { WorkflowNav } from "./workflow-nav";

function loadExecutionEvents(path: string, signal: AbortSignal) {
  return readSeoPages<AuditEvent>(path.split("?", 1)[0], "events", signal);
}

export function Audit({ execution = false }: { execution?: boolean }) {
  const scope = useSiteScope();
  const log = useSeoQuery(execution ? `/audit?${scope.key}` : null, loadExecutionEvents);
  const audit = useSeoList<AuditEvent>("events", !execution);
  const ids = new Set(scope.rows.map(({ id }) => id));
  const events = (log.data?.rows ?? []).filter((event) => {
    if (!event.action.startsWith("execution.")) return false;
    if (!scope.restricted) return true;
    const details = event.details as { site_id?: string } | null;
    return !!details?.site_id && ids.has(details.site_id);
  });
  const scoped = useLocalSeoRecords(events, scope.key, {
    ...log,
    loading: log.loading || scope.loading,
    error: log.error ?? scope.error,
  });
  const list = execution ? scoped : audit;
  const [selected, setSelected] = useState<AuditEvent>();
  return (
    <PageFrame
      title={execution ? seoRoutes.execution.logs.title : seoRoutes.system.audit.title}
      description={
        execution ? "查看执行审计事件。选择站点范围时，仅显示带匹配站点标识的事件。" : "查看组织写入和任务状态变更。"
      }
    >
      {execution && <WorkflowNav label="执行监控" pages={executionNavigation[5]} />}
      {execution && log.data?.complete === false && (
        <p className="text-muted-foreground text-sm">仅显示最近 2,000 条组织审计中的执行事件。</p>
      )}
      <RecordsTable
        label={execution ? "执行事件" : "组织活动"}
        columns={["操作", "身份", "时间（UTC）", "详情"]}
        empty="暂无审计记录"
        list={list}
      >
        {list.records.map((event) => (
          <TableRow key={event.id}>
            <TableCell className="px-3 py-2">
              <div className="font-medium">{event.action}</div>
              <code className="font-mono text-muted-foreground text-xs">{event.resource_id}</code>
            </TableCell>
            <TableCell className="px-3">
              <div>{event.actor_kind}</div>
              {event.actor_user_id && (
                <code className="font-mono text-muted-foreground text-xs">{event.actor_user_id}</code>
              )}
            </TableCell>
            <TableCell className="px-3 tabular-nums">{formatTime(event.created_at)}</TableCell>
            <TableCell className="px-3">
              <Button variant="ghost" size="sm" onClick={() => setSelected(event)}>
                <FileText /> 查看记录
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {selected && (!execution || events.some(({ id }) => id === selected.id)) && (
        <RecordDetails title={selected.action} value={selected.details} onClose={() => setSelected(undefined)} />
      )}
    </PageFrame>
  );
}
