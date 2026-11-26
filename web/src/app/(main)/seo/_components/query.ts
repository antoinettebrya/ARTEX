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
