import type { ReactNode } from "react";

import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "ARTEX SEO OS",
  description: "站点与 SEO 任务管理",
};

export default function Layout({ children }: { children: ReactNode }) {
  return children;
}
