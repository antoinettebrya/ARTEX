import type { Job } from "./client";
import type { Intent, MappingRow, Market, OpportunityRow, PageEvidence, SERPResult } from "./research";

export const contentJobLabels = {
  site_inventory: "发现站点页面",
  page_extraction: "采集页面",
  content_strategy: "生成内容策略",
  content_brief: "生成内容简报",
  content_research: "研究一手资料",
  content_outline: "生成文章大纲",
  content_draft: "撰写文章章节",
  content_fact_check: "核验文章事实",
  content_quality_review: "独立质量审核",
  content_seo_optimization: "优化文章 SEO",
  content_refresh: "分析更新机会",
  content_publish_verify: "验证发布页面",
} as const;

export type ContentJobKind = keyof typeof contentJobLabels;
export const modelJobs: readonly ContentJobKind[] = ["content_outline", "content_draft", "content_quality_review"];
export const inventoryJobs: readonly ContentJobKind[] = ["site_inventory", "page_extraction"];

export type ContentProject = {
  id: string;
  site_id: string;
  keyword: string;
  market: Market;
  revision: number;
  status: string;
  created_at: string;
  updated_at: string;
};

export type ContentHeading = { level: number; heading: string; purpose?: string };
export type ContentBrief = {
  primary_keyword: string;
  secondary_keywords: string[] | null;
  intent: string;
  country_code: string;
  language_code: string;
  need: string;
  page_type: string;
  url?: string;
  title: string;
  description: string;
  h1: string;
  headings: ContentHeading[] | null;
  questions: string[] | null;
  entities: string[] | null;
  topics: string[] | null;
  source_urls: string[] | null;
  gaps: string[] | null;
  images: string[] | null;
  internal_links: string[] | null;
  fact_requirements: string[] | null;
};

export type ContentSource = {
  id?: string;
  url: string;
  kind: string;
  title?: string;
  published_at?: string;
  collected_at?: string;
  fingerprint?: string;
  serp_snapshot_id?: string;
  status?: string;
};

export type FactEvidence = { source_id: string; excerpt: string; conflict?: boolean };
export type ContentFact = {
  id: string;
  statement: string;
  status: string;
  confidence: number;
  market: Market;
  evidence: FactEvidence[];
};
export type ContentClaim = { text: string; fact_ids: string[] };
export type ArticleSection = { level: number; heading: string; body: string; claims: ContentClaim[] | null };
export type ContentArticle = {
  language_code: string;
  title: string;
  description: string;
  h1: string;
  author?: string;
  slug?: string;
  canonical?: string;
  sections: ArticleSection[] | null;
  internal_links: string[];
  image_alts: string[];
  schema: string[];
};

export type ContentStrategy = {
  action: string;
  target_url?: string;
  page_type?: string;
  topic_cluster: boolean;
  reasons: string[];
  evidence: string[];
};
export type ContentQuality = {
  passed: boolean;
  score: number;
  algorithm: string;
  dimensions:
    | { name: string; score: number; weight: number; explanation: string; metrics?: Record<string, number> }[]
    | null;
  issues: string[];
  suggestions: string[];
  reviewed_at: string;
};
export type FactReport = {
  passed: boolean;
  claims: { text: string; status: string; fact_ids: string[] | null; reason: string; material: boolean }[] | null;
};
export type EditorialReview = {
  passed: boolean;
  intent: boolean;
  language: boolean;
  original_value: boolean;
  issues: string[];
  model?: string;
};
export type ContentVersion = {
  number: number;
  created_at: string;
  reason: string;
  brief?: ContentBrief;
  article?: ContentArticle;
};
export type RefreshSignals = {
  traffic_change?: number;
  rank_change?: number;
  ctr_change?: number;
  source_changed: boolean;
  serp_changed: boolean;
  intent_changed: boolean;
  evidence_url?: string;
};
export type ContentPublication = {
  id: string;
  version: number;
  status: string;
  article: ContentArticle;
  checksum: string;
  sources?: ContentSource[] | null;
  published_url?: string;
  verified_at?: string;
};
export type ContentWorkspace = {
  project: ContentProject;
  evidence: {
    keyword: {
      keyword: string;
      intent?: Intent;
      serp: SERPResult[] | null;
      metrics?: { search_volume?: number; keyword_difficulty?: number; cpc_usd?: number };
    };
    serp_snapshot_id?: string;
    mapping?: MappingRow["mapping"];
    opportunity?: OpportunityRow["opportunity"];
    clusters?: { id: string; primary_keyword: string; page_type: string }[];
    competitors?: { snapshot_id: string; page: PageEvidence; gaps: string[] }[];
  };
  strategy?: ContentStrategy;
  brief?: ContentBrief;
  brief_version: number;
  outline?: ContentHeading[];
  article?: ContentArticle;
  article_version: number;
  facts: ContentFact[] | null;
  sources: ContentSource[] | null;
  fact_check?: FactReport;
  review?: EditorialReview;
  quality?: ContentQuality;
  refresh?: { action: string; target_url?: string; snapshot_id?: string; reasons: string[] };
  refresh_signals: RefreshSignals;
  history: ContentVersion[] | null;
  publications: ContentPublication[] | null;
};

