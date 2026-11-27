"use client";

import Link from "next/link";

import { Button } from "@/components/ui/button";

import { useSeoLocation } from "./context";
import { useSeoSession } from "./session";

export function WorkflowNav({
  label,
  pages,
}: {
  label: string;
  pages: readonly { path: string; title: string; ownerOnly?: boolean; planned?: string }[];
}) {
  const { pathname, href } = useSeoLocation();
  const { principal } = useSeoSession();
  return (
    <nav aria-label={label} className="flex flex-wrap gap-2">
      {pages
        .filter(({ ownerOnly }) => !ownerOnly || principal.role === "owner")
        .map((page) => {
          const active = pathname.replace(/\/+$/, "") === page.path;
          return (
            <Button key={page.path} asChild size="sm" variant={active ? "secondary" : "outline"}>
              <Link href={href(page.path)} aria-current={active ? "page" : undefined}>
                {page.title}
                {page.planned ? "（规划）" : ""}
              </Link>
            </Button>
          );
        })}
    </nav>
  );
}
