# Web UI implementation rules

These rules apply to all work under `web/` and supplement the repository-root `AGENTS.md`.

## Non-negotiable visual baseline

The existing ARTEX web application is the source of truth. ARTEX SEO OS is an expansion of this application, not a separate dashboard product.

Before writing UI code, inspect the current implementation and identify the real components used by the active routes. At minimum inspect:

- `src/app/layout.tsx` and `src/app/globals.css`
- the active route layouts and App Shell
- existing sidebar and navbar implementations
- `src/components/` shared primitives and composed components
- `src/styles/` and theme preset CSS
- `src/lib/fonts/`
- `src/lib/preferences/`
- `src/stores/preferences/`
- `src/scripts/theme-boot`
- `src/config/`

Do not assume a component exists from its filename. Trace imports and actual usage.

## Implementation requirements

- Integrate SEO OS routes into the existing App Shell and navigation.
- Reuse current CSS variables, Tailwind conventions, shadcn components, icon system, typography, spacing, and interaction patterns.
- Preserve the existing theme initialization and preference store. Do not replace them with a second theme provider or local-only preference state.
- Keep the existing light/dark modes, theme presets, and font choices working.
- Use existing page headers, content widths, table density, form layouts, dialogs, dropdowns, toast and loading/empty/error states.
- Avoid creating duplicate Button/Card/Dialog/Table primitives or adding another UI framework.
- Keep the visual hierarchy restrained and information-dense. Avoid oversized cards, gratuitous gradients, excessive rounding/shadows, and decorative elements that are not part of ARTEX.
- All new modules must be visually consistent: Dashboard, Sites, Keywords, SERP Research, Content Studio, Image Studio, Theme Factory, Internal Links, Site Network, Technical SEO, Indexing, Analytics, GEO, Agent Center, Task Center, Providers, Integrations, Settings, and Audit Logs.
- Maintain accessible focus states, keyboard operation, semantic HTML, and responsive layouts.

## Verification

For meaningful UI changes:
- run the existing lint/typecheck/build scripts available in `package.json`;
- start the web app and inspect real rendered pages in a browser when available;
- compare the new pages against existing ARTEX pages;
- check light/dark modes and existing theme presets;
- check desktop and narrow/mobile layouts;
- record screenshots or explicitly state why visual verification could not be performed.

A code-only review is not a visual QA pass.