export type CrawlPolicy = {
  site_id: string;
  enabled: boolean;
  sitemap_url?: string;
  connector: "sitemap" | "wordpress" | "json";
  connector_url?: string;
  source_origins: string[];
  max_pages: number;
};
export type SitePage = PageEvidence & {
  id: string;
  site_id: string;
  state: string;
  index_status: string;
  mapped: boolean;
  http_status?: number;
  body?: string;
  language?: string;
  robots?: string;
  images?: { url: string; alt?: string }[];
  fingerprint?: string;
  checked_at?: string;
  discovered_at?: string;
  snapshot_id?: string;
  categories?: string[];
  tags?: string[];
  kind?: string;
};
export type ContentJobInput = {
  site_id: string;
  project_id?: string;
  provider_id?: string;
  kind: ContentJobKind;
  url?: string;
  idempotency_key: string;
  priority: number;
};
export type ContentJobRecord = { job: Job; request: ContentJobInput };

export type ContentAction =
  | { action: "save_brief"; brief: ContentBrief }
  | { action: "save_article"; article: ContentArticle }
  | { action: "restore_brief" | "restore_article"; version: number }
  | { action: "add_source"; source: ContentSource }
  | { action: "save_signals"; signals: RefreshSignals }
  | { action: "approve"; note?: string }
  | { action: "prepare_publication" };

export type ContentModelProfile = {
  provider_id: string;
  role: string;
  system_prompt: string;
  temperature: number;
  max_input_tokens: number;
  daily_tokens: number;
  daily_cost_micros: number;
  input_micros_per_million: number;
  cached_input_micros_per_million?: number;
  output_micros_per_million: number;
  max_retries: number;
  retry_seconds: number;
};

const actionLabels: Record<string, string> = {
  create: "新建页面",
  update: "更新现有页面",
  merge: "合并候选页面",
  keep: "保持原文",
  light_update: "小幅更新",
  rewrite: "重写",
  suggest_noindex: "建议退出索引",
  noindex: "建议退出索引",
  change_page_type: "调整页面类型",
  retype: "调整页面类型",
  stop: "暂停内容任务",
  inventory_missing: "补充页面清单",
};
const labels: Record<string, string> = {
  active: "进行中",
  planned: "规划中",
  draft: "草稿",
  drafting: "草稿",
  pending: "待处理",
  queued: "排队中",
  running: "运行中",
  succeeded: "完成",
  failed: "失败",
  cancelled: "已取消",
  approved: "已批准",
  prepared: "已准备",
  verified: "已核实",
  supported: "已核实",
  partial: "部分核实",
  partially_verified: "部分核实",
  unverified: "无法核实",
  conflict: "存在冲突",
  conflicting: "存在冲突",
  stale: "已过时",
  outdated: "已过时",
  discovered: "已发现",
  fetched: "已采集",
  collected: "已采集",
  extracted: "已采集",
  unavailable: "不可用",
  indexed: "已收录",
  not_indexed: "未收录",
  unknown: "待核实",
  blocked: "未通过",
  passed: "已通过",
};
export const sourceKinds: Record<string, string> = {
  official: "官网或官方文档",
  government: "政府与监管",
  primary: "一手研究",
  authorized: "授权资料",
  serp: "SERP 证据",
  competitor: "竞品结构",
};

export function contentLabel(status: string) {
  return labels[status] ?? status;
}

export function contentActionLabel(value: string) {
  return actionLabels[value] ?? value;
}

export function safeHTTPLink(value?: string) {
  if (!value) return undefined;
  try {
    const parsed = new URL(value);
    return ["https:", "http:"].includes(parsed.protocol) && !parsed.username && !parsed.password
      ? parsed.href
      : undefined;
  } catch {
    return undefined;
  }
}

export function contentLines(value: string) {
  return value
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean);
}
