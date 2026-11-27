import { type Job, seoRequest } from "@/lib/seo/client";
import type { ContentProject, ContentWorkspace } from "@/lib/seo/content";

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

export function utcDay(daysAgo = 0) {
  return new Date(Date.now() - daysAgo * 86_400_000).toISOString().slice(0, 10);
}
