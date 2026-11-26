"use client";

import { useEffect } from "react";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { isID } from "@/lib/seo/client";
import { SEO_HOME, type SeoRoute, seoAliases, seoRoutes } from "@/lib/seo/routes";

import { Audit } from "./audit";
import { ContentStudio } from "./content";
import { Dashboard } from "./dashboard";
import { ExecutionCenter } from "./execution";
import { Jobs } from "./jobs";
import { Members } from "./members";
import { Providers } from "./providers";
import { Research } from "./research";
import { useSeoSession } from "./session";
import { PageFrame, PageSkeleton, StateBadge } from "./shared";
import { SiteSettings, Sites } from "./sites";

function SeoRedirect({ href }: { href: string }) {
  const router = useRouter();
  useEffect(() => router.replace(href), [href, router]);
  return <PageSkeleton />;
}

export function SeoEntry({ section, route }: { section: string; route?: SeoRoute }) {
  const params = useSearchParams();
  const query = new URLSearchParams(params);
  let target: string | undefined = seoAliases[section as keyof typeof seoAliases];

  if (section === "execution") {
    if (params.has("state") && (params.has("code") || params.has("error"))) {
      return <ExecutionCenter view="connections" />;
    }
    if (isID(params.get("package"))) target = seoRoutes.execution.publishing.path;
  } else if (section === "content" && params.get("view") === "refresh") {
    target = seoRoutes.content.refresh.path;
    query.delete("view");
  } else if (section === "jobs" && isID(params.get("site"))) {
    query.set("new", "1");
  }

  if (target) return <SeoRedirect href={query.size ? `${target}?${query}` : target} />;
  if (!route) return <SeoRedirect href={SEO_HOME} />;
  return <SeoPage route={route} />;
}

export function SeoPage({ route }: { route: SeoRoute }) {
  const { principal } = useSeoSession();
  if (route.ownerOnly && principal.role !== "owner") {
    return (
      <Alert>
        <AlertTitle>{route.title}需要所有者权限</AlertTitle>
        <AlertDescription>
          <Button asChild variant="link" className="p-0">
            <Link href={SEO_HOME}>返回运营总览</Link>
          </Button>
        </AlertDescription>
      </Alert>
    );
  }
  if (route.planned) {
    return (
      <PageFrame title={route.title} description="此功能正在规划中。" actions={<StateBadge label="规划" />}>
        <Card className="py-0">
          <Empty>
            <EmptyHeader>
              <EmptyTitle>尚未开放</EmptyTitle>
              <EmptyDescription>{route.planned}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </Card>
      </PageFrame>
    );
  }
  switch (route.section) {
    case "dashboard":
      return <Dashboard view={route.view} />;
    case "sites":
      if (route.view === "connections") return <ExecutionCenter view="connections" />;
      if (route.view === "settings") return <SiteSettings />;
      return <Sites />;
    case "research":
      return <Research view={route.view} />;
    case "content":
      return <ContentStudio view={route.view} />;
    case "execution":
      return <ExecutionCenter view={route.view} />;
    case "system":
      if (route.view === "jobs") return <Jobs />;
      if (route.view === "members") return <Members />;
      if (route.view === "audit") return <Audit />;
      return <Providers />;
  }
}
