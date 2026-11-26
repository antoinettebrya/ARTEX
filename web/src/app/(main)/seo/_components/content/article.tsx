"use client";

import { type FormEvent, useState } from "react";

import { Download, Plus, Save, Trash2 } from "lucide-react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { errorMessage } from "@/lib/seo/client";
import {
  type ArticleSection,
  type ContentAction,
  type ContentArticle,
  type ContentBrief,
  type ContentHeading,
  type ContentPublication,
  contentLines,
  safeHTTPLink,
} from "@/lib/seo/content";

import { Field, field, formatTime } from "../shared";

export type SubmitContent = (action: ContentAction) => Promise<void>;

type DraftSection = Omit<ArticleSection, "claims"> & {
  key: string;
  claims: { key: string; text: string; fact_ids: string[] }[];
};

export function TextList({ label, values }: { label: string; values?: string[] | null }) {
  const observations = [...new Set(values ?? [])];
  if (observations.length === 0) return null;
  return (
    <div className="min-w-0 space-y-1">
      <h3 className="font-medium">{label}</h3>
      <ul className="list-disc space-y-0.5 ps-5 text-muted-foreground">
        {observations.map((value) => (
          <li className="break-words" key={value}>
            {value}
          </li>
        ))}
      </ul>
    </div>
  );
}

export function EvidenceLink({ url, text }: { url?: string; text?: string }) {
  const href = safeHTTPLink(url);
  if (!url) return <span className="text-muted-foreground">—</span>;
  const label = text?.trim() ? text : url;
  return href ? (
    <a className="break-all underline underline-offset-2" href={href} target="_blank" rel="noreferrer noopener">
      {label}
    </a>
  ) : (
    <span className="break-all">{label}</span>
  );
}

export function Outline({ headings }: { headings?: ContentHeading[] | null }) {
  if (!headings?.length) return null;
  const titles = [...new Map(headings.map((heading) => [`${heading.level}/${heading.heading}`, heading])).values()];
  return (
    <ol className="list-decimal space-y-2 ps-5 text-sm" aria-label="文章大纲">
      {titles.map((heading) => (
        <li key={`${heading.level}/${heading.heading}`} className="break-words">
          <Badge variant="outline" className="me-1">
            H{heading.level}
          </Badge>{" "}
          {heading.heading}
          {heading.purpose && <p className="text-muted-foreground">{heading.purpose}</p>}
        </li>
      ))}
    </ol>
  );
}

function useContentSubmit(onSave: SubmitContent) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  async function commit(action: ContentAction) {
    if (saving) return;
    const bytes = new TextEncoder().encode(JSON.stringify(action)).length;
    if (bytes > 53_000) {
      setError("此版本超过内容大小上限。请缩减本次提交。");
      return;
    }
    setSaving(true);
    setError("");
    try {
      await onSave(action);
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setSaving(false);
    }
  }
  return { saving, error, commit };
}

const briefLists = [
  ["secondary_keywords", "次关键词"],
  ["questions", "必须回答的问题"],
  ["entities", "相关实体"],
  ["topics", "必须覆盖的主题"],
  ["source_urls", "官方资料来源"],
  ["gaps", "竞品内容缺口"],
  ["images", "图片需求"],
  ["internal_links", "内链目标"],
  ["fact_requirements", "事实核验要求"],
] as const;
const briefBasics = [
  ["primary_keyword", "主关键词"],
  ["intent", "搜索意图"],
  ["page_type", "推荐页面类型"],
  ["title", "SEO 标题"],
  ["h1", "H1"],
  ["url", "推荐 URL"],
] as const;

function briefHeadings(value: string): ContentHeading[] {
  return contentLines(value).map((line) => {
    const matches = /^H([2-6])\s+(.*)$/u.exec(line);
    const level = matches ? Number(matches[1]) : 2;
    const [heading, ...detail] = (matches?.[2] ?? line).split("|");
    return { level, heading: heading.trim(), purpose: detail.join("|").trim() || undefined };
  });
}

