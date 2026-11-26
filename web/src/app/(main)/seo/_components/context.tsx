"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { isID, type Site } from "@/lib/seo/client";
import { type SeoQuery, seoHref } from "@/lib/seo/routes";

import { useSeoQuery } from "./query";
import { Field } from "./shared";

export function useSeoLocation() {
  const pathname = usePathname();
  const params = useSearchParams();
  const router = useRouter();
  const site = params.get("site");
  const project = params.get("project");
  return {
    params,
    siteID: isID(site) ? site : undefined,
    projectID: isID(project) ? project : undefined,
    href: (path: string, overrides?: SeoQuery) => seoHref(path, pathname, params, overrides),
    update: (updates: SeoQuery, options?: { replace?: boolean }) => {
      const search = new URLSearchParams(params);
      for (const [key, value] of Object.entries(updates)) {
        if (value === null) search.delete(key);
        else if (value !== undefined) search.set(key, value);
      }
      const href = search.size ? `${pathname}?${search}` : pathname;
      if (href === (params.size ? `${pathname}?${params}` : pathname)) return;
      if (options?.replace) router.replace(href, { scroll: false });
      else router.push(href, { scroll: false });
    },
  };
}

export function useSiteScope() {
  const { siteID, update } = useSeoLocation();
  const sites = useSeoQuery<{ sites: Site[] }>("/sites?limit=200");
  const options = sites.data?.sites ?? [];
  const listed = siteID ? options.find((site) => site.id === siteID) : options[0];
  const lookup = useSeoQuery<{ site: Site }>(siteID && sites.data && !listed ? `/sites/${siteID}` : null);
  const site = listed ?? lookup.data?.site;
  return {
    site,
    sites,
    options: site && !listed ? [site, ...options] : options,
    loading: sites.loading || lookup.loading,
    error: sites.error ?? lookup.error,
    refresh: sites.error || !siteID || listed ? sites.refresh : lookup.refresh,
    select(id: string) {
      if (!isID(id) || id === siteID) return;
      update({ site: id, project: null, package: null, keyword: null, job: null });
    },
  };
}

type Option = { id: string; label: string };

function WorkspaceSelector({
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
      options={scope.options.map((site) => ({ id: site.id, label: site.name }))}
      value={scope.site?.id}
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
