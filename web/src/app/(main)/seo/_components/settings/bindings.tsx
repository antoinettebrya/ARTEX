"use client";

import { useState } from "react";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";
import { errorMessage, seoRequest } from "@/lib/seo/client";

import { ConfirmChange, Field, FilterBar, FormDialog, field, RecordsTable, useSeoRecords } from "../shared";
import { IntegrationStatus, RowAction } from "./common";
import {
  type Account,
  type Binding,
  type BindingProvider,
  type IntegrationSite,
  listPath,
  type Resource,
  type SearchProvider,
  serviceLabels,
} from "./model";

type BindAction = { kind: "verify" | "remove"; binding: Binding };
const services = ["gsc", "ga4", "bing", "indexnow"] as const;

export function BindingsPanel({ owner, siteID }: { owner: boolean; siteID?: string }) {
  const [status, setStatus] = useState("");
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<IntegrationSite[]>([]);
  const [editing, setEditing] = useState<IntegrationSite[]>();
  const [action, setAction] = useState<BindAction>();
  const list = useSeoRecords<IntegrationSite>(listPath("sites", { site_id: siteID, status, q: query }), "items");
  const accounts = useSeoRecords<Account>(owner ? listPath("accounts", { status: "CONNECTED" }) : null, "items");
  function toggle(site: IntegrationSite, checked: boolean) {
    setSelected((old) =>
      checked ? [...old.filter((x) => x.id !== site.id), site] : old.filter((x) => x.id !== site.id),
    );
  }
  return (
    <>
      <FilterBar>
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            const data = new FormData(e.currentTarget);
            setQuery(field(data, "q").trim());
            setStatus(field(data, "status"));
            setSelected([]);
          }}
        >
          <Field id="integration-site-search" label="搜索站点">
            <Input id="integration-site-search" name="q" placeholder="名称或站点域名" />
          </Field>
          <Field id="integration-site-status" label="绑定状态">
            <NativeSelect id="integration-site-status" name="status">
              <NativeSelectOption value="">全部</NativeSelectOption>
              <NativeSelectOption value="BOUND">已绑定</NativeSelectOption>
              <NativeSelectOption value="UNBOUND">未绑定</NativeSelectOption>
              <NativeSelectOption value="ISSUES">异常</NativeSelectOption>
            </NativeSelect>
          </Field>
          <Button type="submit" variant="outline">
            筛选
          </Button>
        </form>
        {owner && (
          <Button
            type="button"
            disabled={selected.length === 0 || selected.length > 20}
            onClick={() => setEditing(selected)}
          >
            批量绑定{selected.length ? `（${selected.length}）` : ""}
          </Button>
        )}
      </FilterBar>
      <RecordsTable
        label="站点资源绑定"
        columns={["站点", "GSC", "GA4", "Bing", "IndexNow", ...(owner ? ["操作"] : [])]}
        list={list}
        empty="当前范围没有站点"
      >
        {list.records.map((site) => (
          <TableRow key={site.id}>
            <TableCell>
              <div className="flex items-start gap-2">
                {owner && (
                  <Checkbox
                    aria-label={`选择 ${site.site_name}`}
                    checked={selected.some((x) => x.id === site.id)}
                    onCheckedChange={(x) => toggle(site, x === true)}
                  />
                )}
                <div>
                  <div>{site.site_name}</div>
                  <div className="text-muted-foreground text-xs">{site.site_url}</div>
                </div>
              </div>
            </TableCell>
            {services.map((provider) => {
              const binding = site.bindings.find((x) => x.provider === provider);
              return (
                <TableCell key={provider}>
                  {binding ? (
                    <BindingCell binding={binding} owner={owner} action={setAction} />
                  ) : (
                    <span className="text-muted-foreground">未绑定</span>
                  )}
                </TableCell>
              );
            })}
            {owner && (
              <TableCell>
                <RowAction onClick={() => setEditing([site])}>绑定资源</RowAction>
              </TableCell>
            )}
          </TableRow>
        ))}
      </RecordsTable>
      {editing && (
        <BindingEditor
          sites={editing}
          accounts={accounts.records}
          close={() => setEditing(undefined)}
          saved={() => {
            setSelected([]);
            list.refresh();
          }}
        />
      )}
      {action && (
        <ConfirmChange
          title={action.kind === "remove" ? "解除站点绑定" : "重新验证绑定"}
          description={
            action.kind === "remove"
              ? "该站点将停用此连接，账户及其余站点绑定仍可使用。"
              : "重新检查此站点对资源的权限。验证成功后启用绑定。"
          }
          onClose={() => setAction(undefined)}
          onConfirm={async () => {
            const path = `/integrations/bindings/${action.binding.id}`;
            if (action.kind === "remove") await seoRequest(path, { method: "DELETE" });
            else await seoRequest(`${path}/verify`, { method: "POST", body: { enable: true } });
            list.refresh();
          }}
        />
      )}
    </>
  );
}

function BindingCell({
  binding,
  owner,
  action,
}: {
  binding: Binding;
  owner: boolean;
  action: (action: BindAction) => void;
}) {
  return (
    <div className="space-y-1">
      <div className="max-w-52 break-all text-xs">{binding.external_resource_id}</div>
      {binding.resource?.time_zone && <div className="text-muted-foreground text-xs">{binding.resource.time_zone}</div>}
      <IntegrationStatus status={binding.status} />
      {owner && (
        <div className="flex flex-wrap gap-1">
          <RowAction onClick={() => action({ kind: "verify", binding })}>验证</RowAction>
          <RowAction onClick={() => action({ kind: "remove", binding })}>解除</RowAction>
        </div>
      )}
    </div>
  );
}

