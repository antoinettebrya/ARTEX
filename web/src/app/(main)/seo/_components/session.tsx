"use client";

import { createContext, type ReactNode, useContext, useEffect } from "react";

import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { toast } from "sonner";

import { AppShell } from "@/app/(main)/_components/app-shell";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { errorMessage, type Principal, roles, SeoAPIError, seoRequest } from "@/lib/seo/client";
import { SEO_HOME, safeSeoReturn, seoGroups } from "@/lib/seo/routes";

import { useSeoQuery } from "./query";

const SessionContext = createContext<{ principal: Principal; refresh: () => void } | null>(null);

export function useSeoSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error("Missing SEO session");
  return session;
}

export function SessionLoading() {
  return (
    <div className="flex min-h-dvh items-center justify-center gap-2 text-muted-foreground" role="status">
      <Spinner /> 正在检查登录状态…
    </div>
  );
}

export function SeoSession({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const returnPath = safeSeoReturn(`${pathname}?${params}`);
  const { data: principal, error, refresh } = useSeoQuery<Principal>("/me");
  const unauthorized = error instanceof SeoAPIError && error.status === 401;
  useEffect(() => {
    if (unauthorized) router.replace(returnPath ? `/seo/login?next=${encodeURIComponent(returnPath)}` : "/seo/login");
  }, [router, unauthorized, returnPath]);

  if (!principal) {
    if (error && !unauthorized) {
      return (
        <main className="mx-auto grid min-h-dvh w-full max-w-lg content-center gap-4 p-4">
          <Alert variant="destructive">
            <AlertTitle>无法读取组织信息</AlertTitle>
            <AlertDescription>{errorMessage(error)}</AlertDescription>
          </Alert>
          <Button className="justify-self-start" variant="outline" onClick={refresh}>
            重试
          </Button>
        </main>
      );
    }
    return <SessionLoading />;
  }

  const name = principal.email?.split("@")[0] ?? "配置凭据";
  const user = {
    id: principal.user_id ?? principal.organization_id,
    name,
    username: name,
    email: principal.email ?? principal.organization_id,
    avatar: "",
    role: roles[principal.role],
  };
  function logOut() {
    void seoRequest<void>("/session", { method: "DELETE" }).then(
      () => router.replace("/seo/login"),
      (failure: unknown) => toast.error(errorMessage(failure)),
    );
  }

  return (
    <SessionContext.Provider value={{ principal, refresh }}>
      <AppShell
        user={user}
        navigation={{
          home: SEO_HOME,
          title: "ARTEX SEO OS",
          groups: seoGroups(principal.role, pathname, params),
          onLogout: logOut,
        }}
      >
        {children}
      </AppShell>
    </SessionContext.Provider>
  );
}
