"use client";

import { useCallback, useEffect, useState } from "react";

import { seoRequest } from "@/lib/seo/client";

type Result<T> = { path: string; revision: number; data?: T; error?: unknown };

type QueryResult<T> = {
  data: T | undefined;
  error: unknown;
  loading: boolean;
  refreshing: boolean;
  refresh: () => void;
};

function loadJson<T>(path: string, signal: AbortSignal) {
  return seoRequest<T>(path, { signal });
}

export function useSeoQuery<T>(
  path: string | null,
  load: (path: string, signal: AbortSignal) => Promise<T> = loadJson,
): QueryResult<T> {
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<Result<T>>();
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    if (!path) return;
    const controller = new AbortController();
    load(path, controller.signal).then(
      (data) => {
        if (!controller.signal.aborted) setResult({ path, revision, data });
      },
      (error: unknown) => {
        if (!controller.signal.aborted) setResult({ path, revision, error });
      },
    );
    return () => controller.abort();
  }, [path, revision, load]);

  const completed = result?.path === path ? result : undefined;
  return {
    data: completed?.data,
    error: completed?.error,
    loading: !!path && !completed,
    refreshing: !!completed && completed.revision !== revision,
    refresh,
  };
}

const pageSize = 200;
const pageLimit = 10;

export async function readSeoPages<T>(
  path: string,
  key: string,
  signal: AbortSignal,
  settings: { offset?: number; takeWhile?: (row: T) => boolean } = {},
) {
  const rows: T[] = [];
  const separator = path.includes("?") ? "&" : "?";
  const { takeWhile } = settings;
  for (let page = 0; page < pageLimit; page++) {
    const offset = (settings.offset ?? 0) + page * pageSize;
    const data = await seoRequest<Record<string, T[]>>(`${path}${separator}limit=${pageSize}&offset=${offset}`, {
      signal,
    });
    const records = data[key];
    const stop = takeWhile ? records.findIndex((row) => !takeWhile(row)) : -1;
    if (stop !== -1) return { rows: rows.concat(records.slice(0, stop)), complete: true };
    rows.push(...records);
    if (records.length < pageSize) return { rows, complete: true };
  }
  return { rows, complete: false };
}