function BindingEditor({
  sites,
  accounts,
  close,
  saved,
}: {
  sites: IntegrationSite[];
  accounts: Account[];
  close: () => void;
  saved: () => void;
}) {
  const [provider, setProvider] = useState<SearchProvider>("gsc");
  const [account, setAccount] = useState("");
  const [resources, setResources] = useState<Resource[]>([]);
  const [pageToken, setPageToken] = useState("");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const matching = accounts.filter((a) => a.provider === (provider === "bing" ? "bing" : "google"));
  const needle = query.trim().toLowerCase();
  const visible = resources.filter((r) => [r.id, r.name, r.parent_name].join(" ").toLowerCase().includes(needle));
  function reset() {
    setResources([]);
    setPageToken("");
    setProblem("");
  }
  async function discover(token?: string) {
    setBusy(true);
    setProblem("");
    try {
      const result = await seoRequest<{ items: Resource[]; next_page_token?: string }>(
        `/integrations/accounts/${account}/discover`,
        {
          method: "POST",
          body: { provider, page_token: token },
        },
      );
      setResources((old) =>
        token ? [...old, ...result.items.filter((row) => !old.some((item) => item.id === row.id))] : result.items,
      );
      setPageToken(result.next_page_token ?? "");
      if (!result.items.length && !token) setProblem("该账户暂无可选择的资源。");
    } catch (err) {
      setProblem(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <FormDialog
      title={sites.length === 1 ? "绑定站点资源" : "批量绑定站点资源"}
      description="发现结果需要逐站确认。保存时会检查站点范围和资源权限。"
      submitLabel="确认绑定"
      onClose={close}
      onSubmit={async (data) => {
        if (!account) throw new Error("请选择账户");
        const items = sites.map((site) => ({
          site_id: site.id,
          integration_account_id: account,
          provider,
          external_resource_id: field(data, site.id),
          enabled: true,
          allow_writes: provider === "bing" && data.has("allow_writes"),
        }));
        if (items.some((item) => !item.external_resource_id)) throw new Error("请为每个站点选择资源");
        if (items.length === 1) await seoRequest("/integrations/bindings", { method: "POST", body: items[0] });
        else {
          const result = await seoRequest<{ items: { status: string }[] }>("/integrations/bindings/batch", {
            method: "POST",
            body: { items },
          });
          const completed = result.items.filter((row) => row.status === "bound").length;
          toast.info(`已绑定 ${completed} 个站点，${items.length - completed} 个验证未通过。`);
        }
        saved();
      }}
    >
      <Field id="binding-provider" label="搜索服务">
        <NativeSelect
          id="binding-provider"
          value={provider}
          disabled={busy}
          onChange={(e) => {
            setProvider(e.target.value as SearchProvider);
            setAccount("");
            reset();
          }}
        >
          {services
            .filter((value): value is SearchProvider => value !== "indexnow")
            .map((value: BindingProvider) => (
              <NativeSelectOption key={value} value={value}>
                {serviceLabels[value]}
              </NativeSelectOption>
            ))}
        </NativeSelect>
      </Field>
      <Field id="binding-account" label="已授权账户">
        <NativeSelect
          id="binding-account"
          value={account}
          disabled={busy}
          onChange={(e) => {
            setAccount(e.target.value);
            reset();
          }}
        >
          <NativeSelectOption value="">请选择</NativeSelectOption>
          {matching.map((a) => (
            <NativeSelectOption key={a.id} value={a.id}>
              {a.label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Field>
      <div className="flex flex-wrap gap-2 sm:col-span-2">
        <Button type="button" variant="outline" disabled={!account || busy} onClick={() => void discover()}>
          查找资源
        </Button>
        {pageToken && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void discover(pageToken)}>
            加载下一页资源
          </Button>
        )}
      </div>
      {problem && (
        <p role="status" className="text-muted-foreground text-sm sm:col-span-2">
          {problem}
        </p>
      )}
      {resources.length > 0 && (
        <div className="sm:col-span-2">
          <Field id="binding-resource-search" label="搜索资源">
            <Input
              id="binding-resource-search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="资源名称或地址"
            />
          </Field>
        </div>
      )}
      <div className="max-h-56 space-y-3 overflow-y-auto sm:col-span-2">
        {sites.map((site) => (
          <Field key={site.id} id={`binding-resource-${site.id}`} label={`资源 · ${site.site_name}`}>
            <NativeSelect id={`binding-resource-${site.id}`} name={site.id} defaultValue="" required>
              <NativeSelectOption value="">请选择资源</NativeSelectOption>
              {visible.map((r) => (
                <NativeSelectOption key={r.id} value={r.id}>
                  {[r.name === r.id ? r.id : `${r.name} · ${r.id}`, r.permission, r.parent_name]
                    .filter(Boolean)
                    .join(" · ")}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
        ))}
      </div>
      {provider === "bing" && (
        <div className="flex items-center gap-2">
          <Checkbox id="binding-bing-writes" name="allow_writes" />
          <Label htmlFor="binding-bing-writes">允许提交 URL</Label>
        </div>
      )}
    </FormDialog>
  );
}
