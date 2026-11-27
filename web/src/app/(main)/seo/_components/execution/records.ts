import { useEffect, useRef } from "react";

import { executionPath } from "@/lib/seo/execution";

import { useSeoRecords } from "../shared";

export function useExecutionRecords<T>(
  name: string,
  site: string,
  revision: number,
  filters?: Record<string, string | undefined>,
) {
  const list = useSeoRecords<T>(site ? executionPath(name, site, filters) : null, "items");
  const previous = useRef(revision);
  const { refresh } = list;
  useEffect(() => {
    if (previous.current !== revision) {
      previous.current = revision;
      refresh();
    }
  }, [revision, refresh]);
  return list;
}
