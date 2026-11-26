# ARTEX SEO OS — UI Design System Inheritance

**Status:** Mandatory implementation baseline  
**Applies to:** All SEO OS web routes and UI components  
**Source of truth:** Existing ARTEX application under `web/`

## 1. Product continuity

ARTEX SEO OS is the evolution of ARTEX. The interface must feel like the same application with expanded capabilities, not a newly invented generic SaaS admin template.

The existing ARTEX web application is the visual reference and implementation foundation. Extend it; do not replace it.

## 2. Existing design assets to inspect and reuse

The following are confirmed existing paths in the repository:

- `web/src/app/globals.css` — global tokens, Tailwind integration, light/dark variables, theme preset imports.
- `web/src/app/layout.tsx` — root layout, theme boot script, preference provider, font variables.
- `web/src/styles/presets/` — existing visual presets.
- `web/src/lib/fonts/registry` — font registration.
- `web/src/lib/preferences/` — preference defaults and configuration.
- `web/src/stores/preferences/` — preference state.
- `web/src/scripts/theme-boot` — theme initialization.

The actual App Shell, sidebar, navbar, and reusable component implementations must be located by tracing route imports and component usage. Do not infer their locations or create replacements based on guesses.

## 3. Rules for new SEO OS pages

All new routes must be mounted inside the existing ARTEX application shell and use its actual shared components and design tokens.

This includes:

- Overview and Dashboard
- Sites
- Keywords and SERP Research
- Content Studio
- Image Studio and Media Library
- WordPress Theme Factory
- On-Page SEO and Technical SEO
- Internal Links
- Site Network
- Indexing
- Analytics and GEO
- Agent Center and Task Center
- Workflows
- Providers, Integrations, Settings, and Audit Logs

The exact navigation grouping may follow the existing navigation component's conventions. Do not add a second sidebar, second navbar, or independent shell.

## 4. Visual and interaction consistency

Preserve and reuse:

- Existing light/dark modes and theme presets
- Existing font family and font preferences
- CSS variables and Tailwind conventions
- Page headers and content containers
- Sidebar and navbar behavior
- Buttons, inputs, selects, tables, cards, tabs, dialogs, dropdowns, popovers
- Toasts, tooltips, loading, empty, error, and confirmation states
- Icon library and sizing conventions
- Spacing, borders, radii, shadows, focus and hover states
- Responsive breakpoints and navigation behavior

Avoid:
- A parallel design system or duplicate primitive components
- A competing UI framework
- Arbitrary global CSS overrides
- Unapproved fonts or icon packs
- Gratuitous gradients, giant decorative cards, excessive rounding or shadows
- Emoji as UI icons
- Low-density layouts that waste space needed for SEO data and operations

## 5. Component-first workflow

Before implementing a new component:

1. Search the current codebase for an equivalent component.
2. Trace its implementation and usage.
3. Reuse it if it meets the requirement.
4. If it needs an extension, extend it compatibly.
5. Only create a new component when no suitable existing component exists; follow current conventions and document why.

Do not redesign existing ARTEX screens just to make the new SEO screens easier to implement.

## 6. Visual QA gate

A UI task is not complete until the following are checked where browser access is available:

- Existing ARTEX page as a visual baseline
- New SEO OS page
- Light mode and dark mode
- Existing theme presets and font preferences
- Desktop and narrow/mobile widths
- Navigation, forms, tables, dialogs, and interactive states

Run the available web lint/typecheck/build commands. Capture before/after screenshots for substantial UI changes. If browser access is unavailable, explicitly mark visual QA as blocked rather than claiming it passed.

## 7. Definition of done

A new SEO OS screen is accepted when:
- it uses the existing ARTEX App Shell;
- it reuses the existing design tokens and shared components;
- it preserves theme and preference behavior;
- it is responsive and accessible;
- functional tests and available frontend checks pass;
- rendered visual QA is documented.
