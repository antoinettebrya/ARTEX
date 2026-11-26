# ARTEX SEO OS — Product Scope

## Product statement

A multi-tenant, multi-site AI SEO operations platform that researches search demand, plans pages, produces and maintains useful content, manages site presentation and media, executes approved technical changes, and measures outcomes.

## Supported operating scale

The product must support both a single website and a large site portfolio. The 150+ site target is a load-test and operational acceptance objective, not an assumed capacity claim.

## Product modules

| Domain | Scope |
|---|---|
| Site Management | Organizations, sites, domains, locales, connections, competitors |
| Keyword Intelligence | Discovery, metrics, intent, clustering, opportunity scoring |
| SERP Intelligence | SERP snapshots, competitor page analysis, evidence provenance |
| Content Intelligence | Briefs, drafts, fact/source checks, review, versioning |
| Image Studio | Multi-provider image generation/editing, brand memory, asset pipeline, CMS media sync |
| Theme Factory | WordPress theme generation, isolated build/test, install, activation, rollback |
| On-Page SEO | Metadata, canonical, robots, structured data, page quality |
| Internal Link Engine | Intra-site page graph, recommendations, validation |
| Technical SEO | Crawl diagnostics, indexability, redirects, performance observations |
| Indexing Engine | Sitemap, IndexNow, GSC/Bing status workflows |
| Analytics & GEO | Search/traffic trends, AI answer visibility measurements |
| Site Network | Cross-site relationship graph, relevance-based citations, reviews, link monitoring |
| Autonomous Operations | Master Agent, specialist agents, durable queue, budgets, approvals, audit |

## Operating principles

- Evidence-first: retain source, timestamp, locale, and provider for external data.
- Intent-first: map queries to user needs and page types before drafting.
- Quality-first: no bulk publishing based solely on generated volume or model confidence.
- Tenant isolation: all organization-owned data is scoped and tested.
- Least privilege: agents receive narrow, task-specific tools.
- Safe deployment: themes and content are built/tested in staging before production activation.
- Reversible writes: content, metadata, links, and themes have snapshots and rollback paths.
- Transparent linking: cross-site links must have editorial/user value; do not manufacture link schemes to manipulate rankings. Disclose commercial relationships and apply appropriate link attributes.
- Provider-neutral: model, SEO data, image, and CMS providers are replaceable.
- Measurable autonomy: every scheduled workflow has a budget, timeout, idempotency key, retry policy, and audit trail.

## Explicit exclusions

The final SEO OS must not contain penetration-testing task flows, exploit workflows, vulnerability discovery/retesting, MITM traffic capture, attack-chain planning, or offensive target-probing tools. Any reusable general-purpose infrastructure must be separated from these capabilities and independently reviewed before retention.
