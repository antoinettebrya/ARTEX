import type { Job } from "./client";
import type { RefreshSignals } from "./content";

export const connectionLabels = {
  wordpress: "WordPress",
  generic: "通用 CMS",
  gsc: "Google Search Console",
  ga4: "Google Analytics 4",
  bing: "Bing Webmaster",
  indexnow: "IndexNow",
} as const;

export type ConnectionKind = keyof typeof connectionLabels;
export type AnalyticsKind = "gsc" | "ga4" | "bing";
export type CMSResource = "post" | "page";

export type ConnectionSettings = {
  site_id: string;
  kind: ConnectionKind;
  name: string;
  base_url?: string;
  property?: string;
  key_location?: string;
  redirect_uri?: string;
  enabled: boolean;
  allow_writes: boolean;
  requests_per_minute: number;
  requests_per_day: number;
  timeout_seconds: number;
};

export type CMSCapabilities = {
  read_pages: boolean;
  write_pages: boolean;
  publish: boolean;
  schedule: boolean;
  read_media: boolean;
  write_media: boolean;
  seo_plugin?: string;
  seo_fields?: string[];
};

export type Connection = ConnectionSettings & {
  id: string;
  organization_id: string;
  configured: boolean;
  capabilities: CMSCapabilities;
  version: number;
  updated_at: string;
};

export type PreparedPackage = {
  id: string;
  project_id: string;
  version: number;
  checksum: string;
  article: { title: string; canonical?: string; slug?: string };
};

export const publicationStates = {
  PENDING: "待准备",
  PREPARING: "准备中",
  DRAFT_CREATED: "已保存草稿",
  SCHEDULED: "已排期",
  PUBLISHED: "已发布",
  VERIFYING: "核验中",
  VERIFIED: "已核验",
  FAILED: "失败",
  RETRY_WAIT: "等待重试",
  CANCELLED: "已取消",
  ROLLBACK_REQUIRED: "需要恢复",
  ROLLED_BACK: "已恢复",
} as const;

export type PublicationStatus = keyof typeof publicationStates;

export type ExecutionPublication = {
  id: string;
  site_id: string;
  connection_id: string;
  package_id: string;
  project_id: string;
  article_version: number;
  title: string;
  resource: CMSResource;
  remote_id?: string;
  target_id?: string;
  url?: string;
  status: PublicationStatus;
  verification_job_id?: string;
  error?: string;
  created_at: string;
  updated_at: string;
};

export type PublicationEvent = {
  status: PublicationStatus;
  action: string;
  note?: string;
  created_at: string;
};

export const executionJobLabels = {
  cms_connection_test: "验证 CMS 连接",
  cms_page_sync: "同步 CMS 内容及媒体",
  cms_media_upload: "上传 CMS 媒体",
  cms_publication: "执行发布",
  cms_publication_verify: "核验发布结果",
  gsc_sync: "同步 Search Console",
  ga4_sync: "同步 Google Analytics 4",
  bing_sync: "同步 Bing Webmaster",
  sitemap_refresh: "刷新 Sitemap",
  indexnow_submit: "提交索引请求",
  url_inspection: "检查 URL",
  pdf_extraction: "提取 PDF",
  browser_extraction: "浏览器渲染提取",
} as const;

export type ExecutionJobKind = keyof typeof executionJobLabels;
export type PublicationOperation = "draft" | "publish" | "rollback";

export type ExecutionJobInput = {
  site_id: string;
  kind: ExecutionJobKind;
  connection_id?: string;
  publication_id?: string;
  operation?: PublicationOperation;
  note?: string;
  url?: string;
  from?: string;
  to?: string;
  scheduled_at?: string;
  filename?: string;
  media_type?: string;
  media_base64?: string;
  idempotency_key: string;
  priority: number;
};

export type ExecutionJobRecord = {
  job: Job;
  request: Omit<ExecutionJobInput, "media_base64">;
};

export type CMSPage = {
  id: string;
  connection_id?: string;
  resource: CMSResource;
  title: string;
  status: string;
  url?: string;
  slug?: string;
  body?: string;
  excerpt?: string;
  seo_title?: string;
  description?: string;
  canonical?: string;
  modified_at?: string;
  scheduled_at?: string;
  http_status?: number;
};

export type CMSMedia = { id: string; connection_id?: string; url: string; title?: string; media_type?: string };

export type ExecutionMetric = {
  connection_id?: string;
  date: string;
  page?: string;
  query?: string;
  country?: string;
  device?: string;
  language?: string;
  values: Record<string, number>;
};

export type ExecutionProperty = { id: string; connection_id?: string; name?: string; permission?: string };

export const indexingStates = {
  DISCOVERED: "已发现",
  SUBMITTED: "已提交",
  CRAWLED: "已抓取",
  INDEXED: "已索引",
  NOT_INDEXED: "未索引",
  UNKNOWN: "未知",
} as const;

export type IndexRecord = {
  url: string;
  status: keyof typeof indexingStates;
  provider: string;
  evidence?: string;
  observed_at: string;
  last_crawled?: string;
};

export type IndexSubmission = {
  job_id: string;
  provider: string;
  url: string;
  status: "accepted" | "pending_key_validation" | "failed";
  http_status?: number;
  error?: string;
  created_at: string;
};

export type SitemapRecord = {
  job_id?: string;
  entries?: number;
  parts?: number;
  url: string;
  provider: string;
  status?: string;
  last_read?: string;
  warnings?: number;
  errors?: number;
};

export type SitemapDocument = {
  url: string;
  entries: { url: string; lastmod?: string; alternate?: { hreflang: string; url: string }[] }[] | null;
  children?: string[];
  files?: Record<string, string>;
  truncated: boolean;
};

export type OptimizationFeedback = {
  project_id: string;
  target_url?: string;
  keyword: string;
  signals: RefreshSignals;
  updated_at: string;
};

export type ExtractionRecord = {
  job_id?: string;
  url: string;
  kind: string;
  title?: string;
  text?: string;
  pages?: number;
  metadata?: Record<string, string>;
  requests?: number;
  truncated?: boolean;
};

export function isCMS(kind: ConnectionKind) {
  return kind === "wordpress" || kind === "generic";
}

export function isGoogle(kind: ConnectionKind) {
  return kind === "gsc" || kind === "ga4";
}

export function executionPath(name: string, site: string, filters: Record<string, string | undefined> = {}) {
  const params = new URLSearchParams({ site_id: site });
  for (const [key, value] of Object.entries(filters)) {
    if (value) params.set(key, value);
  }
  return `/execution/${name}?${params}`;
}
