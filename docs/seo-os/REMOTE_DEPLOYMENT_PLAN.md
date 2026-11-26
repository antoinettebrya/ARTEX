# ARTEX SEO OS — Remote Deployment Architecture

**Decision:** Use a newly purchased, dedicated VPS. Publish the management application at an independent HTTPS subdomain.  
**Phase:** Deployment engineering is part of Phase 4. Remote staging must be live during Phase 4; production promotion remains gated by external integration and restore tests.  
**Repository baseline:** Phase 3 `7eaae96` (update this reference when the deployment implementation branch is cut).

## 1. Deployment goals

- Isolate ARTEX SEO OS from the existing VPS and unrelated containers.
- Expose only the management HTTPS entry point publicly.
- Keep PostgreSQL, worker, and internal service ports on a private Docker network.
- Deploy reproducibly from immutable Git commits/images.
- Preserve database and uploaded media across container replacement.
- Support safe schema migration, backup, restore, health checks, monitoring, and rollback.
- Support one site and a growing multi-site portfolio; 150+ sites is a load-test target, not a capacity claim.
- Keep the original ARTEX UI design system and App Shell.

## 2. Recommended VPS sizing

### Initial dedicated host

| Resource | Initial recommendation | Notes |
|---|---:|---|
| CPU | 8 vCPU | API, Next.js, one or more workers, browser extraction only when enabled |
| RAM | 32 GB | Keep PostgreSQL cache and worker concurrency bounded |
| NVMe | 500 GB | OS, images, DB, logs, temporary extraction and media staging |
| Network | 1 Gbps preferred | Provider egress, CMS and data APIs |
| OS | Ubuntu 24.04 LTS | Minimal server installation |
| Docker | Current supported Docker Engine + Compose plugin | Pin versions in deployment runbook |

This is a starting point for staging and early production, not a guaranteed 150-site capacity. Measure actual daily URL fetches, provider calls, content jobs, browser minutes, DB growth and egress before increasing concurrency.

### Growth triggers

Review vertical/horizontal scaling when any of these persist:
- CPU > 70% during normal scheduled work.
- Memory > 75% or OOM events.
- Disk > 70%, DB volume growth exceeds forecast, or backup window misses its target.
- Queue oldest-job age exceeds the agreed SLO.
- PostgreSQL connections or slow queries approach configured limits.
- Worker saturation causes provider timeouts or retry storms.

First scale stateless API/web replicas and worker concurrency separately. Move PostgreSQL to a dedicated managed/isolated database host only when measured load or recovery requirements justify it. Do not introduce Redis merely because it is common in SaaS architectures; Phase 1–3 currently use PostgreSQL-backed durable jobs. Add Redis only for a concrete measured cache/coordination need and with an explicit persistence/failure model.

## 3. DNS and HTTPS

