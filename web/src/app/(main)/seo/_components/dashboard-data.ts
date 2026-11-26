import { type Job, seoRequest } from "@/lib/seo/client";
import type { ContentProject, ContentWorkspace } from "@/lib/seo/content";
import {
  type Connection,
  type ExecutionMetric,
  type ExecutionPublication,
  executionPath,
  type IndexRecord,
} from "@/lib/seo/execution";
import type { ProviderUsage } from "@/lib/seo/research";

import { readSeoPages } from "./query";

const pageSize = 200;
const projectLimit = 40;

export async function loadDailyJobs(key: string, signal: AbortSignal) {
  const day = key.split("/", 1)[0];
  const { jobs: recent } = await seoRequest<{ jobs: Job[] }>(`/jobs?limit=${pageSize}&offset=0`, { signal });
  const page =
    recent.length === pageSize && recent.at(-1)?.created_at.startsWith(day)
      ? await readSeoPages<Job>("/jobs", "jobs", signal, {
          offset: pageSize,
          takeWhile: (job) => job.created_at.slice(0, 10) >= day,
        })
      : undefined;
  const unique = new Map([...recent, ...(page?.rows ?? [])].map((job) => [job.id, job]));
  return {
    recent,
    today: [...unique.values()].filter((job) => job.created_at.startsWith(day)),
    complete: page?.complete ?? true,
  };
}

export function latestJobBySite(jobs: readonly Job[], kind?: Job["kind"], terminal = false) {
  const latest = new Map<string, Job>();
  for (const job of jobs) {
    if (kind && job.kind !== kind) continue;
    if (terminal && job.state !== "succeeded" && job.state !== "failed") continue;
    if (!latest.has(job.site_id)) latest.set(job.site_id, job);
  }
  return latest;
}

export async function loadContentProgress(site: string, signal: AbortSignal) {
  const directory = await readSeoPages<ContentProject>(`/content/projects?site_id=${site}`, "items", signal);
  const projects = directory.rows.slice(0, projectLimit);
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
    total: directory.rows.length,
    totalComplete: directory.complete,
    count: articles.length,
    generation: workspaces.length - articles.length,
    reviewedProjects: workspaces.length,
    complete: directory.complete && directory.rows.length === workspaces.length,
    pending: articles
      .filter(
        ({ project, quality }) => quality?.passed && !["approved", "prepared", "verified"].includes(project.status),
      )
      .map(({ project }) => project),
  };
}

export async function loadPublished(site: string, signal: AbortSignal) {
  const { rows, complete } = await readSeoPages<ExecutionPublication>(
    executionPath("publications", site),
    "items",
    signal,
  );
  const targets = new Set(
    rows
      .filter(({ status }) => status === "PUBLISHED" || status === "VERIFIED")
      .map(({ connection_id, target_id, url, id }) => url || `${connection_id}/${target_id ?? id}`),
  );
  return { count: targets.size, complete };
}

export function utcDay(daysAgo = 0) {
  return new Date(Date.now() - daysAgo * 86400000).toISOString().slice(0, 10);
}

function sumMetric(rows: ExecutionMetric[], key: string) {
  const values = rows.map(({ values }) => values[key]).filter(Number.isFinite);
  return values.length ? values.reduce((total, value) => total + value, 0) : undefined;
}

function searchTotals(rows: ExecutionMetric[]) {
  const clicks = sumMetric(rows, "clicks");
  const impressions = sumMetric(rows, "impressions");
  let weightedPositions = 0;
  let positionedImpressions = 0;
  let ctrClicks = 0;
  let ctrImpressions = 0;
  for (const { values } of rows) {
    if (!Number.isFinite(values.impressions) || values.impressions <= 0) continue;
    if (Number.isFinite(values.position)) {
      positionedImpressions += values.impressions;
      weightedPositions += values.impressions * values.position;
    }
    if (Number.isFinite(values.clicks)) {
      ctrImpressions += values.impressions;
      ctrClicks += values.clicks;
    }
  }
  return {
    clicks,
    impressions,
    ctr: ctrImpressions ? ctrClicks / ctrImpressions : undefined,
    position: positionedImpressions ? weightedPositions / positionedImpressions : undefined,
  };
}

