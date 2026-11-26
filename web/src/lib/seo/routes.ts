import { ClipboardList, Globe, Plug, ScrollText, Users } from "lucide-react";

import type { NavGroup } from "@/navigation/sidebar/sidebar-items";

export const SEO_SECTIONS = ["sites", "jobs", "providers", "members", "audit"] as const;
export type SeoSection = (typeof SEO_SECTIONS)[number];

export const seoTitles: Record<SeoSection, string> = {
  sites: "站点",
  jobs: "任务",
  providers: "Provider",
  members: "成员",
  audit: "审计日志",
};

export function seoGroups(owner: boolean): NavGroup[] {
  return [
    {
      id: 1,
      label: "运营",
      items: [
        { id: "seo-sites", title: seoTitles.sites, url: "/seo/sites", icon: Globe },
        { id: "seo-jobs", title: seoTitles.jobs, url: "/seo/jobs", icon: ClipboardList },
      ],
    },
    {
      id: 2,
      label: "系统",
      items: [
        { id: "seo-providers", title: seoTitles.providers, url: "/seo/providers", icon: Plug },
        ...(owner
          ? [
              { id: "seo-members", title: seoTitles.members, url: "/seo/members", icon: Users },
              { id: "seo-audit", title: seoTitles.audit, url: "/seo/audit", icon: ScrollText },
            ]
          : []),
      ],
    },
  ];
}
