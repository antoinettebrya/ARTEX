"use client";

import { useState } from "react";

import Link from "next/link";
import { useRouter } from "next/navigation";

import { ClipboardCheck, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { type Site, seoRequest } from "@/lib/seo/client";
import {
  type ContentAction,
  type ContentProject,
  type ContentVersion,
  type ContentWorkspace,
  contentLabel,
} from "@/lib/seo/content";
import { marketLabel } from "@/lib/seo/research";
import { type ContentView, seoRoutes } from "@/lib/seo/routes";

import { ProjectSelector, SiteSelector, useSeoLocation, useSiteScope } from "../context";
import { useSeoQuery } from "../query";
import { useSeoSession } from "../session";
import {
  ConfirmChange,
  FilterBar,
  formatTime,
  LoadError,
  PageFrame,
  PageSkeleton,
  RecordsTable,
  StateBadge,
  useSeoRecords,
} from "../shared";
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

const descriptions: Record<ContentView, string> = {
  overview: "按站点管理内容项目，查看项目的市场、研究证据和进度。",
  strategy: "根据搜索证据和站点需求确定页面策略。",
  briefs: "查看和编辑用于写作的内容简报。",
  sources: "维护一手资料与支持文章论点的证据。",
  article: "生成大纲、编辑文章并保存版本。",
  "fact-check": "查阅文章事实与对应的来源。",
  quality: "查看质量门禁，审批当前文章并准备发布包。",
  history: "比较和恢复内容版本，查看 CMS 发布记录。",
  refresh: "依据已记录的更新信号分析旧文优化机会。",
  inventory: "管理只读采集范围，查看已发现的站点页面。",
};

const projectShortcuts = ["overview", "article", "quality", "history"] as const;

export function ContentStudio({ view }: { view: ContentView }) {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const writable = principal.role !== "viewer";
  const router = useRouter();
  const { params, projectID, href } = useSeoLocation();
  const scope = useSiteScope();
  const { site } = scope;
  const [editingSite, setEditingSite] = useState<string>();
  const [projectRevision, setProjectRevision] = useState(0);
  const linkedKeyword = params.get("keyword")?.slice(0, 400);
  const active = writable && site?.status === "active";

  return (
    <PageFrame
      title={seoRoutes.content[view].title}
      description={descriptions[view]}
      actions={
        active && (
          <Button type="button" size="sm" onClick={() => setEditingSite(site.id)}>
            <Plus /> 创建内容项目
          </Button>
        )
      }
    >
      {scope.error !== undefined && <LoadError error={scope.error} retry={scope.refresh} />}
      {scope.loading && <PageSkeleton />}
      {scope.options.length > 0 && (
        <FilterBar>
          <SiteSelector id="content-site" label="内容站点" scope={scope} />
        </FilterBar>
      )}
      {site && view === "inventory" && <SiteInventory key={site.id} site={site} owner={owner} writable={active} />}
      {site && view !== "inventory" && (
        <SiteProjects
          key={`${site.id}/${projectRevision}`}
          site={site}
          owner={owner}
          writable={active}
          view={view}
          preferredProject={projectID}
        />
      )}
      {!site && !scope.loading && !scope.error && (
        <ContentPanel title="请选择已登记站点">
          <Button asChild size="sm" variant="outline">
            <Link href={seoRoutes.sites.list.path}>查看站点</Link>
          </Button>
        </ContentPanel>
      )}
      {site && active && editingSite === site.id && (
        <ProjectEditor
          site={site}
          keyword={linkedKeyword}
          onClose={() => setEditingSite(undefined)}
          onSaved={(project) => {
            setProjectRevision((value) => value + 1);
            router.push(href(seoRoutes.content.overview.path, { site: site.id, project: project.id }));
          }}
        />
      )}
    </PageFrame>
  );
}

function SiteInventory({ site, owner, writable }: { site: Site; owner: boolean; writable: boolean }) {
  const [stage, setStage] = useState<StageSelection>();
  const [revision, setRevision] = useState(0);
  return (
    <>
      <Inventory key={revision} site={site} owner={owner} canWrite={writable} onStage={setStage} />
      <TaskRecords key={`inventory/${revision}`} siteID={site.id} canWrite={writable} inventory />
      {stage && writable && (
        <StageEditor
          selection={stage}
          site={site}
          onClose={() => setStage(undefined)}
          onSaved={() => setRevision((value) => value + 1)}
        />
      )}
    </>
  );
}

function SiteProjects({
  site,
  owner,
  writable,
  view,
  preferredProject,
}: {
  site: Site;
  owner: boolean;
  writable: boolean;
  view: Exclude<ContentView, "inventory">;
  preferredProject?: string;
}) {
  const { href } = useSeoLocation();
  const path = `/content/projects?site_id=${site.id}`;
  const overview = view === "overview";
  const list = useSeoRecords<ContentProject>(overview ? path : null, "items");
  const selector = useSeoQuery<{ items: ContentProject[] }>(overview ? null : `${path}&limit=200`);
  const projects = overview ? list : selector;
  const records = overview ? list.records : (selector.data?.items ?? []);
  const selectedProject = preferredProject ?? records[0]?.id;

  return (
    <>
      {overview ? (
        <RecordsTable
          label="内容项目列表"
          columns={["主关键词", "市场", "状态", "最近更新（UTC）"]}
          empty="暂无内容项目"
          list={list}
        >
          {list.records.map((item) => (
            <TableRow key={item.id}>
              <TableCell className="max-w-72 whitespace-normal break-words px-3 py-2">
                <Link
                  href={href(seoRoutes.content.overview.path, { site: site.id, project: item.id })}
                  aria-current={selectedProject === item.id ? "true" : undefined}
                  className="font-medium underline underline-offset-2"
                  scroll={false}
                >
                  {item.keyword}
                </Link>
              </TableCell>
              <TableCell className="px-3">{marketLabel(item.market)}</TableCell>
              <TableCell className="px-3">
                <StateBadge label={contentLabel(item.status)} />
              </TableCell>
              <TableCell className="px-3 tabular-nums">{formatTime(item.updated_at)}</TableCell>
            </TableRow>
          ))}
        </RecordsTable>
      ) : (
        <>
          {selector.error !== undefined && <LoadError error={selector.error} retry={selector.refresh} />}
          {!selectedProject && selector.loading && <PageSkeleton />}
          {!selectedProject && !selector.loading && !selector.error && (
            <EmptyContent title="暂无内容项目" detail="为已有研究证据创建项目后，可在此选择项目。" />
          )}
        </>
      )}
      {selectedProject && (
        <ProjectWorkspace
          key={selectedProject}
          projectID={selectedProject}
          site={site}
          owner={owner}
          active={writable}
          view={view}
          records={records}
          refreshProjects={projects.refresh}
        />
      )}
    </>
  );
}

function ProjectWorkspace({
  projectID,
  site,
  owner,
  active,
  view,
  records,
  refreshProjects,
}: {
  projectID: string;
  site: Site;
  owner: boolean;
  active: boolean;
  view: Exclude<ContentView, "inventory">;
  records: ContentProject[];
  refreshProjects: () => void;
}) {
  const { href, update } = useSeoLocation();
  const query = useSeoQuery<{ workspace: ContentWorkspace }>(`/content/projects/${projectID}`);
  const [updated, setUpdated] = useState<ContentWorkspace>();
  const [stage, setStage] = useState<StageSelection>();
  const [sourceEditor, setSourceEditor] = useState(false);
  const [signalsEditor, setSignalsEditor] = useState(false);
  const [approval, setApproval] = useState(false);
  const [publication, setPublication] = useState(false);
  const [restore, setRestore] = useState<ContentVersion>();
  const [taskRevision, setTaskRevision] = useState(0);
  const loaded = query.data?.workspace.project.site_id === site.id ? query.data.workspace : undefined;
  const workspace = updated && updated.project.revision >= (loaded?.project.revision ?? 0) ? updated : loaded;
  const current = workspace?.project;
  const options = current && !records.some(({ id }) => id === current.id) ? [current, ...records] : records;

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
    refreshProjects();
    toast.success(action.action === "prepare_publication" ? "CMS 发布包已准备" : "内容记录已保存");
  }

  const canPrepare = owner && workspace?.quality?.passed && ["approved", "prepared"].includes(current?.status ?? "");
  const canApprove = owner && workspace?.quality?.passed && !canPrepare;

  return (
    <>
      {options.length > 0 && (
        <FilterBar>
          <ProjectSelector
            id="content-project"
            label="内容项目"
            options={options.map((item) => ({ id: item.id, label: `${item.keyword} · ${marketLabel(item.market)}` }))}
            value={options.some((option) => option.id === projectID) ? projectID : undefined}
            onChange={(project) => update({ site: site.id, project })}
          />
          {current && (
            <div className="flex flex-wrap items-center gap-2">
              <StateBadge label={contentLabel(current.status)} />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  query.refresh();
                  refreshProjects();
                }}
              >
                <RefreshCw /> 刷新项目
              </Button>
            </div>
          )}
        </FilterBar>
      )}
      {query.error !== undefined && <LoadError error={query.error} retry={query.refresh} />}
      {query.loading && !workspace && <PageSkeleton />}
      {query.data && !workspace && (
        <ContentPanel title="当前站点无法读取所选项目">
          <Button asChild size="sm" variant="outline">
            <Link href={href(seoRoutes.content.overview.path, { site: site.id, project: null })}>返回本站项目</Link>
          </Button>
        </ContentPanel>
      )}
      {workspace && (
        <div className="grid min-w-0 gap-3">
          <nav aria-label="项目快捷导航" className="flex flex-wrap items-center gap-1">
            {projectShortcuts.map((target) => (
              <Button asChild key={target} size="sm" variant={target === view ? "secondary" : "ghost"}>
                <Link
                  href={href(seoRoutes.content[target].path, { site: site.id, project: projectID })}
                  aria-current={target === view ? "page" : undefined}
                >
                  {seoRoutes.content[target].title}
                </Link>
              </Button>
            ))}
          </nav>
          {view === "overview" && <Overview workspace={workspace} onStage={setStage} canWrite={active} />}
          {view === "strategy" && (
            <StrategyView
              workspace={workspace}
              action={active && <StageButton kind="content_strategy" onStage={setStage} />}
            />
          )}
          {view === "briefs" && (
            <>
              <ContentPanel
                title="内容简报"
                actions={
                  active && (
                    <StageButton kind="content_brief" onStage={setStage} enabled={Boolean(workspace.strategy)} />
                  )
                }
              >
                {!workspace.brief ? (
                  <EmptyContent title="暂无内容简报" />
                ) : (
                  <p className="text-muted-foreground text-sm">
                    简报 V{workspace.brief_version} · {workspace.brief.country_code} · {workspace.brief.language_code}
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
          {view === "sources" && (
            <SourcesView
              workspace={workspace}
              onStage={setStage}
              onAdd={() => setSourceEditor(true)}
              canWrite={active}
            />
          )}
          {view === "article" && (
            <>
              <ContentPanel
                title="文章大纲"
                actions={
                  active && (
                    <>
                      <StageButton kind="content_outline" onStage={setStage} enabled={Boolean(workspace.brief)} />
                      <StageButton
                        kind="content_draft"
                        onStage={setStage}
                        enabled={Boolean(workspace.outline?.length)}
                      />
                      <StageButton
                        kind="content_seo_optimization"
                        onStage={setStage}
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
          {view === "fact-check" && (
            <FactView
              workspace={workspace}
              action={
                active && (
                  <StageButton kind="content_fact_check" onStage={setStage} enabled={Boolean(workspace.article)} />
                )
              }
            />
          )}
          {view === "quality" && (
            <QualityView
              workspace={workspace}
              actions={
                active && (
                  <>
                    <StageButton
                      kind="content_quality_review"
                      onStage={setStage}
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
          {view === "history" && (
            <HistoryView
              workspace={workspace}
              owner={owner}
              writable={active}
              onStage={setStage}
              onRestore={setRestore}
            />
          )}
          {view === "refresh" && (
            <RefreshView
              workspace={workspace}
              onEdit={active ? () => setSignalsEditor(true) : undefined}
              action={active && <StageButton kind="content_refresh" onStage={setStage} />}
            />
          )}
          <TaskRecords
            key={`${workspace.project.id}/${taskRevision}`}
            siteID={site.id}
            projectID={workspace.project.id}
            canWrite={active}
            onRefresh={query.refresh}
          />
        </div>
      )}
      {stage && current && active && (
        <StageEditor
          selection={stage}
          site={site}
          project={current}
          onClose={() => setStage(undefined)}
          onSaved={() => {
            setTaskRevision((value) => value + 1);
            query.refresh();
            refreshProjects();
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
