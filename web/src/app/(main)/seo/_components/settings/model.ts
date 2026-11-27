import type { OAuthIntegrationProvider } from "@/lib/seo/google-oauth";

export type AccountProvider = OAuthIntegrationProvider;
export type BindingProvider = "gsc" | "ga4" | "bing" | "indexnow";
export type SearchProvider = Exclude<BindingProvider, "indexnow">;

export const serviceLabels: Record<BindingProvider, string> = {
  gsc: "Google Search Console",
  ga4: "Google Analytics 4",
  bing: "Bing Webmaster",
  indexnow: "IndexNow",
};

export const accountLabels: Record<AccountProvider, string> = { google: "Google", bing: "Bing Webmaster" };

export const providerScopes: Record<AccountProvider, Record<string, string>> = {
  google: {
    "https://www.googleapis.com/auth/webmasters.readonly": "Search Console 只读",
    "https://www.googleapis.com/auth/analytics.readonly": "Google Analytics 只读",
  },
  bing: { "Webmaster.read": "站长数据只读", "Webmaster.manage": "管理及提交 URL" },
};

export type Application = {
  id: string;
  provider: AccountProvider;
  label: string;
  client_id: string;
  redirect_uri: string;
  allowed_scopes: string[];
  enabled: boolean;
  configured: boolean;
  config_version: number;
};

export type Account = {
  id: string;
  label: string;
  provider: AccountProvider;
  application_id?: string;
  auth_mode: "oauth" | "api_key";
  granted_scopes: string[];
  authorization_status: string;
  identity_status: "VERIFIED" | "UNVERIFIED" | "UNAVAILABLE";
  identity_verified_at?: string;
  expires_at?: string;
  last_refresh_at?: string;
  credential_version: number;
  site_count?: number;
};

export type Resource = {
  id: string;
  name: string;
  provider: SearchProvider;
  permission?: string;
  parent_name?: string;
  time_zone?: string;
};

export type Binding = {
  id: string;
  site_id: string;
  provider: BindingProvider;
  integration_account_id?: string;
  external_resource_id: string;
  resource?: Resource;
  enabled: boolean;
  inherited: boolean;
  version: number;
  allow_writes: boolean;
  last_verified_at?: string;
  status: string;
};

export type IntegrationSite = {
  id: string;
  site_id: string;
  site_name: string;
  site_url: string;
  status: string;
  bindings: Binding[];
};

export type IndexNowKey = {
  id: string;
  site_id: string;
  site_name: string;
  host: string;
  directory: string;
  version: number;
  key_version: number;
  allow_writes: boolean;
  verification_status: string;
  verification_error?: string;
  verified_at?: string;
  last_submission_at?: string;
};

export function scopeNames(provider: AccountProvider, scopes: string[]) {
  return scopes.map((scope) => providerScopes[provider][scope] ?? scope).join("、") || "待授权";
}

export function listPath(resource: string, filter: Record<string, string | undefined> = {}) {
  const query = new URLSearchParams();
  for (const [name, value] of Object.entries(filter)) if (value) query.set(name, value);
  return `/integrations/${resource}${query.size ? `?${query}` : ""}`;
}
