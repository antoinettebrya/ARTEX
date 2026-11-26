"use client";

import { useState } from "react";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { Filter, Plus, Search } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { isID, seoRequest, type Site } from "@/lib/seo/client";
import { initialMarket, type Market, marketLabel, type ResearchJob } from "@/lib/seo/research";

import { useSeoQuery } from "../query";
import { useSeoSession } from "../session";
import { ConfirmChange, field, Field, LoadError, PageFrame, PageSkeleton } from "../shared";
import { ResearchDetails } from "./details";
import { type JobDraft, MarketDialog, ResearchJobEditor, SitePageEditor } from "./forms";
import { type Detail, ResearchResults, type ResearchTab, researchTabs } from "./tables";

export function Research() {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const writable = principal.role !== "viewer";
  const sites = useSeoQuery<{ sites: Site[] }>("/sites?limit=200");
  const fromLink = useSearchParams().get("site") ?? "";
  const [selected, setSelected] = useState("");
  const [market, setMarket] = useState<Market>();
  const [search, setSearch] = useState("");
  const [tab, setTab] = useState<ResearchTab>("seeds");
  const [editMarket, setEditMarket] = useState(false);
  const [addPage, setAddPage] = useState(false);
  const [job, setJob] = useState<JobDraft>();
  const [detail, setDetail] = useState<Detail>();
  const [cancel, setCancel] = useState<ResearchJob>();
  const [version, setVersion] = useState(0);

  const available = sites.data?.sites ?? [];
  const candidate = selected || (isID(fromLink) ? fromLink : "") || available[0]?.id || "";
  const known = available.find((site) => site.id === candidate);
  const lookup = useSeoQuery<{ site: Site }>(candidate && !known ? `/sites/${candidate}` : null);
  const site = known ?? lookup.data?.site;
  const siteOptions = site && !known ? [site, ...available] : available;
  const defaultMarket = market ?? (site ? initialMarket(site) : undefined);
  const canStart = writable && site?.status === "active";
  const visible = researchTabs.filter(([id]) => owner || id !== "usage");
  const activeTab = visible.some(([id]) => id === tab) ? tab : "seeds";

  function refresh() {
    setVersion((value) => value + 1);
  }

  return (
    <PageFrame
      title="SEO 研究"
      description="按站点与市场跟踪关键词、自然排名和页面证据。"
      actions={
        canStart &&
        defaultMarket && (
          <Button size="sm" type="button" onClick={() => setJob({ kind: "keyword_discovery", market: defaultMarket })}>
            <Plus /> 创建研究任务
          </Button>
        )
      }
    >
      {sites.error !== undefined && <LoadError error={sites.error} retry={sites.refresh} />}
      {lookup.error !== undefined && <LoadError error={lookup.error} retry={lookup.refresh} />}
      {(sites.loading || lookup.loading) && <PageSkeleton />}
      {siteOptions.length > 0 && (
        <Card className="py-3">
          <CardContent className="flex flex-wrap items-end gap-3 px-3">
            <div className="min-w-40 max-w-full flex-1 sm:max-w-72">
              <Field id="research-site" label="研究站点">
                <NativeSelect
                  id="research-site"
                  className="w-full"
                  value={candidate}
                  onChange={(event) => {
                    setSelected(event.target.value);
                    setDetail(undefined);
                  }}
                >
                  {siteOptions.map((option) => (
                    <NativeSelectOption key={option.id} value={option.id}>
                      {option.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
            </div>
            {site && (
              <>
                <Badge variant="outline" className="max-w-full whitespace-normal">
                  {market ? marketLabel(market) : "全部市场"}
                </Badge>
                <Button size="sm" variant="outline" type="button" onClick={() => setEditMarket(true)}>
                  <Filter /> 筛选市场
                </Button>
                {market && (
                  <Button size="sm" variant="ghost" type="button" onClick={() => setMarket(undefined)}>
                    清除市场筛选
                  </Button>
                )}
                {site.status !== "active" && (
                  <Badge variant="outline">{site.status === "archived" ? "站点已归档" : "站点已暂停"}</Badge>
                )}
              </>
            )}
          </CardContent>
        </Card>
      )}
      <Tabs
        value={activeTab}
        onValueChange={(value) => {
          const match = visible.find(([id]) => id === value);
          if (match) setTab(match[0]);
        }}
      >
        <div className="min-w-0 overflow-x-auto pb-1">
          <TabsList aria-label="研究视图">
            {visible.map(([id, label]) => (
              <TabsTrigger key={id} value={id} className="px-3">
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        <TabsContent value={activeTab} className="min-w-0">
          {activeTab === "keywords" && site && (
            <form
              className="mb-3 flex flex-wrap items-center gap-2"
              onSubmit={(event) => {
                event.preventDefault();
                setSearch(field(new FormData(event.currentTarget), "q").trim());
              }}
            >
              <Input
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
          {site || activeTab === "usage" ? (
            <ResearchResults
              key={`${activeTab}/${version}`}
              tab={activeTab}
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
            !sites.loading &&
            !lookup.loading &&
            !sites.error &&
            !lookup.error && (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>请选择已登记站点</EmptyTitle>
                </EmptyHeader>
                <Button asChild size="sm" variant="outline">
                  <Link href="/seo/sites">查看站点</Link>
                </Button>
              </Empty>
            )
          )}
        </TabsContent>
      </Tabs>
      {site && defaultMarket && editMarket && (
        <MarketDialog market={defaultMarket} onClose={() => setEditMarket(false)} onApply={setMarket} />
      )}
      {site && canStart && job && (
        <ResearchJobEditor
          site={site}
          draft={job}
          owner={owner}
          onClose={() => setJob(undefined)}
          onSaved={() => {
            setTab("jobs");
            refresh();
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
