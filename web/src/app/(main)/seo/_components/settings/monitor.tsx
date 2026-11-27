"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";

import { useSiteScope } from "../context";
import { useSeoQuery } from "../query";
import { Field, FilterBar, field, LoadError, PageSkeleton, RecordsTable, StateBadge, useSeoRecords } from "../shared";
import { IntegrationStatus, TimeValue } from "./common";
import { type BindingProvider, listPath, serviceLabels } from "./model";

type Summary = {
  provider: BindingProvider;
  accounts: number;
  bound_sites: number;
  errors: number;
  healthy_count: number;
  last_success_at?: string;
};

function overviewStatus(row: Summary) {
  if (row.errors > 0) return "需要处理";
  if (row.healthy_count > 0) return "正常";
  return row.bound_sites > 0 ? "待同步" : "未连接";
}

const providerNames = { ...serviceLabels, google: "Google" };

export function OverviewPanel() {
  const query = useSeoQuery<{ items: Summary[] }>(listPath("summary"));
  if (query.error) return <LoadError error={query.error} retry={query.refresh} />;
  if (query.loading) return <PageSkeleton />;
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {query.data?.items.map((row) => (
        <Card key={row.provider}>
          <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
            <CardTitle>
              <h2>{serviceLabels[row.provider]}</h2>
            </CardTitle>
            <StateBadge label={overviewStatus(row)} />
          </CardHeader>
          <CardContent>
            <dl className="grid gap-2 text-sm">
              {row.provider !== "indexnow" && (
                <div className="flex justify-between gap-2">
                  <dt className="text-muted-foreground">账户</dt>
                  <dd>{row.accounts}</dd>
                </div>
              )}
              <div className="flex justify-between gap-2">
                <dt className="text-muted-foreground">绑定站点</dt>
                <dd>{row.bound_sites}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-muted-foreground">异常绑定</dt>
                <dd>{row.errors}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-muted-foreground">最近成功</dt>
                <dd>
                  <TimeValue value={row.last_success_at} />
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

type Health = {
  binding_id: string;
  site_id: string;
  provider: BindingProvider;
  status: string;
  last_success_at?: string;
  last_failure_at?: string;
  last_error_code?: string;
  last_http_status?: string;
  latency_ms?: string;
  next_sync_at?: string;
  requests_per_day?: number;
  daily_requests?: number;
};
type History = {
  id: string;
  provider?: BindingProvider | "google";
  site_id?: string;
  operation: string;
  status: string;
  requested_from?: string;
  requested_to?: string;
  result_summary?: Record<string, unknown>;
  request_count?: number;
  created_at: string;
  completed_at?: string;
  truncated?: boolean;
  error_code?: string;
};
type Usage = {
  id: number;
  provider: BindingProvider | "google";
  site_id?: string;
  account_id?: string;
  operation: string;
  requested_at: string;
  http_status?: number;
  code?: string;
  duration_ms?: number;
  cost_micros?: number;
};

const filterDefaults = { provider: "", status: "", from: "", to: "", account_id: "" };
const filters = ["provider", "status", "from", "to", "account_id"] as const;

function MonitorFilter({
  mode,
  onChange,
}: {
  mode: "health" | "history" | "usage";
  onChange: (filter: typeof filterDefaults) => void;
}) {
  return (
    <FilterBar>
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const form = new FormData(event.currentTarget);
          onChange({
            provider: field(form, "provider"),
            status: field(form, "status"),
            from: field(form, "from"),
            to: field(form, "to"),
            account_id: field(form, "account_id").trim(),
          });
        }}
      >
        <Field id="monitor-provider" label="服务">
          <NativeSelect id="monitor-provider" name="provider">
            <NativeSelectOption value="">全部服务</NativeSelectOption>
            <NativeSelectOption value="google">Google</NativeSelectOption>
            {Object.entries(serviceLabels).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        {mode !== "usage" && (
          <Field id="monitor-status" label="状态">
            <NativeSelect id="monitor-status" name="status">
              <NativeSelectOption value="">全部状态</NativeSelectOption>
              {mode === "history" ? (
                <>
                  <NativeSelectOption value="succeeded">已完成</NativeSelectOption>
                  <NativeSelectOption value="failed">失败</NativeSelectOption>
                  <NativeSelectOption value="queued">排队中</NativeSelectOption>
                  <NativeSelectOption value="audited">已记录</NativeSelectOption>
                </>
              ) : (
                <>
                  <NativeSelectOption value="HEALTHY">正常</NativeSelectOption>
                  <NativeSelectOption value="ERROR">异常</NativeSelectOption>
                  <NativeSelectOption value="REAUTH_REQUIRED">需重新授权</NativeSelectOption>
                  <NativeSelectOption value="UNVERIFIED">待检查</NativeSelectOption>
                </>
              )}
            </NativeSelect>
          </Field>
        )}
        <Field id="monitor-from" label="开始日期">
          <Input id="monitor-from" type="date" name="from" />
        </Field>
        <Field id="monitor-to" label="结束日期">
          <Input id="monitor-to" type="date" name="to" />
        </Field>
        <Field id="monitor-account" label="账户 ID">
          <Input id="monitor-account" name="account_id" />
        </Field>
        <Button variant="outline" type="submit">
          筛选
        </Button>
      </form>
    </FilterBar>
  );
}

export function HealthPanel({ siteID, owner }: { siteID?: string; owner: boolean }) {
  const [kind, setKind] = useState<"health" | "usage">("health");
  return (
    <>
      {owner && (
        <div className="flex gap-2">
          <Button variant={kind === "health" ? "secondary" : "outline"} onClick={() => setKind("health")}>
            连接健康
          </Button>
          <Button variant={kind === "usage" ? "secondary" : "outline"} onClick={() => setKind("usage")}>
            请求用量
          </Button>
        </div>
      )}
      {kind === "usage" && owner ? <UsageTable siteID={siteID} /> : <HealthTable siteID={siteID} />}
    </>
  );
}

function useMonitorPath(name: "health" | "history" | "usage", siteID?: string) {
  const [filter, setFilter] = useState(filterDefaults);
  const active: Record<string, string | undefined> = { site_id: siteID };
  for (const key of filters) active[key] = filter[key];
  return { path: listPath(name, active), setFilter };
}

function HealthTable({ siteID }: { siteID?: string }) {
  const { path, setFilter } = useMonitorPath("health", siteID);
  const list = useSeoRecords<Health>(path, "items");
  const sites = useSiteScope();
  return (
    <>
      <MonitorFilter mode="health" onChange={setFilter} />
      <RecordsTable
        label="连接健康"
        columns={["站点", "服务", "状态", "最近成功", "最近失败", "HTTP／耗时", "今日用量", "下次任务"]}
        list={list}
        empty="暂无绑定的连接"
      >
        {list.records.map((row) => (
          <TableRow key={row.binding_id}>
            <TableCell>{sites.options.find((s) => s.id === row.site_id)?.name ?? row.site_id}</TableCell>
            <TableCell>{serviceLabels[row.provider]}</TableCell>
            <TableCell>
              <IntegrationStatus status={row.status} />
            </TableCell>
            <TableCell>
              <TimeValue value={row.last_success_at} />
            </TableCell>
            <TableCell>
              <TimeValue value={row.last_failure_at} empty="—" />
              {row.last_error_code && <div className="text-muted-foreground text-xs">{row.last_error_code}</div>}
            </TableCell>
            <TableCell>
              {row.last_http_status ?? "—"} {row.latency_ms ? `· ${row.latency_ms} ms` : ""}
            </TableCell>
            <TableCell>
              {row.daily_requests ?? "—"} / {row.requests_per_day ?? "—"}
            </TableCell>
            <TableCell>
              <TimeValue value={row.next_sync_at} empty="未排队" />
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      <Button className="self-start" variant="outline" onClick={list.refresh}>
        刷新状态
      </Button>
    </>
  );
}

function UsageTable({ siteID }: { siteID?: string }) {
  const { path, setFilter } = useMonitorPath("usage", siteID);
  const list = useSeoRecords<Usage>(path, "items");
  return (
    <>
      <MonitorFilter mode="usage" onChange={setFilter} />
      <RecordsTable
        label="请求用量"
        columns={["时间", "服务", "操作", "HTTP", "错误", "耗时", "费用（USD 微单位）"]}
        list={list}
        empty="暂无请求"
      >
        {list.records.map((row) => (
          <TableRow key={`${row.site_id ?? row.account_id}-${row.id}`}>
            <TableCell>
              <TimeValue value={row.requested_at} />
            </TableCell>
            <TableCell>{row.provider === "google" ? "Google" : serviceLabels[row.provider]}</TableCell>
            <TableCell>{row.operation}</TableCell>
            <TableCell>{row.http_status ?? "—"}</TableCell>
            <TableCell>{row.code || "—"}</TableCell>
            <TableCell>{row.duration_ms === undefined ? "—" : `${row.duration_ms} ms`}</TableCell>
            <TableCell>{row.cost_micros ?? "—"}</TableCell>
          </TableRow>
        ))}
      </RecordsTable>
    </>
  );
}

export function HistoryPanel({ siteID }: { siteID?: string }) {
  const { path, setFilter } = useMonitorPath("history", siteID);
  const list = useSeoRecords<History>(path, "items");
  return (
    <>
      <MonitorFilter mode="history" onChange={setFilter} />
      <RecordsTable
        label="集成操作历史"
        columns={["操作", "服务", "时间", "状态", "日期范围", "请求数", "结果"]}
        list={list}
        empty="暂无集成操作记录"
      >
        {list.records.map((row) => {
          const submission =
            typeof row.result_summary?.integration_status === "string"
              ? row.result_summary.integration_status
              : undefined;
          return (
            <TableRow key={row.id}>
              <TableCell>{row.operation}</TableCell>
              <TableCell>{row.provider ? providerNames[row.provider] : "—"}</TableCell>
              <TableCell>
                <TimeValue value={row.completed_at ?? row.created_at} />
              </TableCell>
              <TableCell>
                <IntegrationStatus status={row.status} />
              </TableCell>
              <TableCell>
                {row.requested_from && row.requested_to ? `${row.requested_from} 至 ${row.requested_to}` : "—"}
              </TableCell>
              <TableCell>{row.request_count ?? "—"}</TableCell>
              <TableCell>{row.error_code || submission || (row.truncated ? "明细截断" : "—")}</TableCell>
            </TableRow>
          );
        })}
      </RecordsTable>
      <Button className="self-start" variant="outline" onClick={list.refresh}>
        刷新历史
      </Button>
    </>
  );
}
