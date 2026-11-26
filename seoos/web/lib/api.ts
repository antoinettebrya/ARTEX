import "server-only";
import { cookies } from "next/headers";

export type Role = "owner" | "editor" | "viewer";
export type Principal = { organization_id: string; email?: string; role: Role };
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
  kind: "site_inventory" | "site_brief";
  state: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  attempts: number;
  available_at: string;
  error_code?: string;
  result?: unknown;
};
export type Provider = {
  id: string;
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
};
export type ActionState = { error?: string; notice?: string; token?: string };

export const cookieName = "seoos_session";

const messages: Record<string, string> = {
  unauthorized: "请重新登录。",
  invalid_request: "请检查表单中的数据格式与取值。",
  forbidden: "当前账号没有执行此操作的权限。",
  not_found: "记录不存在或当前组织无权访问。",
  version_conflict: "记录已更新。请刷新页面后再试。",
  conflict: "当前组织已登记相同地址。",
  member_exists: "该邮箱已在当前组织登记。",
  last_owner: "组织需要保留至少一个有效的所有者。",
  site_inactive: "仅启用状态的站点可以创建任务。",
  provider_disabled: "所选 Provider 已停用。",
  provider_name_taken: "当前组织已有相同名称的 Provider。",
  credential_store_unavailable: "服务尚未配置 Provider 凭据加密。",
  job_settled: "任务已结束。",
  idempotency_conflict: "此表单已提交。请刷新页面再创建新任务。",
};

export class APIError extends Error {
  constructor(public readonly status: number, code = "") {
    super(messages[code] ?? "服务暂时无法完成该请求。");
  }
}

function apiOrigin() {
  const url = new URL(process.env.SEOOS_API_ORIGIN ?? "http://127.0.0.1:8789");
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password ||
      url.pathname !== "/" || url.search || url.hash) {
    throw new Error("Invalid SEOOS_API_ORIGIN");
  }
  return url.origin;
}

export async function sessionToken() {
  return (await cookies()).get(cookieName)?.value;
}

export async function api<T>(path: string, token: string, method = "GET", payload?: object): Promise<T> {
  let response: Response;
  try {
    response = await fetch(apiOrigin() + "/api/v1" + path, {
      method,
      headers: {
        Accept: "application/json",
        Authorization: "Bearer " + token,
        ...(payload ? { "Content-Type": "application/json" } : {}),
      },
      body: payload ? JSON.stringify(payload) : undefined,
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(12000),
    });
  } catch {
    throw new APIError(503);
  }
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as { error?: { code?: string } };
    throw new APIError(response.status, payload.error?.code);
  }
  return response.status === 204 ? (undefined as T) : (await response.json() as T);
}

export function isID(value: string): boolean {
  return /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(value);
}
