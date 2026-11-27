import "server-only";

import { type NextRequest, NextResponse } from "next/server";

import { googleOAuthCallbackPath, googleOAuthResultKey } from "./google-oauth";
import { seoRoutes } from "./routes";
import { createHmac, timingSafeEqual } from "node:crypto";

const SESSION_COOKIE = "artex_seo_session";
const ACCEPTED_TOKEN = /^[\x21-\x7e]{32,512}$/;
const ID = "[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}";
const uuid = new RegExp(`^${ID}$`);
const googleOAuthState = /^[A-Za-z0-9_-]{43}$/;
const googleCallback = googleOAuthCallbackPath.slice("/seo/api".length);
const googleCookiePrefix = "__Host-artex_seo_google_";
const sitePath = new RegExp(`^/sites/${ID}$`);
const jobPath = new RegExp(`^/jobs/${ID}$`);
const providerPath = new RegExp(`^/providers/${ID}$`);
const providerTestPath = new RegExp(`^/providers/${ID}/test$`);
const providerReconcilePath = new RegExp(`^/providers/${ID}/reconcile$`);
const memberRolePath = new RegExp(`^/members/${ID}/role$`);
const memberTokenPath = new RegExp(`^/members/${ID}/token$`);
const snapshotPath = new RegExp(`^/research/snapshots/${ID}$`);
const contentProjectPath = new RegExp(`^/content/projects/${ID}$`);
const contentActionPath = new RegExp(`^/content/projects/${ID}/actions$`);
const contentPagePath = new RegExp(`^/content/inventory/${ID}$`);
const executionConnectionPath = new RegExp(`^/execution/connections/${ID}$`);
const executionPublicationPath = new RegExp(`^/execution/publications/${ID}$`);
const executionSitemapPath = new RegExp(`^/execution/sitemaps/${ID}$`);
const executionLists = new Set([
  "/execution/connections",
  "/execution/packages",
  "/execution/publications",
  "/execution/jobs",
  "/execution/pages",
  "/execution/media",
  "/execution/properties",
  "/execution/metrics",
  "/execution/snapshots",
  "/execution/index",
  "/execution/submissions",
  "/execution/sitemaps",
  "/execution/extractions",
  "/execution/usage",
  "/execution/feedback",
]);
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
const contentLists = new Set([
  "/content/projects",
  "/content/jobs",
  "/content/inventory",
  "/content/crawl",
  "/content/models",
  "/content/usage",
]);
const headers = { "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" };
const cookieOptions = { httpOnly: true, secure: true, sameSite: "lax", path: "/seo", maxAge: 8 * 60 * 60 } as const;
const googleCookieOptions = { ...cookieOptions, path: "/", maxAge: 600 };
const callbackHeaders = { ...headers, "Referrer-Policy": "no-referrer" };
const maxBodyBytes = 64 * 1024;
const maxExecutionJobBodyBytes = 6 * 1024 * 1024;

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
        contentLists.has(path) ||
        executionLists.has(path) ||
        executionPublicationPath.test(path) ||
        executionSitemapPath.test(path) ||
        path === "/me" ||
        path === "/dashboard" ||
        path === "/audit" ||
        path === "/providers/receipts" ||
        providerReconcilePath.test(path) ||
        sitePath.test(path) ||
        jobPath.test(path) ||
        snapshotPath.test(path) ||
        contentProjectPath.test(path) ||
        contentPagePath.test(path)
      );
    case "POST":
      return (
        lists.has(path) ||
        path === "/research/jobs" ||
        path === "/research/pages" ||
        path === "/content/projects" ||
        path === "/content/jobs" ||
        path === "/execution/connections" ||
        path === "/execution/publications" ||
        path === "/execution/jobs" ||
        path === "/execution/oauth/start" ||
        path === "/execution/oauth/exchange" ||
        contentActionPath.test(path) ||
        providerTestPath.test(path) ||
        memberTokenPath.test(path)
      );
    case "PUT":
      return (
        sitePath.test(path) ||
        providerPath.test(path) ||
        memberRolePath.test(path) ||
        path === "/content/crawl" ||
        path === "/content/models" ||
        executionConnectionPath.test(path)
      );
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

