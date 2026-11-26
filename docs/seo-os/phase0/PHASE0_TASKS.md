# Phase 0 — Execution Checklist

Branch: `seo-os-v1`  
Status: **IN PROGRESS**  
Rule: audit only; no application-code refactor until audit and legal gate are reviewed.

## A. Repository and build baseline

- [ ] Export complete recursive file inventory and classify source, tests, generated assets, docs, dependencies.
- [ ] Record latest commit SHA and working branch.
- [ ] Inspect Go workspace/module files and all package imports.
- [ ] Inspect frontend package scripts, dependency tree, routes, API client, and build config.
- [ ] Inspect Dockerfile, Compose, install/update scripts, CI workflows.
- [ ] Run `go test ./...` in an isolated local checkout and record exact output.
- [ ] Run frontend install, lint/typecheck/build and record exact output.
- [ ] Identify missing tests and nondeterministic tests.

## B. Runtime and module dependency audit

- [ ] Trace process startup from `cmd` through Manager, Server, Engine, Scheduler, and database.
- [ ] Trace agent construction, prompt injection, tool registration, MCP and Skills wiring.
- [ ] Trace every traffic/intercept/guard dependency and its callers.
- [ ] Trace vulnerability/finding/retest/report workflows and all API/UI references.
- [ ] Trace asset/exploration graph usage and classify reusable generic graph primitives.
- [ ] Identify SQLite imports and call sites; distinguish production paths from historical/test dependencies.
- [ ] Produce a dependency graph and module disposition matrix.

## C. Data and API

- [ ] Inventory all PostgreSQL tables, indexes, constraints, triggers, migrations, and query repositories.
- [ ] Map each table to retained, replaced, migrated, or removed product domains.
- [ ] Identify tenant-scope gaps and define organization/site ownership rules.
- [ ] Inventory API routes and map them to current frontend callers.
- [ ] Propose versioned SEO API contracts and error/idempotency semantics.
- [ ] Define migration and rollback strategy; do not mutate existing production data.

## D. Agent, queue, and provider

- [ ] Document Planner/Worker lifecycle and resumability.
- [ ] Inventory built-in, custom, shell, HTTP, MCP, and deferred tools.
- [ ] Define SEO Master and specialist-agent boundaries.
- [ ] Define least-privilege SEO Tool Registry.
- [ ] Define durable task state machine, lease/CAS, idempotency, retry, dead-letter, cancellation.
- [ ] Define provider abstraction for LLM, SEO data, image generation, CMS, and browser QA.
- [ ] Define per-organization/site/provider budgets and rate limits.

## E. SEO domain design

- [ ] Organization/site/locale/competitor model.
- [ ] Keyword metrics, search intent, clustering, and keyword-to-URL mapping.
- [ ] SERP snapshots and competitor-page evidence model.
- [ ] Content briefs, source evidence, fact checks, versions, reviews, publishing.
- [ ] Internal-link graph and recommendation rules.
- [ ] Technical SEO audit and safe remediation model.
- [ ] Sitemap, IndexNow, GSC, Bing workflow and status semantics.
- [ ] Analytics and GEO measurement methodology.

## F. Image Studio and Theme Factory

- [ ] Image provider interface, job lifecycle, prompt versioning, asset metadata, variants, usage, costs.
- [ ] Brand visual memory per site.
- [ ] Image quality checks, alt text, compression, format conversion, CMS media sync.
- [ ] WordPress theme generation architecture: Classic, Hybrid, Block.
- [ ] Isolated build sandbox and dependency allowlist.
- [ ] PHP compatibility, static checks, browser E2E, responsive and accessibility tests.
- [ ] Staging install/activation, health checks, backup, rollback.
- [ ] Separate theme presentation from SEO business data and plugin-owned metadata.

## G. Site Network Link Intelligence

- [ ] Site relationship graph and page-level cross-site graph.
- [ ] Candidate relevance and editorial/user-value scoring.
- [ ] Commercial relationship disclosure and rel attribute policy.
- [ ] Detect reciprocal-link patterns, link concentration, repeated anchors, and unnatural placements.
- [ ] Approval workflow, link snapshots, monitoring, and rollback.
- [ ] Keep cross-site linking separate from intra-site internal linking.

## H. Security, licensing, and legal gate

- [ ] Review root AGPL-3.0 license obligations.
- [ ] Review README's additional use restrictions and obtain qualified legal review.
- [ ] Establish permission/provenance for all copied or vendored source, especially Norma.
- [ ] Generate Go and npm dependency license inventories.
- [ ] Review JWT/session handling, secret storage, CSRF, RBAC, tenant isolation.
- [ ] Review SSRF, URL fetch, upload, archive extraction, generated code, and deployment boundaries.
- [ ] Define WordPress credential scopes and revocation.
- [ ] Define audit logs and destructive-action approvals.

## Exit criteria

Phase 0 may be marked complete only when all checklist items are either evidenced as complete or explicitly marked blocked with owner, reason, and resolution. The license/usage gate must be resolved before commercial production use or redistribution. No penetration-testing capability is to be retained in the SEO OS product.
