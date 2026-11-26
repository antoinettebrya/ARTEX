"use client";

import { useState } from "react";

import { Pencil, Plus, RefreshCw, Settings2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { type Provider, type ProviderKind, SeoAPIError, seoRequest } from "@/lib/seo/client";
import { usd } from "@/lib/seo/research";

import { ModelProfileEditor } from "./content/forms";
import { EmptyContent } from "./content/views";
import { useSeoQuery } from "./query";
import { useSeoSession } from "./session";
import {
  Field,
  FormDialog,
  field,
  LoadError,
  PageFrame,
  PageSkeleton,
  RecordsTable,
  StateBadge,
  useSeoList,
} from "./shared";

const providerNames: Record<ProviderKind, string> = {
  openai: "OpenAI",
  anthropic: "Claude",
  gemini: "Gemini",
  deepseek: "DeepSeek",
  dataforseo: "DataForSEO",
};

type ContentUsage = {
  provider_id: string;
  name: string;
  requests: number;
  used_tokens: number;
  reserved_tokens: number;
  spent_micros: number;
  reserved_micros: number;
  daily_tokens: number;
  daily_cost_micros: number;
};

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
  const [kind, setKind] = useState<ProviderKind>(provider?.kind ?? "openai");
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
          kind,
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
          onChange={(event) => setKind(event.target.value as ProviderKind)}
        >
          {Object.entries(providerNames).map(([value, label]) => (
            <NativeSelectOption value={value} key={value}>
              {label}
            </NativeSelectOption>
          ))}
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
          <Field id="provider-model" label={`${providerNames[kind]} 模型`}>
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
  const [model, setModel] = useState<Provider>();

  return (
    <PageFrame
      title="Provider"
      description="管理模型和数据服务的调用配额与预算。"
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
                {provider.kind === "dataforseo"
                  ? "DataForSEO"
                  : `${providerNames[provider.kind ?? "openai"]} · ${provider.model}`}
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
                {provider.kind !== "dataforseo" && (
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={`配置 ${provider.name} 内容模型`}
                    onClick={() => setModel(provider)}
                  >
                    <Settings2 />
                  </Button>
                )}
              </TableCell>
            )}
          </TableRow>
        ))}
      </RecordsTable>
      {owner && <ContentModelUsage />}
      {edit !== undefined && owner && (
        <ProviderEditor provider={edit ?? undefined} onClose={() => setEdit(undefined)} onSaved={list.refresh} />
      )}
      {model && owner && <ModelProfileEditor provider={model} onClose={() => setModel(undefined)} />}
    </PageFrame>
  );
}

function ContentModelUsage() {
  const usage = useSeoQuery<{ items: ContentUsage[] }>("/content/usage");
  const rows = usage.data?.items ?? [];
  return (
    <section className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-medium text-sm">今日内容模型用量（UTC）</h2>
        <Button type="button" size="sm" variant="outline" onClick={usage.refresh}>
          <RefreshCw /> 刷新模型用量
        </Button>
      </div>
      {usage.loading && <PageSkeleton />}
      {usage.error !== undefined && <LoadError error={usage.error} retry={usage.refresh} />}
      {!!rows.length && (
        <Table aria-label="内容模型用量">
          <TableHeader>
            <TableRow>
              <TableHead scope="col">Provider</TableHead>
              <TableHead scope="col">请求</TableHead>
              <TableHead scope="col">已用 / 预留 / 预算 Token</TableHead>
              <TableHead scope="col">估算费用 / 预留 / 日预算</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.provider_id}>
                <TableCell>{row.name}</TableCell>
                <TableCell className="tabular-nums">{row.requests}</TableCell>
                <TableCell className="tabular-nums">
                  {row.used_tokens} / {row.reserved_tokens} / {row.daily_tokens}
                </TableCell>
                <TableCell className="tabular-nums">
                  {[row.spent_micros, row.reserved_micros, row.daily_cost_micros]
                    .map((value) => usd.format(value / 1_000_000))
                    .join(" / ")}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {!usage.loading && usage.error === undefined && rows.length === 0 && <EmptyContent title="暂无内容模型用量" />}
    </section>
  );
}
