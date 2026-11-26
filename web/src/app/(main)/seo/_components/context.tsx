"use client";

import { createContext, type ReactNode, useContext } from "react";

import { usePathname, useSearchParams } from "next/navigation";

import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { isID, SeoAPIError, type Site } from "@/lib/seo/client";
import { ALL_SITES, changeSeoQuery, type SeoQuery, seoHref, seoSelection } from "@/lib/seo/routes";

import { readSeoPages, useSeoQuery } from "./query";
import { Field } from "./shared";

export function useSeoLocation() {
  const pathname = usePathname();
  const params = useSearchParams();
  const selection = seoSelection(params);
  return {
    pathname,
    params,
    siteID: isID(selection.site) ? selection.site : undefined,
    projectID: selection.project,
    href: (path: string, overrides?: SeoQuery) => seoHref(path, pathname, params, overrides),
    update: (changes: SeoQuery, options?: { replace?: boolean }) => {
      const current = window.location;
      const search = new URLSearchParams(current.search);
      const original = search.toString();
      changeSeoQuery(search, changes);
      if (search.toString() === original) return;
      const href = search.size ? `${current.pathname}?${search}${current.hash}` : `${current.pathname}${current.hash}`;
      // Next synchronizes same-page history updates with useSearchParams.
      if (options?.replace) window.history.replaceState(null, "", href);
      else window.history.pushState(null, "", href);
    },
  };
}

function loadSites(path: string, signal: AbortSignal) {
  return readSeoPages<Site>(path, "sites", signal);
}

export function siteGroup(site: Site) {
  return `${site.country_code}:${site.language_tag}`;
}

function readScopeCountry(value: string | null) {
  return value && /^[A-Z]{2}$/.test(value) ? value : undefined;
}

function readScopeLanguage(value: string | null) {
  return value && /^[A-Za-z0-9-]{2,64}$/.test(value) ? value : undefined;
}

const resetDetails = { project_id: null, package: null, keyword: null, q: null, job: null, new: null };

function useSiteDirectory() {
  const { siteID, params, update } = useSeoLocation();
  const groupValue = params.get("site_group");
  const group = groupValue && /^[A-Z]{2}:[A-Za-z0-9-]{2,64}$/.test(groupValue) ? groupValue : undefined;
  const country = readScopeCountry(params.get("site_country"));
  const language = readScopeLanguage(params.get("site_language"));
  const sites = useSeoQuery("/sites", loadSites);
  const entries = sites.data?.rows ?? [];
  const listed = entries.find(({ id }) => id === siteID);
  const lookup = useSeoQuery<{ site: Site }>(
    siteID && sites.data?.complete === false && !listed ? `/sites/${siteID}` : null,
  );
  function matches(site: Site) {
    return (
      (!group || siteGroup(site) === group) &&
      (!country || site.country_code === country) &&
      (!language || site.language_tag === language)
    );
  }
  const candidate = listed ?? lookup.data?.site;
  const site = candidate && matches(candidate) ? candidate : undefined;
  const allSites = candidate && !listed ? [...entries, candidate] : entries;
  const options = allSites.filter(matches);
  const rows = siteID ? options.filter(({ id }) => id === siteID) : options;
  const unavailable =
    siteID && sites.data && !lookup.loading && !site
      ? new SeoAPIError(404, candidate ? "site_scope_mismatch" : "not_found")
      : undefined;

  return {
    site,
    requestedSite: siteID,
    allSites,
    options,
    rows,
    key: [siteID, group, country, language].join("/"),
    complete: sites.data?.complete,
    restricted: !!(siteID || group || country || language),
    group,
    country,
    language,
    loading: sites.loading || lookup.loading,
    error: sites.error ?? lookup.error ?? unavailable,
    refresh() {
      sites.refresh();
      if (siteID && !listed && sites.data?.complete === false) lookup.refresh();
    },
    select(id: string) {
      if (id !== ALL_SITES && !options.some((site) => site.id === id)) return;
      if (id === (siteID ?? ALL_SITES)) return;
      update({ site_id: id, ...resetDetails });
    },
    filter(changes: SeoQuery) {
      update({ site_id: ALL_SITES, ...resetDetails, ...changes });
    },
  };
}

type SiteScope = ReturnType<typeof useSiteDirectory>;
const ScopeContext = createContext<SiteScope | null>(null);

export function SeoSiteProvider({ children }: { children: ReactNode }) {
  const scope = useSiteDirectory();
  return <ScopeContext.Provider value={scope}>{children}</ScopeContext.Provider>;
}

export function useSiteScope() {
  const scope = useContext(ScopeContext);
  if (!scope) throw new Error("Missing SEO site scope");
  return scope;
}

type Option = { id: string; label: string };

export function WorkspaceSelector({
  id,
  label,
  options,
  value,
  onChange,
}: {
  id: string;
  label: string;
  options: Option[];
  value?: string;
  onChange: (id: string) => void;
}) {
  if (!options.length) return null;
  return (
    <div className="min-w-0 flex-1 basis-44 sm:max-w-80">
      <Field id={id} label={label}>
        <NativeSelect
          id={id}
          className="w-full min-w-0"
          value={value ?? ""}
          onChange={(event) => onChange(event.target.value)}
        >
          {!value && (
            <NativeSelectOption value="" disabled>
              请选择
            </NativeSelectOption>
          )}
          {options.map(({ id, label }) => (
            <NativeSelectOption key={id} value={id}>
              {label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Field>
    </div>
  );
}

export function SiteSelector({
  id,
  label,
  scope,
}: {
  id: string;
  label: string;
  scope: ReturnType<typeof useSiteScope>;
}) {
  return (
    <WorkspaceSelector
      id={id}
      label={label}
      options={[
        { id: ALL_SITES, label: "全部站点" },
        ...scope.options.map((site) => ({ id: site.id, label: site.name })),
      ]}
      value={scope.requestedSite ?? ALL_SITES}
      onChange={scope.select}
    />
  );
}

export function ProjectSelector({
  id,
  label = "内容项目",
  options,
  value,
  onChange,
}: {
  id: string;
  label?: string;
  options: Option[];
  value?: string;
  onChange: (id: string) => void;
}) {
  return <WorkspaceSelector id={id} label={label} options={options} value={value} onChange={onChange} />;
}