Use a dedicated hostname, for example `seo.example.com` (replace with the user's actual domain).

Recommended path:

1. Create an A/AAAA record to the new VPS.
2. Optionally enable Cloudflare proxy after origin TLS is configured.
3. Terminate TLS at Nginx or Traefik.
4. Redirect HTTP to HTTPS.
5. Use Let's Encrypt or Cloudflare Origin CA with Cloudflare SSL mode **Full (strict)** when proxied.
6. Automate certificate renewal and test renewal before launch.
7. Set HSTS only after HTTPS and subdomain routing are confirmed stable.

Use a second hostname such as `staging-seo.example.com` for remote staging. Protect staging with VPN/IP allowlisting and authentication; never expose staging credentials or test fixtures publicly.

## 4. Network boundaries

### Public inbound

- TCP 443: HTTPS management UI and same-origin API proxy.
- TCP 80: only for HTTP-to-HTTPS redirect and ACME HTTP-01 if used.
- TCP 22: SSH restricted to known administrator IPs or a private access network.

### Never expose publicly

- PostgreSQL 5432.
- SEO Go API 8789.
- Next.js internal port (for example 3000).
- Worker/admin/debug endpoints.
- Docker socket or Docker API.
- Metrics endpoints.
- Redis, if later introduced.

Use UFW/nftables and cloud firewall rules together. Bind host-published app ports to loopback where possible. Services communicate over a dedicated, non-public Docker network. Do not rely on UFW alone to protect Docker-published ports; verify the actual host firewall path.

## 5. Runtime topology

The intended Phase 4 production topology is:

```text
Browser
  |
  | HTTPS :443
  v
Nginx / Traefik (public edge, TLS, security headers, rate limits)
  |
  +---- Next.js web (private network)
  |       |
  |       +---- same-origin /seo/api/* proxy ----+
  |                                              |
  +----------------------------------------------v
                                         Go SEO API
                                               |
                           +-------------------+------------------+
                           |                                      |
                     PostgreSQL 16                         durable jobs
                                                                  |
                                                            SEO Workers
                                                                  |
                           +-------------------+------------------+
                           |                   |                  |
                       WordPress         DataForSEO/LLM       GSC/GA4/Bing
```

### Important current-code gap to close in Phase 4

At Phase 3, `seoos/cmd/seoos serve` starts both the HTTP server and a worker loop in the same process. The existing `seoos/compose.yaml` currently defines PostgreSQL and API only; it does not define the Next.js production service or a separately scalable worker service.

Phase 4 deployment work must:
- Add explicit process modes or entrypoints: `api`, `worker`, and `migrate`.
- Ensure only the worker mode claims and executes jobs.
- Allow multiple worker replicas with PostgreSQL lease/claim safety.
- Keep API replicas stateless apart from configuration and use the shared PostgreSQL database.
- Add the Next.js production image/service using the SEOOS server-side API proxy.
- Add a reverse-proxy service or document integration with an independently managed edge proxy.
- Add health checks and startup dependencies.
- Verify graceful shutdown and in-flight job lease behavior.

Do not run multiple API replicas that each implicitly start a worker until the process split is implemented and tested.

### Services

| Service | Public? | Responsibilities |
|---|---|---|
| edge | Yes, 80/443 | TLS, redirect, headers, request limits |
| web | No | Next.js production UI, ARTEX App Shell |
| api | No | Go HTTP API, authentication, authorization |
| worker | No | Durable job claim/execute/finish |
| migrate | One-shot only | Apply append-only DB migrations during deploy |
| postgres | No | SEO business data, research, content, audit, jobs |
| backup | Outbound only | Encrypted off-site backup and verification |
| monitoring | Private/admin only | Health, metrics, logs, alerting |

Do not add Redis until an accepted architecture change requires it. Do not expose Postgres on a host-published port.

## 6. Environment and secrets

- Keep production `.env` outside the Git checkout, e.g. `/etc/artex-seoos/production.env`, owner root/service group, mode 0600.
- Prefer a secret manager or Docker secrets for long-lived credentials.
- Never put secrets in Compose YAML, image build args, Docker labels, CI logs, screenshots or committed files.
- Generate unique values per environment for DB password, operator bootstrap token and provider encryption key.
- Store the Provider encryption key separately from the DB backup. Loss of the key makes encrypted provider credentials unrecoverable.
- Rotate credentials with a documented overlap/rollback plan.
- Use separate credentials and provider budgets for staging and production.
- Use dedicated least-privilege WordPress Application Passwords; revoke them independently.
- OAuth refresh tokens for GSC/GA4/Bing must be encrypted at rest and have revocation/rotation procedures.

Required environment/config categories:
- Database DSN and TLS policy.
- Organization bootstrap identity.
- Provider encryption key.
- Web-to-API private origin.
- Public canonical URL / trusted proxy configuration.
- Session cookie secure settings and allowed origins.
- Per-provider budgets, timeouts and concurrency.
- Backup destination and encryption configuration.
- Logging level, retention and monitoring endpoint.

## 7. Database and media persistence

- Use a named volume or dedicated encrypted block volume for PostgreSQL data.
- Keep PostgreSQL data separate from container writable layers.
- Configure database connection limits based on API/worker replica counts.
- Use TLS for remote database links if the DB is moved off-host.
- Persist media in an object store or dedicated volume; do not rely on container filesystem.
- Keep generated theme build artifacts and CMS export packages in versioned object storage with retention.
- Never store generated theme archives in a publicly served directory before validation.

### Backup policy

Initial target (adjust after RPO/RTO review):
- Nightly encrypted PostgreSQL custom-format backup.
- Continuous or frequent WAL archiving if point-in-time recovery is required.
- Daily backup of required media/config metadata.
- Off-site copy in a separate account/region or storage provider.
- 30-day daily retention and monthly longer-term retention.
- Backup success/failure alert.
- Verify backup checksum and periodically restore to an isolated database.
- Record recovery point and recovery duration after each drill.

A backup is not accepted until restore has been demonstrated. Never place the only backup on the same VPS or same volume as production.

## 8. Deployment and release process

Use GitHub Actions to build/test immutable images and deploy by commit SHA or version tag.

1. PR checks: Go tests with race detector, PostgreSQL integration, web build/typecheck, Chromium E2E, dependency and image checks.
2. Merge/release: build API, worker and web images with immutable tags; generate SBOM and retain build metadata.
3. Push images to a private registry.
4. Deployment job connects over SSH using a restricted deploy key; no broad personal SSH key.
5. Verify free disk, current version, DB connectivity and backup destination.
6. Put app into maintenance/read-only mode if migration requires it.
7. Create and verify a pre-migration DB backup.
8. Run one-shot migration using the new release image.
9. Deploy API, worker and web services.
10. Wait for health checks and verify the deployed commit SHA.
11. Run smoke tests against the HTTPS hostname.
12. If health checks fail, roll back application images. If schema/data changes are not backward-compatible, restore the pre-deploy backup to a separate DB and switch traffic only after verification; do not blindly run destructive down-migrations.
13. Keep the previous known-good images until the release is accepted.

Never deploy `latest` as the only release reference. Never automatically deploy a PR branch to production.

## 9. Observability and operations

Provide:
- Structured JSON logs with request ID, job ID, organization ID (non-sensitive), provider and outcome.
- No tokens, passwords, raw authorization headers, page private data or unredacted provider secrets in logs.
- API health/readiness and worker liveness.
- PostgreSQL health, connections, locks, slow queries, DB size and backup status.
- Queue depth, oldest queued age, retries, dead-letter count, lease expiries and job duration.
- Per-provider request count, latency, rate-limit events, reserved/actual cost and daily budget.
- CMS publishing success/failure and post-publish verification.
- Disk, memory, CPU, network, certificate expiry and container restart count.
- Alert routes for service outage, backup failure, disk pressure, repeated worker crashes, queue backlog and unusual provider spend.
- Log rotation and retention limits.

Metrics and dashboards must be private or protected by VPN/authentication.

## 10. Security baseline

- SSH keys only; disable password SSH and root login after recovery access is verified.
- Restrict SSH source IPs; use a separate non-root sudo operator.
- Automatic security updates with a controlled reboot window.
- Firewall default deny inbound.
- TLS 1.2+ and secure cookie configuration.
- CSRF/origin validation for state-changing browser requests.
- Rate limits on login, API and expensive job submission.
- Container non-root users, dropped Linux capabilities, read-only root FS where practical, resource/PID limits and no privileged mode.
- No Docker socket mount in app containers.
- Pin base image versions/digests and scan images/dependencies.
- Restrict egress from browser extraction and CMS fetchers; defend against SSRF, private IP ranges, DNS rebinding and redirects.
- Separate staging and production DBs, keys, providers and CMS credentials.
- Document incident response and credential revocation.

## 11. Staging-to-production gates

### Remote staging (required during Phase 4)
- New VPS provisioned and hardened.
- HTTPS staging hostname working.
- API, web and worker process separation proven.
- PostgreSQL migrations, backup and restore tested.
- One explicitly authorized test WordPress site connected.
- Draft creation, publish approval, publishing, read-back verification and rollback tested.
- GSC/GA4/Bing OAuth and real data sync verified where credentials are available.
- Real model/DataForSEO spend and invoices reconciled using explicit budgets.
- 7–14 day stability soak with alerts and restore drill.

### Production promotion (after Phase 4)
- All staging gates passed.
- License and source-use review resolved.
- Production hostname and secrets provisioned independently.
- RPO/RTO agreed.
- Backups restored successfully.
- Security review and load test passed.
- Operator approval recorded.
- Start with a small number of owned sites; increase gradually. Do not onboard 150 sites in one switch.

## 12. Acceptance checklist

- [ ] VPS sizing and provider selected.
- [ ] DNS hostname selected and certificate automation tested.
- [ ] Firewall and SSH hardening verified.
- [ ] Production and staging Compose configurations separated.
- [ ] API/web/worker/migrate process separation implemented.
- [ ] Next.js production container built and served behind HTTPS.
- [ ] PostgreSQL is private, persistent, backed up and restore-tested.
- [ ] Media/object storage persistence verified.
- [ ] Secret handling and key recovery procedure documented.
- [ ] CI builds immutable images and deploys by SHA.
- [ ] Health checks, logs, metrics and alerts verified.
- [ ] Deployment and rollback drills completed.
- [ ] Real WordPress staging workflow passed.
- [ ] Real provider usage and billing reconciled.
- [ ] 7–14 day soak passed.
- [ ] Production go-live approved.

## 13. Official references

- Docker Compose production guidance: https://docs.docker.com/compose/how-tos/production/
- Compose startup ordering and health checks: https://docs.docker.com/compose/how-tos/startup-order/
- Compose resource constraints: https://docs.docker.com/reference/compose-file/deploy/
- PostgreSQL backup and restore: https://www.postgresql.org/docs/16/backup.html
- PostgreSQL continuous archiving / PITR: https://www.postgresql.org/docs/16/continuous-archiving.html
- Cloudflare SSL modes: https://developers.cloudflare.com/ssl/origin-configuration/ssl-modes/
