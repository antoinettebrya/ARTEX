"use client";

import { useState } from "react";

import Link from "next/link";
import { useRouter } from "next/navigation";

import { Filter, Plus, Search } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { seoRequest } from "@/lib/seo/client";
import { marketFilterChanges, readMarketFilter } from "@/lib/seo/market";
import { initialMarket, marketLabel, type ResearchJob } from "@/lib/seo/research";
import { type ResearchView, researchNavigation, seoRoutes, serpNavigation } from "@/lib/seo/routes";

import { SiteSelector, useSeoLocation, useSiteScope } from "../context";
import { useSeoSession } from "../session";
import { ConfirmChange, FilterBar, field, LoadError, PageFrame, PageSkeleton } from "../shared";
import { WorkflowNav } from "../workflow-nav";
import { ResearchDetails } from "./details";
import { type JobDraft, MarketDialog, ResearchJobEditor, SitePageEditor } from "./forms";
import { type Detail, ResearchResults } from "./tables";

const descriptions: Record<ResearchView, string> = {
  seeds: "登记种子词并跟踪关键词研究。",
  expansion: "查看关键词建议、相关搜索和自动补全结果。",
  keywords: "比较搜索量、难度和意图。",
  clusters: "查看关键词分组、推荐 URL 和聚类依据。",
  serps: "查看按市场采集的自然搜索排名。",
  competitors: "查看竞争页面的内容结构和搜索证据。",
  intents: "查看搜索意图和推荐页面类型。",
  mappings: "将关键词映射到本站 URL，核对页面证据。",
  opportunities: "查看关键词机会分数及各项得分依据。",
  jobs: "跟踪当前站点的研究任务。",
  usage: "查看组织的 DataForSEO 请求和费用记录。",
};

export function Research({ view }: { view: ResearchView }) {
  const scope = useSiteScope();
  return <ResearchWorkspace key={`${view}/${scope.site?.id ?? ""}`} view={view} scope={scope} />;
}

function ResearchWorkspace({ view, scope }: { view: ResearchView; scope: ReturnType<typeof useSiteScope> }) {
  const router = useRouter();
  const { params, href, update } = useSeoLocation();
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const writable = principal.role !== "viewer";
  const { site } = scope;
  const market = readMarketFilter(params);
  const search = params.get("q")?.slice(0, 400).trim() ?? "";
  const [editMarket, setEditMarket] = useState(false);
  const [addPage, setAddPage] = useState(false);
  const [job, setJob] = useState<JobDraft>();
  const [detail, setDetail] = useState<Detail>();
  const [cancel, setCancel] = useState<ResearchJob>();
  const [version, setVersion] = useState(0);
  const defaultMarket = market ?? (site ? initialMarket(site) : undefined);
  const canStart = writable && site?.status === "active";
  const marketFilters = view !== "jobs" && view !== "usage";
  const canReadResults = view === "usage" ? owner : site !== undefined;

  function refresh() {
    setVersion((value) => value + 1);
  }

  return (
    <PageFrame
      title={seoRoutes.research[view].title}
      description={descriptions[view]}
      actions={
        view !== "usage" &&
        canStart &&
        defaultMarket && (
          <Button
            size="sm"
            type="button"
            onClick={() =>
              setJob({
                kind: view === "expansion" ? "keyword_suggestions" : "keyword_discovery",
                market: defaultMarket,
              })
            }
          >
            <Plus /> 创建研究任务
          </Button>
        )
      }
    >
      {["seeds", "keywords", "expansion", "jobs", "usage"].includes(view) && (
        <WorkflowNav label="研究工作流" pages={researchNavigation} />
      )}
      {(view === "serps" || view === "competitors") && <WorkflowNav label="竞争研究" pages={serpNavigation} />}
      {view !== "usage" && scope.error !== undefined && <LoadError error={scope.error} retry={scope.refresh} />}
      {view !== "usage" && scope.loading && <PageSkeleton />}
      {view !== "usage" && scope.options.length > 0 && (
        <FilterBar>
          <SiteSelector id="research-site" label="研究站点" scope={scope} />
          {site && marketFilters && (
            <>
              <Badge variant="outline" className="max-w-full whitespace-normal">
                {market ? marketLabel(market) : "全部市场"}
              </Badge>
              <Button size="sm" variant="outline" type="button" onClick={() => setEditMarket(true)}>
                <Filter /> 筛选市场
              </Button>
              {market && (
                <Button size="sm" variant="ghost" type="button" onClick={() => update(marketFilterChanges())}>
                  清除市场筛选
                </Button>
              )}
            </>
          )}
          {site && site.status !== "active" && (
            <Badge variant="outline">{site.status === "archived" ? "站点已归档" : "站点已暂停"}</Badge>
          )}
        </FilterBar>
      )}
      {view === "keywords" && site && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            update({ site: site.id, q: field(new FormData(event.currentTarget), "q").trim() || null });
          }}
        >
          <Input
            key={search}
            type="search"
            name="q"
            aria-label="筛选关键词"
            maxLength={400}
            className="w-52 max-w-full"
            defaultValue={search}
          />
          <Button size="sm" variant="outline" type="submit">
            <Search /> 筛选关键词
          </Button>
        </form>
      )}
      {canReadResults ? (
        <ResearchResults
          key={version}
          view={view}
          site={site?.id ?? ""}
          market={market}
          search={search}
          writable={writable}
          canStart={canStart}
          owner={owner}
          onDetail={setDetail}
          onJob={(kind, keyword, selectedMarket) => setJob({ kind, keyword, market: selectedMarket })}
          onCancel={setCancel}
          onAddPage={() => setAddPage(true)}
        />
      ) : (
        view !== "usage" &&
        !scope.loading &&
        scope.error === undefined && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>请选择已登记站点</EmptyTitle>
            </EmptyHeader>
            <Button asChild size="sm" variant="outline">
              <Link href={href(seoRoutes.sites.list.path)}>查看站点</Link>
            </Button>
          </Empty>
        )
      )}
      {site && defaultMarket && editMarket && (
        <MarketDialog
          market={defaultMarket}
          onClose={() => setEditMarket(false)}
          onApply={(nextMarket) => update({ site: site.id, ...marketFilterChanges(nextMarket) })}
        />
      )}
      {site && canStart && job && (
        <ResearchJobEditor
          site={site}
          draft={job}
          owner={owner}
          onClose={() => setJob(undefined)}
          onSaved={() => {
            refresh();
            router.push(
              href(seoRoutes.research.jobs.path, { site: site.id, ...marketFilterChanges(market), q: search || null }),
            );
          }}
        />
      )}
      {site && defaultMarket && writable && addPage && (
        <SitePageEditor site={site} market={defaultMarket} onClose={() => setAddPage(false)} onSaved={refresh} />
      )}
      {detail && (
        <ResearchDetails
          detail={detail}
          siteURL={site?.base_url}
          owner={owner}
          onClose={() => setDetail(undefined)}
          onJob={
            canStart
              ? (draft) => {
                  setDetail(undefined);
                  setJob(draft);
                }
              : undefined
          }
        />
      )}
      {cancel && (
        <ConfirmChange
          title="取消研究任务"
          description="取消后不再重试。已发送的计费请求仍可能产生费用。"
          onClose={() => setCancel(undefined)}
          onConfirm={async () => {
            await seoRequest(`/jobs/${cancel.job.id}`, { method: "DELETE" });
            refresh();
            toast.success("研究任务已取消");
          }}
        />
      )}
    </PageFrame>
  );
}
