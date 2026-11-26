"use client";

import { type ReactNode, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { isID } from "@/lib/seo/client";
import { type Market, marketLabel, type PageEvidence, type PageSource, researchJobLabels } from "@/lib/seo/research";

import { useSeoQuery } from "../query";
import { LoadError, PageSkeleton, RecordDetails } from "../shared";
import type { JobDraft } from "./forms";
import { type Detail, intentLabel, mappingLabel, percent, sourceLabel, time, URLText } from "./tables";

function EvidenceDialog({
  title,
  description,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  description: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className={wide ? "sm:max-w-4xl" : "sm:max-w-2xl"}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {children}
        <DialogClose asChild>
          <Button type="button" variant="outline" className="justify-self-end">
            关闭
          </Button>
        </DialogClose>
      </DialogContent>
    </Dialog>
  );
}

function EvidenceList({ label, values }: { label: string; values?: string[] | null }) {
  if (!values?.length) return null;
  return (
    <div className="space-y-1">
      <h3 className="font-medium">{label}</h3>
      <ul aria-label={label} className="list-disc space-y-1 pl-5 text-muted-foreground">
        {[...new Set(values)].map((value) => (
          <li key={value} className="break-words">
            {value}
          </li>
        ))}
      </ul>
    </div>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0 space-y-1">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="break-words font-medium">{children}</dd>
    </div>
  );
}

function collected(row: { market: Market; source?: string | null; collected_at?: string | null }) {
  const fields = [marketLabel(row.market)];
  if (row.source) fields.push(row.source);
  if (row.collected_at) fields.push(`${time(row.collected_at)} UTC`);
  return fields.join(" · ");
}

function isCompetitorURL(value: string, siteURL: string | undefined) {
  if (!siteURL) return false;
  try {
    const base = new URL(siteURL);
    const page = new URL(value);
    if (page.protocol !== "https:" && page.protocol !== "http:") return false;
    if (page.origin !== base.origin) return true;
    const prefix = decodeURIComponent(base.pathname).replace(/\/$/, "");
    const path = decodeURIComponent(page.pathname);
    return Boolean(prefix && path !== prefix && !path.startsWith(`${prefix}/`));
  } catch {
    return false;
  }
}

const scoreLabels: Record<string, string> = {
  demand: "搜索需求",
  difficulty: "竞争难度",
  intent_fit: "搜索意图匹配",
  site_relevance: "网站主题相关性",
  current_ranking: "当前排名基础",
  business_value: "商业价值",
};

function PageSections({ sections }: { sections: NonNullable<PageEvidence["sections"]> }) {
  return (
    <div className="space-y-1">
      <h3 className="font-medium">正文分块</h3>
      <Table aria-label="正文分块">
        <TableHeader>
          <TableRow>
            <TableHead scope="col">标题</TableHead>
            <TableHead scope="col">正文块</TableHead>
            <TableHead scope="col">辅助块</TableHead>
            <TableHead scope="col">表格数</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sections.map((section, index) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: This snapshot preserves source order and headings can repeat.
            <TableRow key={`${section.level}/${section.heading}/${index}`}>
              <TableCell className="min-w-36 whitespace-normal break-words">
                {section.level > 0 && <span className="mr-1 text-muted-foreground">H{section.level}</span>}
                {section.heading}
              </TableCell>
              <TableCell className="tabular-nums">{section.primary_blocks}</TableCell>
              <TableCell className="tabular-nums">{section.secondary_blocks}</TableCell>
              <TableCell className="tabular-nums">{section.tables}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function PageSources({ sources, onSnapshot }: { sources: PageSource[]; onSnapshot?: (id: string) => void }) {
  return (
    <div className="space-y-1">
      <h3 className="font-medium">采集来源</h3>
      <ul>
        {sources.map((source) => (
          <li
            key={source.snapshot_id}
            className="flex flex-wrap items-center justify-between gap-2 border-b py-1 text-muted-foreground last:border-0"
          >
            <span className="break-words" title={source.source}>
              {sourceLabel(source.source)} · {time(source.collected_at)} UTC
            </span>
            {onSnapshot && isID(source.snapshot_id) && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={`原始响应 ${sourceLabel(source.source)}`}
                onClick={() => onSnapshot(source.snapshot_id)}
              >
                原始响应
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function PageFacts({
  page,
  gaps,
  onSnapshot,
}: {
  page: PageEvidence;
  gaps?: string[];
  onSnapshot?: (id: string) => void;
}) {
  return (
    <div className="space-y-4 text-sm">
      <dl className="grid gap-3 sm:grid-cols-2">
        <Fact label="页面 URL">
          <URLText value={page.url} />
        </Fact>
        <Fact label="Canonical URL">
          <URLText value={page.canonical_url} />
        </Fact>
        <Fact label="页面类型">{page.page_type ? page.page_type : "—"}</Fact>
        <Fact label="已核实更新时间（UTC）">{time(page.last_modified)}</Fact>
      </dl>
      {page.description && <p>{page.description}</p>}
      <EvidenceList label="正文标题结构" values={page.headings?.map(({ level, text }) => `H${level} ${text}`)} />
      {page.sections && page.sections.length > 0 && <PageSections sections={page.sections} />}
      <EvidenceList label="覆盖主题" values={page.topics} />
      <EvidenceList label="实体" values={page.entities} />
      <EvidenceList label="FAQ" values={page.faq} />
      <EvidenceList label="Schema 类型" values={page.schema} />
      <EvidenceList label="待研究的内容缺口" values={gaps} />
      {page.source_snapshots && page.source_snapshots.length > 0 && (
        <PageSources sources={page.source_snapshots} onSnapshot={onSnapshot} />
      )}
    </div>
  );
}

function Snapshot({ id, onClose }: { id: string; onClose: () => void }) {
  const query = useSeoQuery<unknown>(isID(id) ? `/research/snapshots/${id}` : null);
  if (query.data !== undefined) return <RecordDetails title="Provider 原始响应" value={query.data} onClose={onClose} />;
  return (
    <EvidenceDialog title="Provider 原始响应" description="保存的调用记录和数据快照。" onClose={onClose}>
      {query.loading && <PageSkeleton />}
      {query.error !== undefined && <LoadError error={query.error} retry={query.refresh} />}
    </EvidenceDialog>
  );
}

export function ResearchDetails({
  detail,
  siteURL,
  owner,
  onClose,
  onJob,
}: {
  detail: Detail;
  siteURL?: string;
  owner: boolean;
  onClose: () => void;
  onJob?: (draft: JobDraft) => void;
}) {
  const [pageSnapshot, setPageSnapshot] = useState<string>();
  if (pageSnapshot) return <Snapshot id={pageSnapshot} onClose={() => setPageSnapshot(undefined)} />;
  switch (detail.kind) {
    case "snapshot":
      return <Snapshot id={detail.id} onClose={onClose} />;
    case "job":
      return (
        <RecordDetails
          title={researchJobLabels[detail.row.request.kind]}
          onClose={onClose}
          value={{
            state: detail.row.job.state,
            result: detail.row.job.result,
            error_code: detail.row.job.error_code,
            request: detail.row.request,
          }}
        />
      );
    case "serp": {
      const { row } = detail;
      return (
        <EvidenceDialog title={`${row.keyword} 的自然搜索结果`} description={collected(row)} onClose={onClose} wide>
          <Table aria-label="自然搜索结果">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">排名</TableHead>
                <TableHead scope="col">页面</TableHead>
                <TableHead scope="col">摘要</TableHead>
                {onJob && <TableHead scope="col">操作</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {(row.results ?? []).map((result) => (
                <TableRow key={`${result.rank}/${result.url}`}>
                  <TableCell className="tabular-nums">{result.rank}</TableCell>
                  <TableCell className="min-w-44">
                    <div className="whitespace-normal break-words font-medium">{result.title || result.domain}</div>
                    <URLText value={result.url} />
                    {result.page_type && <div className="text-muted-foreground text-xs">{result.page_type}</div>}
                  </TableCell>
                  <TableCell className="min-w-44 whitespace-normal break-words text-muted-foreground">
                    {result.description || "—"}
                  </TableCell>
                  {onJob && (
                    <TableCell>
                      {isCompetitorURL(result.url, siteURL) && (
                        <Button
                          variant="ghost"
                          size="sm"
                          type="button"
                          onClick={() =>
                            onJob({
                              kind: "competitor_analysis",
                              keyword: row.keyword,
                              market: row.market,
                              targetURL: result.url,
                            })
                          }
                        >
                          分析页面
                        </Button>
                      )}
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!row.results?.length && <p className="text-muted-foreground">Provider 未返回自然结果。</p>}
        </EvidenceDialog>
      );
    }
    case "competitor":
    case "page":
      return (
        <EvidenceDialog
          title={detail.row.page.title || "页面证据"}
          description={collected(detail.row)}
          onClose={onClose}
        >
          <PageFacts
            page={detail.row.page}
            gaps={detail.kind === "competitor" ? detail.row.gaps : undefined}
            onSnapshot={owner ? setPageSnapshot : undefined}
          />
        </EvidenceDialog>
      );
    case "cluster": {
      const { row } = detail;
      return (
        <EvidenceDialog title={`${row.primary_keyword} · 聚类依据`} description={collected(row)} onClose={onClose}>
          <dl className="grid gap-3 sm:grid-cols-2">
            <Fact label="推荐 URL">
              <URLText value={row.recommended_url} />
            </Fact>
            <Fact label="页面类型">{row.page_type || "—"}</Fact>
            <Fact label="置信度">{percent(row.confidence)}</Fact>
            <Fact label="聚类 ID">{row.id}</Fact>
          </dl>
          <EvidenceList label="次关键词" values={row.secondary_keywords} />
          <EvidenceList label="长尾关键词" values={row.long_tail_keywords} />
          <EvidenceList label="聚类理由" values={row.reasons} />
        </EvidenceDialog>
      );
    }
    case "intent": {
      const { row } = detail;
      return (
        <EvidenceDialog title={`${row.keyword} · 搜索意图`} description={collected(row)} onClose={onClose}>
          <dl className="grid gap-3 sm:grid-cols-2">
            <Fact label="主意图">{intentLabel(row.intent.primary)}</Fact>
            <Fact label="次意图">{intentLabel(row.intent.secondary)}</Fact>
            <Fact label="推荐页面类型">{row.intent.page_type ? row.intent.page_type : "—"}</Fact>
            <Fact label="置信度">{percent(row.intent.confidence)}</Fact>
          </dl>
          {row.intent.need && <p>{row.intent.need}</p>}
          <EvidenceList label="分类依据" values={row.intent.reasons} />
          <EvidenceList label="证据来源" values={row.intent.sources} />
        </EvidenceDialog>
      );
    }
    case "mapping": {
      const { row } = detail;
      return (
        <EvidenceDialog title={`${row.keyword} · 页面映射`} description={collected(row)} onClose={onClose}>
          <dl className="grid gap-3 sm:grid-cols-2">
            <Fact label="建议">{mappingLabel(row.mapping.action)}</Fact>
            <Fact label="目标 URL">
              <URLText value={row.mapping.target_url} />
            </Fact>
            <Fact label="Canonical 问题">{row.mapping.canonical_issue ? "需要核对" : "未发现问题"}</Fact>
          </dl>
          <EvidenceList label="可能存在蚕食的 URL" values={row.mapping.cannibalizing_urls} />
          <EvidenceList label="判断理由" values={row.mapping.reasons} />
          <EvidenceList label="页面与排名证据" values={row.mapping.evidence} />
        </EvidenceDialog>
      );
    }
    case "opportunity": {
      const { row } = detail;
      const evidence = row.opportunity.evidence_metrics;
      return (
        <EvidenceDialog title={`${row.keyword} · 评分依据`} description={collected(row)} onClose={onClose}>
          <p className="text-muted-foreground">机会分用于比较研究优先级。算法版本 {row.opportunity.version}。</p>
          {evidence && (
            <dl className="grid gap-3 sm:grid-cols-2">
              <Fact label="提供方月搜索量">{evidence.search_volume ?? "—"}</Fact>
              <Fact label="提供方自然搜索难度">{evidence.keyword_difficulty ?? "—"}</Fact>
            </dl>
          )}
          <Table aria-label="机会评分分项">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">维度</TableHead>
                <TableHead scope="col">权重</TableHead>
                <TableHead scope="col">得分</TableHead>
                <TableHead scope="col">证据</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {row.opportunity.parts.map((part) => (
                <TableRow key={part.name}>
                  <TableCell>{scoreLabels[part.name] ?? part.name}</TableCell>
                  <TableCell>{part.weight}</TableCell>
                  <TableCell>{part.points?.toFixed(1) ?? "—"}</TableCell>
                  <TableCell className="min-w-40 whitespace-normal break-words">{part.evidence}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </EvidenceDialog>
      );
    }
  }
}
