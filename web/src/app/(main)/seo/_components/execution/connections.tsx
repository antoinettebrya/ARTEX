"use client";

import { useEffect, useRef, useState } from "react";

import { useRouter } from "next/navigation";

import { KeyRound, Pencil, Plug, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";
import { TableCell, TableRow } from "@/components/ui/table";
import { errorMessage, isID, SeoAPIError, type Site, seoRequest } from "@/lib/seo/client";
import {
  type Connection,
  type ConnectionKind,
  type ConnectionSettings,
  connectionLabels,
  isCMS,
  isGoogle,
} from "@/lib/seo/execution";
import { googleOAuthCallbackPath, googleOAuthResultKey } from "@/lib/seo/google-oauth";
import { seoRoutes } from "@/lib/seo/routes";

import { useSeoLocation } from "../context";
import { Field, FormDialog, field, formatTime, RecordsTable, StateBadge } from "../shared";
import { useExecutionRecords } from "./records";
import type { TaskSelection } from "./tasks";

const oauthSession = "artex.execution.oauth";
type GoogleSession = { state: string; site_id: string; connection_id: string; project?: string; returnQuery?: string };

export function useGoogleReturn(owner: boolean) {
  const router = useRouter();
  const { params, href } = useSeoLocation();
  const state = params.get("state");
  const code = params.get("code");
  const authError = params.get("error");
  const returned = params.get(googleOAuthResultKey);
  const outcome = returned === "connected" || returned === "cancelled" || returned === "failed" ? returned : null;
  const resultSite = params.get("site_id");
  const returnTo = href(seoRoutes.sites.connections.path);
  const handled = useRef<string | null>(null);

  useEffect(() => {
    const legacyMarker = state && (code || authError) ? state : null;
    const marker = outcome ? `${resultSite}/${outcome}` : legacyMarker;
    if (!owner || !marker || handled.current === marker) return;
    handled.current = marker;

    let pending: GoogleSession | undefined;
    try {
      pending = JSON.parse(sessionStorage.getItem(oauthSession) ?? "null") ?? undefined;
    } catch {
      // An expired or unavailable browser session cannot complete authorization.
    }
    if (
      !pending ||
      !isID(pending.site_id) ||
      !isID(pending.connection_id) ||
      (outcome ? pending.site_id !== resultSite : pending.state !== state)
    ) {
      toast.error("Google 授权会话已失效，请重新授权。");
      router.replace(returnTo, { scroll: false });
      return;
    }
    sessionStorage.removeItem(oauthSession);

    const clean = new URL(seoRoutes.sites.connections.path, window.location.origin);
    if (pending.returnQuery) clean.search = pending.returnQuery;
    for (const parameter of ["state", "code", "error", googleOAuthResultKey]) clean.searchParams.delete(parameter);
    clean.searchParams.set("site_id", pending.site_id);
    if (pending.project && isID(pending.project)) clean.searchParams.set("project_id", pending.project);
    function finish() {
      const current = new URLSearchParams(window.location.search);
      if (outcome ? current.get(googleOAuthResultKey) === outcome : current.get("state") === state) {
        router.replace(`${clean.pathname}${clean.search}`, { scroll: false });
      }
    }

    if (outcome) {
      if (outcome === "connected") toast.success("Google 账号已授权");
      else if (outcome === "cancelled") toast.error("Google 授权已取消或未完成。");
      else toast.error("Google 授权未完成，请重新发起授权。");
      finish();
      return;
    }

    if (authError || !code || !state) {
      toast.error("Google 授权已取消或未完成。");
      finish();
      return;
    }
    const { site_id, connection_id } = pending;
    void seoRequest<{ connected: boolean }>("/execution/oauth/exchange", {
      method: "POST",
      body: { site_id, connection_id, state, code },
    })
      .then((result) => {
        if (!result.connected) throw new SeoAPIError(400, "execution_oauth_response");
        toast.success("Google 账号已授权");
      })
      .catch((error: unknown) => toast.error(errorMessage(error)))
      .finally(finish);
  }, [authError, code, outcome, owner, resultSite, returnTo, router, state]);

  return owner && (!!outcome || (!!state && !!(code || authError)));
}

async function startGoogle(connection: Connection, project: string | undefined, returnQuery: string | undefined) {
  const { authorization_url, state } = await seoRequest<{ authorization_url: string; state: string }>(
    "/execution/oauth/start",
    { method: "POST", body: { site_id: connection.site_id, connection_id: connection.id } },
  );
  const url = new URL(authorization_url);
  if (
    !state ||
    !["/o/oauth2/v2/auth", "/o/oauth2/auth"].includes(url.pathname) ||
    url.origin !== "https://accounts.google.com" ||
    url.searchParams.get("state") !== state
  ) {
    throw new SeoAPIError(400, "execution_oauth_response");
  }
  const pending: GoogleSession = {
    site_id: connection.site_id,
    connection_id: connection.id,
    state,
    project,
    returnQuery,
  };
  sessionStorage.setItem(oauthSession, JSON.stringify(pending));
  window.location.assign(url.href);
}

type Credentials = {
  username?: string;
  secret?: string;
  client_id?: string;
  client_secret?: string;
};

function readCredentials(kind: ConnectionKind, data: FormData): Credentials | undefined {
  const username = field(data, "username").trim();
  const secret = field(data, "credential").trim();
  const clientID = field(data, "client_id").trim();
  const clientSecret = field(data, "client_secret").trim();

  if (isGoogle(kind)) {
    if (!clientID && !clientSecret) return undefined;
    if (!clientID || !clientSecret) throw new SeoAPIError(400, "execution_credential_pair");
    return { client_id: clientID, client_secret: clientSecret };
  }
  if (!username && !secret) return undefined;
  if (kind === "wordpress") {
    if (!username || username.includes(":")) throw new SeoAPIError(400, "wordpress_username");
    if (!secret) throw new SeoAPIError(400, "wordpress_credential");
    return { username, secret };
  }
  if (!secret) throw new SeoAPIError(400, "execution_credential_pair");
  return { secret };
}

const wordpressFields = new Set(["base_url", "name", "username", "credential", "limits"]);

function ConnectionEditor({
  site,
  initial,
  onClose,
  onSaved,
}: {
  site: Site;
  initial?: Connection;
  onClose: () => void;
  onSaved: (connection: Connection) => void;
}) {
  const [kind, setKind] = useState<ConnectionKind>(initial?.kind ?? site.cms);
  const wordpress = kind === "wordpress";
  const isGoogleConnection = isGoogle(kind);
  const writesAvailable = isCMS(kind) || kind === "bing" || kind === "indexnow";
  const name = initial?.name;
  const title = initial ? "编辑连接" : "新建连接";
  const credentialLabel: Partial<Record<ConnectionKind, string>> = {
    wordpress: "WordPress 应用程序密码",
    generic: "CMS 访问令牌",
  };
  const defaultReturn =
    typeof window === "undefined" ? "" : new URL(googleOAuthCallbackPath, window.location.origin).href;

  return (
    <FormDialog
      title={title}
      description={`${site.name} · ${initial ? "留空凭据以继续使用已保存的凭据。" : "仅配置你有权访问的服务。"}`}
      submitLabel="保存连接"
      onClose={onClose}
      onSubmit={async (data) => {
        const settings: ConnectionSettings = {
          site_id: site.id,
          kind,
          name: field(data, "name").trim(),
          base_url: wordpress ? field(data, "base_url").trim() : undefined,
          property: field(data, "property").trim() || undefined,
          redirect_uri: isGoogleConnection ? field(data, "redirect_uri").trim() : undefined,
          key_location: kind === "indexnow" ? field(data, "key_location").trim() : undefined,
          enabled: data.has("enabled"),
          allow_writes: writesAvailable && data.has("allow_writes"),
          requests_per_minute: Number(field(data, "requests_per_minute")),
          requests_per_day: Number(field(data, "requests_per_day")),
          timeout_seconds: Number(field(data, "timeout_seconds")),
        };
        try {
          const { connection } = await seoRequest<{ connection: Connection }>(
            initial ? `/execution/connections/${initial.id}` : "/execution/connections",
            {
              method: initial ? "PUT" : "POST",
              body: { ...settings, credential: readCredentials(kind, data), version: initial?.version },
            },
          );
          onSaved(connection);
          toast.success("连接已保存");
        } catch (error) {
          if (
            wordpress &&
            error instanceof SeoAPIError &&
            error.code === "invalid_request" &&
            error.field &&
            wordpressFields.has(error.field)
          ) {
            throw new SeoAPIError(error.status, `wordpress_${error.field}`);
          }
          throw error;
        }
      }}
    >
      <Field id="execution-connection-kind" label="连接类型">
        <NativeSelect
          id="execution-connection-kind"
          className="w-full"
          value={kind}
          disabled={!!initial}
          onChange={(event) => setKind(event.target.value as ConnectionKind)}
        >
          {Object.entries(connectionLabels).map(([value, label]) => (
            <NativeSelectOption key={value} value={value}>
              {label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Field>
      <Field id="execution-connection-name" label="连接名称">
        <Input id="execution-connection-name" name="name" defaultValue={name} maxLength={120} required />
      </Field>
      {isCMS(kind) && (
        <div className="sm:col-span-2">
          <Field id="execution-connection-url" label={wordpress ? "WordPress 站点地址" : "CMS 站点地址"}>
            {wordpress ? (
              <Input
                id="execution-connection-url"
                type="url"
                name="base_url"
                defaultValue={initial?.base_url ?? site.base_url}
                required
                maxLength={2048}
                aria-describedby="execution-connection-url-hint"
              />
            ) : (
              <Input
                id="execution-connection-url"
                type="url"
                value={site.base_url}
                readOnly
                aria-describedby="execution-connection-url-hint"
              />
            )}
            <p id="execution-connection-url-hint" className="text-muted-foreground text-xs">
              {wordpress
                ? "可填写同站点的 HTTPS 安装子目录。更换域名请先编辑「站点管理」。更改安装地址后须重填凭据。"
                : "沿用站点管理中的 HTTPS 地址。"}
            </p>
          </Field>
        </div>
      )}
      {isGoogleConnection && (
        <>
          <div className="sm:col-span-2">
            <Field id="execution-google-property" label={kind === "gsc" ? "资源地址或 sc-domain" : "GA4 属性 ID"}>
              <Input
                id="execution-google-property"
                name="property"
                placeholder={kind === "gsc" ? site.base_url : "properties/123456789"}
                defaultValue={initial?.property}
                required
                maxLength={2048}
              />
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Field id="execution-google-redirect" label="Google 授权回调地址">
              <Input
                id="execution-google-redirect"
                name="redirect_uri"
                type="url"
                defaultValue={initial?.redirect_uri ?? defaultReturn}
                required
                maxLength={2048}
              />
            </Field>
          </div>
          <Field id="execution-google-client" label="Google 客户端 ID">
            <Input id="execution-google-client" name="client_id" maxLength={4096} required={!initial} />
          </Field>
          <Field id="execution-google-secret" label="Google 客户端密钥">
            <Input
              id="execution-google-secret"
              name="client_secret"
              type="password"
              autoComplete="new-password"
              maxLength={4096}
              required={!initial}
            />
          </Field>
        </>
      )}
      {kind === "bing" && (
        <div className="sm:col-span-2">
          <Field id="execution-bing-property" label="已验证的站点地址">
            <Input
              id="execution-bing-property"
              name="property"
              type="url"
              defaultValue={initial?.property ?? site.base_url}
              maxLength={2048}
              required
            />
          </Field>
        </div>
      )}
      {kind === "indexnow" && (
        <div className="sm:col-span-2">
          <Field id="execution-indexnow-location" label="IndexNow 密钥文件地址">
            <Input
              id="execution-indexnow-location"
              name="key_location"
              type="url"
              defaultValue={initial?.key_location}
              required
              maxLength={2048}
            />
          </Field>
        </div>
      )}
      {wordpress && (
        <Field id="execution-wordpress-user" label="WordPress 用户名">
          <Input id="execution-wordpress-user" name="username" maxLength={200} required={!initial} />
        </Field>
      )}
      {!isGoogleConnection && (
        <div className={wordpress ? undefined : "sm:col-span-2"}>
          <Field id="execution-connection-secret" label={credentialLabel[kind] ?? "API 密钥"}>
            <Input
              id="execution-connection-secret"
              name="credential"
              type="password"
              autoComplete="new-password"
              maxLength={kind === "indexnow" ? 128 : 4096}
              required={!initial}
              aria-describedby={wordpress ? "execution-wordpress-password-hint" : undefined}
            />
            {wordpress && (
              <p id="execution-wordpress-password-hint" className="text-muted-foreground text-xs">
                在 WordPress 的「用户 → 个人资料 → 应用程序密码」生成。启用并保存后可测试连接。
              </p>
            )}
          </Field>
        </div>
      )}
      <Field id="execution-connection-minute" label="每分钟请求上限">
        <Input
          id="execution-connection-minute"
          name="requests_per_minute"
          type="number"
          min={1}
          max={120}
          defaultValue={initial?.requests_per_minute ?? 30}
          required
        />
      </Field>
      <Field id="execution-connection-day" label="每日请求上限">
        <Input
          id="execution-connection-day"
          name="requests_per_day"
          type="number"
          min={1}
          max={25000}
          defaultValue={initial?.requests_per_day ?? 500}
          required
        />
      </Field>
      <Field id="execution-connection-timeout" label="请求超时（秒）">
        <Input
          id="execution-connection-timeout"
          name="timeout_seconds"
          type="number"
          min={1}
          max={30}
          defaultValue={initial?.timeout_seconds ?? 12}
          required
        />
      </Field>
      <Field id="execution-connection-enabled" label="启用连接">
        <Switch id="execution-connection-enabled" name="enabled" defaultChecked={initial?.enabled ?? false} />
      </Field>
      {writesAvailable && (
        <Field id="execution-connection-writes" label={isCMS(kind) ? "授权此连接写入站点" : "授权此连接提交索引请求"}>
          <Switch
            id="execution-connection-writes"
            name="allow_writes"
            defaultChecked={initial?.allow_writes ?? false}
          />
        </Field>
      )}
    </FormDialog>
  );
}

function connectionCapabilities(connection: Connection): string[] {
  const caps = connection.capabilities;
  if (!caps) return [];
  return [
    caps.read_pages ? "页面读取" : "",
    connection.allow_writes && caps.write_pages ? "草稿写入" : "",
    connection.allow_writes && caps.publish ? "发布" : "",
    connection.allow_writes && caps.schedule ? "定时发布" : "",
    caps.read_media ? "媒体读取" : "",
    connection.allow_writes && caps.write_media ? "媒体上传" : "",
  ].filter(Boolean);
}

export function ConnectionsPanel({
  site,
  owner,
  writable,
  onTask,
  changed,
  revision,
}: {
  site: Site;
  owner: boolean;
  writable: boolean;
  onTask: (selection: TaskSelection) => void;
  changed: () => void;
  revision: number;
}) {
  const [editor, setEditor] = useState<"new" | Connection>();
  const [authorizing, setAuthorizing] = useState<string>();
  const { href, projectID, siteID } = useSeoLocation();
  const records = useExecutionRecords<Connection>("connections", site.id, revision);
  const { refresh } = records;

  async function authorize(connection: Connection) {
    setAuthorizing(connection.id);
    try {
      await startGoogle(
        connection,
        siteID === connection.site_id ? projectID : undefined,
        href(seoRoutes.sites.connections.path, { site_id: connection.site_id }).split("?", 2)[1],
      );
    } catch (error) {
      toast.error(errorMessage(error));
      setAuthorizing(undefined);
    }
  }

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-semibold">站点连接</h2>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" onClick={refresh} disabled={records.refreshing}>
            <RefreshCw /> 刷新连接
          </Button>
          {owner && (
            <Button type="button" size="sm" onClick={() => setEditor("new")}>
              <Plus /> 新建连接
            </Button>
          )}
        </div>
      </div>
      <RecordsTable
        label="站点连接"
        columns={["连接", "状态", "已检测能力", "最近更新（UTC）", "操作"]}
        empty="暂无连接"
        list={records}
      >
        {records.records.map((connection) => (
          <TableRow key={connection.id}>
            <TableCell className="px-3 py-2">
              <div className="font-medium">{connection.name}</div>
              <div className="text-muted-foreground text-xs">{connectionLabels[connection.kind]}</div>
              <div className="max-w-64 whitespace-normal break-all text-muted-foreground text-xs">
                {connection.property || connection.base_url}
              </div>
            </TableCell>
            <TableCell className="px-3">
              <StateBadge
                label={connection.enabled ? "已启用" : "已停用"}
                tone={connection.enabled ? "secondary" : "outline"}
              />
              <div className="mt-1 text-muted-foreground text-xs">
                {connection.configured ? "已保存凭据" : "待配置凭据"}
              </div>
            </TableCell>
            <TableCell className="px-3">
              <div>{isCMS(connection.kind) ? connectionCapabilities(connection).join("、") || "待检测" : "—"}</div>
              {connection.capabilities?.seo_plugin && (
                <div className="text-muted-foreground text-xs">
                  {connection.capabilities.seo_plugin} {connection.capabilities.seo_fields?.join("、")}
                </div>
              )}
            </TableCell>
            <TableCell className="px-3 tabular-nums">{formatTime(connection.updated_at)}</TableCell>
            <TableCell className="px-3">
              {writable && connection.enabled && connection.configured && isCMS(connection.kind) && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => onTask({ kind: "cms_connection_test", connection })}
                >
                  <Plug /> 测试连接
                </Button>
              )}
              {owner && connection.enabled && connection.configured && isGoogle(connection.kind) && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={!!authorizing}
                  onClick={() => void authorize(connection)}
                >
                  <KeyRound /> 授权 Google
                </Button>
              )}
              {owner && (
                <Button type="button" variant="ghost" size="sm" onClick={() => setEditor(connection)}>
                  <Pencil /> 编辑
                </Button>
              )}
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {editor && (
        <ConnectionEditor
          initial={editor === "new" ? undefined : editor}
          site={site}
          onClose={() => setEditor(undefined)}
          onSaved={() => {
            refresh();
            changed();
          }}
        />
      )}
    </div>
  );
}
