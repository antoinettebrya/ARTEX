import { Suspense } from "react";

import { notFound } from "next/navigation";

import { SEO_SECTIONS, type SeoSection } from "@/lib/seo/routes";

import { SeoPage } from "../_components/page";
import { PageSkeleton } from "../_components/shared";

export const dynamicParams = false;

export function generateStaticParams() {
  return SEO_SECTIONS.map((section) => ({ section }));
}

export default async function Page({ params }: { params: Promise<{ section: string }> }) {
  const { section } = await params;
  if (!SEO_SECTIONS.includes(section as SeoSection)) notFound();
  return (
    <Suspense fallback={<PageSkeleton />}>
      <SeoPage section={section as SeoSection} />
    </Suspense>
  );
}
