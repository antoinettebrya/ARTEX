"use client";

import { useState } from "react";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { ClipboardCheck, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { isID, type Site, seoRequest } from "@/lib/seo/client";
import {
  type ContentAction,
  type ContentProject,
  type ContentVersion,
  type ContentWorkspace,
  contentLabel,
  inventoryJobs,
} from "@/lib/seo/content";
import { marketLabel } from "@/lib/seo/research";

import { useSeoQuery } from "../query";
import { useSeoSession } from "../session";
import { ConfirmChange, Field, LoadError, PageFrame, PageSkeleton, StateBadge } from "../shared";
import { ArticleEditor, ArticlePreview, BriefEditor, BriefPreview, Outline } from "./article";
import { ApprovalEditor, ProjectEditor, RefreshEditor, SourceEditor, StageEditor, type StageSelection } from "./forms";
import { Inventory, TaskRecords } from "./inventory";
import {
  ContentPanel,
  EmptyContent,
  FactView,
  HistoryView,
  Overview,
  PublicationReview,
  QualityView,
  RefreshView,
  SourcesView,
  StageButton,
  StrategyView,
} from "./views";

const views = [
  ["overview", "项目概览"],
  ["strategy", "内容策略"],
  ["briefs", "内容简报"],
  ["sources", "研究来源"],
  ["article", "文章编辑"],
  ["fact-check", "事实核验"],
  ["quality", "质量审核"],
  ["history", "版本历史"],
  ["refresh", "内容更新"],
  ["inventory", "站点清单"],
] as const;

type ContentTab = (typeof views)[number][0];

export function ContentStudio() {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const writable = principal.role !== "viewer";
  const params = useSearchParams();
  const linkedSite = params.get("site") ?? "";
  const linkedKeyword = params.get("keyword")?.slice(0, 400);
  const sites = useSeoQuery<{ sites: Site[] }>("/sites?limit=200");
  const [chosenSite, chooseSite] = useState("");
  const [tab, setTab] = useState<ContentTab>("overview");
  const choices = sites.data?.sites ?? [];
  const siteID = chosenSite || (isID(linkedSite) ? linkedSite : (choices[0]?.id ?? ""));
  const listed = choices.find((site) => site.id === siteID);
  const siteQuery = useSeoQuery<{ site: Site }>(siteID && !listed ? `/sites/${siteID}` : null);
  const site = listed ?? siteQuery.data?.site;
  const options = site && !listed ? [site, ...choices] : choices;
  const [showProject, setShowProject] = useState(false);
  const [newSelection, setNewSelection] = useState<{ site: string; id: string }>();
  const selected = newSelection?.site === site?.id ? newSelection?.id : undefined;

  return (
    <PageFrame
      title="内容工作室"
      description="从站点清单和搜索证据制作、审核并管理内容版本。"
      actions={
        site?.status === "active" &&
        writable && (
          <Button type="button" size="sm" onClick={() => setShowProject(true)}>
            <Plus /> 创建内容项目
          </Button>
        )
      }
    >
      {sites.error !== undefined && <LoadError error={sites.error} retry={sites.refresh} />}
      {siteQuery.error !== undefined && <LoadError error={siteQuery.error} retry={siteQuery.refresh} />}
      {(sites.loading || siteQuery.loading) && <PageSkeleton />}
      {options.length > 0 && (
        <Card className="py-3">
          <CardContent className="px-3">
            <div className="max-w-full sm:max-w-72">
              <Field id="content-site" label="内容站点">
                <NativeSelect
                  id="content-site"
                  className="w-full"
                  value={siteID}
                  onChange={(event) => chooseSite(event.target.value)}
                >
                  {options.map((option) => (
                    <NativeSelectOption key={option.id} value={option.id}>
                      {option.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
            </div>
          </CardContent>
        </Card>
      )}
      {site ? (
        <SiteStudio
          key={site.id}
          site={site}
          owner={owner}
          writable={writable}
          tab={tab}
          setTab={setTab}
          createdProject={selected}
        />
      ) : (
        !sites.loading &&
        !siteQuery.loading &&
        !sites.error &&
        !siteQuery.error && (
          <ContentPanel title="请选择已登记站点">
            <Button asChild size="sm" variant="outline">
              <Link href="/seo/sites">查看站点</Link>
            </Button>
          </ContentPanel>
        )
      )}
      {site && writable && showProject && (
        <ProjectEditor
          site={site}
          keyword={linkedKeyword}
          onClose={() => setShowProject(false)}
          onSaved={(project) => {
            setNewSelection({ site: site.id, id: project.id });
            setTab("overview");
          }}
        />
      )}
    </PageFrame>
  );
}

function SiteStudio({
  site,
  owner,
  writable,
  tab,
  setTab,
  createdProject,
}: {
  site: Site;
  owner: boolean;
  writable: boolean;
  tab: ContentTab;
  setTab: (tab: ContentTab) => void;
  createdProject?: string;
}) {
  const projects = useSeoQuery<{ items: ContentProject[] }>(`/content/projects?site_id=${site.id}&limit=200`);
  const [projectSelection, setProjectSelection] = useState<{ created?: string; id: string }>();
  const selectedProject =
    (projectSelection?.created === createdProject ? projectSelection?.id : undefined) ??
    createdProject ??
    projects.data?.items?.[0]?.id;
  const query = useSeoQuery<{ workspace: ContentWorkspace }>(
    selectedProject && tab !== "inventory" ? `/content/projects/${selectedProject}` : null,
  );
  const [updated, setUpdated] = useState<ContentWorkspace>();
  const [stage, setStage] = useState<StageSelection>();
  const [sourceEditor, setSourceEditor] = useState(false);
  const [signalsEditor, setSignalsEditor] = useState(false);
  const [approval, setApproval] = useState(false);
  const [publication, setPublication] = useState(false);
  const [restore, setRestore] = useState<ContentVersion>();
  const [taskRevision, setTaskRevision] = useState(0);
  const active = writable && site.status === "active";
  const loaded = query.data?.workspace;
  const local = updated?.project.id === selectedProject && tab !== "inventory" ? updated : undefined;
  const workspace = local && local.project.revision >= (loaded?.project.revision ?? 0) ? local : loaded;
  const current = workspace?.project;
  const records =
    current && !projects.data?.items?.some(({ id }) => id === current.id)
      ? [current, ...(projects.data?.items ?? [])]
      : (projects.data?.items ?? []);

  async function save(action: ContentAction) {
    if (!workspace) return;
    const { workspace: result } = await seoRequest<{ workspace: ContentWorkspace }>(
      `/content/projects/${workspace.project.id}/actions`,
      {
        method: "POST",
        body: { ...action, revision: workspace.project.revision },
      },
    );
    setUpdated(result);
    query.refresh();
    toast.success(action.action === "prepare_publication" ? "CMS 发布包已准备" : "内容记录已保存");
  }

  function openStage(selection: StageSelection) {
    setStage(selection);
  }

  const canPrepare = owner && workspace?.quality?.passed && ["approved", "prepared"].includes(current?.status ?? "");
  const canApprove = owner && workspace?.quality?.passed && !canPrepare;
  return (
    <>
      {projects.error !== undefined && <LoadError error={projects.error} retry={projects.refresh} />}
      {tab !== "inventory" && records.length > 0 && (
        <Card className="py-3">
          <CardContent className="flex flex-wrap items-end justify-between gap-2 px-3">
            <div className="min-w-0 flex-1 sm:max-w-lg">
              <Field id="content-project" label="内容项目">
                <NativeSelect
                  id="content-project"
                  className="w-full"
                  value={selectedProject}
                  onChange={(event) => {
                    setProjectSelection({ created: createdProject, id: event.target.value });
                    projects.refresh();
                  }}
                >
                  {records.map((item) => (
                    <NativeSelectOption key={item.id} value={item.id}>
                      {item.keyword} · {marketLabel(item.market)}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
            </div>
            {current && (
              <div className="flex flex-wrap items-center gap-2">
                <StateBadge label={contentLabel(current.status)} />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    query.refresh();
                    projects.refresh();
                  }}
                >
                  <RefreshCw /> 刷新项目
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      )}
      <Tabs
        value={tab}
        onValueChange={(value) => {
          const selectedView = views.find(([id]) => id === value);
          if (!selectedView) return;
          setTab(selectedView[0]);
          query.refresh();
          projects.refresh();
        }}
      >
        <div className="min-w-0 overflow-x-auto pb-1">
          <TabsList aria-label="内容工作室视图">
            {views.map(([id, label]) => (
              <TabsTrigger key={id} value={id} className="px-3">
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        <TabsContent value={tab} className="grid min-w-0 gap-3">
          {tab === "inventory" ? (
            <>
              <Inventory key={taskRevision} site={site} owner={owner} canWrite={active} onStage={openStage} />
              <TaskRecords key={`inventory/${taskRevision}`} siteID={site.id} canWrite={active} inventory />
            </>
          ) : (
            <>
              {query.error !== undefined && <LoadError error={query.error} retry={query.refresh} />}
              {(projects.loading || query.loading) && !workspace && <PageSkeleton />}
              {!workspace && !projects.loading && !query.loading && !query.error && (
                <EmptyContent title="暂无内容项目" detail="可先在站点清单授权只读采集，也可为已有研究证据创建项目。" />
              )}
              {workspace && (
                <>
                  {tab === "overview" && <Overview workspace={workspace} onStage={openStage} canWrite={active} />}
                  {tab === "strategy" && (
                    <StrategyView
                      workspace={workspace}
                      action={active && <StageButton kind="content_strategy" onStage={openStage} />}
                    />
                  )}
                  {tab === "briefs" && (
                    <>
                      <ContentPanel
                        title="内容简报"
                        actions={
                          active && (
                            <StageButton
                              kind="content_brief"
                              onStage={openStage}
                              enabled={Boolean(workspace.strategy)}
                            />
                          )
                        }
                      >
                        {!workspace.brief ? (
                          <EmptyContent title="暂无内容简报" />
                        ) : (
                          <p className="text-muted-foreground text-sm">
                            简报 V{workspace.brief_version} · {workspace.brief.country_code} ·{" "}
                            {workspace.brief.language_code}
                          </p>
                        )}
                      </ContentPanel>
                      {workspace.brief &&
                        (active ? (
                          <BriefEditor
                            key={`${workspace.project.id}/${workspace.brief_version}`}
                            brief={workspace.brief}
                            onSave={save}
                          />
                        ) : (
                          <BriefPreview brief={workspace.brief} />
                        ))}
                    </>
                  )}
                  {tab === "sources" && (
                    <SourcesView
                      workspace={workspace}
                      onStage={openStage}
                      onAdd={() => setSourceEditor(true)}
                      canWrite={active}
                    />
                  )}
                  {tab === "article" && (
                    <>
                      <ContentPanel
                        title="文章大纲"
                        actions={
                          active && (
                            <>
                              <StageButton
                                kind="content_outline"
                                onStage={openStage}
                                enabled={Boolean(workspace.brief)}
                              />
                              <StageButton
                                kind="content_draft"
                                onStage={openStage}
                                enabled={Boolean(workspace.outline?.length)}
                              />
                              <StageButton
                                kind="content_seo_optimization"
                                onStage={openStage}
                                enabled={Boolean(workspace.article)}
                              />
                            </>
                          )
                        }
                      >
                        {workspace.outline?.length ? (
                          <Outline headings={workspace.outline} />
                        ) : (
                          <EmptyContent title="暂无大纲" />
                        )}
                      </ContentPanel>
                      {workspace.article &&
                        (active ? (
                          <ArticleEditor
                            key={`${workspace.project.id}/${workspace.article_version}`}
                            article={workspace.article}
                            onSave={save}
                          />
                        ) : (
                          <ContentPanel title={`文章 V${workspace.article_version}`}>
                            <ArticlePreview article={workspace.article} />
                          </ContentPanel>
                        ))}
                    </>
                  )}
                  {tab === "fact-check" && (
                    <FactView
                      workspace={workspace}
                      action={
                        active && (
                          <StageButton
                            kind="content_fact_check"
                            onStage={openStage}
                            enabled={Boolean(workspace.article)}
                          />
                        )
                      }
                    />
                  )}
                  {tab === "quality" && (
                    <QualityView
                      workspace={workspace}
                      actions={
                        active && (
                          <>
                            <StageButton
                              kind="content_quality_review"
                              onStage={openStage}
                              enabled={Boolean(workspace.fact_check)}
                            />
                            {canApprove && (
                              <Button size="sm" variant="outline" onClick={() => setApproval(true)}>
                                <ClipboardCheck /> 批准当前文章
                              </Button>
                            )}
                            {canPrepare && (
                              <Button size="sm" onClick={() => setPublication(true)}>
                                预览 CMS 发布包
                              </Button>
                            )}
                          </>
                        )
                      }
                    />
                  )}
                  {tab === "history" && (
                    <HistoryView
                      workspace={workspace}
                      owner={owner}
                      writable={active}
                      onStage={openStage}
                      onRestore={setRestore}
                    />
                  )}
                  {tab === "refresh" && (
                    <RefreshView
                      workspace={workspace}
                      onEdit={active ? () => setSignalsEditor(true) : undefined}
                      action={active && <StageButton kind="content_refresh" onStage={openStage} />}
                    />
                  )}
                  <TaskRecords
                    key={`${workspace.project.id}/${taskRevision}`}
                    siteID={site.id}
                    projectID={workspace.project.id}
                    canWrite={active}
                    onRefresh={query.refresh}
                  />
                </>
              )}
            </>
          )}
        </TabsContent>
      </Tabs>
      {stage && active && (inventoryJobs.includes(stage.kind) || current) && (
        <StageEditor
          selection={stage}
          site={site}
          project={current}
          onClose={() => setStage(undefined)}
          onSaved={() => {
            setTaskRevision((value) => value + 1);
            query.refresh();
          }}
        />
      )}
      {current && sourceEditor && active && <SourceEditor onClose={() => setSourceEditor(false)} onSave={save} />}
      {workspace && signalsEditor && active && (
        <RefreshEditor signals={workspace.refresh_signals} onClose={() => setSignalsEditor(false)} onSave={save} />
      )}
      {workspace && approval && canApprove && (
        <ApprovalEditor version={workspace.article_version} onClose={() => setApproval(false)} onSave={save} />
      )}
      {workspace && publication && canPrepare && (
        <PublicationReview workspace={workspace} onClose={() => setPublication(false)} onSave={save} />
      )}
      {workspace && restore && active && (
        <ConfirmChange
          title="恢复历史内容"
          description={`由 V${restore.number} 创建一个新版本并保留现有历史。`}
          onClose={() => setRestore(undefined)}
          onConfirm={() =>
            save({ action: restore.article ? "restore_article" : "restore_brief", version: restore.number })
          }
        />
      )}
    </>
  );
}
