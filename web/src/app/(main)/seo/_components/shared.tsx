"use client";

import { type FormEvent, type ReactNode, useRef, useState } from "react";

import { ChevronLeft, ChevronRight } from "lucide-react";
import { toast } from "sonner";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { Pagination, PaginationContent, PaginationItem } from "@/components/ui/pagination";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { errorMessage } from "@/lib/seo/client";

import { useSeoQuery } from "./query";

const pageSize = 50;

type ListState = {
  offset?: number;
  count: number;
  hasNext?: boolean;
  loading: boolean;
  error: unknown;
  refresh: () => void;
  setOffset?: (offset: number) => void;
};

export function useSeoRecords<T>(path: string | null, name: string) {
  const [position, setPosition] = useState<{ path: string | null; offset: number }>({ path, offset: 0 });
  const offset = position.path === path ? position.offset : 0;
  const query = useSeoQuery<Record<string, T[]>>(
    path ? `${path}${path.includes("?") ? "&" : "?"}limit=${pageSize + 1}&offset=${offset}` : null,
  );
  const records = query.data?.[name] ?? [];
  return {
    ...query,
    records: records.slice(0, pageSize),
    offset,
    count: Math.min(records.length, pageSize),
    hasNext: records.length > pageSize && offset + pageSize <= 100000,
    setOffset: (offset: number) => setPosition({ path, offset }),
  };
}

export function useSeoList<T>(name: "sites" | "jobs" | "providers" | "members" | "events") {
  return useSeoRecords<T>(name === "events" ? "/audit" : `/${name}`, name);
}

export function PageFrame({
  title,
  description,
  actions,
  children,
}: {
  title: string;
  description: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="flex min-w-0 flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="font-semibold text-xl tracking-tight">{title}</h1>
          <p className="text-muted-foreground text-sm">{description}</p>
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

export function FilterBar({ children }: { children: ReactNode }) {
  return (
    <Card className="py-3">
      <CardContent className="flex min-w-0 flex-wrap items-end gap-3 px-3">{children}</CardContent>
    </Card>
  );
}

export function PageSkeleton() {
  return (
    <div className="grid gap-4" role="status" aria-label="正在加载">
      <Skeleton className="h-6 w-36" />
      <Skeleton className="h-36 w-full" />
    </div>
  );
}

export function LoadError({ error, retry }: { error: unknown; retry: () => void }) {
  return (
    <Alert variant="destructive">
      <AlertTitle>读取失败</AlertTitle>
      <AlertDescription>{errorMessage(error)}</AlertDescription>
      <Button type="button" variant="outline" size="sm" className="mt-2 justify-self-start" onClick={retry}>
        重试
      </Button>
    </Alert>
  );
}

export function RecordsTable({
  label,
  columns,
  empty,
  list,
  children,
}: {
  label: string;
  columns: readonly string[];
  empty: string;
  list: ListState;
  children: ReactNode;
}) {
  if (list.loading) return <PageSkeleton />;
  if (list.error) return <LoadError error={list.error} retry={list.refresh} />;
  const { offset = 0, hasNext, setOffset } = list;
  return (
    <Card className="gap-0 overflow-hidden p-0">
      <Table aria-label={label}>
        <TableHeader className="bg-muted/50">
          <TableRow>
            {columns.map((column) => (
              <TableHead key={column} scope="col" className="px-3">
                {column}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>{children}</TableBody>
      </Table>
      {list.count === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{empty}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      )}
      {setOffset && (offset > 0 || hasNext) && (
        <Pagination className="justify-end border-t px-3 py-2" aria-label="列表分页">
          <PaginationContent>
            <PaginationItem>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="上一页"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - pageSize))}
              >
                <ChevronLeft />
              </Button>
            </PaginationItem>
            <PaginationItem className="text-muted-foreground text-xs tabular-nums">
              第 {offset / pageSize + 1} 页
            </PaginationItem>
            <PaginationItem>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="下一页"
                disabled={!hasNext}
                onClick={() => setOffset(offset + pageSize)}
              >
                <ChevronRight />
              </Button>
            </PaginationItem>
          </PaginationContent>
        </Pagination>
      )}
    </Card>
  );
}

export function StateBadge({
  label,
  tone = "outline",
}: {
  label: string;
  tone?: "outline" | "secondary" | "destructive";
}) {
  return <Badge variant={tone}>{label}</Badge>;
}

export function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="min-w-0 space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  );
}

export function field(data: FormData, name: string) {
  return String(data.get(name) ?? "");
}

export function FormDialog({
  title,
  description,
  submitLabel,
  onClose,
  onSubmit,
  children,
}: {
  title: string;
  description: string;
  submitLabel: string;
  onClose: () => void;
  onSubmit: (data: FormData) => Promise<void>;
  children: ReactNode;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const focusOrigin = useRef<HTMLElement | null>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    const data = new FormData(event.currentTarget);
    setError("");
    setPending(true);
    try {
      await onSubmit(data);
      onClose();
    } catch (failure) {
      setError(errorMessage(failure));
      setPending(false);
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent
        className="sm:max-w-lg"
        showCloseButton={!pending}
        onOpenAutoFocus={() => {
          focusOrigin.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        }}
        onCloseAutoFocus={(event) => {
          if (focusOrigin.current?.isConnected) {
            event.preventDefault();
            focusOrigin.current.focus();
          }
        }}
      >
        <DialogHeader className="pr-7">
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">{children}</div>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline" disabled={pending}>
                取消
              </Button>
            </DialogClose>
            <Button type="submit" disabled={pending}>
              {pending && <Spinner />}
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function ConfirmChange({
  title,
  description,
  onClose,
  onConfirm,
}: {
  title: string;
  description: string;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  const [pending, setPending] = useState(false);
  async function confirm() {
    if (pending) return;
    setPending(true);
    try {
      await onConfirm();
      onClose();
    } catch (error) {
      toast.error(errorMessage(error));
      setPending(false);
    }
  }
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>返回</AlertDialogCancel>
          <Button type="button" variant="destructive" disabled={pending} onClick={confirm}>
            {pending && <Spinner />} 确认
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function RecordDetails({ title, value, onClose }: { title: string; value: unknown; onClose: () => void }) {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>记录详情</DialogDescription>
        </DialogHeader>
        <pre className="max-h-[60dvh] overflow-auto rounded-md border bg-muted/50 p-3 font-mono text-xs">
          {JSON.stringify(value, null, 2)}
        </pre>
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

export function formatTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "short", timeZone: "UTC" }).format(
    new Date(value),
  );
}
