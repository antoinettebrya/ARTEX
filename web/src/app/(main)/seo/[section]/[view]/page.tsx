import { Suspense } from "react";

import { notFound } from "next/navigation";

import { getSeoPage, seoPages } from "@/lib/seo/routes";

import { SeoPage } from "../../_components/page";
import { PageSkeleton } from "../../_components/shared";

export const dynamicParams = false;

export function generateStaticParams() {
  return seoPages.flatMap(({ path }) => {
    const [, , section, view] = path.split("/");
    return view ? [{ section, view }] : [];
  });
}

export default async function Page({ params }: { params: Promise<{ section: string; view: string }> }) {
  const { section, view } = await params;
  const route = getSeoPage(`/seo/${section}/${view}`);
  if (!route) notFound();
  return (
    <Suspense fallback={<PageSkeleton />}>
      <SeoPage route={route} />
    </Suspense>
  );
}
