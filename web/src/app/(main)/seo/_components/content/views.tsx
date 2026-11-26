"use client";

import { type ReactNode, useState } from "react";

import Link from "next/link";

import { ArrowRight, GitCompareArrows, History, Plus, RotateCcw } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  type ContentJobKind,
  type ContentVersion,
  type ContentWorkspace,
  contentActionLabel,
  contentJobLabels,
  contentLabel,
  sourceKinds,
} from "@/lib/seo/content";
import { marketLabel } from "@/lib/seo/research";
import { seoRoutes } from "@/lib/seo/routes";

import { useSeoLocation } from "../context";
import { Field, formatTime, StateBadge } from "../shared";
import {
  ArticlePreview,
  BriefPreview,
  DownloadPublication,
  EvidenceLink,
  type SubmitContent,
  TextList,
} from "./article";
import type { StageSelection } from "./forms";

export type StartStage = (selection: StageSelection) => void;

export function EmptyContent({ title, detail }: { title: string; detail?: string }) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{title}</EmptyTitle>
        {detail && <EmptyDescription>{detail}</EmptyDescription>}
      </EmptyHeader>
    </Empty>
  );
}

export function ContentPanel({
  title,
  actions,
  children,
}: {
  title: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Card className="min-w-0 gap-3 py-3">
      <CardHeader className="flex flex-wrap items-center justify-between gap-2 px-3">
        <CardTitle>{title}</CardTitle>
        {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
      </CardHeader>
      <CardContent className="min-w-0 px-3">{children}</CardContent>
    </Card>
  );
}

export function StageButton({
  kind,
  onStage,
  enabled = true,
  primary = false,
}: {
  kind: ContentJobKind;
  onStage: StartStage;
  enabled?: boolean;
  primary?: boolean;
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={primary ? "default" : "outline"}
      disabled={!enabled}
      onClick={() => onStage({ kind })}
    >
      {contentJobLabels[kind]}
    </Button>
  );
}

export function DataFact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="break-words font-medium">{children ?? "—"}</dd>
    </div>
  );
}

function observationCount(values?: readonly unknown[] | null) {
  return (values ?? []).length;
}

