import type { Job, Site } from "./client";

export const researchJobLabels = {
  keyword_discovery: "关键词拓展",
  keyword_metrics: "关键词指标",
  keyword_suggestions: "关键词建议",
  keyword_intent: "搜索意图",
  keyword_clustering: "关键词聚类",
  serp_analysis: "SERP 前十研究",
  competitor_analysis: "竞争页面分析",
  keyword_url_mapping: "Keyword-to-URL",
  opportunity_scoring: "机会评分",
} as const;

export type ResearchKind = keyof typeof researchJobLabels;

const paidJobs = new Set<ResearchKind>([
  "keyword_discovery",
  "keyword_metrics",
  "keyword_suggestions",
  "serp_analysis",
  "competitor_analysis",
]);

export function usesDataProvider(kind: ResearchKind) {
  return paidJobs.has(kind);
}

export type Market = {
  country_code: string;
  location_code: number;
  language_code: string;
  search_engine: "google" | "bing";
  device: "desktop" | "mobile";
};

export function initialMarket(site: Site): Market {
  return {
    country_code: site.country_code,
    location_code: 0,
    language_code: site.language_tag.split("-")[0],
    search_engine: "google",
    device: "desktop",
  };
}

export function marketLabel(market: Market): string {
  const engine = market.search_engine === "google" ? "Google" : "Bing";
  return `${market.country_code} · ${market.language_code} · ${market.location_code} · ${engine} · ${market.device === "desktop" ? "桌面" : "移动"}`;
}

export function marketQuery(params: URLSearchParams, market: Market) {
  params.set("country_code", market.country_code);
  params.set("location_code", String(market.location_code));
  params.set("language_code", market.language_code);
  params.set("search_engine", market.search_engine);
  params.set("device", market.device);
}

export const usd = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 6,
});

export type ResearchInput = {
  site_id: string;
  provider_id?: string;
  intent_provider_id?: string;
  kind: ResearchKind;
  keyword: string;
  market: Market;
  target_url?: string;
  idempotency_key: string;
  priority: number;
  business_value?: number;
};

export type ResearchJob = {
  job: Job;
  request: ResearchInput;
};

export type Measured<T> = {
  id?: string;
  market: Market;
  source?: string | null;
  collected_at?: string | null;
  snapshot_id?: string | null;
} & T;

export type KeywordMetric = {
  search_volume?: number;
  cpc_usd?: number;
  competition?: number;
  competition_index?: number;
  keyword_difficulty?: number;
  provider_intent?: string;
  device_scope?: string;
};

export type Intent = {
  primary: string;
  secondary?: string;
  need: string;
  page_type: string;
  confidence: number;
  reasons: string[];
  sources: string[];
};

export type KeywordRow = Measured<{ keyword: string; metrics: KeywordMetric | null; intent?: Intent | null }>;

export type Cluster = Measured<{
  id: string;
  primary_keyword: string;
  secondary_keywords: string[];
  long_tail_keywords: string[];
  member_ids: string[];
  recommended_url?: string;
  page_type: string;
  confidence: number;
  reasons: string[];
}>;

export type SERPResult = {
  rank: number;
  title: string;
  url: string;
  domain: string;
  description: string;
  page_type?: string;
};

export type SERP = Measured<{ keyword: string; results: SERPResult[] }>;

export type PageSource = {
  source: string;
  snapshot_id: string;
  collected_at: string;
};

export type PageEvidence = {
  url: string;
  title?: string;
  description?: string;
  canonical_url?: string;
  headings?: { level: number; text: string }[];
  topics?: string[];
  entities?: string[];
  faq?: string[];
  schema?: string[];
  sections?: {
    heading: string;
    level: number;
    primary_blocks: number;
    secondary_blocks: number;
    tables: number;
  }[];
  source_snapshots?: PageSource[];
  page_type?: string;
  last_modified?: string;
};

export type Competitor = Measured<{ keyword: string; page: PageEvidence; gaps: string[] }>;
export type SitePage = Measured<{ page: PageEvidence }>;
export type IntentRow = Measured<{ keyword: string; intent: Intent }>;

export type MappingRow = Measured<{
  keyword: string;
  mapping: {
    target_url?: string;
    action: string;
    cannibalizing_urls?: string[];
    canonical_issue: boolean;
    reasons: string[];
    evidence: string[];
  };
}>;

export type OpportunityRow = Measured<{
  keyword: string;
  opportunity: {
    score?: number;
    known_points: number;
    coverage: number;
    version: string;
    evidence_metrics?: KeywordMetric;
    parts: { name: string; weight: number; points?: number; evidence: string }[];
  };
}>;

export type ProviderUsage = {
  provider_id: string;
  name: string;
  requests: number;
  spent_micros: number;
  reserved_micros: number;
  day_limit_micros: number;
  date_utc: string;
};

export type Seed = Measured<{ keyword: string }>;
export type Suggestion = Measured<{ seed: string; keyword: string; operation: string }>;
