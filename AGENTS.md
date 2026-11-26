# Repository-wide development rules

These instructions apply to all work in this repository. More specific `AGENTS.md` files may add constraints for their directory, but must not weaken these rules.

## Product direction

ARTEX is being transitioned to ARTEX SEO OS: a general-purpose SEO operations platform for one site through large multi-site portfolios. Product scope is defined in `docs/seo-os/PRODUCT_SCOPE.md`.

The final product must not retain penetration-testing workflows, exploit workflows, vulnerability discovery/retesting, MITM traffic capture, attack-chain planning, or offensive target-probing tools. Do not wire the legacy runtime into the new SEO service. Reuse legacy infrastructure only after code, dependency, provenance, and license review.

## Git and change safety

- Work on `seo-os-v1` or a dedicated feature branch based on it. Never commit directly to `main`.
- Inspect `git status`, current branch, and recent commits before editing.
- Do not overwrite or discard user changes.
- Keep changes scoped; do not perform unrelated cleanup.
- Use separate branches/worktrees for parallel agents. Coordinate shared interfaces and migrations before parallel edits.
- Every change requires tests appropriate to its risk, and documentation updates when behavior or contracts change.
- Do not claim tests, browser checks, deployments, or external integrations succeeded unless actually executed and evidenced.
- Never deploy to production sites or use production credentials during local development or tests.
- Keep secrets out of source, fixtures, logs, screenshots, and commits.

## Architecture rules

- The new SEO service lives under `seoos/` and remains isolated from the legacy ARTEX runtime.
- Use versioned, append-only PostgreSQL migrations. Never edit an already-applied migration; add a new migration.
- Enforce organization/site scoping at repository boundaries and test cross-tenant isolation.
- External providers must have timeouts, bounded retries, rate limits, usage/cost budgets, and auditable errors.
- Writes to CMS content, metadata, links, and themes must be versioned, validated, auditable, and reversible.
- Site registration does not imply permission to connect to or modify that site.
- Cross-site links must be editorially relevant and useful to readers. Do not create artificial reciprocal-link networks or link schemes to manipulate search rankings.
- Generated WordPress themes must be built and tested in isolation, installed in staging first, and have a verified rollback path.

## UI design rule — mandatory

**ARTEX SEO OS must look and feel like the existing ARTEX application, not like a newly invented generic SaaS dashboard.**

- Treat the existing `web/` application as the sole visual and interaction reference.
- Before implementing or changing any UI, inspect the actual existing App Shell, navigation, page containers, typography, CSS variables, theme boot process, preferences store, and reusable components.
- Reuse the existing sidebar, navbar, layout, buttons, cards, tables, forms, dialogs, dropdowns, tooltips, toasts, icons, spacing, typography, and theme tokens wherever possible.
- Extend the existing application shell and navigation. Do not create a second shell or a parallel design system for SEO OS.
- Preserve existing light/dark modes, theme presets, font preferences, responsive behavior, and user preference persistence.
- New SEO modules (Dashboard, Sites, Keywords, SERP, Content, Image Studio, Theme Factory, Internal Links, Site Network, Technical SEO, Indexing, Analytics, GEO, Agent Center, Task Center, Providers, Settings) must use the same visual language and shared components.
- Do not introduce a competing component framework, duplicate primitive components, arbitrary global CSS, unapproved fonts, decorative gradients, excessive rounded cards/shadows, or emoji in place of the existing icon system.
- Prefer dense, clear, professional information layouts over oversized decorative cards. Visual polish must not reduce operational data density or usability.
- If an existing component is missing, first document the gap and extend the current component conventions before introducing a new primitive.
- For meaningful UI changes, run the app and verify rendered screens in a browser. Check light and dark modes, existing presets, desktop and narrow/mobile widths. Capture before/after screenshots when practical.
- If browser verification is unavailable, state that limitation; do not claim visual consistency has been verified.

## Required delivery note

At the end of each implementation task, report:
1. changed files and behavior;
2. tests/commands actually run and their results;
3. browser/visual checks actually performed;
4. remaining gaps and risks;
5. commit/branch information.

Do not mark a feature complete merely because code was generated.
