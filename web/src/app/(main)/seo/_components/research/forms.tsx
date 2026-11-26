"use client";

import { useState } from "react";

import Link from "next/link";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { type Job, type Provider, type Site, seoRequest } from "@/lib/seo/client";
import {
  type Market,
  type PageEvidence,
  type ResearchInput,
  type ResearchKind,
  researchJobLabels,
  usesDataProvider,
} from "@/lib/seo/research";

import { useSeoQuery } from "../query";
import { Field, FormDialog, field, LoadError } from "../shared";

export type JobDraft = { kind: ResearchKind; keyword?: string; market: Market; targetURL?: string };

export function MarketFields({ market }: { market: Market }) {
  return (
    <>
      <Field id="research-market-country" label="国家或地区">
        <Input
          id="research-market-country"
          name="country_code"
          required
          pattern="[A-Za-z]{2}"
          maxLength={2}
          defaultValue={market.country_code}
        />
      </Field>
      <Field id="research-market-language" label="语言代码">
        <Input
          id="research-market-language"
          name="language_code"
          required
          maxLength={20}
          placeholder="en"
          defaultValue={market.language_code}
        />
      </Field>
      <Field id="research-market-location" label="DataForSEO 地区代码">
        <Input
          id="research-market-location"
          name="location_code"
          type="number"
          required
          min={1}
          max={100_000_000}
          defaultValue={market.location_code || undefined}
        />
      </Field>
      <Field id="research-market-engine" label="搜索引擎">
        <NativeSelect
          id="research-market-engine"
          name="search_engine"
          className="w-full"
          defaultValue={market.search_engine}
        >
          <NativeSelectOption value="google">Google</NativeSelectOption>
          <NativeSelectOption value="bing">Bing</NativeSelectOption>
        </NativeSelect>
      </Field>
      <Field id="research-market-device" label="设备">
        <NativeSelect id="research-market-device" name="device" className="w-full" defaultValue={market.device}>
          <NativeSelectOption value="desktop">桌面</NativeSelectOption>
          <NativeSelectOption value="mobile">移动</NativeSelectOption>
        </NativeSelect>
      </Field>
      <div className="sm:col-span-2">
        <Button size="sm" variant="link" className="h-auto px-0" asChild>
          <a href="https://docs.dataforseo.com/v3/serp/google/locations/" target="_blank" rel="noreferrer noopener">
            DataForSEO 官方地区代码
          </a>
        </Button>
      </div>
    </>
  );
}

export function readMarket(data: FormData): Market {
  return {
    country_code: field(data, "country_code").trim().toUpperCase(),
    language_code: field(data, "language_code").trim(),
    location_code: Number(field(data, "location_code")),
    search_engine: field(data, "search_engine") === "bing" ? "bing" : "google",
    device: field(data, "device") === "mobile" ? "mobile" : "desktop",
  };
}

type FormProps = { market: Market; onClose: () => void };

export function MarketDialog({ market, onClose, onApply }: FormProps & { onApply: (market: Market) => void }) {
  return (
    <FormDialog
      title="筛选市场"
      description="同一关键词在不同地区、语言、搜索引擎和设备下的数据分开保存。"
      submitLabel="应用筛选"
      onClose={onClose}
      onSubmit={async (data) => onApply(readMarket(data))}
    >
      <MarketFields market={market} />
    </FormDialog>
  );
}