async function readBody(request: NextRequest, limit = maxBodyBytes): Promise<string> {
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
      if (size > limit) {
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

function googlePendingSignature(token: string, state: string, site: string, connection: string) {
  return createHmac("sha256", token).update(`artex:google-oauth-v1:${state}:${site}:${connection}`).digest("base64url");
}

async function googlePendingCookie(response: Response, body: string, token: string) {
  const { state, authorization_url } = (await response.json()) as { state?: unknown; authorization_url?: unknown };
  const { site_id, connection_id } = JSON.parse(body) as { site_id?: unknown; connection_id?: unknown };
  if (
    typeof state !== "string" ||
    !googleOAuthState.test(state) ||
    typeof authorization_url !== "string" ||
    typeof site_id !== "string" ||
    !uuid.test(site_id) ||
    typeof connection_id !== "string" ||
    !uuid.test(connection_id)
  ) {
    return;
  }
  const authorize = new URL(authorization_url);
  const redirect = authorize.searchParams.get("redirect_uri");
  if (
    !redirect ||
    authorize.origin !== "https://accounts.google.com" ||
    authorize.searchParams.get("state") !== state
  ) {
    return;
  }
  const callback = new URL(redirect);
  if (
    callback.protocol !== "https:" ||
    callback.pathname !== googleOAuthCallbackPath ||
    callback.search ||
    callback.hash
  )
    return;
  const signature = googlePendingSignature(token, state, site_id, connection_id);
  return { name: googleCookiePrefix + state, value: `${site_id}.${connection_id}.${signature}` };
}

function googleCallbackProblem(status: number, code: string) {
  return NextResponse.json({ error: { code } }, { status, headers: callbackHeaders });
}

async function googleBrowserCallback(request: NextRequest, origin: string) {
  if (request.method !== "GET") {
    return new NextResponse(null, { status: 405, headers: { ...callbackHeaders, Allow: "GET" } });
  }
  const params = request.nextUrl.searchParams;
  const states = params.getAll("state");
  const codes = params.getAll("code");
  const errors = params.getAll("error");
  const [state] = states;
  const [code] = codes;
  const [providerError] = errors;
  const hasGrant = codes.length === 1 && /^[\x21-\x7e]{1,4096}$/.test(code) && errors.length === 0;
  const hasError = errors.length === 1 && /^[a-zA-Z_]{1,64}$/.test(providerError) && codes.length === 0;
  if (states.length !== 1 || !googleOAuthState.test(state) || (!hasGrant && !hasError)) {
    return googleCallbackProblem(400, "invalid_google_callback");
  }

  const token = request.cookies.get(SESSION_COOKIE)?.value;
  if (!token || !ACCEPTED_TOKEN.test(token)) return googleCallbackProblem(401, "unauthorized");
  const cookie = googleCookiePrefix + state;
  const tuple = request.cookies.get(cookie)?.value.split(".") ?? [];
  const [site, connection, signature] = tuple;
  if (tuple.length !== 3 || !uuid.test(site) || !uuid.test(connection) || !googleOAuthState.test(signature)) {
    return googleCallbackProblem(403, "invalid_google_state");
  }
  const expected = googlePendingSignature(token, state, site, connection);
  if (!timingSafeEqual(Buffer.from(expected), Buffer.from(signature))) {
    return googleCallbackProblem(403, "invalid_google_state");
  }

  let result = providerError === "access_denied" ? "cancelled" : "failed";
  if (hasGrant) {
    try {
      const response = await upstream(
        origin,
        "/execution/oauth/exchange",
        token,
        "POST",
        JSON.stringify({ site_id: site, connection_id: connection, code, state }),
      );
      if (response.ok && ((await response.json()) as { connected?: unknown }).connected === true) result = "connected";
    } catch {
      // Send a safe result back even if the API aborts or returns malformed data.
    }
  }

  const destination = new URL(seoRoutes.sites.connections.path, request.nextUrl.origin);
  destination.search = new URLSearchParams({ site_id: site, [googleOAuthResultKey]: result }).toString();
  const response = NextResponse.redirect(destination, { status: 303, headers: callbackHeaders });
  response.cookies.set(cookie, "", { ...googleCookieOptions, maxAge: 0 });
  return response;
}

export async function seoProxy(request: NextRequest, origin: string, path: string) {
  if (path === googleCallback) return googleBrowserCallback(request, origin);
  const method = request.method;
  if (method !== "GET" && method !== "HEAD" && !sameOrigin(request)) {
    return problem(403, "forbidden_origin");
  }
  try {
    if (path === "/session") return await session(request, origin);
    if (!permits(path, method)) return problem(404, "not_found");

    const token = request.cookies.get(SESSION_COOKIE)?.value;
    if (!token || !ACCEPTED_TOKEN.test(token)) return problem(401, "unauthorized");
    const expectsBody =
      (method === "POST" || method === "PUT") && !memberTokenPath.test(path) && !providerTestPath.test(path);
    const bodyLimit = path === "/execution/jobs" ? maxExecutionJobBodyBytes : maxBodyBytes;
    const body = expectsBody ? await readBody(request, bodyLimit) : undefined;
    const response = await upstream(origin, path + request.nextUrl.search, token, method, body);
    const contentType = response.headers.get("Content-Type");
    const pending =
      path === "/execution/oauth/start" && method === "POST" && body && response.ok
        ? await googlePendingCookie(response.clone(), body, token)
        : undefined;
    const proxyResponse = new NextResponse(response.body, {
      status: response.status,
      headers: contentType ? { ...headers, "Content-Type": contentType } : headers,
    });
    if (pending) proxyResponse.cookies.set(pending.name, pending.value, googleCookieOptions);
    return proxyResponse;
  } catch (error) {
    if (error instanceof BodyError) return problem(error.status, error.code);
    if (error instanceof SyntaxError) return problem(400, "invalid_json");
    return problem(503, "api_unavailable");
  }
}
