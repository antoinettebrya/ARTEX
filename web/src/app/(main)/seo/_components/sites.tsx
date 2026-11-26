"use client";

import { useState } from "react";

import Link from "next/link";

import { ClipboardPlus, Pencil, Plus } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";
import { type Site, seoRequest } from "@/lib/seo/client";

import { field, Field, FormDialog, PageFrame, RecordsTable, StateBadge, useSeoList } from "./shared";
import { useSeoSession } from "./session";

const states = { active: "启用", paused: "暂停", archived: "归档" } as const;

export function Sites() {
  const { principal } = useSeoSession();
  const writable = principal.role !== "viewer";
  const list = useSeoList<Site>("sites");
  const [edit, setEdit] = useState<Site | null>();

  async function save(data: FormData) {
    const site = {
      name: field(data, "name"),
      base_url: field(data, "base_url"),
      cms: field(data, "cms"),
      country_code: field(data, "country_code"),
      language_tag: field(data, "language_tag"),
      status: field(data, "status"),
    };
    await seoRequest(edit ? `/sites/${edit.id}` : "/sites", {
      method: edit ? "PUT" : "POST",
      body: edit ? { ...site, version: edit.version } : site,
    });
    list.refresh();
    toast.success("站点已保存");
  }

  return (
    <PageFrame
      title="站点"
      description="管理当前组织登记的站点、国家和语言。"
      actions={
        writable && (
          <Button size="sm" variant="outline" onClick={() => setEdit(null)}>
            <Plus /> 添加站点
          </Button>
        )
      }
    >
      <RecordsTable
        label="已登记站点"
        columns={["站点", "市场", "CMS", "状态", ...(writable ? ["操作"] : [])]}
        empty="暂无登记站点"
        list={list}
      >
        {list.records.map((site) => (
          <TableRow key={site.id}>
            <TableCell className="px-3 py-2">
              <Link href={`/seo/research?site=${site.id}`} className="font-medium hover:underline">
                {site.name}
              </Link>
              <div className="text-muted-foreground text-xs">{site.base_url}</div>
            </TableCell>
            <TableCell className="px-3">
              {site.country_code} · {site.language_tag}
            </TableCell>
            <TableCell className="px-3">{site.cms === "wordpress" ? "WordPress" : "通用 CMS"}</TableCell>
            <TableCell className="px-3">
              <StateBadge label={states[site.status]} tone={site.status === "active" ? "secondary" : "outline"} />
            </TableCell>
            {writable && (
              <TableCell className="px-3">
                <div className="flex items-center gap-1">
                  {site.status === "active" && (
                    <Button asChild variant="ghost" size="sm">
                      <Link href={`/seo/jobs?site=${site.id}`} aria-label={`为 ${site.name} 创建任务`}>
                        <ClipboardPlus /> 创建任务
                      </Link>
                    </Button>
                  )}
                  <Button variant="ghost" size="icon-sm" aria-label={`编辑 ${site.name}`} onClick={() => setEdit(site)}>
                    <Pencil />
                  </Button>
                </div>
              </TableCell>
            )}
          </TableRow>
        ))}
      </RecordsTable>
      {edit !== undefined && writable && (
        <FormDialog
          title={edit ? "编辑站点" : "添加站点"}
          description="设置站点地址、CMS 和目标市场。"
          submitLabel="保存站点"
          onSubmit={save}
          onClose={() => setEdit(undefined)}
        >
          <Field id="site-name" label="站点名称">
            <Input id="site-name" name="name" required maxLength={120} defaultValue={edit?.name} />
          </Field>
          <Field id="site-url" label="站点 URL">
            <Input
              id="site-url"
              name="base_url"
              type="url"
              required
              maxLength={2048}
              placeholder="https://example.com"
              defaultValue={edit?.base_url}
            />
          </Field>
          <Field id="site-cms" label="CMS">
            <NativeSelect id="site-cms" name="cms" className="w-full" defaultValue={edit?.cms ?? "wordpress"}>
              <NativeSelectOption value="wordpress">WordPress</NativeSelectOption>
              <NativeSelectOption value="generic">通用 CMS</NativeSelectOption>
            </NativeSelect>
          </Field>
          <Field id="site-country" label="国家或地区">
            <Input
              id="site-country"
              name="country_code"
              required
              pattern="[A-Za-z]{2}"
              maxLength={2}
              placeholder="CN"
              defaultValue={edit?.country_code}
            />
          </Field>
          <Field id="site-language" label="语言标签">
            <Input
              id="site-language"
              name="language_tag"
              required
              maxLength={64}
              placeholder="zh-Hans"
              defaultValue={edit?.language_tag}
            />
          </Field>
          <Field id="site-status" label="状态">
            <NativeSelect id="site-status" name="status" className="w-full" defaultValue={edit?.status ?? "active"}>
              {Object.entries(states).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
        </FormDialog>
      )}
    </PageFrame>
  );
}
