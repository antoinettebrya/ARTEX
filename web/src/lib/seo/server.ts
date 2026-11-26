import "server-only";

import { type NextRequest, NextResponse } from "next/server";

const SESSION_COOKIE = "artex_seo_session";
const ACCEPTED_TOKEN = /^[\x21-\x7e]{32,512}$/;
const ID = "[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}";
const sitePath = new RegExp(`^/sites/${ID}$`);
const jobPath = new RegExp(`^/jobs/${ID}$`);
const providerPath = new RegExp(`^/providers/${ID}$`);
const memberRolePath = new RegExp(`^/members/${ID}/role$`);
const memberTokenPath = new RegExp(`^/members/${ID}/token$`);
const snapshotPath = new RegExp(`^/research/snapshots/${ID}$`);
const researchLists = new Set([
  "/research/seeds",
  "/research/suggestions",
  "/research/keywords",
  "/research/clusters",
  "/research/serps",
  "/research/competitors",
  "/research/intents",
  "/research/mappings",
  "/research/opportunities",
  "/research/jobs",
  "/research/pages",
  "/research/usage",
]);
const lists = new Set(["/sites", "/jobs", "/providers", "/members"]);
const headers = { "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" };
const cookieOptions = { httpOnly: true, secure: true, sameSite: "lax", path: "/seo", maxAge: 8 * 60 * 60 } as const;
const maxBodyBytes = 64 * 1024;

function problem(status: number, code: string) {
  return NextResponse.json({ error: { code } }, { status, headers });
}

function permits(path: string, method: string): boolean {
  switch (method) {
    case "HEAD":
    case "GET":
      return (
        lists.has(path) ||
        researchLists.has(path) ||
        path === "/me" ||
        path === "/audit" ||
        sitePath.test(path) ||
        jobPath.test(path) ||
        snapshotPath.test(path)
      );
    case "POST":
      return lists.has(path) || path === "/research/jobs" || path === "/research/pages" || memberTokenPath.test(path);
    case "PUT":
      return sitePath.test(path) || providerPath.test(path) || memberRolePath.test(path);
    case "DELETE":
      return jobPath.test(path) || memberTokenPath.test(path);
    default:
      return false;
  }
}

function sameOrigin(request: NextRequest): boolean {
  // Browsers send Fetch Metadata based on the real URL, before any reverse proxy
  // normalizes it. A different origin cannot send the custom header without CORS.
  if (request.headers.get("X-SEOOS-Request") !== "1") return false;
  const site = request.headers.get("Sec-Fetch-Site");
  if (site !== null) return site === "same-origin";
  return request.headers.get("Origin") === request.nextUrl.origin;
}

class BodyError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(code);
  }
}

async function readBody(request: NextRequest): Promise<string> {
  if (request.headers.get("Content-Type")?.split(";")[0]?.trim().toLowerCase() !== "application/json") {
    throw new BodyError(415, "unsupported_media_type");
  }
  const reader = request.body?.getReader();
  if (!reader) throw new BodyError(400, "invalid_json");
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let size = 0;
  let body = "";
  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) return body + decoder.decode();
      size += chunk.value.byteLength;
      if (size > maxBodyBytes) {
        await reader.cancel();
        throw new BodyError(413, "body_too_large");
      }
      body += decoder.decode(chunk.value, { stream: true });
    }
  } catch (error) {
    if (error instanceof BodyError) throw error;
    throw new BodyError(400, "invalid_json");
  } finally {
    reader.releaseLock();
  }
}

export function seoApiOrigin(value: string): string {
  const url = new URL(value);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  ) {
    throw new Error("Invalid SEOOS_API_ORIGIN");
  }
  return url.origin;
}

async function upstream(origin: string, path: string, token: string, method: string, body?: string) {
  return fetch(`${seoApiOrigin(origin)}/api/v1${path}`, {
    method,
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body,
    cache: "no-store",
    redirect: "error",
    signal: AbortSignal.timeout(12000),
  });
}

async function session(request: NextRequest, origin: string) {
  if (request.method === "DELETE") {
    const response = new NextResponse(null, { status: 204, headers });
    response.cookies.set(SESSION_COOKIE, "", { ...cookieOptions, maxAge: 0 });
    return response;
  }
  if (request.method !== "POST") return problem(405, "method_not_allowed");

  const input: unknown = JSON.parse(await readBody(request));
  const token = input && typeof input === "object" && "token" in input ? input.token : undefined;
  if (typeof token !== "string" || !ACCEPTED_TOKEN.test(token)) return problem(401, "unauthorized");
  const verified = await upstream(origin, "/me", token, "GET");
  if (verified.status === 401) return problem(401, "unauthorized");
  if (!verified.ok) return problem(503, "authentication_unavailable");

  const response = new NextResponse(null, { status: 204, headers });
  response.cookies.set(SESSION_COOKIE, token, cookieOptions);
  return response;
}

export async function seoProxy(request: NextRequest, origin: string, path: string) {
  const method = request.method;
  if (method !== "GET" && method !== "HEAD" && !sameOrigin(request)) {
    return problem(403, "forbidden_origin");
  }
  try {
    if (path === "/session") return await session(request, origin);
    if (!permits(path, method)) return problem(404, "not_found");

    const token = request.cookies.get(SESSION_COOKIE)?.value;
    if (!token || !ACCEPTED_TOKEN.test(token)) return problem(401, "unauthorized");
    const expectsBody = (method === "POST" || method === "PUT") && !memberTokenPath.test(path);
    const body = expectsBody ? await readBody(request) : undefined;
    const response = await upstream(origin, path + request.nextUrl.search, token, method, body);
    const contentType = response.headers.get("Content-Type");
    return new NextResponse(response.body, {
      status: response.status,
      headers: contentType ? { ...headers, "Content-Type": contentType } : headers,
    });
  } catch (error) {
    if (error instanceof BodyError) return problem(error.status, error.code);
    if (error instanceof SyntaxError) return problem(400, "invalid_json");
    return problem(503, "api_unavailable");
  }
}
