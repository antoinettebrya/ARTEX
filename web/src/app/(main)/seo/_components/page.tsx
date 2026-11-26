"use client";

import Link from "next/link";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { type SeoSection, seoTitles } from "@/lib/seo/routes";

import { Audit } from "./audit";
import { Jobs } from "./jobs";
import { Members } from "./members";
import { Providers } from "./providers";
import { Research } from "./research";
import { useSeoSession } from "./session";
import { Sites } from "./sites";

export function SeoPage({ section }: { section: SeoSection }) {
  const { principal } = useSeoSession();
  if (principal.role !== "owner" && (section === "members" || section === "audit")) {
    return (
      <Alert>
        <AlertTitle>{seoTitles[section]}需要所有者权限</AlertTitle>
        <AlertDescription>
          <Button asChild variant="link" className="p-0">
            <Link href="/seo/sites">返回站点</Link>
          </Button>
        </AlertDescription>
      </Alert>
    );
  }
  switch (section) {
    case "sites":
      return <Sites />;
    case "jobs":
      return <Jobs />;
    case "research":
      return <Research />;
    case "providers":
      return <Providers />;
    case "members":
      return <Members />;
    case "audit":
      return <Audit />;
  }
}
