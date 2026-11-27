"use client";

import { useState } from "react";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";
import { seoRequest } from "@/lib/seo/client";
import { integrationOAuthCallbackPath } from "@/lib/seo/google-oauth";

import { ConfirmChange, Field, FormDialog, field, RecordsTable, useSeoRecords } from "../shared";
import { IntegrationStatus, RowAction, TimeValue } from "./common";
import {
  type Account,
  type AccountProvider,
  type Application,
  accountLabels,
  listPath,
  providerScopes,
  scopeNames,
} from "./model";

type Edit = { open: true; value?: Application };

export function ApplicationsPanel() {
  const list = useSeoRecords<Application>(listPath("applications"), "items");
  const [edit, setEdit] = useState<Edit>();
  const [removing, setRemoving] = useState<Application>();
  return (
    <>
      <div>
        <Button onClick={() => setEdit({ open: true })}>添加 OAuth 应用</Button>
      </div>
      <RecordsTable
        label="OAuth 应用"
        columns={["应用", "服务", "回调", "权限", "状态", "操作"]}
        list={list}
        empty="暂无 OAuth 应用"
      >
        {list.records.map((app) => (
          <TableRow key={app.id}>
            <TableCell>{app.label}</TableCell>
            <TableCell>{accountLabels[app.provider]}</TableCell>
            <TableCell className="max-w-72 break-all">{app.redirect_uri}</TableCell>
            <TableCell>{scopeNames(app.provider, app.allowed_scopes)}</TableCell>
            <TableCell>
              <IntegrationStatus status={app.enabled ? "ENABLED" : "DISABLED"} />{" "}
              <span>{app.configured ? "密钥已保存" : "待配置"}</span>
            </TableCell>
            <TableCell>
              <div className="flex flex-wrap gap-2">
                <RowAction onClick={() => setEdit({ open: true, value: app })}>编辑</RowAction>
                <RowAction onClick={() => setRemoving(app)}>移除</RowAction>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {edit && <ApplicationEditor initial={edit.value} close={() => setEdit(undefined)} saved={list.refresh} />}
      {removing && (
        <ConfirmChange
          title="移除 OAuth 应用"
          description={`移除 ${removing.label}。存在依赖账户时会拒绝移除。`}
          onClose={() => setRemoving(undefined)}
          onConfirm={async () => {
            await seoRequest(`/integrations/applications/${removing.id}`, { method: "DELETE" });
            list.refresh();
          }}
        />
      )}
    </>
  );
}

function ScopeChoices({
  provider,
  selected,
  available,
}: {
  provider: AccountProvider;
  selected: string[];
  available?: string[];
}) {
  const options = Object.entries(providerScopes[provider]).filter(([value]) => !available || available.includes(value));
  return (
    <fieldset className="space-y-2 sm:col-span-2">
      <legend className="mb-1 font-medium text-sm">授权范围</legend>
      {options.map(([scope, label], index) => {
        const id = `integration-scope-${index}`;
        return (
          <div className="flex items-center gap-2" key={scope}>
            <Checkbox id={id} name="scopes" value={scope} defaultChecked={selected.includes(scope)} />
            <Label htmlFor={id}>{label}</Label>
          </div>
        );
      })}
    </fieldset>
  );
}

function ApplicationEditor({ initial, close, saved }: { initial?: Application; close: () => void; saved: () => void }) {
  const [provider, setProvider] = useState<AccountProvider>(initial?.provider ?? "google");
  const callback =
    typeof window === "undefined" ? "" : new URL(integrationOAuthCallbackPath(provider), window.location.origin).href;
  return (
    <FormDialog
      title={initial ? "编辑 OAuth 应用" : "添加 OAuth 应用"}
      description="在服务商控制台登记相同的 HTTPS 回调。编辑时留空密钥可保留当前凭据。"
      submitLabel="保存应用"
      onClose={close}
      onSubmit={async (data) => {
        const clientSecret = field(data, "client_secret").trim();
        await seoRequest(initial ? `/integrations/applications/${initial.id}` : "/integrations/applications", {
          method: initial ? "PUT" : "POST",
          body: {
            provider,
            label: field(data, "label").trim(),
            client_id: field(data, "client_id").trim(),
            client_secret: clientSecret || undefined,
            redirect_uri: field(data, "redirect_uri").trim(),
            allowed_scopes: data.getAll("scopes"),
            enabled: data.has("enabled"),
            config_version: initial?.config_version,
          },
        });
        toast.success("OAuth 应用已保存");
        saved();
      }}
    >
      <Field id="integration-app-provider" label="服务商">
        <NativeSelect
          id="integration-app-provider"
          value={provider}
          disabled={!!initial}
          onChange={(e) => setProvider(e.target.value as AccountProvider)}
        >
          <NativeSelectOption value="google">Google</NativeSelectOption>
          <NativeSelectOption value="bing">Bing Webmaster</NativeSelectOption>
        </NativeSelect>
      </Field>
      <Field id="integration-app-label" label="应用名称">
        <Input id="integration-app-label" name="label" required defaultValue={initial?.label} />
      </Field>
      <Field id="integration-app-client-id" label="Client ID">
        <Input id="integration-app-client-id" name="client_id" required defaultValue={initial?.client_id} />
      </Field>
      <Field id="integration-app-secret" label="Client Secret">
        <Input
          id="integration-app-secret"
          name="client_secret"
          type="password"
          autoComplete="new-password"
          required={!initial}
        />
      </Field>
      <div className="sm:col-span-2">
        <Field id="integration-app-callback" label="回调地址">
          <Input
            key={provider}
            id="integration-app-callback"
            type="url"
            name="redirect_uri"
            required
            defaultValue={initial?.redirect_uri || callback}
          />
        </Field>
      </div>
      <ScopeChoices key={provider} provider={provider} selected={initial?.allowed_scopes ?? []} />
      <div className="flex items-center gap-2">
        <Checkbox id="integration-app-enabled" name="enabled" defaultChecked={initial?.enabled ?? true} />
        <Label htmlFor="integration-app-enabled">启用</Label>
      </div>
    </FormDialog>
  );
}

type AccountAction = { type: "authorize" | "refresh" | "revoke" | "rotate-key" | "delete"; value: Account };

export function AccountsPanel() {
  const list = useSeoRecords<Account>(listPath("accounts"), "items");
  const apps = useSeoRecords<Application>(listPath("applications"), "items");
  const [creating, setCreating] = useState(false);
  const [action, setAction] = useState<AccountAction>();
  return (
    <>
      <div>
        <Button onClick={() => setCreating(true)}>添加外部账户</Button>
      </div>
      <RecordsTable
        label="外部账户"
        columns={["账户", "服务", "权限", "状态", "最后刷新", "站点数", "操作"]}
        list={list}
        empty="暂无授权账户"
      >
        {list.records.map((account) => (
          <TableRow key={account.id}>
            <TableCell>{account.label}</TableCell>
            <TableCell>
              {accountLabels[account.provider]}
              {account.auth_mode === "api_key" && " · API Key"}
            </TableCell>
            <TableCell>
              {account.auth_mode === "api_key" ? "Bing 账户密钥" : scopeNames(account.provider, account.granted_scopes)}
            </TableCell>
            <TableCell>
              <IntegrationStatus status={account.authorization_status} />
            </TableCell>
            <TableCell>
              <TimeValue value={account.last_refresh_at} empty="待刷新" />
            </TableCell>
            <TableCell>{account.site_count ?? "—"}</TableCell>
            <TableCell>
              <div className="flex flex-wrap gap-2">
                {account.auth_mode === "oauth" && (
                  <RowAction onClick={() => setAction({ type: "authorize", value: account })}>授权或增加权限</RowAction>
                )}
                {account.auth_mode === "oauth" && account.authorization_status === "CONNECTED" && (
                  <RowAction onClick={() => setAction({ type: "refresh", value: account })}>刷新授权</RowAction>
                )}
                {account.auth_mode === "api_key" && (
                  <RowAction onClick={() => setAction({ type: "rotate-key", value: account })}>轮换密钥</RowAction>
                )}
                {account.authorization_status !== "REVOKED" && (
                  <RowAction onClick={() => setAction({ type: "revoke", value: account })}>撤销授权</RowAction>
                )}
                <RowAction onClick={() => setAction({ type: "delete", value: account })}>移除</RowAction>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {creating && <AccountEditor apps={apps.records} close={() => setCreating(false)} saved={list.refresh} />}
      {action && (
        <AccountActionDialog
          selection={action}
          apps={apps.records}
          close={() => setAction(undefined)}
          saved={list.refresh}
        />
      )}
    </>
  );
}

function AccountEditor({ apps, close, saved }: { apps: Application[]; close: () => void; saved: () => void }) {
  const [mode, setMode] = useState("oauth");
  return (
    <FormDialog
      title="添加外部账户"
      description="账户凭据可供多个已验证站点使用。"
      submitLabel="保存账户"
      onClose={close}
      onSubmit={async (data) => {
        const application = apps.find((app) => app.id === field(data, "application_id"));
        await seoRequest("/integrations/accounts", {
          method: "POST",
          body: {
            label: field(data, "label").trim(),
            auth_mode: mode,
            provider: mode === "api_key" ? "bing" : application?.provider,
            application_id: mode === "oauth" ? application?.id : undefined,
            api_key: mode === "api_key" ? field(data, "api_key").trim() : undefined,
          },
        });
        toast.success("账户已保存");
        saved();
      }}
    >
      <Field id="integration-account-name" label="账户名称">
        <Input id="integration-account-name" name="label" required />
      </Field>
      <Field id="integration-account-mode" label="认证方式">
        <NativeSelect id="integration-account-mode" value={mode} onChange={(e) => setMode(e.target.value)}>
          <NativeSelectOption value="oauth">OAuth</NativeSelectOption>
          <NativeSelectOption value="api_key">Bing API Key</NativeSelectOption>
        </NativeSelect>
      </Field>
      {mode === "oauth" ? (
        <Field id="integration-account-app" label="OAuth 应用">
          <NativeSelect id="integration-account-app" name="application_id" required defaultValue="">
            <NativeSelectOption value="" disabled>
              请选择
            </NativeSelectOption>
            {apps
              .filter((app) => app.enabled)
              .map((app) => (
                <NativeSelectOption key={app.id} value={app.id}>
                  {app.label}
                </NativeSelectOption>
              ))}
          </NativeSelect>
        </Field>
      ) : (
        <Field id="integration-account-key" label="Bing API Key">
          <Input id="integration-account-key" name="api_key" type="password" required autoComplete="new-password" />
        </Field>
      )}
    </FormDialog>
  );
}

function AccountActionDialog({
  selection,
  apps,
  close,
  saved,
}: {
  selection: AccountAction;
  apps: Application[];
  close: () => void;
  saved: () => void;
}) {
  const { type, value } = selection;
  const target = `/integrations/accounts/${value.id}`;
  if (type === "authorize") {
    const app = apps.find((row) => row.id === value.application_id);
    return (
      <FormDialog
        title="授权外部账户"
        description="勾选本次需要的权限。"
        submitLabel="前往授权"
        onClose={close}
        onSubmit={async (data) => {
          const result = await seoRequest<{ authorization_url: string; state: string }>(`${target}/authorize`, {
            method: "POST",
            body: { scopes: data.getAll("scopes") },
          });
          const url = new URL(result.authorization_url);
          const expected =
            value.provider === "google"
              ? "https://accounts.google.com/o/oauth2/v2/auth"
              : "https://www.bing.com/webmasters/oauth/authorize";
          if (url.origin + url.pathname !== expected || url.searchParams.get("state") !== result.state)
            throw new Error("授权地址无效");
          window.location.assign(url.href);
        }}
      >
        <ScopeChoices provider={value.provider} selected={value.granted_scopes} available={app?.allowed_scopes ?? []} />
      </FormDialog>
    );
  }
  if (type === "rotate-key")
    return (
      <FormDialog
        title="轮换 Bing 密钥"
        description="新密钥将用于此账户的站点绑定。"
        submitLabel="轮换密钥"
        onClose={close}
        onSubmit={async (data) => {
          await seoRequest(`${target}/rotate-key`, {
            method: "POST",
            body: { api_key: field(data, "api_key").trim(), credential_version: value.credential_version },
          });
          saved();
        }}
      >
        <Field id="integration-new-bing-key" label="新 Bing API Key">
          <Input id="integration-new-bing-key" name="api_key" type="password" autoComplete="new-password" required />
        </Field>
      </FormDialog>
    );

  const title = { refresh: "刷新授权", revoke: "撤销授权", delete: "移除账户" }[type];
  return (
    <ConfirmChange
      title={title}
      description={`${value.label}${type === "revoke" ? " 的站点绑定将停止同步。" : ""}`}
      onClose={close}
      onConfirm={async () => {
        if (type === "delete") await seoRequest(target, { method: "DELETE" });
        else {
          const result = await seoRequest<{ provider_result?: string }>(`${target}/${type}`, {
            method: "POST",
            body: {},
          });
          if (result.provider_result === "unconfirmed") toast.warning("本地授权已撤销，服务商撤销待确认。");
        }
        saved();
      }}
    />
  );
}
