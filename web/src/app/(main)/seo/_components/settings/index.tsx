"use client";

import { useEffect, useRef } from "react";

import Link from "next/link";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { SeoAPIError } from "@/lib/seo/client";
import { integrationOAuthResultKey } from "@/lib/seo/google-oauth";
import { seoRoutes } from "@/lib/seo/routes";

import { SiteSelector, useSeoLocation, useSiteScope } from "../context";
import { ConnectionsPanel } from "../execution/connections";
import { useSeoSession } from "../session";
import { FilterBar, PageFrame } from "../shared";
import { AccountsPanel, ApplicationsPanel } from "./accounts";
import { BindingsPanel } from "./bindings";
import { IndexNowPanel } from "./indexnow";
import { HealthPanel, HistoryPanel, OverviewPanel } from "./monitor";

const tabs = [
  ["overview", "搜索与分析集成"],
  ["applications", "OAuth 应用配置"],
  ["accounts", "外部账户授权"],
  ["bindings", "站点资源绑定"],
  ["indexnow", "IndexNow 密钥管理"],
  ["health", "同步与健康监控"],
  ["history", "集成操作历史"],
] as const;

export function SearchIntegrationSettings() {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const { siteID, params, href, update } = useSeoLocation();
  const scope = useSiteScope();
  const available = tabs.filter(([tab]) => owner || (tab !== "applications" && tab !== "accounts"));
  const active = available.find(([tab]) => tab === params.get("tab"))?.[0] ?? "overview";
  const oauth = params.get(integrationOAuthResultKey);
  const processed = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!oauth || processed.current === oauth) return;
    processed.current = oauth;
    if (oauth === "connected") toast.success("外部账户已授权");
    else if (oauth === "cancelled") toast.error("授权已取消");
    else if (oauth === "failed") toast.error("授权未完成，请检查应用回调和账户权限。");
    else if (oauth.startsWith("integration_oauth_")) toast.error(new SeoAPIError(400, oauth).message);
    update({ [integrationOAuthResultKey]: null }, { replace: true });
  }, [oauth, update]);
  return (
    <PageFrame title="系统设置" description="管理搜索平台的应用、账户授权、站点资源和同步状态。">
      <nav aria-label="搜索集成设置" className="flex flex-wrap gap-2">
        {available.map(([tab, label]) => (
          <Button asChild key={tab} variant={tab === active ? "secondary" : "outline"} size="sm">
            <Link
              href={href(seoRoutes.system.settings.path, { tab, legacy: null, [integrationOAuthResultKey]: null })}
              aria-current={tab === active ? "page" : undefined}
              onClick={(event) => {
                if (event.button !== 0 || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
                event.preventDefault();
                update({ tab, legacy: null, [integrationOAuthResultKey]: null });
              }}
            >
              {label}
            </Link>
          </Button>
        ))}
      </nav>
      {["bindings", "indexnow", "health", "history"].includes(active) && scope.options.length > 0 && (
        <FilterBar>
          <SiteSelector id="integration-scope" scope={scope} label="集成站点" />
        </FilterBar>
      )}
      {active === "overview" && <OverviewPanel />}
      {owner && active === "applications" && <ApplicationsPanel />}
      {owner && active === "accounts" && <AccountsPanel />}
      {active === "bindings" && <BindingsPanel key={siteID ?? "all"} owner={owner} siteID={siteID} />}
      {owner && active === "bindings" && scope.site && (
        <>
          <div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => update({ legacy: params.get("legacy") === "1" ? null : "1" })}
            >
              {params.get("legacy") === "1" ? "隐藏迁移前的连接" : "检查迁移前的连接"}
            </Button>
          </div>
          {params.get("legacy") === "1" && (
            <ConnectionsPanel
              legacy
              site={scope.site}
              owner
              writable={false}
              onTask={() => undefined}
              changed={() => undefined}
              revision={0}
            />
          )}
        </>
      )}
      {active === "indexnow" && <IndexNowPanel key={siteID ?? "all"} owner={owner} siteID={siteID} />}
      {active === "health" && <HealthPanel owner={owner} siteID={siteID} />}
      {active === "history" && <HistoryPanel siteID={siteID} />}
    </PageFrame>
  );
}
