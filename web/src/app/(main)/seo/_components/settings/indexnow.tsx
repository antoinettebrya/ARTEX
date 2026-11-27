"use client";

import { useState } from "react";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { errorMessage, SeoAPIError, seoRequest } from "@/lib/seo/client";

import { useSiteScope } from "../context";
import { ConfirmChange, Field, FormDialog, field, RecordsTable, useSeoRecords } from "../shared";
import { IntegrationStatus, RowAction, TimeValue } from "./common";
import { type IndexNowKey, listPath } from "./model";

type KeyMaterial = { key: string; verification_url: string; filename: string };
type Open = { mode: "create" } | { mode: "rotate" | "verify" | "reveal" | "submit"; item: IndexNowKey };
const indexNowMaxURLs = 1000;

export function IndexNowPanel({ owner, siteID }: { owner: boolean; siteID?: string }) {
  const sites = useSiteScope();
  const list = useSeoRecords<IndexNowKey>(listPath("indexnow/keys", { site_id: siteID }), "items");
  const [open, setOpen] = useState<Open>();
  const [material, setMaterial] = useState<KeyMaterial>();
  async function reveal(item: IndexNowKey) {
    try {
      const data = await seoRequest<KeyMaterial>(`/integrations/indexnow/keys/${item.id}/reveal`);
      setMaterial(data);
      setOpen({ mode: "reveal", item });
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }
  function close() {
    setMaterial(undefined);
    setOpen(undefined);
  }
  return (
    <>
      <div className="flex flex-wrap gap-2">
        {owner && <Button onClick={() => setOpen({ mode: "create" })}>添加站点密钥</Button>}
        <Button variant="outline" onClick={list.refresh}>
          刷新状态
        </Button>
      </div>
      <RecordsTable
        label="IndexNow 密钥"
        columns={["站点", "Host", "验证目录", "验证状态", "最后验证", "最后提交", "操作"]}
        list={list}
        empty="暂无 IndexNow 密钥"
      >
        {list.records.map((item) => (
          <TableRow key={item.id}>
            <TableCell>{item.site_name}</TableCell>
            <TableCell>{item.host}</TableCell>
            <TableCell>{item.directory}</TableCell>
            <TableCell>
              <IntegrationStatus status={item.verification_status} />
              {item.verification_error && (
                <div className="mt-1 text-muted-foreground text-xs">
                  {errorMessage({ code: item.verification_error })}
                </div>
              )}
            </TableCell>
            <TableCell>
              <TimeValue value={item.verified_at} empty="待验证" />
            </TableCell>
            <TableCell>
              <TimeValue value={item.last_submission_at} empty="待提交" />
            </TableCell>
            <TableCell>
              {owner && (
                <div className="flex flex-wrap gap-2">
                  <RowAction onClick={() => void reveal(item)}>查看密钥</RowAction>
                  <Button asChild variant="outline" size="sm">
                    <a href={`/seo/api/integrations/indexnow/keys/${item.id}/file`}>下载 TXT</a>
                  </Button>
                  <RowAction onClick={() => setOpen({ mode: "verify", item })}>重新验证</RowAction>
                  <RowAction onClick={() => setOpen({ mode: "rotate", item })}>轮换</RowAction>
                  {item.allow_writes && (
                    <RowAction onClick={() => setOpen({ mode: "submit", item })}>提交 URL</RowAction>
                  )}
                </div>
              )}
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {(open?.mode === "create" || open?.mode === "rotate") && (
        <FormDialog
          title={open.mode === "rotate" ? "轮换 IndexNow 密钥" : "添加站点密钥"}
          description={
            open.mode === "rotate"
              ? "轮换后需要重新部署验证文件。留空密钥可自动生成。"
              : "默认使用站点根目录。留空密钥可自动生成。"
          }
          submitLabel={open.mode === "rotate" ? "确认轮换" : "生成或保存"}
          onClose={close}
          onSubmit={async (data) => {
            const item = open.mode === "rotate" ? open.item : undefined;
            const result = await seoRequest<{ verification_queued: boolean }>(
              item ? `/integrations/indexnow/keys/${item.id}/rotate` : "/integrations/indexnow/keys",
              {
                method: "POST",
                body: {
                  site_id: item?.site_id ?? field(data, "site_id"),
                  directory: field(data, "directory"),
                  key: field(data, "key") || undefined,
                  version: item?.version,
                  allow_writes: data.has("allow_writes"),
                },
              },
            );
            toast.success(
              result.verification_queued
                ? "已保存并安排验证。部署 TXT 文件后可以重新验证。"
                : "已保存。部署文件后请重新验证。",
            );
            list.refresh();
          }}
        >
          {open.mode === "create" && (
            <Field id="indexnow-site" label="站点">
              <NativeSelect id="indexnow-site" name="site_id" defaultValue={siteID ?? ""} required>
                <NativeSelectOption value="">请选择</NativeSelectOption>
                {sites.options
                  .filter((site) => site.status === "active")
                  .map((site) => (
                    <NativeSelectOption key={site.id} value={site.id}>
                      {site.name}
                    </NativeSelectOption>
                  ))}
              </NativeSelect>
            </Field>
          )}
          <Field id="indexnow-dir" label="验证目录">
            <Input
              id="indexnow-dir"
              name="directory"
              defaultValue={open.mode === "rotate" ? open.item.directory : "/"}
            />
          </Field>
          <Field id="indexnow-key" label="自定义密钥">
            <Input
              id="indexnow-key"
              name="key"
              type="password"
              autoComplete="new-password"
              placeholder="留空自动生成"
              pattern="[A-Za-z0-9\-]{8,128}"
              maxLength={128}
            />
          </Field>
          <div className="flex items-center gap-2">
            <Checkbox
              id="indexnow-writes"
              name="allow_writes"
              defaultChecked={open.mode === "rotate" && open.item.allow_writes}
            />
            <Label htmlFor="indexnow-writes">允许提交 URL</Label>
          </div>
        </FormDialog>
      )}
      {open?.mode === "verify" && (
        <ConfirmChange
          title="验证 IndexNow 文件"
          description="请先将下载的 TXT 文件部署到显示的验证目录。"
          onClose={close}
          onConfirm={async () => {
            await seoRequest(`/integrations/indexnow/keys/${open.item.id}/verify`, {
              method: "POST",
              body: { idempotency_key: crypto.randomUUID() },
            });
            toast.success("验证任务已排队");
            list.refresh();
          }}
        />
      )}
      {open?.mode === "reveal" && material && (
        <FormDialog
          title="IndexNow 验证文件"
          description="将下载的 TXT 文件上传到下方地址对应的站点目录。文件内容须与密钥一致。"
          submitLabel="复制密钥"
          onClose={close}
          onSubmit={async () => {
            await navigator.clipboard.writeText(material.key);
            toast.success("已复制密钥");
          }}
        >
          <div className="space-y-2 break-all text-sm sm:col-span-2">
            <p>{material.verification_url}</p>
            <p>
              <code>{material.key}</code>
            </p>
            <Button asChild variant="outline" size="sm">
              <a href={`/seo/api/integrations/indexnow/keys/${open.item.id}/file`}>下载 TXT</a>
            </Button>
          </div>
        </FormDialog>
      )}
      {open?.mode === "submit" && (
        <FormDialog
          title="提交 IndexNow URL"
          description={`每行一个 HTTPS URL，每批最多 ${indexNowMaxURLs} 条。提交会先验证文件及 Host 范围。`}
          submitLabel="提交索引请求"
          onClose={close}
          onSubmit={async (data) => {
            const urls = field(data, "urls")
              .split(/\r?\n/)
              .map((value) => value.trim())
              .filter(Boolean);
            if (urls.length > indexNowMaxURLs) throw new SeoAPIError(400, "execution_submission_limit");
            await seoRequest("/execution/jobs", {
              method: "POST",
              body: {
                site_id: open.item.site_id,
                connection_id: open.item.id,
                kind: "indexnow_submit",
                urls,
                idempotency_key: crypto.randomUUID(),
              },
            });
            toast.success("已排队，可在集成操作历史查看结果。");
          }}
        >
          <div className="sm:col-span-2">
            <Field id="indexnow-batch-urls" label="提交 URL">
              <Textarea id="indexnow-batch-urls" name="urls" required rows={5} />
            </Field>
          </div>
        </FormDialog>
      )}
    </>
  );
}
