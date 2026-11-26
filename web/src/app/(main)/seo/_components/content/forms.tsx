"use client";

import { useState } from "react";

import Link from "next/link";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { type Job, type Provider, type Site, seoRequest } from "@/lib/seo/client";
import {
  type ContentAction,
  type ContentJobInput,
  type ContentJobKind,
  type ContentModelProfile,
  type ContentProject,
  type CrawlPolicy,
  contentJobLabels,
  contentLines,
  inventoryJobs,
  modelJobs,
  type RefreshSignals,
  sourceKinds,
} from "@/lib/seo/content";
import { initialMarket } from "@/lib/seo/research";

import { useSeoQuery } from "../query";
import { MarketFields, readMarket } from "../research/forms";
import { Field, FormDialog, field, LoadError, PageSkeleton } from "../shared";

type Close = { onClose: () => void };
type SubmitAction = (action: ContentAction) => Promise<void>;

export function ProjectEditor({
  site,
  onClose,
  onSaved,
  keyword,
}: Close & { site: Site; onSaved: (project: ContentProject) => void; keyword?: string }) {
  return (
    <FormDialog
      title="创建内容项目"
      description="选择已有 Phase 2 证据的关键词和市场。不同市场使用独立的项目与版本。"
      submitLabel="创建项目"
      onClose={onClose}
      onSubmit={async (data) => {
        const { project } = await seoRequest<{ project: ContentProject }>("/content/projects", {
          method: "POST",
          body: { site_id: site.id, keyword: field(data, "keyword").trim(), market: readMarket(data) },
        });
        onSaved(project);
        toast.success("内容项目已创建");
      }}
    >
      <div className="sm:col-span-2">
        <Field id="content-project-keyword" label="主关键词">
          <Input id="content-project-keyword" name="keyword" maxLength={400} defaultValue={keyword} required />
        </Field>
      </div>
      <MarketFields market={initialMarket(site)} />
    </FormDialog>
  );
}

export type StageSelection = { kind: ContentJobKind; url?: string };