export function BriefEditor({ brief, onSave }: { brief: ContentBrief; onSave: SubmitContent }) {
  const { commit, saving, error } = useContentSubmit(onSave);
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const values = new FormData(event.currentTarget);
    const changed = { ...brief };
    for (const [key] of briefBasics) changed[key] = field(values, key).trim();
    for (const [key] of briefLists) changed[key] = contentLines(field(values, key));
    changed.need = field(values, "need").trim();
    changed.description = field(values, "description").trim();
    changed.headings = briefHeadings(field(values, "headings"));
    void commit({ action: "save_brief", brief: changed });
  }
  return (
    <Card className="gap-3 py-3">
      <CardHeader className="px-3">
        <CardTitle>简报内容</CardTitle>
      </CardHeader>
      <CardContent className="px-3">
        <form className="space-y-4" onSubmit={submit}>
          <fieldset className="grid min-w-0 gap-3 sm:grid-cols-2" disabled={saving}>
            {briefBasics.map(([key, label]) => (
              <Field id={`content-brief-${key}`} label={label} key={key}>
                <Input
                  id={`content-brief-${key}`}
                  name={key}
                  maxLength={key === "url" ? 4096 : 400}
                  type={key === "url" ? "url" : "text"}
                  required={key !== "url"}
                  defaultValue={brief[key]}
                />
              </Field>
            ))}
            <div className="sm:col-span-2">
              <Field id="content-brief-description" label="Meta Description">
                <Textarea id="content-brief-description" name="description" defaultValue={brief.description} rows={2} />
              </Field>
            </div>
            <div className="sm:col-span-2">
              <Field id="content-brief-need" label="用户需求">
                <Textarea id="content-brief-need" name="need" defaultValue={brief.need} rows={2} />
              </Field>
            </div>
            <div className="sm:col-span-2">
              <Field id="content-brief-headings" label="标题结构（每行 H2 标题 | 用途）">
                <Textarea
                  id="content-brief-headings"
                  name="headings"
                  rows={5}
                  defaultValue={(brief.headings ?? [])
                    .map(({ level, heading, purpose }) => [`H${level} ${heading}`, purpose].filter(Boolean).join(" | "))
                    .join("\n")}
                />
              </Field>
            </div>
            {briefLists.map(([key, label]) => (
              <Field id={`content-brief-${key}`} label={`${label}（每行一项）`} key={key}>
                <Textarea id={`content-brief-${key}`} name={key} rows={3} defaultValue={brief[key]?.join("\n")} />
              </Field>
            ))}
          </fieldset>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <Button size="sm" type="submit" disabled={saving}>
            {saving ? <Spinner /> : <Save />} 保存简报版本
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

export function BriefPreview({ brief }: { brief: ContentBrief }) {
  return (
    <Card className="gap-3 py-3">
      <CardHeader className="px-3">
        <CardTitle>{brief.title}</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-3 px-3 text-sm">
        <p className="break-words text-muted-foreground">{brief.description}</p>
        <dl className="grid gap-x-4 gap-y-2 sm:grid-cols-2">
          <div>
            <dt className="text-muted-foreground">主关键词</dt>
            <dd className="break-words">{brief.primary_keyword}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">搜索意图</dt>
            <dd>{brief.intent}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">用户需求</dt>
            <dd className="break-words">{brief.need}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">页面类型</dt>
            <dd>{brief.page_type}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">推荐 URL</dt>
            <dd>
              <EvidenceLink url={brief.url} />
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">H1</dt>
            <dd className="break-words">{brief.h1}</dd>
          </div>
        </dl>
        <Outline headings={brief.headings} />
        <div className="grid gap-3 sm:grid-cols-2">
          {briefLists.map(([key, label]) => (
            <TextList key={key} label={label} values={brief[key]} />
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

const metadata = [
  ["title", "SEO 标题"],
  ["h1", "H1"],
  ["author", "作者"],
  ["slug", "URL Slug"],
  ["canonical", "Canonical URL"],
] as const;
const articleMetaLength = { title: 400, h1: 400, author: 200, slug: 400, canonical: 4096 };
const seoLists = [
  ["internal_links", "内链 URL"],
  ["image_alts", "图片替代文本"],
  ["schema", "Schema 类型"],
] as const;

function editableSections(sections: ArticleSection[] | null): DraftSection[] {
  return (sections ?? []).map((section, index) => ({
    ...section,
    key: `saved-${index}`,
    claims: (section.claims ?? []).map((claim, position) => ({ ...claim, key: `saved-${position}` })),
  }));
}

export function ArticleEditor({ article, onSave }: { article: ContentArticle; onSave: SubmitContent }) {
  const { commit, saving, error } = useContentSubmit(onSave);
  const [sections, setSections] = useState(() => editableSections(article.sections));

  function patch(key: string, change: Partial<DraftSection>) {
    setSections((rows) => rows.map((row) => (row.key === key ? { ...row, ...change } : row)));
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const values = new FormData(event.currentTarget);
    const changed = {
      ...article,
      sections: sections.map(({ key: _key, claims, ...rest }) => ({
        ...rest,
        heading: rest.heading.trim(),
        claims: claims.map(({ text, fact_ids }) => ({ text: text.trim(), fact_ids })),
      })),
      description: field(values, "description").trim(),
    };
    for (const [key] of metadata) changed[key] = field(values, key).trim();
    for (const [key] of seoLists) changed[key] = contentLines(field(values, key));
    void commit({ action: "save_article", article: changed });
  }

  return (
    <form className="grid min-w-0 gap-3" onSubmit={submit}>
      <Card className="gap-3 py-3">
        <CardHeader className="px-3">
          <CardTitle>文章与 SEO</CardTitle>
        </CardHeader>
        <CardContent className="px-3">
          <fieldset className="grid min-w-0 gap-3 sm:grid-cols-2" disabled={saving}>
            {metadata.map(([name, label]) => (
              <Field key={name} id={`content-article-${name}`} label={label}>
                <Input
                  id={`content-article-${name}`}
                  name={name}
                  defaultValue={article[name]}
                  maxLength={articleMetaLength[name]}
                  type={name === "canonical" ? "url" : "text"}
                  required={name === "title" || name === "h1"}
                />
              </Field>
            ))}
            <div className="sm:col-span-2">
              <Field id="content-article-description" label="Meta Description">
                <Textarea
                  id="content-article-description"
                  name="description"
                  rows={2}
                  defaultValue={article.description}
                />
              </Field>
            </div>
            {seoLists.map(([key, label]) => (
              <Field id={`content-article-${key}`} label={`${label}（每行一项）`} key={key}>
                <Textarea id={`content-article-${key}`} name={key} rows={2} defaultValue={article[key]?.join("\n")} />
              </Field>
            ))}
          </fieldset>
        </CardContent>
      </Card>
      {sections.map((section, index) => (
        <Card key={section.key} className="gap-3 py-3">
          <CardHeader className="flex items-center justify-between gap-2 px-3">
            <CardTitle>章节 {index + 1}</CardTitle>
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-label={`删除章节 ${index + 1}`}
              disabled={sections.length === 1 || saving}
              onClick={() => setSections((current) => current.filter((item) => item.key !== section.key))}
            >
              <Trash2 />
            </Button>
          </CardHeader>
          <CardContent className="space-y-3 px-3">
            <fieldset className="grid min-w-0 gap-3" disabled={saving}>
              <div className="grid gap-3 sm:grid-cols-[6rem_minmax(0,1fr)]">
                <Field id={`content-section-level-${section.key}`} label="标题级别">
                  <NativeSelect
                    id={`content-section-level-${section.key}`}
                    value={section.level}
                    onChange={(event) => patch(section.key, { level: Number(event.target.value) })}
                  >
                    {[2, 3, 4, 5, 6].map((level) => (
                      <NativeSelectOption key={level} value={level}>
                        H{level}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </Field>
                <Field id={`content-section-title-${section.key}`} label={`章节 ${index + 1} 标题`}>
                  <Input
                    id={`content-section-title-${section.key}`}
                    value={section.heading}
                    maxLength={400}
                    onChange={(event) => patch(section.key, { heading: event.target.value })}
                    required
                  />
                </Field>
              </div>
              <Field id={`content-section-body-${section.key}`} label={`章节 ${index + 1} 正文`}>
                <Textarea
                  id={`content-section-body-${section.key}`}
                  className="min-h-28"
                  value={section.body}
                  maxLength={6000}
                  onChange={(event) => patch(section.key, { body: event.target.value })}
                  required
                />
              </Field>
              <details className="space-y-2">
                <summary className="cursor-pointer font-medium text-sm">事实引用（{section.claims.length}）</summary>
                <div className="space-y-3">
                  {section.claims.map((claim, claimIndex) => {
                    const prefix = `${section.key}-${claim.key}`;
                    const change = (update: Partial<(typeof section.claims)[number]>) =>
                      patch(section.key, {
                        claims: section.claims.map((item) => (item.key === claim.key ? { ...item, ...update } : item)),
                      });
                    return (
                      <div key={claim.key} className="grid min-w-0 items-end gap-2 sm:grid-cols-[2fr_1fr_auto]">
                        <Field id={`content-claim-${prefix}`} label={`声明 ${claimIndex + 1}`}>
                          <Input
                            id={`content-claim-${prefix}`}
                            value={claim.text}
                            onChange={(event) => change({ text: event.target.value })}
                          />
                        </Field>
                        <Field id={`content-citation-${prefix}`} label="事实 ID（逗号分隔）">
                          <Input
                            id={`content-citation-${prefix}`}
                            defaultValue={claim.fact_ids.join(", ")}
                            onChange={(event) =>
                              change({
                                fact_ids: event.target.value
                                  .split(",")
                                  .map((item) => item.trim())
                                  .filter(Boolean),
                              })
                            }
                          />
                        </Field>
                        <Button
                          size="icon-sm"
                          type="button"
                          variant="ghost"
                          aria-label={`删除声明 ${claimIndex + 1}`}
                          onClick={() =>
                            patch(section.key, { claims: section.claims.filter((item) => item.key !== claim.key) })
                          }
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    );
                  })}
                  <Button
                    size="sm"
                    type="button"
                    variant="outline"
                    onClick={() =>
                      patch(section.key, {
                        claims: [...section.claims, { key: crypto.randomUUID(), text: "", fact_ids: [] }],
                      })
                    }
                  >
                    <Plus /> 添加事实引用
                  </Button>
                </div>
              </details>
            </fieldset>
          </CardContent>
        </Card>
      ))}
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={saving || sections.length >= 16}
          onClick={() =>
            setSections((items) => [
              ...items,
              { key: crypto.randomUUID(), level: 2, heading: "", body: "", claims: [] },
            ])
          }
        >
          <Plus /> 添加章节
        </Button>
        <Button size="sm" type="submit" disabled={saving || !sections.length}>
          {saving ? <Spinner /> : <Save />} 保存文章版本
        </Button>
      </div>
    </form>
  );
}

export function ArticlePreview({ article }: { article: ContentArticle }) {
  const sections = (article.sections ?? []).map((section, index) => ({ ...section, key: `section-${index}` }));
  return (
    <article lang={article.language_code} className="grid min-w-0 gap-3 text-sm">
      <div className="min-w-0 space-y-1">
        <h2 className="break-words font-semibold text-lg">{article.h1 || article.title}</h2>
        {article.author && <p className="text-muted-foreground">{article.author}</p>}
        <p className="break-words text-muted-foreground">{article.description}</p>
      </div>
      {sections.map(({ key, heading, level, body }) => (
        <section className="min-w-0 border-t pt-3" key={key}>
          <h3 className="mb-1 break-words font-medium">
            <Badge variant="outline" className="me-2">
              H{level}
            </Badge>
            {heading}
          </h3>
          <p className="whitespace-pre-wrap break-words leading-relaxed">{body}</p>
        </section>
      ))}
    </article>
  );
}

export function DownloadPublication({ publication }: { publication: ContentPublication }) {
  function download() {
    const blob = new Blob([JSON.stringify(publication, null, 2)], { type: "application/json" });
    const href = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.download = `content-v${publication.version}.json`;
    link.href = href;
    link.click();
    setTimeout(() => URL.revokeObjectURL(href), 1000);
  }
  return (
    <Button type="button" variant="outline" size="sm" onClick={download}>
      <Download /> 下载 CMS 发布包
    </Button>
  );
}

export function VersionDate({ number, time }: { number: number; time: string }) {
  return (
    <span className="text-muted-foreground text-xs tabular-nums">
      版本 {number} · {formatTime(time)} UTC
    </span>
  );
}
