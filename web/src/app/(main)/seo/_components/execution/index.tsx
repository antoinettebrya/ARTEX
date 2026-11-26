"use client";

import { useCallback, useState } from "react";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";

import { Plug } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { isID, type Job, type Site } from "@/lib/seo/client";
import { type Connection, executionPath } from "@/lib/seo/execution";

import { useSeoQuery } from "../query";
import { useSeoSession } from "../session";
import { Field, LoadError, PageFrame, PageSkeleton } from "../shared";
import { ConnectionsPanel, useGoogleReturn } from "./connections";
import { PublishingPanel } from "./publishing";
import {
  AnalyticsPanel,
  CMSContentPanel,
  ExtractionPanel,
  FeedbackPanel,
  IndexPanel,
  SitemapPanel,
  UsagePanel,
} from "./reports";
import { ExecutionJobHistory, ExecutionTaskEditor, LatestExecutionJob, type TaskSelection } from "./tasks";

const tabs = [
  ["connections", "CMS 连接"],
  ["cms", "内容及媒体"],
  ["publishing", "发布中心"],
  ["gsc", "Search Console"],
  ["ga4", "GA4"],
  ["bing", "Bing Webmaster"],
  ["index", "索引与 URL 检查"],
  ["sitemaps", "Sitemap"],
  ["extractions", "提取监控"],
  ["feedback", "优化反馈"],
  ["jobs", "执行任务"],
  ["usage", "用量"],
] as const;

type ExecutionTab = (typeof tabs)[number][0];

export function ExecutionCenter() {
  const router = useRouter();
  const params = useSearchParams();
  const preferredSite = params.get("site") ?? "";
  const preferredPackage = params.get("package") ?? "";
  const sites = useSeoQuery<{ sites: Site[] }>("/sites?limit=200");
  const listed = sites.data?.sites ?? [];
  const lookup = useSeoQuery<{ site: Site }>(
    isID(preferredSite) && sites.data && !listed.some((site) => site.id === preferredSite)
      ? `/sites/${preferredSite}`
      : null,
  );
  const selected = isID(preferredSite)
    ? (listed.find((site) => site.id === preferredSite) ?? lookup.data?.site)
    : listed[0];
  const choices = lookup.data?.site ? [...listed, lookup.data.site] : listed;
  const owner = useSeoSession().principal.role === "owner";

  return (
    <PageFrame title="站点执行" description="连接 CMS 和搜索平台，执行发布并核验站点状态。">
      {(sites.loading || lookup.loading) && <PageSkeleton />}
      {sites.error !== undefined && <LoadError error={sites.error} retry={sites.refresh} />}
      {lookup.error !== undefined && <LoadError error={lookup.error} retry={lookup.refresh} />}
      {selected ? (
        <>
          <Card className="py-3">
            <CardContent className="px-3">
              <div className="max-w-md">
                <Field id="execution-site" label="执行站点">
                  <NativeSelect
                    id="execution-site"
                    className="w-full"
                    value={selected.id}
                    onChange={(event) => router.replace(`/seo/execution?site=${event.target.value}`, { scroll: false })}
                  >
                    {choices.map((site) => (
                      <NativeSelectOption key={site.id} value={site.id}>
                        {site.name}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </Field>
              </div>
            </CardContent>
          </Card>
          <SiteExecution
            key={selected.id}
            site={selected}
            requestedPackage={isID(preferredPackage) ? preferredPackage : undefined}
          />
        </>
      ) : (
        sites.data &&
        !lookup.loading &&
        lookup.error === undefined && (
          <Card>
            <Empty>
              <EmptyHeader>
                <EmptyTitle>暂无可用站点</EmptyTitle>
                <EmptyDescription>站点登记后可配置连接及执行任务。</EmptyDescription>
              </EmptyHeader>
              {owner && (
                <Button asChild size="sm">
                  <Link href="/seo/sites">
                    <Plug /> 打开站点管理
                  </Link>
                </Button>
              )}
            </Empty>
          </Card>
        )
      )}
    </PageFrame>
  );
}

function SiteExecution({ site, requestedPackage }: { site: Site; requestedPackage?: string }) {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const writable = principal.role !== "viewer" && site.status === "active";
  const [tab, setTab] = useState<ExecutionTab>(requestedPackage ? "publishing" : "connections");
  const [task, setTask] = useState<TaskSelection>();
  const [latest, setLatest] = useState<Job>();
  const [revision, setRevision] = useState(0);
  const query = useSeoQuery<{ items: Connection[] }>(
    tab === "connections" ? null : executionPath("connections", site.id, { limit: "200" }),
  );
  const { refresh } = query;
  const changed = useCallback(() => {
    refresh();
    setRevision((value) => value + 1);
  }, [refresh]);

  useGoogleReturn(owner, changed);
  const scope = { site, owner, writable, connections: query.data?.items ?? [], onTask: setTask, revision };

  return (
    <>
      {query.error !== undefined && <LoadError error={query.error} retry={refresh} />}
      {latest && <LatestExecutionJob key={latest.id} initial={latest} onSettled={changed} />}
      <Tabs value={tab} onValueChange={(value) => setTab(value as ExecutionTab)} className="min-w-0">
        <div className="max-w-full overflow-x-auto px-1 pb-1">
          <TabsList aria-label="站点执行视图">
            {tabs
              .filter(([id]) => id !== "usage" || owner)
              .map(([id, label]) => (
                <TabsTrigger key={id} value={id} className="px-3">
                  {label}
                </TabsTrigger>
              ))}
          </TabsList>
        </div>
        <TabsContent value="connections">
          <ConnectionsPanel {...scope} changed={changed} />
        </TabsContent>
        <TabsContent value="cms">
          <CMSContentPanel {...scope} />
        </TabsContent>
        <TabsContent value="publishing">
          <PublishingPanel {...scope} requestedPackage={requestedPackage} />
        </TabsContent>
        <TabsContent value="gsc">
          <AnalyticsPanel {...scope} kind="gsc" />
        </TabsContent>
        <TabsContent value="ga4">
          <AnalyticsPanel {...scope} kind="ga4" />
        </TabsContent>
        <TabsContent value="bing">
          <AnalyticsPanel {...scope} kind="bing" />
        </TabsContent>
        <TabsContent value="index">
          <IndexPanel {...scope} />
        </TabsContent>
        <TabsContent value="sitemaps">
          <SitemapPanel {...scope} />
        </TabsContent>
        <TabsContent value="extractions">
          <ExtractionPanel {...scope} />
        </TabsContent>
        <TabsContent value="feedback">
          <FeedbackPanel site={site} revision={revision} />
        </TabsContent>
        <TabsContent value="jobs">
          <ExecutionJobHistory site={site} owner={owner} writable={writable} revision={revision} />
        </TabsContent>
        {owner && (
          <TabsContent value="usage">
            <UsagePanel site={site} revision={revision} />
          </TabsContent>
        )}
      </Tabs>
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
