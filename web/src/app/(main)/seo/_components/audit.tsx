"use client";

import { useState } from "react";

import { FileText } from "lucide-react";

import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import type { AuditEvent } from "@/lib/seo/client";

import { formatTime, PageFrame, RecordDetails, RecordsTable, useSeoList } from "./shared";

export function Audit() {
  const list = useSeoList<AuditEvent>("events");
  const [selected, setSelected] = useState<AuditEvent>();
  return (
    <PageFrame title="审计日志" description="查看组织写入和任务状态变更。">
      <RecordsTable label="组织活动" columns={["操作", "身份", "时间（UTC）", "详情"]} empty="暂无审计记录" list={list}>
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
      {selected && (
        <RecordDetails title={selected.action} value={selected.details} onClose={() => setSelected(undefined)} />
      )}
    </PageFrame>
  );
}