export function StageEditor({
  selection,
  site,
  project,
  onClose,
  onSaved,
}: Close & {
  selection: StageSelection;
  site: Site;
  project?: ContentProject;
  onSaved: (job: Job) => void;
}) {
  const { kind } = selection;
  const [idempotencyKey] = useState(() => crypto.randomUUID());
  const requiresModel = modelJobs.includes(kind);
  const urls = kind === "page_extraction" || kind === "content_publish_verify";
  const providers = useSeoQuery<{ providers: Provider[] }>(requiresModel ? "/providers?limit=200" : null);
  const profiles = useSeoQuery<{ items: ContentModelProfile[] }>(requiresModel ? "/content/models" : null);
  const requiredRole = kind === "content_quality_review" ? "reviewer" : "writer";
  const models = (providers.data?.providers ?? []).filter(
    (provider) =>
      provider.enabled &&
      provider.kind !== "dataforseo" &&
      (profiles.data?.items ?? []).some(
        (profile) => profile.provider_id === provider.id && [requiredRole, "any"].includes(profile.role),
      ),
  );

  async function submit(data: FormData) {
    const request: ContentJobInput = {
      site_id: site.id,
      project_id: inventoryJobs.includes(kind) ? undefined : project?.id,
      kind,
      url: urls ? field(data, "url").trim() : undefined,
      provider_id: requiresModel ? field(data, "provider_id") : undefined,
      priority: Number(field(data, "priority")),
      idempotency_key: idempotencyKey,
    };
    const { job } = await seoRequest<{ job: Job }>("/content/jobs", { method: "POST", body: request });
    onSaved(job);
    toast.success("内容任务已提交");
  }

  return (
    <FormDialog
      title={contentJobLabels[kind]}
      description={inventoryJobs.includes(kind) ? `站点 · ${site.name}` : `项目 · ${project?.keyword ?? site.name}`}
      submitLabel="提交任务"
      onSubmit={submit}
      onClose={onClose}
    >
      <Field id="content-stage-priority" label="优先级（0–100）">
        <Input id="content-stage-priority" name="priority" type="number" min={0} max={100} defaultValue={0} required />
      </Field>
      {urls && (
        <div className="sm:col-span-2">
          <Field id="content-stage-url" label="页面 URL">
            <Input
              id="content-stage-url"
              name="url"
              type="url"
              maxLength={4096}
              defaultValue={selection.url}
              required
            />
          </Field>
        </div>
      )}
      {requiresModel && (
        <div className="sm:col-span-2">
          <Field id="content-stage-provider" label="模型 Provider">
            <NativeSelect id="content-stage-provider" name="provider_id" className="w-full" required defaultValue="">
              <NativeSelectOption value="" disabled>
                {providers.loading || profiles.loading ? "正在读取模型" : "选择已配置角色的模型"}
              </NativeSelectOption>
              {models.map((provider) => (
                <NativeSelectOption key={provider.id} value={provider.id}>
                  {provider.name} · {provider.model}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
          {providers.error !== undefined && <LoadError error={providers.error} retry={providers.refresh} />}
          {profiles.error !== undefined && <LoadError error={profiles.error} retry={profiles.refresh} />}
          {!providers.loading && !profiles.loading && models.length === 0 && (
            <Button asChild variant="link" size="sm" className="px-0">
              <Link href="/seo/providers">查看内容模型配置</Link>
            </Button>
          )}
        </div>
      )}
    </FormDialog>
  );
}

export function CrawlEditor({
  site,
  policy,
  onClose,
  onSaved,
}: Close & { site: Site; policy: CrawlPolicy | null; onSaved: () => void }) {
  const [connector, setConnector] = useState<CrawlPolicy["connector"]>(policy?.connector ?? "sitemap");
  return (
    <FormDialog
      title="配置只读采集"
      description="确认有权读取此站点。本站页面与以下来源的授权范围分别保留。"
      submitLabel="保存采集授权"
      onClose={onClose}
      onSubmit={async (data) => {
        await seoRequest<{ policy: CrawlPolicy }>(`/content/crawl?site_id=${site.id}`, {
          method: "PUT",
          body: {
            site_id: site.id,
            enabled: data.has("enabled"),
            sitemap_url: field(data, "sitemap_url").trim() || undefined,
            connector,
            connector_url: field(data, "connector_url").trim() || undefined,
            source_origins: contentLines(field(data, "source_origins")),
            max_pages: Number(field(data, "max_pages")),
          } satisfies CrawlPolicy,
        });
        onSaved();
        toast.success("采集授权已保存");
      }}
    >
      <div className="flex items-center gap-2 sm:col-span-2">
        <Switch id="content-crawl-enable" name="enabled" defaultChecked={policy?.enabled ?? false} />
        <Label htmlFor="content-crawl-enable">我授权只读访问本站和下列来源</Label>
      </div>
      <Field id="content-crawl-method" label="发现来源">
        <NativeSelect
          id="content-crawl-method"
          className="w-full"
          value={connector}
          onChange={(event) => setConnector(event.target.value as CrawlPolicy["connector"])}
        >
          <NativeSelectOption value="sitemap">XML Sitemap</NativeSelectOption>
          <NativeSelectOption value="wordpress">WordPress REST</NativeSelectOption>
          <NativeSelectOption value="json">通用 JSON</NativeSelectOption>
        </NativeSelect>
      </Field>
      <Field id="content-crawl-limit" label="每次最多发现页面">
        <Input
          id="content-crawl-limit"
          name="max_pages"
          type="number"
          min={1}
          max={1000}
          defaultValue={policy?.max_pages ?? 20}
          required
        />
      </Field>
      <div className="sm:col-span-2">
        <Field id="content-crawl-sitemap" label="Sitemap URL">
          <Input
            id="content-crawl-sitemap"
            name="sitemap_url"
            type="url"
            maxLength={4096}
            placeholder={new URL("sitemap.xml", site.base_url.endsWith("/") ? site.base_url : `${site.base_url}/`).href}
            defaultValue={policy?.sitemap_url}
          />
        </Field>
      </div>
      {connector !== "sitemap" && (
        <div className="sm:col-span-2">
          <Field id="content-crawl-connector" label="CMS 只读接口 URL">
            <Input
              id="content-crawl-connector"
              name="connector_url"
              type="url"
              maxLength={4096}
              required={connector === "json"}
              defaultValue={policy?.connector_url}
            />
          </Field>
        </div>
      )}
      <div className="sm:col-span-2">
        <Field id="content-crawl-origins" label="一手来源域名（每行一个源）">
          <Textarea
            id="content-crawl-origins"
            name="source_origins"
            rows={3}
            defaultValue={policy?.source_origins?.join("\n")}
            placeholder="https://authority.example.com"
          />
        </Field>
      </div>
    </FormDialog>
  );
}

export function SourceEditor({ onClose, onSave }: Close & { onSave: SubmitAction }) {
  return (
    <FormDialog
      title="登记一手来源"
      description="来源域名须已获得组织所有者授权。研究任务将采集来源并生成引用证据。"
      submitLabel="登记来源"
      onClose={onClose}
      onSubmit={(data) =>
        onSave({
          action: "add_source",
          source: { url: field(data, "url").trim(), kind: field(data, "kind") },
        })
      }
    >
      <div className="sm:col-span-2">
        <Field id="content-source-url" label="来源 URL">
          <Input id="content-source-url" name="url" type="url" maxLength={4096} required />
        </Field>
      </div>
      <Field id="content-source-kind" label="来源类型">
        <NativeSelect className="w-full" name="kind" id="content-source-kind" defaultValue="official">
          {["official", "government", "primary", "authorized"].map((kind) => (
            <NativeSelectOption key={kind} value={kind}>
              {sourceKinds[kind]}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Field>
    </FormDialog>
  );
}

export function RefreshEditor({ signals, onClose, onSave }: Close & { signals: RefreshSignals; onSave: SubmitAction }) {
  const numeric = [
    ["traffic_change", "流量变动（%）"],
    ["rank_change", "排名名次变动"],
    ["ctr_change", "CTR 变动（%）"],
  ] as const;
  const switches = [
    ["source_changed", "一手来源变化"],
    ["serp_changed", "竞品或 SERP 变化"],
    ["intent_changed", "搜索意图变化"],
  ] as const;

  return (
    <FormDialog
      title="登记更新信号"
      description="可使用已核实的外部分析数据，保留其证据链接。"
      submitLabel="保存更新信号"
      onClose={onClose}
      onSubmit={(data) => {
        function measured(name: string) {
          const value = field(data, name);
          return value === "" ? undefined : Number(value);
        }
        return onSave({
          action: "save_signals",
          signals: {
            traffic_change: measured("traffic_change"),
            rank_change: measured("rank_change"),
            ctr_change: measured("ctr_change"),
            source_changed: data.has("source_changed"),
            serp_changed: data.has("serp_changed"),
            intent_changed: data.has("intent_changed"),
            evidence_url: field(data, "evidence_url").trim() || undefined,
          },
        });
      }}
    >
      {numeric.map(([id, label]) => (
        <Field id={`content-signal-${id}`} label={label} key={id}>
          <Input id={`content-signal-${id}`} name={id} type="number" step="any" defaultValue={signals[id]} />
        </Field>
      ))}
      <div className="sm:col-span-2">
        <Field id="content-signal-evidence" label="证据 URL">
          <Input id="content-signal-evidence" name="evidence_url" type="url" defaultValue={signals.evidence_url} />
        </Field>
      </div>
      {switches.map(([id, label]) => (
        <div key={id} className="flex items-center gap-2">
          <Switch id={`content-signal-${id}`} name={id} defaultChecked={signals[id]} />
          <Label htmlFor={`content-signal-${id}`}>{label}</Label>
        </div>
      ))}
    </FormDialog>
  );
}

export function ApprovalEditor({ version, onClose, onSave }: Close & { version: number; onSave: SubmitAction }) {
  return (
    <FormDialog
      title="批准当前文章"
      description={`文章版本 ${version} 会记录此审批意见。可在版本历史查看预览及审批状态。`}
      submitLabel="批准文章"
      onClose={onClose}
      onSubmit={(data) => onSave({ action: "approve", note: field(data, "note").trim() })}
    >
      <div className="sm:col-span-2">
        <Field id="content-review-note" label="审批意见">
          <Textarea id="content-review-note" name="note" required rows={3} maxLength={2000} />
        </Field>
      </div>
    </FormDialog>
  );
}

const modelLimits = [
  ["temperature", "温度", 0.2, 0, 1, 0.1],
  ["max_input_tokens", "单次输入 Token 上限", 24000, 1024, 48000, 1],
  ["daily_tokens", "每日 Token 预算", 500000, 1, 1_000_000_000, 1],
  ["daily_cost_micros", "每日费用预算（USD）", 5, 0.000001, 100_000, 0.000001],
  ["input_micros_per_million", "每百万输入 Token 单价（USD）", 1, 0.000001, 10_000, 0.000001],
  ["output_micros_per_million", "每百万输出 Token 单价（USD）", 2, 0.000001, 10_000, 0.000001],
  ["max_retries", "最多重试次数", 2, 0, 2, 1],
  ["retry_seconds", "重试间隔（秒）", 5, 1, 120, 1],
] as const;

export function ModelProfileEditor({ provider, onClose }: Close & { provider: Provider }) {
  const profileQuery = useSeoQuery<{ items: ContentModelProfile[] }>("/content/models");
  const profile = profileQuery.data?.items?.find((item) => item.provider_id === provider.id);
  const costField = (name: string) => name.includes("micros");

  if (profileQuery.loading) return <PageSkeleton />;
  if (profileQuery.error) {
    return (
      <div className="space-y-2">
        <LoadError error={profileQuery.error} retry={profileQuery.refresh} />
        <Button size="sm" variant="outline" onClick={onClose}>
          关闭
        </Button>
      </div>
    );
  }
  return (
    <FormDialog
      title="配置内容模型"
      description={`${provider.name} · ${provider.model}`}
      submitLabel="保存模型配置"
      onClose={onClose}
      onSubmit={async (data) => {
        const value = (key: string) =>
          costField(key) ? Math.round(Number(field(data, key)) * 1_000_000) : Number(field(data, key));
        const body: ContentModelProfile = {
          provider_id: provider.id,
          role: field(data, "role"),
          system_prompt: field(data, "system_prompt"),
          temperature: value("temperature"),
          max_input_tokens: value("max_input_tokens"),
          daily_tokens: value("daily_tokens"),
          daily_cost_micros: value("daily_cost_micros"),
          input_micros_per_million: value("input_micros_per_million"),
          cached_input_micros_per_million:
            field(data, "cached_input_micros_per_million") === ""
              ? undefined
              : value("cached_input_micros_per_million"),
          output_micros_per_million: value("output_micros_per_million"),
          max_retries: value("max_retries"),
          retry_seconds: value("retry_seconds"),
        };
        await seoRequest("/content/models", { method: "PUT", body });
        toast.success("模型配置已保存");
      }}
    >
      <Field id="content-model-role" label="模型角色">
        <NativeSelect name="role" id="content-model-role" className="w-full" defaultValue={profile?.role ?? "writer"}>
          <NativeSelectOption value="writer">写作</NativeSelectOption>
          <NativeSelectOption value="reviewer">独立审核</NativeSelectOption>
          <NativeSelectOption value="any">写作和审核</NativeSelectOption>
        </NativeSelect>
      </Field>
      {modelLimits.map(([key, label, fallback, min, max, step]) => {
        const remote = profile?.[key];
        let value = fallback as number;
        if (remote !== undefined) value = costField(key) ? remote / 1_000_000 : remote;
        return (
          <Field id={`content-model-${key}`} label={label} key={key}>
            <Input
              id={`content-model-${key}`}
              name={key}
              type="number"
              defaultValue={value}
              min={min}
              max={max}
              step={step}
              required
            />
          </Field>
        );
      })}
      <Field id="content-model-cached-input" label="每百万缓存输入 Token 单价（USD，选填）">
        <Input
          id="content-model-cached-input"
          name="cached_input_micros_per_million"
          type="number"
          min={0}
          max={10_000}
          step={0.000001}
          defaultValue={
            profile?.cached_input_micros_per_million === undefined
              ? undefined
              : profile.cached_input_micros_per_million / 1_000_000
          }
        />
      </Field>
      <div className="sm:col-span-2">
        <Field id="content-model-system" label="System Prompt">
          <Textarea
            id="content-model-system"
            name="system_prompt"
            rows={4}
            maxLength={3000}
            defaultValue={profile?.system_prompt}
          />
        </Field>
      </div>
    </FormDialog>
  );
}
