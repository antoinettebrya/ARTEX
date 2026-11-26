import { type Job, type Site, seoRequest } from "@/lib/seo/client";
import type { ContentProject, ContentWorkspace } from "@/lib/seo/content";
import { type Connection, type ExecutionMetric, type ExecutionPublication, executionPath } from "@/lib/seo/execution";

const pageSize = 200;
const pageLimit = 10;
const projectLimit = 40;

async function readPages<T>(
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

export function loadRemainingSites(path: string, signal: AbortSignal) {
  return readPages<Site>(path, "sites", signal, { offset: pageSize });
}

export async function loadDailyJobs(day: string, signal: AbortSignal) {
  const { rows, complete } = await readPages<Job>("/jobs", "jobs", signal, {
    takeWhile: (job) => job.created_at.slice(0, 10) >= day,
  });
  const jobs = rows.filter((job) => job.created_at.startsWith(day));
  const failures = jobs.filter((job) => job.state === "failed");
  const succeeded = jobs.filter((job) => job.state === "succeeded").length;
  const finished = succeeded + failures.length;
  return {
    complete,
    failures,
    attempted: jobs.filter((job) => job.attempts > 0).length,
    rate: finished ? succeeded / finished : undefined,
  };
}

export async function loadContentProgress(site: string, signal: AbortSignal) {
  const { items } = await seoRequest<{ items: ContentProject[] }>(
    `/content/projects?site_id=${site}&limit=${projectLimit + 1}`,
    { signal },
  );
  const projects = items.slice(0, projectLimit);
  const workspaces: ContentWorkspace[] = [];
  for (let offset = 0; offset < projects.length; offset += 4) {
    const batch = await Promise.all(
      projects.slice(offset, offset + 4).map(async ({ id }) => {
        const { workspace } = await seoRequest<{ workspace: ContentWorkspace }>(`/content/projects/${id}`, { signal });
        return workspace;
      }),
    );
    workspaces.push(...batch.filter(({ project }) => project.site_id === site));
  }
  const articles = workspaces.filter((item) => item.article_version > 0);
  return {
    count: articles.length,
    reviewedProjects: workspaces.length,
    complete: items.length <= projectLimit && workspaces.length === projects.length,
    pending: articles
      .filter(
        ({ project, quality }) => quality?.passed && !["approved", "prepared", "verified"].includes(project.status),
      )
      .map(({ project }) => project),
  };
}

export async function loadPublished(site: string, signal: AbortSignal) {
  const { rows, complete } = await readPages<ExecutionPublication>(
    executionPath("publications", site),
    "items",
    signal,
  );
  return { count: rows.filter(({ status }) => status === "PUBLISHED" || status === "VERIFIED").length, complete };
}

export function utcDay(daysAgo = 0) {
  return new Date(Date.now() - daysAgo * 86400000).toISOString().slice(0, 10);
}

export async function loadSearchReports(site: string, signal: AbortSignal) {
  const from = utcDay(28);
  const to = utcDay(1);
  const { items } = await seoRequest<{ items: Connection[] }>(executionPath("connections", site, { limit: "200" }), {
    signal,
  });

  async function report(kind: "gsc" | "ga4", fields: string[]) {
    const connection = items.find((item) => item.kind === kind && item.configured && item.enabled && item.property);
    if (!connection) return;
    const { rows, complete } = await readPages<ExecutionMetric>(
      executionPath("metrics", site, { kind, connection_id: connection.id, from, to }),
      "items",
      signal,
    );
    return {
      totals: fields.map((field) => {
        const values = rows.flatMap(({ values }) => (Number.isFinite(values[field]) ? [values[field]] : []));
        return values.length > 0 ? values.reduce((sum, value) => sum + value, 0) : undefined;
      }),
      days: new Set(rows.map(({ date }) => date)).size,
      source: connection.name,
      complete,
    };
  }

  const [gsc, ga4] = await Promise.all([report("gsc", ["clicks", "impressions"]), report("ga4", ["sessions"])]);
  return { from, to, gsc, ga4 };
}

type Receipt = {
  call_id: string;
  occurred_at: string;
  diagnostic: { cost_micros?: number; cost_source?: string; may_be_charged?: boolean };
};

export async function loadDailyCost(day: string, signal: AbortSignal) {
  const { rows, complete } = await readPages<Receipt>("/providers/receipts", "items", signal, {
    takeWhile: ({ occurred_at }) => occurred_at.slice(0, 10) >= day,
  });
  const calls = rows.filter(
    ({ occurred_at, call_id }) =>
      occurred_at.startsWith(day) && !call_id.startsWith("test-") && !call_id.startsWith("execution-"),
  );
  const costs = calls.flatMap(({ diagnostic }) =>
    typeof diagnostic.cost_micros === "number" && ["provider", "estimated"].includes(diagnostic.cost_source ?? "")
      ? [diagnostic.cost_micros]
      : [],
  );
  return {
    amount: costs.length ? costs.reduce((sum, cost) => sum + cost, 0) / 1000000 : undefined,
    unknown: calls.filter(
      ({ diagnostic }) =>
        diagnostic.may_be_charged && (diagnostic.cost_micros == null || diagnostic.cost_source === "unknown"),
    ).length,
    complete,
  };
}
