"use client";

import { useState } from "react";

import { Download } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { executionPath, type SitemapDocument } from "@/lib/seo/execution";

import { useSeoQuery } from "../query";
import { LoadError, PageSkeleton } from "../shared";
import { WebAddress } from "./publishing";

const pageSize = 50;

function downloadXML(name: string, text: string) {
  const href = URL.createObjectURL(new Blob([text], { type: "application/xml" }));
  const link = document.createElement("a");
  link.href = href;
  const filename = name.split("/").pop() ?? "";
  link.download = /^[a-z0-9][a-z0-9._-]{0,119}\.xml$/iu.test(filename) ? filename : "sitemap.xml";
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(href), 1000);
}

export function SitemapDetails({ siteID, jobID, onClose }: { siteID: string; jobID: string; onClose: () => void }) {
  const path = executionPath(`sitemaps/${jobID}`, siteID);
  const query = useSeoQuery<{ document: SitemapDocument }>(path);
  const [offset, setOffset] = useState(0);
  const sitemap = query.data?.document;
  const entries = sitemap?.entries ?? [];

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>Sitemap 详情</DialogTitle>
          <DialogDescription>{sitemap?.url ?? "查看 URL、lastmod、hreflang 及生成的 XML 文件。"}</DialogDescription>
        </DialogHeader>
        {query.loading && <PageSkeleton />}
        {query.error !== undefined && <LoadError error={query.error} retry={query.refresh} />}
        {sitemap && (
          <div className="grid min-w-0 gap-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-muted-foreground text-sm">{entries.length} 个 URL</span>
              {sitemap.truncated && <Badge variant="outline">采集已截断</Badge>}
            </div>
            {sitemap.children?.length ? (
              <details className="rounded-md border px-3 py-2 text-sm">
                <summary className="cursor-pointer font-medium">{sitemap.children.length} 个子 Sitemap</summary>
                <ul className="mt-2 list-disc space-y-1 pl-4">
                  {sitemap.children.map((url) => (
                    <li key={url}>
                      <WebAddress url={url} />
                    </li>
                  ))}
                </ul>
              </details>
            ) : null}
            {entries.length > 0 && (
              <Table aria-label="Sitemap URL">
                <TableHeader>
                  <TableRow>
                    <TableHead scope="col">URL</TableHead>
                    <TableHead scope="col">lastmod</TableHead>
                    <TableHead scope="col">hreflang</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {entries.slice(offset, offset + pageSize).map((entry) => (
                    <TableRow key={entry.url}>
                      <TableCell className="min-w-40 whitespace-normal">
                        <WebAddress url={entry.url} />
                      </TableCell>
                      <TableCell>{entry.lastmod || "—"}</TableCell>
                      <TableCell className="whitespace-normal">
                        {entry.alternate?.length ? (
                          <ul className="min-w-32 space-y-1">
                            {entry.alternate.map((alternate) => (
                              <li key={`${alternate.hreflang}:${alternate.url}`}>
                                <span className="mr-1 font-medium">{alternate.hreflang}</span>
                                <WebAddress url={alternate.url} />
                              </li>
                            ))}
                          </ul>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            {entries.length > pageSize && (
              <nav aria-label="Sitemap 分页" className="flex flex-wrap items-center justify-end gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={offset === 0}
                  onClick={() => setOffset(offset - pageSize)}
                >
                  上一页
                </Button>
                <span className="text-muted-foreground text-xs tabular-nums">
                  {offset + 1}–{Math.min(entries.length, offset + pageSize)} / {entries.length}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={offset + pageSize >= entries.length}
                  onClick={() => setOffset(offset + pageSize)}
                >
                  下一页
                </Button>
              </nav>
            )}
            <div className="flex flex-wrap gap-2">
              {Object.entries(sitemap.files ?? {}).map(([name, xml]) => (
                <Button key={name} type="button" variant="outline" size="sm" onClick={() => downloadXML(name, xml)}>
                  <Download /> {name}
                </Button>
              ))}
            </div>
          </div>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="outline">
              关闭
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
