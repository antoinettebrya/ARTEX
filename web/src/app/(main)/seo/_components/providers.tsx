"use client";

import { useState } from "react";

import { Pencil, Plus } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";
import { TableCell, TableRow } from "@/components/ui/table";
import { type Provider, SeoAPIError, seoRequest } from "@/lib/seo/client";
import { usd } from "@/lib/seo/research";

import { useSeoSession } from "./session";
import { field, Field, FormDialog, PageFrame, RecordsTable, StateBadge, useSeoList } from "./shared";

function limitLabel(provider: Provider) {
  if (provider.kind !== "dataforseo") return `${provider.max_output_tokens} Token`;
  const cost = provider.daily_cost_micros;
  return cost === undefined ? "—" : usd.format(cost / 1_000_000);
}

function ProviderEditor({
  provider,
  onClose,
  onSaved,
}: {
  provider?: Provider;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [kind, setKind] = useState(provider?.kind ?? "openai");
  const isData = kind === "dataforseo";

  async function save(data: FormData) {
    const login = field(data, "login").trim();
    const password = field(data, "password");
    if (isData && Boolean(login) !== Boolean(password)) {
      throw new SeoAPIError(400, "provider_credentials_pair");
    }
    const providerSettings = isData
      ? {
          kind,
          login: login || undefined,
          password: password || undefined,
          daily_cost_micros: Math.round(Number(field(data, "daily_cost")) * 1_000_000),
        }
      : {
          model: field(data, "model"),
          api_key: field(data, "api_key") || undefined,
          max_output_tokens: Number(field(data, "max_output_tokens")),
        };
    await seoRequest(provider ? `/providers/${provider.id}` : "/providers", {
      method: provider ? "PUT" : "POST",
      body: {
        ...providerSettings,
        name: field(data, "name"),
        timeout_seconds: Number(field(data, "timeout_seconds")),
        requests_per_minute: Number(field(data, "requests_per_minute")),
        requests_per_day: Number(field(data, "requests_per_day")),
        enabled: data.has("enabled"),
        version: provider?.version,
      },
    });
    onSaved();
    toast.success("Provider 已保存");
  }

  return (
    <FormDialog
      title={provider ? "编辑 Provider" : "添加 Provider"}
      description="设置凭据和请求限制。每日配额以 UTC 日为准。"
      submitLabel="保存 Provider"
      onSubmit={save}
      onClose={onClose}
    >
      <Field id="provider-kind" label="类型">
        <NativeSelect
          id="provider-kind"
          value={kind}
          disabled={!!provider}
          className="w-full"
          onChange={(event) => setKind(event.target.value === "dataforseo" ? "dataforseo" : "openai")}
        >
          <NativeSelectOption value="openai">OpenAI</NativeSelectOption>
          <NativeSelectOption value="dataforseo">DataForSEO</NativeSelectOption>
        </NativeSelect>
      </Field>
      <Field id="provider-name" label="名称">
        <Input id="provider-name" name="name" required maxLength={100} defaultValue={provider?.name} />
      </Field>
      {isData ? (
        <>
          <Field id="provider-login" label="DataForSEO 登录名">
            <Input
              id="provider-login"
              name="login"
              autoComplete="off"
              maxLength={254}
              required={!provider}
              placeholder={provider ? "与 API 密码一起更新" : undefined}
            />
          </Field>
          <Field id="provider-password" label="DataForSEO API 密码">
            <Input
              id="provider-password"
              name="password"
              type="password"
              autoComplete="new-password"
              maxLength={512}
              required={!provider}
              placeholder={provider ? "两项留空保留凭据" : undefined}
            />
          </Field>
          <Field id="provider-cost" label="每日费用上限（USD）">
            <Input
              id="provider-cost"
              name="daily_cost"
              type="number"
              required
              min={0.000001}
              max={100_000}
              step={0.000001}
              defaultValue={(provider?.daily_cost_micros ?? 1_000_000) / 1_000_000}
            />
          </Field>
        </>
      ) : (
        <>
          <Field id="provider-model" label="OpenAI 模型">
            <Input id="provider-model" name="model" required maxLength={100} defaultValue={provider?.model} />
          </Field>
          <Field id="provider-key" label="API 密钥">
            <Input
              id="provider-key"
              name="api_key"
              type="password"
              required={!provider}
              autoComplete="off"
              minLength={16}
              maxLength={512}
              placeholder={provider ? "留空保留现有密钥" : undefined}
            />
          </Field>
          <Field id="provider-output" label="单次输出 Token 上限">
            <Input
              id="provider-output"
              name="max_output_tokens"
              type="number"
              min={16}
              max={4096}
              defaultValue={provider?.max_output_tokens ?? 1024}
              required
            />
          </Field>
        </>
      )}
      <Field id="provider-timeout" label="超时（秒）">
        <Input
          id="provider-timeout"
          name="timeout_seconds"
          type="number"
          min={1}
          max={30}
          defaultValue={provider?.timeout_seconds ?? 20}
          required
        />
      </Field>
      <Field id="provider-rpm" label="每分钟请求上限">
        <Input
          id="provider-rpm"
          name="requests_per_minute"
          type="number"
          min={1}
          max={isData ? 12 : 60}
          defaultValue={provider?.requests_per_minute ?? 5}
          required
        />
      </Field>
      <Field id="provider-rpd" label="每日请求上限">
        <Input
          id="provider-rpd"
          name="requests_per_day"
          type="number"
          min={1}
          max={10000}
          defaultValue={provider?.requests_per_day ?? 30}
          required
        />
      </Field>
      <div className="flex items-center gap-2 sm:col-span-2">
        <Switch id="provider-enabled" name="enabled" defaultChecked={provider?.enabled ?? true} />
        <Label htmlFor="provider-enabled">启用</Label>
      </div>
    </FormDialog>
  );
}

export function Providers() {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const list = useSeoList<Provider>("providers");
  const [edit, setEdit] = useState<Provider | null>();

  return (
    <PageFrame
      title="Provider"
      description="管理 OpenAI 和 DataForSEO 的调用配额与预算。"
      actions={
        owner && (
          <Button variant="outline" size="sm" onClick={() => setEdit(null)}>
            <Plus /> 添加 Provider
          </Button>
        )
      }
    >
      <RecordsTable
        label="已配置的 Provider"
        columns={["Provider", "每分钟 / 每日请求", "输出上限 / 日预算", "超时", "状态", ...(owner ? ["操作"] : [])]}
        empty="暂无 Provider"
        list={list}
      >
        {list.records.map((provider) => (
          <TableRow key={provider.id}>
            <TableCell className="px-3 py-2">
              <div className="font-medium">{provider.name}</div>
              <code className="font-mono text-muted-foreground text-xs">
                {provider.kind === "dataforseo" ? "DataForSEO" : provider.model}
              </code>
            </TableCell>
            <TableCell className="px-3 tabular-nums">
              {provider.requests_per_minute} / {provider.requests_per_day}
            </TableCell>
            <TableCell className="px-3 tabular-nums">{limitLabel(provider)}</TableCell>
            <TableCell className="px-3 tabular-nums">{provider.timeout_seconds} 秒</TableCell>
            <TableCell className="px-3">
              <StateBadge
                label={provider.enabled ? "启用" : "停用"}
                tone={provider.enabled ? "secondary" : "outline"}
              />
            </TableCell>
            {owner && (
              <TableCell className="px-3">
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={`编辑 ${provider.name}`}
                  onClick={() => setEdit(provider)}
                >
                  <Pencil />
                </Button>
              </TableCell>
            )}
          </TableRow>
        ))}
      </RecordsTable>
      {edit !== undefined && owner && (
        <ProviderEditor provider={edit ?? undefined} onClose={() => setEdit(undefined)} onSaved={list.refresh} />
      )}
    </PageFrame>
  );
}
