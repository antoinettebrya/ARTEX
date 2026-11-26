"use client";

import { type ReactNode, useEffect, useState } from "react";

import { usePathname } from "next/navigation";

import { useCurrentUser } from "@/hooks/use-current-user";
import { auth } from "@/lib/auth";

import { AppShell } from "./_components/app-shell";
import { SeoSession } from "./seo/_components/session";

function LegacyGate({ children }: { children: ReactNode }) {
  const user = useCurrentUser();
  const [authed, setAuthed] = useState(false);

  useEffect(() => {
    if (auth.getToken()) {
      setAuthed(true);
    } else {
      // Keep the legacy cookie and client credential in sync on logout.
      auth.clearToken();
      window.location.href = "/login";
    }
  }, []);

  if (!authed) return null;
  return <AppShell user={user}>{children}</AppShell>;
}

export default function Layout({ children }: { readonly children: ReactNode }) {
  const pathname = usePathname();
  if (pathname === "/seo" || pathname.startsWith("/seo/")) {
    return <SeoSession>{children}</SeoSession>;
  }
  return <LegacyGate>{children}</LegacyGate>;
}
