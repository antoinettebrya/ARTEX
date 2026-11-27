import { Suspense } from "react";

import { notFound } from "next/navigation";

import { getSeoPage, seoAliases, seoPages } from "@/lib/seo/routes";

import { SeoEntry } from "../_components/page";
import { PageSkeleton } from "../_components/shared";

export const dynamicParams = false;

export function generateStaticParams() {
  return [
    ...seoPages.flatMap(({ path }) => {
      const [, , section, view] = path.split("/");
      return view ? [] : [{ section }];
    }),
    ...Object.keys(seoAliases).map((section) => ({ section })),
  ];
}

export default async function Page({ params }: { params: Promise<{ section: string }> }) {
  const { section } = await params;
  const route = getSeoPage(`/seo/${section}`);
  if (!route && !Object.hasOwn(seoAliases, section)) notFound();
  return (
    <Suspense fallback={<PageSkeleton />}>
      <SeoEntry section={section} route={route} />
    </Suspense>
  );
}
