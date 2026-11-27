"use client";

import { useState } from "react";

import { Pencil, Plug, Plus, RefreshCw, Settings2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  errorMessage,
  type ModelOptions,
  type Provider,
  type ProviderKind,
  SeoAPIError,
  seoRequest,
} from "@/lib/seo/client";
import { usd } from "@/lib/seo/research";

import { ModelProfileEditor } from "./content/forms";
import { EmptyContent } from "./content/views";
import { useSeoQuery } from "./query";
import { useSeoSession } from "./session";
import {
  Field,
  FormDialog,
  field,
  formatTime,
  LoadError,
  PageFrame,
  PageSkeleton,
  RecordsTable,
  StateBadge,
  useSeoRecords,
} from "./shared";

const modelNames: Record<Exclude<ProviderKind, "dataforseo">, string> = {
  openai: "OpenAI",
  anthropic: "Claude",
  gemini: "Gemini",
  deepseek: "DeepSeek",
  openai_compatible: "OpenAI 兼容",
};

type ProviderCategory = "model" | "data";

const providerCategories = {
  model: {
    label: "大模型服务",
    item: "大模型",
    description: "用于内容工作室等 AI 任务。测试时检查 API 密钥及所填模型的访问权限。",
    limit: "单次输出上限",
  },
  data: {
    label: "DataForSEO 数据源",
    item: "DataForSEO",
    description: "用于关键词、搜索量和 SERP 研究。测试时验证 DataForSEO API 账号。",
    limit: "每日费用上限（USD）",
  },
} satisfies Record<ProviderCategory, { label: string; item: string; description: string; limit: string }>;

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
  category,
  provider,
  onClose,
  onSaved,
}: {
  category: ProviderCategory;
  provider?: Provider;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [kind, setKind] = useState(provider?.kind && provider.kind !== "dataforseo" ? provider.kind : "openai");
  const isData = category === "data";
  const { item } = providerCategories[category];

  async function save(data: FormData) {
    const login = field(data, "login").trim();
    const password = field(data, "password");
    if (isData && Boolean(login) !== Boolean(password)) {
      throw new SeoAPIError(400, "provider_credentials_pair");
    }
    const options = Object.fromEntries(
      [
        "base_url",
        "protocol",
        "auth_header",
        "auth_mode",
        "token_field",
        "temperature",
        "reasoning_effort",
        "output_format",
        "workspace_id",
      ]
        .map((name) => [name, field(data, name).trim()])
        .filter(([, value]) => value),
    );
    const providerSettings = isData
      ? {
          kind: "dataforseo",
          login: login || undefined,
          password: password || undefined,
          daily_cost_micros: Math.round(Number(field(data, "daily_cost")) * 1_000_000),
        }
      : {
          kind,
          model: field(data, "model"),
          api_key: field(data, "api_key") || undefined,
          max_output_tokens: Number(field(data, "max_output_tokens")),
          model_options: options,
        };
    try {
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
    } catch (error) {
      if (
        !isData &&
        error instanceof SeoAPIError &&
        error.code === "invalid_request" &&
        ["api_key", "base_url", "auth_header", "output_format", "reasoning_effort", "workspace_id"].includes(
          error.field ?? "",
        )
      ) {
        throw new SeoAPIError(error.status, `provider_model_${error.field}`);
      }
      throw error;
    }
    onSaved();
    toast.success("配置已保存，可在列表中测试连接。");
  }

  return (
    <FormDialog
      title={`${provider ? "编辑" : "添加"}${item}`}
      description={
        isData
          ? "凭据取自 DataForSEO 后台的 API Access。每日费用和请求配额按 UTC 计算。"
          : "填入所选厂商的 API 密钥和模型 ID。每日请求配额按 UTC 计算。"
      }
      submitLabel="保存配置"
      onSubmit={save}
      onClose={onClose}
    >
      {!isData && (
        <Field id="provider-kind" label="模型厂商">
          <NativeSelect
            id="provider-kind"
            value={kind}
            disabled={!!provider}
            className="w-full"
            onChange={(event) => setKind(event.target.value as typeof kind)}
          >
            {Object.entries(modelNames).map(([value, label]) => (
              <NativeSelectOption value={value} key={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
      )}
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
          <Field id="provider-model" label={`${modelNames[kind]} 模型 ID`}>
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
          <ModelCapabilities key={kind} kind={kind} options={provider?.model_options} />
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

function ModelCapabilities({ kind, options }: { kind: Exclude<ProviderKind, "dataforseo">; options?: ModelOptions }) {
  const compatible = kind === "openai_compatible";
  const [protocol, setProtocol] = useState(options?.protocol ?? "chat");
  const efforts = kind === "deepseek" ? ["none", "high", "max"] : ["none", "minimal", "low", "medium", "high"];
  if (kind === "openai" || compatible) efforts.push("xhigh");
  return (
    <details open={compatible || undefined} className="space-y-3 sm:col-span-2">
      <summary className="cursor-pointer font-medium text-sm">接口和模型能力</summary>
      <div className="grid gap-3 sm:grid-cols-2">
        {compatible && (
          <>
            <div className="sm:col-span-2">
              <Field id="model-base-url" label="API Base URL">
                <Input
                  id="model-base-url"
                  name="base_url"
                  type="url"
                  placeholder="https://api.example.com/v1"
                  defaultValue={options?.base_url}
                  required
                  maxLength={2048}
                />
              </Field>
            </div>
            <Field id="model-protocol" label="接口协议">
              <NativeSelect
                id="model-protocol"
                name="protocol"
                value={protocol}
                onChange={(e) => setProtocol(e.target.value)}
                className="w-full"
              >
                <NativeSelectOption value="chat">Chat Completions</NativeSelectOption>
                <NativeSelectOption value="responses">Responses</NativeSelectOption>
              </NativeSelect>
            </Field>
            {protocol === "chat" && (
              <Field id="model-token-field" label="输出 Token 参数">
                <NativeSelect
                  id="model-token-field"
                  name="token_field"
                  defaultValue={options?.token_field ?? ""}
                  className="w-full"
                >
                  <NativeSelectOption value="">max_tokens（默认）</NativeSelectOption>
                  <NativeSelectOption value="max_completion_tokens">max_completion_tokens</NativeSelectOption>
                </NativeSelect>
              </Field>
            )}
            <Field id="model-auth-header" label="认证 Header">
              <Input
                id="model-auth-header"
                name="auth_header"
                placeholder="默认 Authorization"
                defaultValue={options?.auth_header}
                maxLength={64}
              />
            </Field>
            <Field id="model-auth-mode" label="认证方式">
              <NativeSelect
                id="model-auth-mode"
                name="auth_mode"
                defaultValue={options?.auth_mode ?? ""}
                className="w-full"
              >
                <NativeSelectOption value="">随 Header 自动选择</NativeSelectOption>
                <NativeSelectOption value="bearer">Bearer 密钥</NativeSelectOption>
                <NativeSelectOption value="raw">原始密钥</NativeSelectOption>
              </NativeSelect>
            </Field>
          </>
        )}
        {kind === "anthropic" && (
          <>
            <Field id="model-claude-protocol" label="接口协议">
              <NativeSelect
                id="model-claude-protocol"
                name="protocol"
                defaultValue={options?.protocol ?? ""}
                className="w-full"
              >
                <NativeSelectOption value="">OpenAI 兼容</NativeSelectOption>
                <NativeSelectOption value="anthropic_messages">Claude Messages</NativeSelectOption>
              </NativeSelect>
            </Field>
            <Field id="model-workspace" label="Claude Workspace ID（选填）">
              <Input id="model-workspace" name="workspace_id" defaultValue={options?.workspace_id} maxLength={120} />
            </Field>
          </>
        )}
        <Field id="model-temperature" label="Temperature">
          <NativeSelect
            id="model-temperature"
            name="temperature"
            defaultValue={options?.temperature ?? ""}
            className="w-full"
          >
            <NativeSelectOption value="">按模型能力自动处理</NativeSelectOption>
            <NativeSelectOption value="off">省略</NativeSelectOption>
            <NativeSelectOption value="on">允许发送配置值</NativeSelectOption>
          </NativeSelect>
        </Field>
        {kind !== "anthropic" && (
          <Field id="model-reasoning" label="推理强度">
            <NativeSelect
              id="model-reasoning"
              name="reasoning_effort"
              defaultValue={options?.reasoning_effort ?? ""}
              className="w-full"
            >
              <NativeSelectOption value="">厂商默认</NativeSelectOption>
              {efforts.map((effort) => (
                <NativeSelectOption key={effort} value={effort}>
                  {effort}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
        )}
        <Field id="model-output-format" label="输出格式">
          <NativeSelect
            id="model-output-format"
            name="output_format"
            defaultValue={options?.output_format ?? ""}
            className="w-full"
          >
            <NativeSelectOption value="">按模型自动选择</NativeSelectOption>
            <NativeSelectOption value="text">纯文本传输</NativeSelectOption>
            {kind !== "anthropic" && <NativeSelectOption value="json_object">JSON Object</NativeSelectOption>}
            {kind !== "deepseek" && <NativeSelectOption value="json_schema">严格 JSON Schema</NativeSelectOption>}
          </NativeSelect>
        </Field>
      </div>
    </details>
  );
}

export function Providers() {
  const { principal } = useSeoSession();
  return (
    <PageFrame title="大模型服务商" description="管理大模型和 SEO 数据源的凭据、配额及连接状态。">
      <Tabs defaultValue="model">
        <TabsList aria-label="服务类型">
          <TabsTrigger value="model">{providerCategories.model.label}</TabsTrigger>
          <TabsTrigger value="data">{providerCategories.data.label}</TabsTrigger>
          {principal.role === "owner" && <TabsTrigger value="diagnostics">请求诊断</TabsTrigger>}
        </TabsList>
        <TabsContent value="model">
          <ProviderPanel category="model" />
        </TabsContent>
        <TabsContent value="data">
          <ProviderPanel category="data" />
        </TabsContent>
        {principal.role === "owner" && (
          <TabsContent value="diagnostics">
            <ProviderDiagnostics />
          </TabsContent>
        )}
      </Tabs>
    </PageFrame>
  );
}

type Receipt = {
  call_id: string;
  occurred_at: string;
  diagnostic: {
    provider: string;
    operation: string;
    http_status?: number;
    request_id?: string;
    code?: string;
    upstream_code?: string;
    parameter?: string;
    category: string;
    retryable?: boolean;
    may_be_charged?: boolean;
    duration_ms?: number;
    input_tokens?: number;
    output_tokens?: number;
    cost_micros?: number;
    cost_source?: string;
  };
};

const costSources: Record<string, string> = {
  provider: "厂商回执",
  estimated: "按 Token 估算",
  no_metered_fee: "无单次计费",
  unbilled: "未计费",
  unknown: "金额待确认",
};

function ProviderDiagnostics() {
  const list = useSeoRecords<Receipt>("/providers/receipts", "items");
  return (
    <section className="space-y-3">
      <div className="flex justify-end">
        <Button type="button" variant="outline" size="sm" onClick={list.refresh}>
          <RefreshCw /> 更新诊断
        </Button>
      </div>
      <RecordsTable
        label="Provider 请求诊断"
        columns={[
          "服务 / API",
          "HTTP / 耗时",
          "分类 / 错误",
          "请求 ID",
          "Token（输入 / 输出）",
          "费用（USD）",
          "时间（UTC）",
        ]}
        empty="暂无调用记录"
        list={list}
      >
        {list.records.map(({ call_id, occurred_at, diagnostic: d }) => (
          <TableRow key={call_id}>
            <TableCell className="px-3 py-2">
              <div>{d.provider}</div>
              <div className="text-muted-foreground">{d.operation}</div>
            </TableCell>
            <TableCell className="px-3 tabular-nums">
              {d.http_status || "—"} / {d.duration_ms === undefined ? "—" : `${d.duration_ms} ms`}
            </TableCell>
            <TableCell className="px-3">
              <div>{d.category}</div>
              <div className="max-w-60 whitespace-normal break-all text-muted-foreground">
                {[d.code, d.upstream_code, d.parameter].filter(Boolean).join(" · ")}
              </div>
              <div className="text-xs">
                {[d.retryable && "可重试", d.may_be_charged && "可能已计费"].filter(Boolean).join(" · ")}
              </div>
            </TableCell>
            <TableCell className="px-3 font-mono text-xs">{d.request_id || "—"}</TableCell>
            <TableCell className="px-3 tabular-nums">
              {d.input_tokens ?? "—"} / {d.output_tokens ?? "—"}
            </TableCell>
            <TableCell className="px-3 tabular-nums">
              <div>{d.cost_micros == null ? "—" : (d.cost_micros / 1_000_000).toFixed(6)}</div>
              <div className="text-muted-foreground text-xs">{costSources[d.cost_source ?? "unknown"]}</div>
            </TableCell>
            <TableCell className="px-3 tabular-nums">{formatTime(occurred_at)}</TableCell>
          </TableRow>
        ))}
      </RecordsTable>
    </section>
  );
}

function ProviderPanel({ category }: { category: ProviderCategory }) {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const isData = category === "data";
  const { label, item, description, limit } = providerCategories[category];
  const list = useSeoRecords<Provider>(`/providers?category=${category}`, "providers");
  const [edit, setEdit] = useState<Provider | null>();
  const [model, setModel] = useState<Provider>();

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-muted-foreground text-sm">{description}</p>
        {owner && (
          <Button variant="outline" size="sm" onClick={() => setEdit(null)}>
            <Plus /> 添加{item}
          </Button>
        )}
      </div>
      <RecordsTable
        label={label}
        columns={[isData ? "数据源" : "模型", "每分钟 / 每日请求", limit, "超时", "状态", ...(owner ? ["操作"] : [])]}
        empty={`暂无${item}配置`}
        list={list}
      >
        {list.records.map((provider) => (
          <TableRow key={provider.id}>
            <TableCell className="px-3 py-2">
              <div className="font-medium">{provider.name}</div>
              <code className="font-mono text-muted-foreground text-xs">
                {provider.kind === "dataforseo"
                  ? "DataForSEO"
                  : `${modelNames[provider.kind ?? "openai"]} · ${provider.model}`}
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
              <TableCell className="px-3 py-2">
                <div className="flex items-center gap-1">
                  <ProviderConnectionTest key={`${provider.id}:${provider.version}`} provider={provider} />
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={`编辑 ${provider.name}`}
                    onClick={() => setEdit(provider)}
                  >
                    <Pencil />
                  </Button>
                  {!isData && (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={`配置 ${provider.name} 内容模型`}
                      onClick={() => setModel(provider)}
                    >
                      <Settings2 />
                    </Button>
                  )}
                </div>
              </TableCell>
            )}
          </TableRow>
        ))}
      </RecordsTable>
      {owner && !isData && <ContentModelUsage />}
      {edit !== undefined && owner && (
        <ProviderEditor
          category={category}
          provider={edit ?? undefined}
          onClose={() => setEdit(undefined)}
          onSaved={list.refresh}
        />
      )}
      {model && owner && <ModelProfileEditor provider={model} onClose={() => setModel(undefined)} />}
    </section>
  );
}

const connectionMessages: Record<string, string> = {
  invalid_credentials: "厂商拒绝了凭据，请检查密钥或 API 登录信息。",
  access_denied: "厂商拒绝访问，请检查账号或项目权限。",
  address_not_allowed: "DataForSEO 拒绝了当前服务器的 IP，请检查 API Access 中的 IP 白名单。",
  account_unverified: "请先在 DataForSEO 后台完成账号验证。",
  insufficient_balance: "厂商返回余额不足，请检查账单。",
  model_not_found: "在厂商处查不到该模型，请核对模型 ID 和访问权限。",
  model_unsupported: "该 Gemini 模型不支持文本生成。",
  upstream_rate_limited: "厂商触发了限流，请稍后再试。",
  timeout: "连接厂商超时，请稍后重试。",
  unavailable: "暂时无法连接厂商，请稍后重试。",
  cancelled: "连接测试已取消。",
};

function ProviderConnectionTest({ provider }: { provider: Provider }) {
  const [pending, setPending] = useState(false);
  const [feedback, setFeedback] = useState<{ ok: boolean; message: string }>();

  async function testConnection() {
    setPending(true);
    setFeedback(undefined);
    try {
      const result = await seoRequest<{ status: string; duration_ms: number; provider_version: number }>(
        `/providers/${provider.id}/test`,
        { method: "POST" },
      );
      if (result.provider_version !== provider.version) throw new SeoAPIError(409, "version_conflict");
      const ok = result.status === "ok";
      setFeedback({
        ok,
        message: ok
          ? `连接成功（${result.duration_ms} ms）`
          : (connectionMessages[result.status] ?? "厂商返回了无法识别的响应。"),
      });
    } catch (error) {
      setFeedback({ ok: false, message: errorMessage(error) });
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="min-w-0">
      <Button type="button" variant="outline" size="sm" disabled={pending} onClick={testConnection}>
        {pending ? <Spinner aria-hidden="true" /> : <Plug />} {pending ? "测试中…" : "测试连接"}
      </Button>
      <div
        role="status"
        className={`max-w-48 whitespace-normal text-xs ${feedback?.ok ? "text-foreground" : "text-destructive"}`}
      >
        {feedback?.message}
      </div>
    </div>
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