export function ResearchJobEditor({
  draft,
  site,
  owner,
  onClose,
  onSaved,
}: {
  draft: JobDraft;
  site: Site;
  owner: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [kind, setKind] = useState<ResearchKind>(draft.kind);
  const [idempotencyKey] = useState(() => crypto.randomUUID());
  const paid = usesDataProvider(kind);
  const providers = useSeoQuery<{ providers: Provider[] }>(
    paid || kind === "keyword_intent" ? "/providers?limit=200" : null,
  );
  const selectable = (providers.data?.providers ?? []).filter(
    (provider) =>
      provider.enabled && (paid ? provider.kind === "dataforseo" : (provider.kind ?? "openai") === "openai"),
  );

  async function save(data: FormData) {
    const value = field(data, "business_value");
    const payload: ResearchInput = {
      site_id: site.id,
      kind,
      keyword: field(data, "keyword").trim(),
      market: readMarket(data),
      provider_id: paid ? field(data, "provider_id") : undefined,
      intent_provider_id: kind === "keyword_intent" ? field(data, "intent_provider_id") || undefined : undefined,
      target_url: kind === "competitor_analysis" ? field(data, "target_url").trim() : undefined,
      idempotency_key: idempotencyKey,
      priority: Number(field(data, "priority")),
      business_value: kind === "opportunity_scoring" && value !== "" ? Number(value) : undefined,
    };
    await seoRequest<{ job: Job }>("/research/jobs", { method: "POST", body: payload });
    toast.success("研究任务已提交");
    onSaved();
  }

  return (
    <FormDialog
      title="创建研究任务"
      description={`选择 ${site.name} 的研究类型、查询词和市场。`}
      submitLabel="提交研究任务"
      onSubmit={save}
      onClose={onClose}
    >
      <Field id="research-job-kind" label="研究任务类型">
        <NativeSelect
          id="research-job-kind"
          className="w-full"
          value={kind}
          onChange={(event) => setKind(event.target.value as ResearchKind)}
        >
          {Object.entries(researchJobLabels).map(([value, label]) => (
            <NativeSelectOption key={value} value={value}>
              {label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Field>
      <Field id="research-job-keyword" label="关键词">
        <Input id="research-job-keyword" name="keyword" maxLength={400} required defaultValue={draft.keyword} />
      </Field>
      <MarketFields market={draft.market} />
      {kind === "competitor_analysis" && (
        <div className="sm:col-span-2">
          <Field id="research-job-target" label="已观测 SERP URL">
            <Input
              id="research-job-target"
              name="target_url"
              type="url"
              required
              maxLength={4096}
              defaultValue={draft.targetURL}
              placeholder="可在 SERP 结果中直接选取 URL"
            />
          </Field>
        </div>
      )}
      {paid && (
        <Field id="research-job-provider" label="DataForSEO Provider">
          <NativeSelect id="research-job-provider" name="provider_id" required defaultValue="" className="w-full">
            <NativeSelectOption value="" disabled>
              {providers.loading ? "正在读取 Provider" : "选择 Provider"}
            </NativeSelectOption>
            {selectable.map((provider) => (
              <NativeSelectOption key={provider.id} value={provider.id}>
                {provider.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          {providers.error !== undefined && <LoadError error={providers.error} retry={providers.refresh} />}
          {!providers.loading && !providers.error && !selectable.length && (
            <p className="text-muted-foreground text-xs">
              当前组织暂无启用的 DataForSEO Provider。
              {owner && (
                <Link className="ml-1 underline" href="/seo/providers">
                  配置 Provider
                </Link>
              )}
            </p>
          )}
        </Field>
      )}
      {kind === "keyword_intent" && (
        <Field id="research-job-intent-provider" label="可选 OpenAI 补充分析">
          <NativeSelect id="research-job-intent-provider" name="intent_provider_id" defaultValue="" className="w-full">
            <NativeSelectOption value="">使用关键词和 SERP 证据</NativeSelectOption>
            {selectable.map((provider) => (
              <NativeSelectOption key={provider.id} value={provider.id}>
                {provider.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          {providers.error !== undefined && <LoadError error={providers.error} retry={providers.refresh} />}
        </Field>
      )}
      {kind === "opportunity_scoring" && (
        <Field id="research-job-business" label="商业价值（0–10，可选）">
          <Input id="research-job-business" name="business_value" type="number" min={0} max={10} step={0.1} />
        </Field>
      )}
      <Field id="research-job-priority" label="优先级（0–100）">
        <Input id="research-job-priority" name="priority" type="number" required min={0} max={100} defaultValue={50} />
      </Field>
    </FormDialog>
  );
}

export function SitePageEditor({ site, market, onClose, onSaved }: FormProps & { site: Site; onSaved: () => void }) {
  async function save(data: FormData) {
    const h1 = field(data, "h1").trim();
    const topics = field(data, "topics")
      .split("\n")
      .map((topic) => topic.trim())
      .filter(Boolean);
    const page: PageEvidence = {
      url: field(data, "url").trim(),
      title: field(data, "title").trim(),
      canonical_url: field(data, "canonical_url").trim() || undefined,
      page_type: field(data, "page_type") || undefined,
      headings: h1 ? [{ level: 1, text: h1 }] : undefined,
      topics: topics.length ? topics : undefined,
    };
    await seoRequest("/research/pages", { method: "POST", body: { site_id: site.id, market: readMarket(data), page } });
    toast.success("本站页面证据已保存");
    onSaved();
  }

  return (
    <FormDialog
      title="登记本站页面"
      description={`填写 ${site.name} 已核实的页面信息，用于关键词映射。`}
      submitLabel="保存页面证据"
      onClose={onClose}
      onSubmit={save}
    >
      <div className="sm:col-span-2">
        <Field id="research-page-url" label="页面 URL">
          <Input id="research-page-url" name="url" type="url" maxLength={4096} placeholder={site.base_url} required />
        </Field>
      </div>
      <Field id="research-page-title" label="页面标题">
        <Input id="research-page-title" name="title" required maxLength={200} />
      </Field>
      <Field id="research-page-canonical" label="Canonical URL（可选）">
        <Input id="research-page-canonical" name="canonical_url" type="url" maxLength={4096} />
      </Field>
      <Field id="research-page-type" label="页面类型">
        <NativeSelect id="research-page-type" name="page_type" className="w-full" defaultValue="">
          <NativeSelectOption value="">未确认</NativeSelectOption>
          <NativeSelectOption value="article">文章</NativeSelectOption>
          <NativeSelectOption value="comparison">比较页</NativeSelectOption>
          <NativeSelectOption value="product">产品页</NativeSelectOption>
          <NativeSelectOption value="category">分类页</NativeSelectOption>
          <NativeSelectOption value="homepage">首页</NativeSelectOption>
          <NativeSelectOption value="local">本地服务页</NativeSelectOption>
        </NativeSelect>
      </Field>
      <Field id="research-page-h1" label="H1（可选）">
        <Input id="research-page-h1" name="h1" maxLength={500} />
      </Field>
      <div className="sm:col-span-2">
        <Field id="research-page-topics" label="覆盖主题（每行一项）">
          <Textarea id="research-page-topics" name="topics" maxLength={2000} rows={3} />
        </Field>
      </div>
      <MarketFields market={market} />
    </FormDialog>
  );
}
