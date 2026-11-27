# ARTEX SEO OS — Phase 4 Execution Plan
# Site Integration, SEO Execution & Remote Staging Deployment

**Baseline:** Phase 3 `7eaae96a0b4c35c3cf87104634a1a33ecad2bf38`  
**Branch:** `seo-os-v1`  
**Status:** Planned; implementation must be evidenced by code and tests.

## 1. Phase 4 outcome

Complete the first real website operations loop:

Research → Content → Quality Gate → CMS draft/publish → read-back verification → indexing workflows → GSC/GA4/Bing data feedback.

At the same time, deploy ARTEX SEO OS to a newly purchased dedicated VPS behind an independent HTTPS subdomain. Remote staging deployment is a Phase 4 deliverable, not a deferred Phase 8 task.

Production promotion is a separate approval gate after external-account, security, backup/restore, and stability tests.

## 2. Workstreams and dependencies

### P0 — Runtime and deployment foundation

1. Split the current `seoos serve` process into explicit `api`, `worker`, and `migrate` modes. At Phase 3, API startup also starts a worker loop; this must be changed before independent scaling.
2. Add Next.js production Docker image/service using the existing ARTEX App Shell and SEO server-side proxy.
3. Create separate development, remote-staging and production Compose overlays.
4. Add edge proxy with HTTPS, HTTP redirect, trusted proxy handling, security headers and rate limits.
5. Add readiness/liveness checks and graceful shutdown.
6. Add immutable image build/publish and GitHub Actions deployment by commit SHA.
7. Add pre-migration backup, one-shot migration, smoke test and application rollback workflow.
8. Add off-site encrypted PostgreSQL backup, restore verification, log rotation, metrics and alerts.
9. Keep PostgreSQL and internal Go API private; expose only the HTTPS edge.
10. Do not add Redis unless a concrete accepted use case is demonstrated. Current durable jobs are PostgreSQL-backed.

Acceptance:
- API, web, worker and migration are separate deployable processes.
- Multiple workers can claim jobs safely using the existing lease/claim model.
- API replicas do not unexpectedly start additional worker loops.
- Reboot, container recreation, worker termination and database restart recover safely.
- Restore a backup to a separate database and start the matching release.
- A browser can access the staging subdomain over HTTPS; no internal port is publicly reachable.

### P1 — WordPress and generic CMS

- WordPress connection with least-privilege Application Password.
- Read posts/pages/categories/tags and supported SEO metadata.
- Draft create/update, approved publishing, media upload, read-back verification and reversible changes.
- Detect Yoast SEO / Rank Math and verify actual fields/capabilities.
- Generic CMS connector contract and capability detection.
- Idempotent publishing and publication history.
- Never infer success from HTTP 200 alone; read the actual public page and compare expected title/content/metadata.

Acceptance:
- Use one user-authorized staging WordPress site.
- Complete read → draft → review → publish → read-back verify → rollback.
- No production sites are touched without separate explicit approval.

### P1 — Search data and indexing connectors

- GSC OAuth, property discovery, Search Analytics sync, sitemap status and URL Inspection within quota.
- GA4 Data API property connection, landing-page and organic traffic/conversion sync.
- Bing Webmaster authorized API integration for capabilities actually supported.
- Sitemap generation/refresh and IndexNow submission tracking.
- Explicit status semantics: discovered, submitted, crawled, indexed, not indexed, unknown. Only assert states backed by a reliable source.
- Incremental sync, pagination, quotas, retry/backoff and audit logs.

Acceptance:
- Local contract tests plus real authorized account tests where credentials are supplied.
- Store source, timestamp, property, market, query dimensions and provider response provenance.
- Do not claim real-account acceptance from local fixtures.

### P2 — Content extraction and operations

- PDF text extraction with page/size limits and optional OCR.
- JavaScript-rendered page extraction via isolated browser workers.
- Strict host/path allowlists, DNS/IP checks, redirect revalidation, request/response/time limits and egress restrictions to mitigate SSRF.
- Add page inventory refresh and content-change detection.
- Feed actual GSC/GA4 signals back into Content Refresh only after data source and freshness are verified.

Acceptance:
- PDF and JS fixture suites, resource-limit tests, SSRF regression tests and browser E2E.
- Clearly label incomplete/truncated extraction.

## 3. Remote VPS target

See `REMOTE_DEPLOYMENT_PLAN.md` for the complete topology, firewall, DNS/TLS, secrets, backup, monitoring, deployment and acceptance specification.

Initial target:
- New dedicated Ubuntu 24.04 VPS.
- 8 vCPU, 32 GB RAM, 500 GB NVMe starting allocation.
- Independent HTTPS subdomain such as `seo.example.com`.
- Separate staging hostname, protected by VPN/IP allowlist and authentication.
- Public inbound only 443 and 80 (redirect/ACME), plus restricted SSH.
- PostgreSQL, Go API, Next.js internal port, workers, metrics and Docker API are not public.
- PostgreSQL 16 initially; use versioned image tags and test upgrades.
- Dedicated private Docker network and persistent DB/media storage.
- Off-site encrypted backups; test restoration, not just backup creation.

These are initial sizing assumptions, not a claim of 150-site capacity. Use measured workload and queue/DB/provider telemetry to scale.

## 4. Required Phase 4 UI

Use the original ARTEX App Shell and `web/AGENTS.md` rules.

- CMS Integrations and WordPress Connections
- Publishing Center and Publication History
- GSC Dashboard
- GA4 Dashboard
- Bing Webmaster
- Indexing Center and Sitemap Manager
- URL Inspection
- Extraction Monitor
- Deployment/health overview for authorized operators (no public secrets or internal endpoints)

## 5. Required tests

- Go unit and PostgreSQL integration tests with race detector.
- Multi-tenant and role isolation.
- WordPress local integration environment.
- Real authorized staging WordPress test.
- CMS idempotency, read-back and rollback.
- OAuth refresh/revocation and provider quotas.
- GSC/GA4/Bing pagination and data provenance.
- Sitemap/IndexNow retry and status accuracy.
- PDF and browser extraction boundaries.
- SSRF, redirect, DNS rebinding and private-IP regression tests.
- API/web/worker separate process tests.
- Multi-worker concurrency and lease recovery.
- Container health checks and non-root runtime.
- Backup/restore and deploy/rollback drills.
- Chromium E2E in ARTEX visual style.
- Remote HTTPS smoke test and 7–14 day staging soak.

## 6. Phase 4 exit criteria

Phase 4 is complete only when:
- A real authorized WordPress staging site completes the publishing lifecycle.
- GSC/GA4/Bing connections are validated or explicitly blocked with evidence.
- Index submission and verified index status are distinguished.
- API, web and workers run as separate deployable services.
- A new dedicated VPS serves the management UI on the independent HTTPS staging subdomain.
- No internal service/database/metrics port is public.
- Off-site backup is restored successfully to an isolated database.
- Deployment and rollback are demonstrated.
- Real provider usage and billing are reconciled or clearly marked not yet available.
- 7–14 day staging soak has no unresolved critical reliability/security issue.
- Existing ARTEX visual system is preserved.
- Phase 0 licensing/source-use restrictions have a documented resolution before commercial production deployment.

## 7. Explicit non-goals

- Do not deploy to the existing production VPS.
- Do not modify `main`.
- Do not connect or publish to user production WordPress sites without explicit authorization.
- Do not claim 150+ full SEO workloads are supported from the previous 160-site inventory/job enqueue test.
- Do not introduce Kubernetes or Redis without measured need.
- Do not treat URL submission as indexing confirmation.
