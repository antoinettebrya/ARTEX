"use client";

import { type FormEvent, useState } from "react";

import { useRouter } from "next/navigation";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { errorMessage, seoRequest } from "@/lib/seo/client";

export function SeoLogin() {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const token = String(new FormData(event.currentTarget).get("token") ?? "").trim();
    setPending(true);
    setError("");
    try {
      await seoRequest<void>("/session", { method: "POST", body: { token } });
      router.replace("/seo/sites");
    } catch (failure) {
      setError(errorMessage(failure));
      setPending(false);
    }
  }

  return (
    <form onSubmit={signIn} className="flex flex-col gap-4">
      <div className="space-y-1.5">
        <Label htmlFor="seo-access-token">访问凭据</Label>
        <Input
          id="seo-access-token"
          name="token"
          type="password"
          required
          minLength={32}
          maxLength={512}
          autoComplete="current-password"
        />
      </div>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending && <Spinner />} {pending ? "登录中…" : "登录"}
      </Button>
    </form>
  );
}
