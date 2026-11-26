"use client";

import { useCallback, useState } from "react";

import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { isID, type Job, type Site } from "@/lib/seo/client";
import { type AnalyticsKind, type Connection, executionPath } from "@/lib/seo/execution";
import { type ExecutionView, seoRoutes } from "@/lib/seo/routes";

import { SiteSelector, useSeoLocation, useSiteScope } from "../context";
import { useSeoQuery } from "../query";
import { useSeoSession } from "../session";
import { FilterBar, LoadError, PageFrame, PageSkeleton } from "../shared";
import { ConnectionsPanel, useGoogleReturn } from "./connections";
import { PublishingPanel } from "./publishing";
import {
  AnalyticsPanel,
  CMSContentPanel,
  ExtractionPanel,
  FeedbackPanel,
  IndexPanel,
  IndexSubmissionPanel,
  type PanelScope,
  SitemapPanel,
  UsagePanel,
} from "./reports";
import { ExecutionJobHistory, ExecutionTaskEditor, LatestExecutionJob, type TaskSelection } from "./tasks";

type ExecutionPageView = ExecutionView | "connections";

const descriptions = {
  connections: "配置 WordPress、通用 CMS 和搜索平台连接，管理凭据及授权范围。",
  cms: "同步已连接 CMS 的页面、文章和媒体，查看同步结果。",
  publishing: "从已审核的内容版本创建发布记录、核验结果，并恢复已备份的版本。",
  gsc: "查看 Google Search Console 的查询、排名与点击数据。",
  ga4: "查看 GA4 已同步的自然搜索流量和互动数据。",
  bing: "查看 Bing Webmaster 已同步的搜索指标。",
  index: "检查站内 URL，查看抓取和索引证据。",
  sitemaps: "读取站点地图和解析结果，并下载已保存的 XML。",
  indexnow: "通过已授权的 IndexNow 或 Bing Webmaster 连接提交 URL，查看平台回执。",
  "internal-links": "",
  network: "",
  technical: "管理已授权页面的浏览器和 PDF 提取任务，查看提取证据。",
  jobs: "追踪站点执行任务、重试状态和最近结果。",
  usage: "核对各站点连接和采集任务的今日请求额度与用量。",
  feedback: "查看已满足观测条件的流量、排名及点击率变化。",
} satisfies Record<ExecutionPageView, string>;

const connectedViews: readonly ExecutionView[] = ["cms", "publishing", "gsc", "ga4", "bing", "index", "indexnow"];
const analyticsViews = [
  ["gsc", "Search Console"],
  ["ga4", "GA4"],
  ["bing", "Bing Webmaster"],
] as const;

function AnalyticsNavigation({ view, siteID }: { view: AnalyticsKind; siteID: string }) {
  const { href } = useSeoLocation();

  return (
    <nav aria-label="搜索分析来源" className="flex flex-wrap gap-2">
      {analyticsViews.map(([id, label]) => (
        <Button key={id} asChild size="sm" variant={id === view ? "secondary" : "outline"}>
          <Link
            href={href(seoRoutes.execution[id].path, { site: siteID })}
            aria-current={id === view ? "page" : undefined}
          >
            {label}
          </Link>
        </Button>
      ))}
    </nav>
  );
}

export function ExecutionCenter({ view }: { view: ExecutionPageView }) {
  const { params } = useSeoLocation();
  const scope = useSiteScope();
  const owner = useSeoSession().principal.role === "owner";
  const authorizationReturn = useGoogleReturn(owner && view === "connections");
  const preferredPackage = params.get("package") ?? "";
  const title = view === "connections" ? seoRoutes.sites.connections.title : seoRoutes.execution[view].title;

  return (
    <PageFrame title={title} description={descriptions[view]}>
      {authorizationReturn ? (
        <PageSkeleton />
      ) : (
        <>
          {scope.options.length > 0 && (
            <FilterBar>
              <SiteSelector id="execution-site" label="执行站点" scope={scope} />
            </FilterBar>
          )}
          {scope.error !== undefined && <LoadError error={scope.error} retry={scope.refresh} />}
          {scope.loading && <PageSkeleton />}
          {scope.site && (
            <SiteExecution
              key={`${view}/${scope.site.id}`}
              view={view}
              site={scope.site}
              requestedPackage={isID(preferredPackage) ? preferredPackage : undefined}
            />
          )}
          {!scope.site && !scope.loading && scope.error === undefined && (
            <Card>
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>暂无可用站点</EmptyTitle>
                  <EmptyDescription>站点登记后可配置连接及执行任务。</EmptyDescription>
                </EmptyHeader>
                <Button asChild size="sm" variant="outline">
                  <Link href={seoRoutes.sites.list.path}>打开站点管理</Link>
                </Button>
              </Empty>
            </Card>
          )}
        </>
      )}
    </PageFrame>
  );
}

function ExecutionPanel({
  view,
  scope,
  requestedPackage,
}: {
  view: Exclude<keyof typeof descriptions, "connections">;
  scope: PanelScope;
  requestedPackage?: string;
}) {
  switch (view) {
    case "cms":
      return <CMSContentPanel {...scope} />;
    case "publishing":
      return <PublishingPanel {...scope} requestedPackage={requestedPackage} />;
    case "gsc":
    case "ga4":
    case "bing":
      return (
        <>
          <AnalyticsNavigation view={view} siteID={scope.site.id} />
          <AnalyticsPanel {...scope} kind={view} />
        </>
      );
    case "index":
      return <IndexPanel {...scope} />;
    case "indexnow":
      return <IndexSubmissionPanel {...scope} />;
    case "sitemaps":
      return <SitemapPanel {...scope} />;
    case "technical":
      return <ExtractionPanel {...scope} />;
    case "feedback":
      return <FeedbackPanel site={scope.site} revision={scope.revision} />;
    case "jobs":
      return <ExecutionJobHistory {...scope} />;
    case "usage":
      return scope.owner ? <UsagePanel site={scope.site} revision={scope.revision} /> : null;
    default:
      return null;
  }
}

function SiteExecution({
  site,
  view,
  requestedPackage,
}: {
  site: Site;
  view: ExecutionPageView;
  requestedPackage?: string;
}) {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const writable = principal.role !== "viewer" && site.status === "active";
  const [task, setTask] = useState<TaskSelection>();
  const [latest, setLatest] = useState<Job>();
  const [revision, setRevision] = useState(0);
  const query = useSeoQuery<{ items: Connection[] }>(
    view !== "connections" && connectedViews.includes(view)
      ? executionPath("connections", site.id, { limit: "200" })
      : null,
  );
  const { refresh } = query;
  const changed = useCallback(() => {
    refresh();
    setRevision((value) => value + 1);
  }, [refresh]);
  const scope = { site, owner, writable, connections: query.data?.items ?? [], onTask: setTask, revision };

  return (
    <>
      {query.error !== undefined && <LoadError error={query.error} retry={refresh} />}
      {latest && <LatestExecutionJob key={latest.id} initial={latest} onSettled={changed} />}
      {query.loading && <PageSkeleton />}
      {!query.loading &&
        (view === "connections" ? (
          <ConnectionsPanel {...scope} changed={changed} />
        ) : (
          <ExecutionPanel view={view} scope={scope} requestedPackage={requestedPackage} />
        ))}
      {task && (
        <ExecutionTaskEditor
          selection={task}
          site={site}
          onClose={() => setTask(undefined)}
          onSaved={(job) => {
            setLatest(job);
            changed();
          }}
        />
      )}
    </>
  );
}
