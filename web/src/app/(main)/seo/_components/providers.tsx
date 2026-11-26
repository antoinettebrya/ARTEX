"use client";

import { useState } from "react";

import { Pencil, Plus } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { TableCell, TableRow } from "@/components/ui/table";
import { type Provider, seoRequest } from "@/lib/seo/client";

import { useSeoSession } from "./session";
import { field, Field, FormDialog, PageFrame, RecordsTable, StateBadge, useSeoList } from "./shared";

export function Providers() {
  const { principal } = useSeoSession();
  const owner = principal.role === "owner";
  const list = useSeoList<Provider>("providers");
  const [edit, setEdit] = useState<Provider | null>();

  async function save(data: FormData) {
    await seoRequest(edit ? `/providers/${edit.id}` : "/providers", {
      method: edit ? "PUT" : "POST",
      body: {
        name: field(data, "name"),
        model: field(data, "model"),
        api_key: field(data, "api_key") || undefined,
        timeout_seconds: Number(field(data, "timeout_seconds")),
        requests_per_minute: Number(field(data, "requests_per_minute")),
        requests_per_day: Number(field(data, "requests_per_day")),
        max_output_tokens: Number(field(data, "max_output_tokens")),
        enabled: data.has("enabled"),
        version: edit?.version,
      },
    });
    list.refresh();
    toast.success("Provider 已保存");
  }

  return (
    <PageFrame
      title="Provider"
      description="管理 OpenAI 模型、请求配额和输出上限。"
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
        columns={["Provider", "每分钟 / 每日请求", "输出 Token / 次", "超时", "状态", ...(owner ? ["操作"] : [])]}
        empty="暂无 Provider"
        list={list}
      >
        {list.records.map((provider) => (
          <TableRow key={provider.id}>
            <TableCell className="px-3 py-2">
              <div className="font-medium">{provider.name}</div>
              <code className="font-mono text-muted-foreground text-xs">{provider.model}</code>
            </TableCell>
            <TableCell className="px-3 tabular-nums">
              {provider.requests_per_minute} / {provider.requests_per_day}
            </TableCell>
            <TableCell className="px-3 tabular-nums">{provider.max_output_tokens}</TableCell>
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
        <FormDialog
          title={edit ? "编辑 Provider" : "添加 Provider"}
          description="设置模型和请求限制。每日配额以 UTC 日为准。"
          submitLabel="保存 Provider"
          onSubmit={save}
          onClose={() => setEdit(undefined)}
        >
          <Field id="provider-name" label="名称">
            <Input id="provider-name" name="name" required maxLength={100} defaultValue={edit?.name} />
          </Field>
          <Field id="provider-model" label="OpenAI 模型">
            <Input id="provider-model" name="model" required maxLength={100} defaultValue={edit?.model} />
          </Field>
          <div className="sm:col-span-2">
            <Field id="provider-key" label="API 密钥">
              <Input
                id="provider-key"
                name="api_key"
                type="password"
                required={!edit}
                autoComplete="off"
                minLength={16}
                maxLength={512}
                placeholder={edit ? "留空保留现有密钥" : undefined}
              />
            </Field>
          </div>
          <Field id="provider-timeout" label="超时（秒）">
            <Input
              id="provider-timeout"
              name="timeout_seconds"
              type="number"
              min={1}
              max={30}
              defaultValue={edit?.timeout_seconds ?? 20}
              required
            />
          </Field>
          <Field id="provider-rpm" label="每分钟请求上限">
            <Input
              id="provider-rpm"
              name="requests_per_minute"
              type="number"
              min={1}
              max={60}
              defaultValue={edit?.requests_per_minute ?? 5}
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
              defaultValue={edit?.requests_per_day ?? 30}
              required
            />
          </Field>
          <Field id="provider-output" label="单次输出 Token 上限">
            <Input
              id="provider-output"
              name="max_output_tokens"
              type="number"
              min={16}
              max={4096}
              defaultValue={edit?.max_output_tokens ?? 1024}
              required
            />
          </Field>
          <div className="flex items-center gap-2 sm:col-span-2">
            <Switch id="provider-enabled" name="enabled" defaultChecked={edit?.enabled ?? true} />
            <Label htmlFor="provider-enabled">启用</Label>
          </div>
        </FormDialog>
      )}
    </PageFrame>
  );
}
