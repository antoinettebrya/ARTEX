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

export function useSeoQuery<T>(path: string | null): QueryResult<T> {
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<Result<T>>();
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    if (!path) return;
    const controller = new AbortController();
    seoRequest<T>(path, { signal: controller.signal }).then(
      (data) => {
        if (!controller.signal.aborted) setResult({ path, revision, data });
      },
      (error: unknown) => {
        if (!controller.signal.aborted) setResult({ path, revision, error });
      },
    );
    return () => controller.abort();
  }, [path, revision]);

  const completed = result?.path === path ? result : undefined;
  return {
    data: completed?.data,
    error: completed?.error,
    loading: !!path && !completed,
    refreshing: !!completed && completed.revision !== revision,
    refresh,
  };
}
