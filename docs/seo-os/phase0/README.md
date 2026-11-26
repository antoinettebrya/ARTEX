# ARTEX SEO OS — Phase 0

Status: **IN PROGRESS — audit and legal gate only**

Branch: `seo-os-v1`  
Baseline: `main` (must remain unchanged)

## Product decision

- Replace the ARTEX penetration-testing product with a general-purpose AI SEO operations platform.
- Support a single site and large multi-site networks (target validation: 150+ sites).
- Remove penetration-testing workflows, tools, UI, data models, prompts, and deployment assumptions from the final product.
- Retain only independently reviewed, technically reusable infrastructure.
- Add SEO research, content operations, image generation, WordPress theme generation/deployment, technical SEO, indexing, analytics/GEO, and cross-site link intelligence.

## Phase 0 rules

1. Read-only audit of the existing application. Do not delete or rewrite existing business code.
2. Do not run scans, probes, exploit tools, or security tests against live websites. Use local fixtures and isolated test services only.
3. Do not connect production WordPress sites or publish content/themes during this phase.
4. Do not claim tests passed unless they were actually executed and their output recorded.
5. Every material finding must cite a repository path and symbol/line range or a reproducible command.
6. Record unknowns and blocked items explicitly.
7. Do not begin commercial deployment or distribute a derivative until the license/usage restrictions have been reviewed and resolved.

## Audit workstreams

- Repository inventory and entrypoints
- Go package dependency graph and runtime wiring
- Norma SDK provenance, license, and reuse boundary
- PostgreSQL schema, migrations, transactions, indexes, and SQLite call sites
- Agent lifecycle, prompts, tools, MCP, Skills, budgets, and recovery
- API routes, auth, SSE, frontend pages, and state management
- Scheduler, durable queue requirements, idempotency, leases, and retries
- Security boundaries: tenant isolation, secrets, SSRF, uploads, CMS credentials
- Existing tests, CI, build reproducibility, and deployment
- Third-party dependency license inventory

## Required product domains

1. Organization, users, roles, sites, locales, and site connections
2. Keyword intelligence and search intent
3. SERP and competitor research
4. Content briefs, drafting, evidence, review, and publishing
5. Image Studio: providers, prompts, generation jobs, assets, variants, usage, cost
6. WordPress Theme Factory: design systems, templates, builds, tests, deployments, rollback
7. On-page SEO and structured data
8. Internal-link graph and recommendations
9. Technical SEO audits and remediation
10. Sitemap, IndexNow, GSC, and Bing indexing workflows
11. Analytics and GEO measurement
12. Site Network Link Intelligence: cross-site relevance, candidates, reviews, link monitoring, and risk controls
13. Autonomous operations, budgets, audit logs, and durable task orchestration

## Deliverables

Create and maintain the Phase 0 documents listed in the project brief, including:
- executive audit
- repository inventory
- dependency map
- module disposition
- database redesign
- agent architecture
- API migration
- frontend redesign
- WordPress connector
- image generation
- theme factory
- site network link intelligence
- security and license review
- test baseline
- implementation roadmap
- risk register
- acceptance checklist

## Definition of done

Phase 0 is complete only when:
- all tracked repository files have an inventory classification;
- runtime dependency paths are understood for the modules proposed for reuse/removal;
- existing tests/build checks have a recorded baseline;
- the target schema and API boundaries are reviewed;
- all feature domains above are included;
- license and usage restrictions have a documented resolution path;
- implementation tasks have dependencies, owners, and measurable acceptance criteria.

No application code refactor is authorized by this Phase 0 document.