export async function loadSearchReports(site: string, signal: AbortSignal) {
  const from = utcDay(28);
  const to = utcDay(1);
  const { rows: connections, complete } = await readSeoPages<Connection>(
    executionPath("connections", site),
    "items",
    signal,
  );

  async function report(kind: "gsc" | "ga4") {
    const connection = connections.find(
      (item) => item.kind === kind && item.configured && item.enabled && item.property,
    );
    if (!connection) return;
    const data = await readSeoPages<ExecutionMetric>(
      executionPath("metrics", site, { kind, connection_id: connection.id, from, to }),
      "items",
      signal,
    );
    return {
      rows: data.rows,
      days: new Set(data.rows.map(({ date }) => date)).size,
      source: connection.name,
      complete: data.complete,
    };
  }

  const [gsc, ga4] = await Promise.all([report("gsc"), report("ga4")]);
  return {
    from,
    to,
    connectionsComplete: complete,
    gsc: gsc && { ...searchTotals(gsc.rows), source: gsc.source, days: gsc.days, complete: gsc.complete },
    ga4: ga4 && {
      sessions: sumMetric(ga4.rows, "sessions"),
      source: ga4.source,
      days: ga4.days,
      complete: ga4.complete,
    },
  };
}

export async function loadIndexStatus(site: string, signal: AbortSignal) {
  const { rows, complete } = await readSeoPages<IndexRecord>(executionPath("index", site), "items", signal);
  const latest = new Map<string, IndexRecord>();
  for (const row of rows) {
    const existing = latest.get(row.url);
    if (!existing || existing.observed_at < row.observed_at) latest.set(row.url, row);
  }
  const observed = [...latest.values()];
  const indexed = observed.filter(({ status }) => status === "INDEXED").length;
  const notIndexed = observed.filter(({ status }) => status === "NOT_INDEXED").length;
  return { complete, observed: observed.length, indexed, notIndexed, pending: observed.length - indexed - notIndexed };
}

type Receipt = {
  call_id: string;
  state: string;
  occurred_at: string;
  diagnostic: {
    provider: string;
    input_tokens?: number;
    output_tokens?: number;
    cost_micros?: number;
    cost_source?: string;
    may_be_charged?: boolean;
  };
};
type ModelUsage = { daily_cost_micros: number; spent_micros: number; reserved_micros: number };

function receiptSummary(calls: Receipt[]) {
  const actual = calls.filter(({ state }) => state !== "reserved");
  const counted = actual.flatMap(({ diagnostic }) =>
    typeof diagnostic.input_tokens === "number" && typeof diagnostic.output_tokens === "number"
      ? [diagnostic.input_tokens + diagnostic.output_tokens]
      : [],
  );
  const costs = calls.flatMap(({ diagnostic }) =>
    typeof diagnostic.cost_micros === "number" &&
    ["provider", "estimated", "unbilled"].includes(diagnostic.cost_source ?? "")
      ? [diagnostic.cost_micros]
      : [],
  );
  return {
    count: actual.length,
    tokens: counted.length ? counted.reduce((sum, value) => sum + value, 0) : undefined,
    tokenCoverage: counted.length,
    amount: costs.length ? costs.reduce((sum, cost) => sum + cost, 0) / 1_000_000 : undefined,
    unknown: calls.filter(
      ({ diagnostic }) =>
        diagnostic.may_be_charged && (diagnostic.cost_micros == null || diagnostic.cost_source === "unknown"),
    ).length,
  };
}

function budgetUse(rows: { limit: number; spent: number; reserved: number }[]) {
  let limit = 0;
  let committed = 0;
  for (const row of rows) {
    if (!Number.isFinite(row.limit) || row.limit <= 0) continue;
    limit += row.limit;
    committed += row.spent + row.reserved;
  }
  return limit ? committed / limit : undefined;
}

export async function loadDailyAI(day: string, signal: AbortSignal) {
  const [ledger, models, data] = await Promise.all([
    readSeoPages<Receipt>("/providers/receipts", "items", signal, {
      takeWhile: ({ occurred_at }) => occurred_at.slice(0, 10) >= day,
    }),
    seoRequest<{ items: ModelUsage[] }>("/content/usage", { signal }),
    readSeoPages<ProviderUsage>("/research/usage", "items", signal),
  ]);
  const calls = ledger.rows.filter(
    ({ occurred_at, call_id }) =>
      occurred_at.startsWith(day) && !call_id.startsWith("test-") && !call_id.startsWith("execution-"),
  );
  return {
    model: receiptSummary(
      calls.filter(({ diagnostic }) => diagnostic.provider && diagnostic.provider !== "dataforseo"),
    ),
    data: receiptSummary(calls.filter(({ diagnostic }) => diagnostic.provider === "dataforseo")),
    modelBudget: budgetUse(
      models.items.map((record) => ({
        limit: record.daily_cost_micros,
        spent: record.spent_micros,
        reserved: record.reserved_micros,
      })),
    ),
    dataBudget: budgetUse(
      data.rows.map((record) => ({
        limit: record.day_limit_micros,
        spent: record.spent_micros,
        reserved: record.reserved_micros,
      })),
    ),
    complete: ledger.complete,
    budgetComplete: data.complete,
  };
}