export function Overview({
  workspace,
  onStage,
  canWrite,
}: {
  workspace: ContentWorkspace;
  onStage: StartStage;
  canWrite: boolean;
}) {
  const keywords = workspace.evidence.keyword;
  const flow: {
    title: string;
    ready: boolean;
    canStart: boolean;
    kind: ContentJobKind;
  }[] = [
    { title: "依据关键词与市场生成策略", ready: Boolean(workspace.strategy), canStart: true, kind: "content_strategy" },
    {
      title: "保存对应语种的内容简报",
      ready: Boolean(workspace.brief),
      canStart: Boolean(workspace.strategy),
      kind: "content_brief",
    },
    {
      title: "采集可追溯的一手资料",
      ready: Boolean(workspace.facts?.length || workspace.sources?.some((source) => source.collected_at)),
      canStart: Boolean(workspace.brief),
      kind: "content_research",
    },
    {
      title: "生成分章节大纲",
      ready: Boolean(workspace.outline?.length),
      canStart: Boolean(workspace.brief),
      kind: "content_outline",
    },
    {
      title: "按大纲撰写章节",
      ready: Boolean(workspace.article),
      canStart: Boolean(workspace.outline?.length),
      kind: "content_draft",
    },
    {
      title: "核验事实引用",
      ready: Boolean(workspace.fact_check),
      canStart: Boolean(workspace.article),
      kind: "content_fact_check",
    },
    {
      title: "运行规则与独立模型审核",
      ready: Boolean(workspace.quality),
      canStart: Boolean(workspace.fact_check),
      kind: "content_quality_review",
    },
  ];
  const next = flow.find((item) => !item.ready && item.canStart);
  return (
    <>
      <ContentPanel title="项目证据">
        <dl className="grid gap-3 text-sm sm:grid-cols-2 xl:grid-cols-4">
          <DataFact label="主关键词">{workspace.project.keyword}</DataFact>
          <DataFact label="市场">{marketLabel(workspace.project.market)}</DataFact>
          <DataFact label="关键词搜索量">{keywords.metrics?.search_volume?.toLocaleString()}</DataFact>
          <DataFact label="自然 SERP 结果">{observationCount(keywords.serp)}</DataFact>
          <DataFact label="意图">{keywords.intent?.primary}</DataFact>
          <DataFact label="目标页">
            <EvidenceLink url={workspace.evidence.mapping?.target_url} />
          </DataFact>
          <DataFact label="机会评分">{workspace.evidence.opportunity?.score ?? "—"}</DataFact>
          <DataFact label="聚类数">{observationCount(workspace.evidence.clusters)}</DataFact>
        </dl>
      </ContentPanel>
      <ContentPanel
        title="内容生产进度"
        actions={canWrite && next && <StageButton kind={next.kind} onStage={onStage} primary />}
      >
        <Table aria-label="内容生产进度">
          <TableHeader>
            <TableRow>
              <TableHead scope="col">阶段</TableHead>
              <TableHead scope="col">状态</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {flow.map((item) => (
              <TableRow key={item.kind}>
                <TableCell className="whitespace-normal">{item.title}</TableCell>
                <TableCell>
                  <StateBadge label={item.ready ? "有结果" : "待运行"} tone={item.ready ? "secondary" : "outline"} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </ContentPanel>
    </>
  );
}

export function StrategyView({ workspace, action }: { workspace: ContentWorkspace; action?: ReactNode }) {
  const strategy = workspace.strategy;
  const { mapping, keyword, clusters, competitors, serp_snapshot_id: snapshot } = workspace.evidence;
  const results = keyword.serp ?? [];
  return (
    <>
      <ContentPanel title="内容策略" actions={action}>
        {strategy ? (
          <div className="space-y-3 text-sm">
            <dl className="grid gap-3 sm:grid-cols-2">
              <DataFact label="建议动作">{contentActionLabel(strategy.action)}</DataFact>
              <DataFact label="目标 URL">
                <EvidenceLink url={strategy.target_url} />
              </DataFact>
              <DataFact label="建议页面类型">{strategy.page_type}</DataFact>
              <DataFact label="Topic Cluster">{strategy.topic_cluster ? "建议建立" : "暂无建议"}</DataFact>
            </dl>
            <TextList label="决策理由" values={strategy.reasons} />
            <TextList label="判断证据" values={strategy.evidence} />
          </div>
        ) : (
          <EmptyContent title="暂无策略" detail="运行策略任务以读取 Phase 2 和站点清单。" />
        )}
      </ContentPanel>
      <ContentPanel title="Phase 2 证据">
        <div className="grid gap-3 text-sm sm:grid-cols-2">
          <div className="space-y-2">
            <dl className="space-y-2">
              <DataFact label="搜索意图">{keyword.intent?.primary}</DataFact>
              <DataFact label="搜索需求">{keyword.intent?.need}</DataFact>
              <DataFact label="目标页">
                <EvidenceLink url={mapping?.target_url} />
              </DataFact>
              <DataFact label="Canonical">{mapping?.canonical_issue ? "发现潜在问题" : "—"}</DataFact>
            </dl>
            <TextList label="蚕食候选 URL" values={mapping?.cannibalizing_urls} />
          </div>
          <div className="min-w-0 space-y-2">
            {snapshot && <p className="break-all font-mono text-muted-foreground text-xs">SERP {snapshot}</p>}
            <TextList label="关联聚类" values={clusters?.map((cluster) => cluster.primary_keyword)} />
            <TextList label="观测内容缺口" values={competitors?.flatMap((entry) => entry.gaps ?? [])} />
          </div>
        </div>
        {results.length > 0 && (
          <Table className="mt-3" aria-label="关联自然搜索结果">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">排名</TableHead>
                <TableHead scope="col">自然结果</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {results.map((row) => (
                <TableRow key={`${row.rank}/${row.url}`}>
                  <TableCell>{row.rank}</TableCell>
                  <TableCell className="whitespace-normal">
                    <EvidenceLink url={row.url} text={row.title} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </ContentPanel>
    </>
  );
}

export function SourcesView({
  workspace,
  onStage,
  onAdd,
  canWrite,
}: {
  workspace: ContentWorkspace;
  onStage: StartStage;
  onAdd: () => void;
  canWrite: boolean;
}) {
  const sources = workspace.sources ?? [];
  return (
    <>
      <ContentPanel
        title="研究来源"
        actions={
          canWrite && (
            <>
              <Button size="sm" type="button" variant="outline" onClick={onAdd}>
                <Plus /> 登记一手来源
              </Button>
              <StageButton kind="content_research" onStage={onStage} enabled={Boolean(workspace.brief)} />
            </>
          )
        }
      >
        {sources.length ? (
          <Table aria-label="研究来源">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">来源</TableHead>
                <TableHead scope="col">类型</TableHead>
                <TableHead scope="col">状态</TableHead>
                <TableHead scope="col">发表与采集时间（UTC）</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sources.map((source) => (
                <TableRow key={source.id ?? source.url}>
                  <TableCell className="max-w-80 whitespace-normal">
                    <EvidenceLink url={source.url} text={source.title} />
                    {source.serp_snapshot_id && (
                      <p className="break-all font-mono text-muted-foreground text-xs">
                        SERP {source.serp_snapshot_id}
                      </p>
                    )}
                  </TableCell>
                  <TableCell>{sourceKinds[source.kind] ?? source.kind}</TableCell>
                  <TableCell>
                    <StateBadge label={contentLabel(source.status ?? "pending")} />
                  </TableCell>
                  <TableCell className="space-y-1 text-xs tabular-nums">
                    {source.published_at && <p>发表 {formatTime(source.published_at)}</p>}
                    {source.collected_at ? <p>采集 {formatTime(source.collected_at)}</p> : "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyContent title="暂无采集来源" />
        )}
      </ContentPanel>
      <ContentPanel title="事实证据">
        {workspace.facts?.length ? (
          <div className="grid gap-3">
            {workspace.facts.map((fact) => (
              <section key={fact.id} className="space-y-2 rounded-md border p-3 text-sm">
                <div className="flex flex-wrap items-center gap-2">
                  <StateBadge
                    label={contentLabel(fact.status)}
                    tone={fact.status === "verified" ? "secondary" : "outline"}
                  />
                  <span className="text-muted-foreground text-xs">
                    可信度 {new Intl.NumberFormat("zh", { style: "percent" }).format(fact.confidence)}
                  </span>
                  <code className="break-all font-mono text-muted-foreground text-xs">ID {fact.id}</code>
                </div>
                <h3 className="break-words font-medium">{fact.statement}</h3>
                {(fact.evidence ?? []).map((part) => {
                  const source = sources.find((entry) => entry.id === part.source_id);
                  return (
                    <blockquote key={`${part.source_id}/${part.excerpt}`} className="space-y-1 border-s-2 ps-2">
                      <p className="break-words text-muted-foreground">{part.excerpt}</p>
                      <p className="text-xs">
                        <EvidenceLink url={source?.url} text={source?.title} />{" "}
                        {part.conflict && <StateBadge label="冲突" tone="destructive" />}
                      </p>
                    </blockquote>
                  );
                })}
              </section>
            ))}
          </div>
        ) : (
          <EmptyContent title="暂无事实证据" />
        )}
      </ContentPanel>
    </>
  );
}

export function FactView({ workspace, action }: { workspace: ContentWorkspace; action?: ReactNode }) {
  const report = workspace.fact_check;
  return (
    <ContentPanel title="事实核验" actions={action}>
      {!report ? (
        <EmptyContent title="暂无核验报告" detail="生成或保存文章版本后可开始核验。" />
      ) : (
        <div className="grid gap-3">
          <div>
            <StateBadge
              label={report.passed ? "事实核验通过" : "存在未解决的事实问题"}
              tone={report.passed ? "secondary" : "destructive"}
            />
          </div>
          <Table aria-label="事实核验结果">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">声明</TableHead>
                <TableHead scope="col">结果</TableHead>
                <TableHead scope="col">依据</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(report.claims ?? []).map((claim) => (
                <TableRow key={`${claim.text}/${claim.status}`}>
                  <TableCell className="max-w-80 whitespace-normal break-words">
                    {claim.text}
                    {claim.material && <p className="text-muted-foreground text-xs">重要事实</p>}
                  </TableCell>
                  <TableCell>{contentLabel(claim.status)}</TableCell>
                  <TableCell className="max-w-80 whitespace-normal break-words">
                    <p>{claim.reason}</p>
                    {(claim.fact_ids ?? []).map((id) => (
                      <code key={id} className="block break-all text-muted-foreground text-xs">
                        {id}
                      </code>
                    ))}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!report.claims?.length && <p className="text-muted-foreground text-sm">此版本未记录需核验的声明。</p>}
        </div>
      )}
    </ContentPanel>
  );
}

const qualityNames: Record<string, string> = {
  search_intent: "搜索意图",
  completeness: "内容完整度",
  original_value: "原创价值",
  fact_accuracy: "事实准确性",
  credibility: "信息可信度",
  topic_depth: "主题深度",
  entity_coverage: "实体覆盖",
  structure: "结构质量",
  readability: "可读性",
  duplicate_content: "内容重复",
  seo_metadata: "SEO",
  reader_value: "用户价值",
};
const reportNumber = new Intl.NumberFormat("zh", { maximumFractionDigits: 2 });

export function QualityView({ workspace, actions }: { workspace: ContentWorkspace; actions?: ReactNode }) {
  const quality = workspace.quality;
  const review = workspace.review;
  return (
    <>
      <ContentPanel title="质量审核" actions={actions}>
        {quality ? (
          <div className="grid gap-3 text-sm">
            <div className="flex flex-wrap items-center gap-3">
              <StateBadge
                label={quality.passed ? "已通过质量门禁" : "未通过质量门禁"}
                tone={quality.passed ? "secondary" : "destructive"}
              />
              <span className="font-medium tabular-nums">{reportNumber.format(quality.score)} / 100</span>
              <span className="text-muted-foreground">
                {quality.algorithm} · {formatTime(quality.reviewed_at)} UTC
              </span>
            </div>
            <Table aria-label="质量评分明细">
              <TableHeader>
                <TableRow>
                  <TableHead scope="col">维度</TableHead>
                  <TableHead scope="col">评分 / 权重</TableHead>
                  <TableHead scope="col">解释与原始指标</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(quality.dimensions ?? []).map((item) => (
                  <TableRow key={item.name}>
                    <TableCell>{qualityNames[item.name] ?? item.name}</TableCell>
                    <TableCell className="tabular-nums">
                      {reportNumber.format(item.score)} / {item.weight}
                    </TableCell>
                    <TableCell className="max-w-lg whitespace-normal break-words">
                      <p>{item.explanation}</p>
                      {Object.entries(item.metrics ?? {}).map(([name, value]) => (
                        <code key={name} className="me-2 text-muted-foreground text-xs">
                          {name} {reportNumber.format(value)}
                        </code>
                      ))}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <TextList label="质量问题" values={quality.issues} />
            <TextList label="修改建议" values={quality.suggestions} />
          </div>
        ) : (
          <EmptyContent title="暂无质量审核报告" detail="核验事实后可提交独立模型审核。" />
        )}
      </ContentPanel>
      {review && (
        <ContentPanel title="独立模型审核意见">
          <div className="grid gap-3 text-sm">
            <dl className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
              <DataFact label="审核模型">{review.model}</DataFact>
              <DataFact label="意图匹配">{review.intent ? "通过" : "待修改"}</DataFact>
              <DataFact label="本地语言质量">{review.language ? "通过" : "待修改"}</DataFact>
              <DataFact label="原创增量">{review.original_value ? "通过" : "待修改"}</DataFact>
            </dl>
            <TextList label="审核意见" values={review.issues} />
          </div>
        </ContentPanel>
      )}
    </>
  );
}

function versionKey(version: ContentVersion) {
  return `${version.article ? "article" : "brief"}-${version.number}`;
}

function versionText(version: ContentVersion) {
  if (version.article)
    return [
      version.article.title,
      version.article.description,
      version.article.h1,
      ...(version.article.sections ?? []).flatMap((section) => [`H${section.level} ${section.heading}`, section.body]),
    ]
      .filter(Boolean)
      .join("\n\n");
  if (!version.brief) return "";
  return [
    version.brief.title,
    version.brief.description,
    version.brief.need,
    ...(version.brief.headings ?? []).map((heading) => `H${heading.level} ${heading.heading}`),
    ...(version.brief.topics ?? []),
  ]
    .filter(Boolean)
    .join("\n\n");
}

function compareParts(before: ContentVersion, after: ContentVersion) {
  const left = versionText(before).split("\n\n");
  const right = versionText(after).split("\n\n");
  return {
    removed: left.filter((part) => !right.includes(part)),
    added: right.filter((part) => !left.includes(part)),
  };
}

function compareOptions(items: ContentVersion[], excluded?: string) {
  return items
    .filter((item) => versionKey(item) !== excluded)
    .map((item) => (
      <NativeSelectOption key={versionKey(item)} value={versionKey(item)}>
        {item.article ? "文章" : "简报"} V{item.number}
      </NativeSelectOption>
    ));
}

function CompareVersions({ records, onClose }: { records: ContentVersion[]; onClose: () => void }) {
  const type = records.filter((item) => item.article).length > 1 ? "article" : "brief";
  const defaultValues = records.filter((item) => versionKey(item).startsWith(type));
  const [beforeKey, setBefore] = useState(versionKey(defaultValues[1] ?? defaultValues[0]));
  const [afterKey, setAfter] = useState(versionKey(defaultValues[0]));
  const before = records.find((item) => versionKey(item) === beforeKey);
  const after = records.find((item) => versionKey(item) === afterKey);
  const allowed = records.filter((item) => Boolean(item.article) === Boolean(before?.article));
  const diff =
    before && after && Boolean(before.article) === Boolean(after.article) ? compareParts(before, after) : undefined;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>比较内容版本</DialogTitle>
          <DialogDescription>查看标题、摘要、段落和主题的变化。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field id="content-before" label="对照版本">
            <NativeSelect
              id="content-before"
              className="w-full"
              value={beforeKey}
              onChange={(event) => {
                const key = event.target.value;
                setBefore(key);
                const next = records.find(
                  (item) => Boolean(item.article) === key.startsWith("article") && versionKey(item) !== key,
                );
                if (next) setAfter(versionKey(next));
              }}
            >
              {compareOptions(records)}
            </NativeSelect>
          </Field>
          <Field id="content-after" label="比较版本">
            <NativeSelect
              id="content-after"
              className="w-full"
              value={afterKey}
              onChange={(event) => setAfter(event.target.value)}
            >
              {compareOptions(allowed, beforeKey)}
            </NativeSelect>
          </Field>
        </div>
        {diff ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1 rounded-md border p-3">
              <h3 className="font-medium">移除内容</h3>
              {diff.removed.length ? (
                <TextList label="旧版本" values={diff.removed} />
              ) : (
                <p className="text-muted-foreground">无移除内容</p>
              )}
            </div>
            <div className="space-y-1 rounded-md border p-3">
              <h3 className="font-medium">新增内容</h3>
              {diff.added.length ? (
                <TextList label="新版本" values={diff.added} />
              ) : (
                <p className="text-muted-foreground">无新增内容</p>
              )}
            </div>
          </div>
        ) : (
          <EmptyContent title="还没有同类版本可比较" />
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">关闭</Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function HistoryView({
  workspace,
  owner,
  writable,
  onStage,
  onRestore,
}: {
  workspace: ContentWorkspace;
  owner: boolean;
  writable: boolean;
  onStage: StartStage;
  onRestore: (version: ContentVersion) => void;
}) {
  const { href } = useSeoLocation();
  const history = workspace.history ?? [];
  const publications = workspace.publications ?? [];
  const [compare, setCompare] = useState(false);
  const [preview, setPreview] = useState<ContentVersion>();
  const canCompare =
    history.filter((item) => item.article).length > 1 || history.filter((item) => item.brief).length > 1;

  return (
    <>
      <ContentPanel
        title="版本历史"
        actions={
          canCompare && (
            <Button size="sm" variant="outline" onClick={() => setCompare(true)}>
              <GitCompareArrows /> 比较版本
            </Button>
          )
        }
      >
        {history.length ? (
          <Table aria-label="内容版本历史">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">版本</TableHead>
                <TableHead scope="col">更新时间（UTC）</TableHead>
                <TableHead scope="col">变更理由</TableHead>
                <TableHead scope="col">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.map((version) => (
                <TableRow key={versionKey(version)}>
                  <TableCell>
                    {version.article ? "文章" : "简报"} V{version.number}
                  </TableCell>
                  <TableCell className="tabular-nums">{formatTime(version.created_at)}</TableCell>
                  <TableCell className="max-w-64 whitespace-normal">
                    {contentJobLabels[version.reason as ContentJobKind] ?? version.reason}
                  </TableCell>
                  <TableCell>
                    <Button size="sm" variant="ghost" onClick={() => setPreview(version)}>
                      <History /> 查看版本
                    </Button>
                    {writable && (
                      <Button size="sm" variant="ghost" onClick={() => onRestore(version)}>
                        <RotateCcw /> 恢复为新版本
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyContent title="暂无内容版本" />
        )}
      </ContentPanel>
      <ContentPanel title="CMS 发布记录">
        {publications.length ? (
          <Table aria-label="CMS 发布记录">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">版本</TableHead>
                <TableHead scope="col">状态</TableHead>
                <TableHead scope="col">校验和与验证</TableHead>
                <TableHead scope="col">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {publications.map((publication, index) => (
                <TableRow key={publication.id}>
                  <TableCell>文章 V{publication.version}</TableCell>
                  <TableCell>
                    <StateBadge label={contentLabel(publication.status)} />
                  </TableCell>
                  <TableCell className="max-w-72 space-y-1 whitespace-normal">
                    <code className="block break-all font-mono text-xs">{publication.checksum}</code>
                    {publication.published_url && <EvidenceLink url={publication.published_url} />}
                    {publication.verified_at && (
                      <p className="text-muted-foreground text-xs">{formatTime(publication.verified_at)} UTC</p>
                    )}
                  </TableCell>
                  <TableCell>
                    {owner && (
                      <div className="flex flex-wrap items-center gap-2">
                        <DownloadPublication publication={publication} />
                        {writable &&
                          workspace.project.status === "prepared" &&
                          publication.version === workspace.article_version && (
                            <Button asChild size="sm" variant="outline">
                              <Link
                                href={href(seoRoutes.execution.publishing.path, {
                                  site: workspace.project.site_id,
                                  project: workspace.project.id,
                                  package: publication.id,
                                })}
                              >
                                <ArrowRight /> 前往站点发布
                              </Link>
                            </Button>
                          )}
                        {index === 0 && writable && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => onStage({ kind: "content_publish_verify", url: publication.published_url })}
                          >
                            <ArrowRight /> 验证发布页面
                          </Button>
                        )}
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyContent title="暂无发布包" />
        )}
      </ContentPanel>
      {compare && <CompareVersions records={history} onClose={() => setCompare(false)} />}
      {preview && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) setPreview(undefined);
          }}
        >
          <DialogContent className="sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>
                {preview.article ? "文章" : "简报"} V{preview.number}
              </DialogTitle>
              <DialogDescription>{formatTime(preview.created_at)} UTC</DialogDescription>
            </DialogHeader>
            {preview.article && <ArticlePreview article={preview.article} />}
            {preview.brief && <BriefPreview brief={preview.brief} />}
            <DialogFooter>
              <DialogClose asChild>
                <Button variant="outline">关闭</Button>
              </DialogClose>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}

export function RefreshView({
  workspace,
  onEdit,
  action,
}: {
  workspace: ContentWorkspace;
  onEdit?: () => void;
  action?: ReactNode;
}) {
  const signals = workspace.refresh_signals;
  const result = workspace.refresh;
  return (
    <>
      <ContentPanel
        title="内容更新"
        actions={
          <>
            {onEdit && (
              <Button size="sm" variant="outline" onClick={onEdit}>
                登记更新信号
              </Button>
            )}
            {action}
          </>
        }
      >
        {result ? (
          <div className="grid gap-3 text-sm">
            <dl className="grid gap-2 sm:grid-cols-2">
              <DataFact label="建议">{contentActionLabel(result.action)}</DataFact>
              <DataFact label="目标 URL">
                <EvidenceLink url={result.target_url} />
              </DataFact>
              <DataFact label="原页面快照">
                <code className="font-mono text-xs">{result.snapshot_id ?? "—"}</code>
              </DataFact>
            </dl>
            <TextList label="建议依据" values={result.reasons} />
          </div>
        ) : (
          <EmptyContent title="暂无更新建议" detail="分析任务将使用原页面快照、关键词映射和已登记更新信号。" />
        )}
      </ContentPanel>
      <ContentPanel title="已登记的信号">
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          <DataFact label="流量变动（%）">{signals.traffic_change}</DataFact>
          <DataFact label="排名名次变动">{signals.rank_change}</DataFact>
          <DataFact label="CTR 变动（%）">{signals.ctr_change}</DataFact>
          <DataFact label="其他变化">
            {[signals.source_changed && "一手来源", signals.serp_changed && "SERP", signals.intent_changed && "意图"]
              .filter(Boolean)
              .join(" · ") || "—"}
          </DataFact>
          <DataFact label="证据链接">
            <EvidenceLink url={signals.evidence_url} />
          </DataFact>
        </dl>
      </ContentPanel>
    </>
  );
}

export function PublicationReview({
  workspace,
  onSave,
  onClose,
}: {
  workspace: ContentWorkspace;
  onSave: SubmitContent;
  onClose: () => void;
}) {
  const [saving, setSaving] = useState(false);
  const [failed, setFailed] = useState(false);
  const article = workspace.article;
  if (!article) return null;
  async function prepare() {
    setSaving(true);
    setFailed(false);
    try {
      await onSave({ action: "prepare_publication" });
      onClose();
    } catch {
      setFailed(true);
    } finally {
      setSaving(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !saving) onClose();
      }}
    >
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>发布前最终预览</DialogTitle>
          <DialogDescription>确认文章 V{workspace.article_version} 的发布包内容。</DialogDescription>
        </DialogHeader>
        <ArticlePreview article={article} />
        {failed && (
          <Alert variant="destructive">
            <AlertTitle>无法准备发布包</AlertTitle>
            <AlertDescription>请刷新并确认当前版本的审核与批准状态。</AlertDescription>
          </Alert>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="outline" disabled={saving}>
              关闭
            </Button>
          </DialogClose>
          <Button type="button" disabled={saving} onClick={() => void prepare()}>
            准备 CMS 发布包
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
