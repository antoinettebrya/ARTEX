import type { ContentJobKind } from "./content";
import type { ExecutionJobKind } from "./execution";
import type { ResearchKind } from "./research";

export type Role = "owner" | "editor" | "viewer";
export type Principal = { organization_id: string; user_id?: string; email?: string; role: Role };
export type Site = {
  id: string;
  name: string;
  base_url: string;
  cms: "wordpress" | "generic";
  country_code: string;
  language_tag: string;
  status: "active" | "paused" | "archived";
  version: number;
};
export type Job = {
  id: string;
  site_id: string;
  kind: "site_inventory" | "site_brief" | ResearchKind | ContentJobKind | ExecutionJobKind;
  state: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  attempts: number;
  available_at: string;
  error_code?: string;
  result?: unknown;
};
export type ProviderKind = "openai" | "anthropic" | "gemini" | "deepseek" | "dataforseo";
export type Provider = {
  id: string;
  kind?: ProviderKind;
  daily_cost_micros?: number;
  name: string;
  model: string;
  timeout_seconds: number;
  requests_per_minute: number;
  requests_per_day: number;
  max_output_tokens: number;
  enabled: boolean;
  version: number;
};
export type Member = { user_id: string; email: string; role: Role; enabled: boolean };
export type AuditEvent = {
  id: number;
  actor_kind: string;
  actor_user_id?: string;
  action: string;
  resource_id: string;
  created_at: string;
  details: unknown;
};

const messages: Record<string, string> = {
  unauthorized: "访问凭据无效或已撤销。",
  invalid_request: "请检查表单中的数据格式与取值。",
  forbidden: "当前账号没有执行此操作的权限。",
  forbidden_origin: "此页面与服务的地址不一致，请从后台地址重新打开。",
  not_found: "记录不存在或当前组织无权访问。",
  version_conflict: "记录已更新。请刷新页面后再试。",
  conflict: "当前组织已登记相同地址。",
  member_exists: "该邮箱已在当前组织登记。",
  last_owner: "组织需要保留至少一个有效的所有者。",
  site_inactive: "仅启用状态的站点可以创建任务。",
  market_not_supported: "当前数据接口不支持此市场组合。",
  research_evidence_missing: "请先收集当前市场的关键词或 SERP 证据。",
  provider_credentials_pair: "请同时填写 DataForSEO 登录名和 API 密码。",
  provider_kind: "请选择此任务所需的 Provider 类型。",
  provider_disabled: "所选 Provider 已停用。",
  provider_name_taken: "当前组织已有相同名称的 Provider。",
  provider_test_limited: "已达到该连接的测试次数上限。请稍后再试。",
  credential_store_unavailable: "服务尚未配置 Provider 凭据加密。",
  job_settled: "任务已结束。",
  idempotency_conflict: "此表单已提交，请重新打开后再创建任务。",
  content_prerequisite: "请先完成所需的采集、研究或内容版本。",
  content_gate_blocked: "当前版本需要质量审核通过并获得所有者批准。",
  execution_state: "当前状态无法执行此操作。请刷新发布记录并检查可用操作。",
  execution_unauthorized: "请确认连接的权限与显式站点授权。",
  execution_unsupported: "此连接未声明支持该操作。",
  execution_media_too_large: "请选择不超过 4 MiB 的媒体文件。",
  execution_media_read: "无法读取媒体文件，请重新选择。",
  execution_oauth_response: "授权响应无效。请重新发起 Google 授权。",
  execution_credential_pair: "请提供完整的一组连接凭据。",
  wordpress_base_url: "请到站点管理核对 WordPress 安装地址。使用 HTTPS 首页地址，保留安装子目录。",
  wordpress_name: "请输入有效的连接名称（1–120 个字符）。",
  wordpress_username: "请输入生成应用程序密码的 WordPress 用户名。",
  wordpress_credential: "请填写 WordPress 应用程序密码。更新凭据时请同时填写用户名和应用程序密码。",
  wordpress_limits: "请检查 WordPress 的每分钟、每日请求上限和超时设置。",
  crawl_unauthorized: "请让组织所有者先授权本站的读取范围。",
};

export class SeoAPIError extends Error {
  constructor(
    readonly status: number,
    readonly code = "",
    readonly field?: string,
  ) {
    super(Object.hasOwn(messages, code) ? messages[code] : "服务暂时无法完成该请求。");
  }
}

export async function seoRequest<T>(
  path: string,
  options: { method?: "GET" | "POST" | "PUT" | "DELETE"; body?: object; signal?: AbortSignal } = {},
): Promise<T> {
  const response = await fetch(`/seo/api${path}`, {
    method: options.method ?? "GET",
    headers: {
      Accept: "application/json",
      "X-SEOOS-Request": "1",
      ...(options.body ? { "Content-Type": "application/json" } : {}),
    },
    body: options.body ? JSON.stringify(options.body) : undefined,
    credentials: "same-origin",
    cache: "no-store",
    signal: options.signal,
  });
  if (!response.ok) {
    const payload: { error?: { code?: string; field?: string } } | null = await response.json().catch(() => ({}));
    throw new SeoAPIError(response.status, payload?.error?.code, payload?.error?.field);
  }
  return response.status === 204 ? (undefined as T) : ((await response.json()) as T);
}

export function errorMessage(error: unknown): string {
  return error instanceof SeoAPIError ? error.message : "暂时无法连接 SEO 服务，请重试。";
}

export const roles = { owner: "所有者", editor: "编辑者", viewer: "查看者" } as const;

export function isID(value: string): boolean {
  return /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(value);
}
