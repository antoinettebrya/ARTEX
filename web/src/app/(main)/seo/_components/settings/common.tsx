"use client";

import { Button } from "@/components/ui/button";

import { formatTime, StateBadge } from "../shared";

const labels: Record<string, string> = {
  CONNECTED: "已授权",
  REAUTH_REQUIRED: "需重新授权",
  REVOKED: "已撤销",
  EXPIRED: "已过期",
  ENABLED: "已启用",
  DISABLED: "已停用",
  SITE_CHANGED: "站点已变更",
  HEALTHY: "正常",
  UNVERIFIED: "待检查",
  PENDING: "待验证",
  VERIFIED: "已验证",
  ERROR: "异常",
  FAILED: "验证失败",
  queued: "排队中",
  running: "进行中",
  succeeded: "已完成",
  failed: "失败",
  cancelled: "已取消",
  audited: "已记录",
};

export function IntegrationStatus({ status }: { status: string }) {
  const danger = ["REVOKED", "ERROR", "FAILED", "failed", "SITE_CHANGED"].includes(status);
  return <StateBadge label={labels[status] ?? status} tone={danger ? "destructive" : "outline"} />;
}

export function TimeValue({ value, empty = "待同步" }: { value?: string | null; empty?: string }) {
  return <span>{value ? formatTime(value) : empty}</span>;
}

export function RowAction({
  children,
  onClick,
  disabled,
}: {
  children: React.ReactNode;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <Button type="button" size="sm" variant="outline" disabled={disabled} onClick={onClick}>
      {children}
    </Button>
  );
}
