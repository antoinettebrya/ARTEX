"""Loopback HTTP contracts for Phase 4 search providers and generic CMS."""

from collections import Counter
from datetime import datetime, time, timedelta, timezone
from html import escape
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from threading import Event, Lock, Thread
from time import monotonic
from urllib.parse import parse_qs, quote, unquote, urlsplit

HOST = "phase3.example.com"
SITE = "https://" + HOST
PAGE = SITE + "/existing"
CMS_PREFIX = "/api/artex-cms/v1"
CMS_TOKEN = "phase4-generic-token"
BING_KEY = "phase4-bing-key"
INDEXNOW_KEY = "phase4-indexnow-key"
INDEXNOW_RETRY_PAGE = SITE + "/indexnow-retry"
INDEXNOW_RETRY_AFTER = 8
GA_PROPERTY = "properties/54321"
CENTER_HOSTS = ("center-one.example.test", "center-two.example.test")
CENTER_PROPERTIES = ("properties/65431", "properties/65432")
CENTER_KEYS = ("0123456789abcdef", "fedcba9876543210")
CENTER_LOCATIONS = ("/", "/updates/")
REPORT_DAY = (datetime.now(timezone.utc).date() - timedelta(days=3)).isoformat()
GOOGLE_CLIENTS = {
    "gsc": ("phase4-gsc-client", "phase4-gsc-secret"),
    "ga4": ("phase4-ga4-client", "phase4-ga4-secret"),
    "shared": ("integration-shared-client", "integration-shared-secret"),
}
GOOGLE_SCOPES = {
    "gsc": "https://www.googleapis.com/auth/webmasters.readonly",
    "ga4": "https://www.googleapis.com/auth/analytics.readonly",
    "shared": "https://www.googleapis.com/auth/webmasters.readonly https://www.googleapis.com/auth/analytics.readonly",
}

SITEMAP_INDEX = (
    '<?xml version="1.0" encoding="UTF-8"?>'
    '<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">'
    f"<sitemap><loc>{SITE}/sitemap-pages.xml</loc></sitemap></sitemapindex>"
)
SITEMAP_PAGES = (
    '<?xml version="1.0" encoding="UTF-8"?>'
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" '
    'xmlns:xhtml="http://www.w3.org/1999/xhtml">'
    f"<url><loc>{PAGE}</loc><lastmod>{REPORT_DAY}</lastmod>"
    f'<xhtml:link rel="alternate" hreflang="en-US" href="{PAGE}"/>'
    "</url></urlset>"
)


def utc_now():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def bing_date(day):
    stamp = datetime.combine(datetime.fromisoformat(day), time(12), timezone.utc)
    return f"/Date({int(stamp.timestamp()) * 1000}+0000)/"


def render_page(page):
    title = escape(page.get("seo_title") or page["title"])
    canonical = escape(page.get("canonical") or page["url"])
    desc = escape(page.get("description", ""))
    return (
        '<!doctype html><html lang="en"><head>'
        f'<title>{title}</title><meta name="description" content="{desc}">'
        f'<link rel="canonical" href="{canonical}"></head>'
        f'<body><main><h1>{escape(page["title"])}</h1>{page["body"]}</main></body></html>'
    )


class ExecutionHandler(BaseHTTPRequestHandler):
    def handle_fixture(self):
        host = self.headers.get("X-SEOOS-Fixture-Host", "")
        route = urlsplit(self.path)
        path, query = unquote(route.path), parse_qs(route.query, keep_blank_values=True)
        record = host, self.command, path
        with self.server.lock:
            self.server.calls[record] += 1
            self.server.requests.append(record)
        try:
            if host == HOST and path.startswith(CMS_PREFIX + "/"):
                return self.cms(path.removeprefix(CMS_PREFIX), query)
            if host == HOST:
                return self.site(path)
            if host in CENTER_HOSTS:
                index = CENTER_HOSTS.index(host)
                if self.command == "GET" and path == CENTER_LOCATIONS[index] + CENTER_KEYS[index] + ".txt":
                    return self.respond(200, "text/plain; charset=utf-8", CENTER_KEYS[index])
                return self.respond(404, "text/plain", "not published in fixture")
            if host == "oauth2.googleapis.com":
                return self.oauth(path)
            if host == "www.googleapis.com":
                return self.gsc(path)
            if host == "searchconsole.googleapis.com":
                return self.inspect_google(path)
            if host in ("analyticsadmin.googleapis.com", "analyticsdata.googleapis.com"):
                return self.ga4(host, path, query)
            if host == "ssl.bing.com":
                return self.bing(path, query)
            if host == "api.indexnow.org":
                return self.indexnow(path)
        except (IndexError, KeyError, TypeError, ValueError):
            return self.respond_json(400, {"error": "invalid fixture request"})
        return self.respond_json(404, {"error": "outside fixture scope"})

    do_GET = handle_fixture
    do_HEAD = handle_fixture
    do_POST = handle_fixture
    do_PATCH = handle_fixture

    def cms(self, path, query):
        if self.headers.get("Authorization") != "Bearer " + CMS_TOKEN:
            return self.respond_json(401, {"error": "invalid fixture CMS bearer"})
        if self.command == "GET" and path == "/site":
            return self.respond_json(200, {
                "name": "Phase 4 Generic CMS Fixture", "url": SITE,
                "capabilities": {
                    "read_pages": True, "write_pages": True, "publish": True, "schedule": True,
                    "read_media": True, "write_media": True, "seo_fields": ["seo_title", "description", "canonical"],
                },
            })
        if self.command == "GET" and path == "/sitemap":
            return self.respond(200, "application/xml", SITEMAP_PAGES)

        parts = path.strip("/").split("/")
        if parts[0] == "pages":
            if self.command == "GET":
                return self.cms_read_pages(parts, query)
            if self.command == "POST" and len(parts) == 1:
                return self.cms_write_page("", creating=True, publishing=False)
            if self.command == "PATCH" and len(parts) == 2:
                return self.cms_write_page(parts[1], creating=False, publishing=False)
            if self.command == "POST" and len(parts) == 3 and parts[2] == "publish":
                return self.cms_write_page(parts[1], creating=False, publishing=True)
        if path == "/media":
            if self.command == "GET":
                limit = self.limit(query, 100)
                with self.server.lock:
                    items = list(self.server.media.values())[:limit]
                return self.respond_json(200, {"items": items})
            if self.command == "POST":
                body = self.read_payload(8 << 20)
                filename = self.headers.get_filename()
                media_type = self.headers.get_content_type()
                if not filename or "/" in filename or not media_type.startswith("image/"):
                    return self.respond_json(400, {"error": "invalid fixture media"})
                with self.server.lock:
                    item_id = f"media-{len(self.server.media) + 1}"
                    path = "/fixture-media/" + item_id + "/" + quote(filename, safe="")
                    media = {"id": item_id, "url": SITE + path, "title": filename, "media_type": media_type}
                    self.server.media[item_id] = media
                    self.server.media_bytes[unquote(path)] = (media_type, body)
                return self.respond_json(201, media)
        return self.respond_json(404, {"error": "unknown CMS operation"})

    def cms_read_pages(self, parts, query):
        resource = query["resource"][0]
        if resource not in ("post", "page"):
            return self.respond_json(400, {"error": "invalid CMS resource"})
        if len(parts) == 2:
            with self.server.lock:
                page = self.server.pages.get(parts[1], {})
            if page.get("resource") == resource:
                return self.respond_json(200, page)
            return self.respond_json(404, {"error": "unknown CMS page"})
        if len(parts) != 1:
            return self.respond_json(404, {"error": "unknown CMS route"})
        slug = query.get("slug", [""])[0]
        limit = self.limit(query, 250)
        with self.server.lock:
            items = [page for page in self.server.pages.values()
                     if page["resource"] == resource and (not slug or page["slug"] == slug)]
        return self.respond_json(200, {"items": items[:limit]})

    def cms_write_page(self, page_id, *, creating, publishing):
        request = self.read_json()
        resource = request["resource"]
        status = request.get("status", "publish" if publishing else "draft")
        if resource not in ("post", "page") or status not in ("draft", "publish", "future", "private"):
            return self.respond_json(400, {"error": "invalid CMS write"})
        if (creating and status != "draft") or (publishing and status not in ("publish", "future")):
            return self.respond_json(400, {"error": "invalid CMS operation state"})
        with self.server.lock:
            before = self.server.pages.get(page_id)
            if not creating and (before is None or before["resource"] != resource):
                code, page = 404, {"error": "unknown CMS page"}
            else:
                page = dict(before or {}, **request)
                slug = page["slug"]
                if not slug or "/" in slug:
                    raise ValueError("invalid fixture slug")
                if creating:
                    page_id = f"page-{len(self.server.pages) + 1}"
                page.update(id=page_id, status=status, url=SITE + "/" + quote(slug, safe=""), modified_at=utc_now())
                if status != "future":
                    page.pop("scheduled_at", None)
                self.server.pages[page_id] = page
                code = 201 if creating else 200
        return self.respond_json(code, page)

    def site(self, path):
        if self.command not in ("GET", "HEAD") or self.headers.get("Authorization"):
            return self.respond_json(403, {"error": "unexpected public request"})
        if path == "/robots.txt":
            return self.respond(200, "text/plain", f"User-agent: *\nAllow: /\nSitemap: {SITE}/sitemap.xml\n")
        if path == "/sitemap.xml":
            return self.respond(200, "application/xml", SITEMAP_INDEX)
        if path == "/sitemap-pages.xml":
            return self.respond(200, "application/xml", SITEMAP_PAGES)
        if path == "/" + INDEXNOW_KEY + ".txt":
            return self.respond(200, "text/plain", INDEXNOW_KEY + "\n")
        with self.server.lock:
            media = self.server.media_bytes.get(path)
            page = next((item for item in self.server.pages.values()
                         if unquote(urlsplit(item["url"]).path) == path and item["status"] == "publish"), None)
        if media is not None:
            return self.respond(200, *media)
        if page is not None:
            return self.respond(200, "text/html; charset=utf-8", render_page(page))
        return self.respond(404, "text/plain", "not published in fixture")

    def oauth(self, path):
        if self.command != "POST" or path != "/token":
            return self.respond_json(404, {"error": "unsupported OAuth fixture route"})
        params = {k: v[0] for k, v in parse_qs(self.read_payload().decode()).items()}
        kind = next((name for name, credentials in GOOGLE_CLIENTS.items()
                     if credentials == (params.get("client_id"), params.get("client_secret"))), "")
        if not kind:
            return self.respond_json(401, {"error": "invalid_client"})
        grant = params["grant_type"]
        self.count("oauth_" + kind + "_" + grant)
        if grant == "authorization_code":
            if (params.get("code") != f"phase4-{kind}-code" or
                    not 43 <= len(params.get("code_verifier", "")) <= 128 or
                    not params.get("redirect_uri", "").startswith("https://")):
                return self.respond_json(400, {"error": "invalid_grant"})
            return self.respond_json(200, {
                "access_token": f"phase4-{kind}-first", "refresh_token": f"phase4-{kind}-refresh",
                "expires_in": 30, "token_type": "Bearer", "scope": GOOGLE_SCOPES[kind],
            })
        if grant == "refresh_token" and params.get("refresh_token") == f"phase4-{kind}-refresh":
            return self.respond_json(200, {
                "access_token": f"phase4-{kind}-access", "expires_in": 3600,
                "token_type": "Bearer", "scope": GOOGLE_SCOPES[kind],
            })
        return self.respond_json(400, {"error": "invalid_grant"})

    def gsc(self, path):
        shared = self.authorized_google("gsc")
        root = "/webmasters/v3/sites"
        if shared:
            return self.center_gsc(root, path)
        properties = (SITE + "/", "sc-domain:" + HOST)
        if self.command == "GET" and path == root:
            return self.respond_json(200, {"siteEntry": [
                {"siteUrl": value, "permissionLevel": "siteOwner"} for value in properties
            ]})
        if any(path == f"{root}/{prop}/sitemaps" for prop in properties) and self.command == "GET":
            return self.respond_json(200, {"sitemap": [{
                "path": SITE + "/sitemap.xml", "isPending": False, "warnings": "0",
                "errors": "0", "lastDownloaded": REPORT_DAY + "T12:00:00Z",
            }]})
        if any(path == f"{root}/{prop}/searchAnalytics/query" for prop in properties) and self.command == "POST":
            data = self.read_json()
            dimensions = ("date", "page", "query", "country", "device")
            scope = data["dimensionFilterGroups"][0]["filters"][0]
            selected = tuple(data.get("dimensions", ()))
            if (selected not in (dimensions, ("date",)) or data.get("dataState") != "final" or
                    scope["dimension"] != "page" or HOST.replace(".", r"\.") not in scope["expression"]):
                raise ValueError("incorrect GSC site filter or dimensions")
            start, limit, end = data["startRow"], data["rowLimit"], data["endDate"]
            if not 0 <= start <= 10000 or not 0 < limit <= 25000:
                raise ValueError("invalid Search Analytics window")
            with self.server.lock:
                key = (end, start, selected == ("date",))
                self.server.gsc_pages[key] += 1
                fail_page = self.server.gsc_failure == key and self.server.gsc_pages[key] == 1
            if fail_page:
                return self.respond_json(503, {"error": {"status": "UNAVAILABLE"}})
            if selected == ("date",):
                rows = [{"keys": [end], "clicks": 12012, "impressions": 84084, "ctr": 12 / 84, "position": 6.5}]
                return self.respond_json(200, {"rows": rows, "responseAggregationType": "byPage"})
            rows = [
                {"keys": [end, PAGE, f"phase4 organic query {n:04d}", "usa", "MOBILE"],
                 "clicks": 12, "impressions": 84, "ctr": 12 / 84, "position": 6.5}
                for n in range(start, min(start + limit, 1001))
            ]
            return self.respond_json(200, {"rows": rows, "responseAggregationType": "byPage"})
        return self.respond_json(404, {"error": "unsupported GSC fixture route"})

    def center_gsc(self, root, path):
        domain_properties = ["sc-domain:" + host for host in CENTER_HOSTS]
        if self.command == "GET" and path == root:
            return self.respond_json(200, {"siteEntry": [
                {"siteUrl": item, "permissionLevel": "siteOwner"}
                for item in (*domain_properties, "https://" + CENTER_HOSTS[0] + "/")
            ]})
        if self.command == "GET" and any(path == f"{root}/{prop}/sitemaps" for prop in domain_properties):
            return self.respond_json(200, {"sitemap": []})
        if self.command == "POST":
            for prop, host in zip(domain_properties, CENTER_HOSTS):
                if path != f"{root}/{prop}/searchAnalytics/query":
                    continue
                data = self.read_json()
                if data["type"] not in ("web", "discover") or data["dataState"] not in ("final", "all"):
                    raise ValueError("invalid GSC series")
                dimensions = data["dimensions"]
                if data["type"] == "discover" and "query" in dimensions:
                    raise ValueError("Discover has no query dimension")
                restriction = data["dimensionFilterGroups"][0]["filters"][0]
                if restriction["dimension"] != "page" or host.replace(".", r"\.") not in restriction["expression"]:
                    raise ValueError("incorrect site scope")
                values = {"date": data["startDate"], "page": "https://" + host + "/existing",
                          "query": "integration fixture", "country": "usa", "device": "MOBILE"}
                metadata = {"first_incomplete_date": data["startDate"]} if data["dataState"] == "all" else {}
                return self.respond_json(200, {"rows": [{
                    "keys": [values[name] for name in dimensions], "clicks": 2,
                    "impressions": 9, "ctr": 2 / 9, "position": 5,
                }], "responseAggregationType": "byPage", "metadata": metadata})
        return self.respond_json(404, {"error": "unsupported center GSC fixture route"})

    def inspect_google(self, path):
        shared = self.authorized_google("gsc")
        if self.command != "POST" or path != "/v1/urlInspection/index:inspect":
            return self.respond_json(404, {"error": "unsupported inspection route"})
        data = self.read_json()
        scopes = [(PAGE, SITE + "/"), (PAGE, "sc-domain:" + HOST)]
        if shared:
            scopes = [("https://" + host + "/existing", "sc-domain:" + host) for host in CENTER_HOSTS]
        if (data.get("inspectionUrl"), data.get("siteUrl")) not in scopes:
            raise ValueError("out-of-scope inspection")
        return self.respond_json(200, {"inspectionResult": {"indexStatusResult": {
            "verdict": "PASS", "coverageState": "Submitted and indexed", "lastCrawlTime": REPORT_DAY + "T12:00:00Z",
        }}})

    def ga4(self, host, path, query):
        if self.authorized_google("ga4"):
            return self.center_ga4(host, path, query)
        if host == "analyticsadmin.googleapis.com" and path == "/v1beta/accountSummaries" and self.command == "GET":
            return self.respond_json(200, {"accountSummaries": [{"propertySummaries": [{
                "property": GA_PROPERTY, "displayName": "Phase 4 Analytics Fixture",
            }]}]})
        if host == "analyticsdata.googleapis.com" and path == "/v1beta/" + GA_PROPERTY + ":runReport" and self.command == "POST":
            data = self.read_json()
            filters = json.dumps(data["dimensionFilter"])
            if "Organic Search" not in filters or HOST not in filters:
                raise ValueError("GA4 report is not scoped")
            end = data["dateRanges"][0]["endDate"].replace("-", "")
            dims = {"date": end, "landingPage": "/existing", "country": "United States",
                    "deviceCategory": "mobile", "language": "English", "hostName": HOST}
            metrics = {"sessions": "17", "activeUsers": "14", "engagedSessions": "11",
                       "engagementRate": "0.647", "userEngagementDuration": "186", "keyEvents": "4",
                       "sessionKeyEventRate": "0.176"}
            return self.respond_json(200, {
                "dimensionHeaders": data["dimensions"], "metricHeaders": data["metrics"], "rowCount": 1,
                "rows": [{"dimensionValues": [{"value": dims[item["name"]]} for item in data["dimensions"]],
                          "metricValues": [{"value": metrics[item["name"]]} for item in data["metrics"]]}],
            })
        return self.respond_json(404, {"error": "unsupported GA4 fixture route"})

    def center_ga4(self, host, path, query):
        if host == "analyticsadmin.googleapis.com" and self.command == "GET":
            if path == "/v1beta/accountSummaries":
                index = 1 if query.get("pageToken") == ["integration-ga4-next"] else 0
                return self.respond_json(200, {"accountSummaries": [{
                    "account": "accounts/123", "displayName": "Fixture Analytics",
                    "propertySummaries": [{
                        "property": CENTER_PROPERTIES[index], "displayName": f"Fixture GA4 {index + 1}",
                    }],
                }], **({} if index else {"nextPageToken": "integration-ga4-next"})})
            for index, prop in enumerate(CENTER_PROPERTIES):
                if path == f"/v1beta/{prop}":
                    return self.respond_json(200, {"name": prop, "displayName": f"Fixture GA4 {index + 1}",
                                                  "account": "accounts/123", "timeZone": "America/New_York",
                                                  "currencyCode": "USD"})
                if path == f"/v1beta/{prop}/dataStreams":
                    return self.respond_json(200, {"dataStreams": [{
                        "webStreamData": {"defaultUri": "https://" + CENTER_HOSTS[index]},
                    }]})
        if host == "analyticsdata.googleapis.com" and self.command == "POST":
            for index, prop in enumerate(CENTER_PROPERTIES):
                if path != f"/v1beta/{prop}:runReport":
                    continue
                data = self.read_json()
                filters = json.dumps(data["dimensionFilter"])
                if not data.get("returnPropertyQuota") or CENTER_HOSTS[index] not in filters or "Organic Search" not in filters:
                    raise ValueError("GA4 data needs hostname, channel, and quota")
                return self.respond_json(200, {
                    "dimensionHeaders": data["dimensions"], "metricHeaders": data["metrics"], "rowCount": 0,
                    "metadata": {"timeZone": "America/New_York",
                                 "subjectToThresholding": True, "dataLossFromOtherRow": True,
                                 "samplingMetadatas": [{"samplesReadCount": "75", "samplingSpaceSize": "100"}],
                                 "emptyReason": "No fixture data for the selected date."},
                    "propertyQuota": {"tokensPerDay": {"consumed": 1, "remaining": 199999}},
                })
        return self.respond_json(404, {"error": "unsupported center GA4 fixture route"})

    def bing(self, path, query):
        if query.get("apikey") != [BING_KEY] or self.headers.get("Authorization"):
            return self.respond_json(401, {"ErrorCode": 3})
        if not path.startswith("/webmaster/api.svc/json/"):
            return self.respond_json(404, {"error": "unsupported Bing fixture route"})
        if path.endswith("/SubmitUrlBatch") and self.command == "POST":
            data = self.read_json()
            if data.get("siteUrl", "").rstrip("/") != SITE or data.get("urlList") != [PAGE]:
                raise ValueError("out-of-scope Bing submission")
            return self.respond_json(200, {"d": None})
        if self.command != "GET":
            return self.respond_json(405, {"error": "unexpected Bing method"})
        name = path.rsplit("/", 1)[-1]
        if name != "GetUserSites" and query.get("siteUrl", [""])[0].rstrip("/") != SITE:
            raise ValueError("out-of-scope Bing property")
        day = bing_date(REPORT_DAY)
        stats = {"Date": day, "Clicks": 9, "Impressions": 91}
        data = {
            "GetUserSites": [{"Url": SITE + "/", "IsVerified": True}] + [
                {"Url": "https://" + host + "/", "IsVerified": True} for host in CENTER_HOSTS
            ],
            "GetFeeds": [{"Url": SITE + "/sitemap.xml", "Status": "Success", "LastCrawled": day}],
            "GetRankAndTrafficStats": [stats],
            "GetQueryStats": [dict(stats, Query="phase4 bing query", AvgClickPosition=5, AvgImpressionPosition=7)],
            "GetPageStats": [dict(stats, Query=PAGE, AvgClickPosition=5, AvgImpressionPosition=7)],
            "GetUrlInfo": {"DiscoveryDate": day, "LastCrawledDate": day},
            "GetUrlSubmissionQuota": {"DailyQuota": 10, "MonthlyQuota": 15},
        }
        if name == "GetUrlInfo" and query.get("url") != [json.dumps(PAGE)]:
            raise ValueError("out-of-scope Bing URL")
        if name not in data:
            return self.respond_json(404, {"error": "unknown Bing operation"})
        return self.respond_json(200, {"d": data[name]})

    def indexnow(self, path):
        if self.command != "POST" or path != "/indexnow":
            return self.respond_json(404, {"error": "unsupported IndexNow route"})
        data = self.read_json()
        targets = data.get("urlList")
        if data.get("host") in CENTER_HOSTS:
            index = CENTER_HOSTS.index(data["host"])
            origin = "https://" + CENTER_HOSTS[index]
            if (data.get("key") != CENTER_KEYS[index] or not targets or
                    any(not value.startswith(origin + CENTER_LOCATIONS[index]) for value in targets) or
                    data.get("keyLocation") != (origin + CENTER_LOCATIONS[index] + CENTER_KEYS[index] + ".txt" if index else None)):
                return self.respond_json(422, {"error": "out-of-scope center IndexNow request"})
            return self.respond(200 if index else 202, "text/plain", "")
        if (data.get("key") != INDEXNOW_KEY or targets not in ([PAGE], [INDEXNOW_RETRY_PAGE]) or
                data.get("host") != HOST or data.get("keyLocation") != SITE + "/" + INDEXNOW_KEY + ".txt"):
            return self.respond_json(422, {"error": "out-of-scope IndexNow request"})
        if targets == [PAGE]:
            return self.respond(202, "text/plain", "")
        with self.server.lock:
            self.server.indexnow_retry_times.append(monotonic())
            attempt = len(self.server.indexnow_retry_times)
        if attempt == 1:
            return self.respond(429, "text/plain", "", retry_after=INDEXNOW_RETRY_AFTER)
        if attempt == 2 and self.server.indexnow_retry_gate.wait(timeout=8):
            return self.respond(200, "text/plain", "")
        return self.respond(503, "text/plain", "unexpected retry")

    def authorized_google(self, kind):
        auth = self.headers.get("Authorization")
        if auth == "Bearer phase4-shared-access":
            return True
        if auth != f"Bearer phase4-{kind}-access":
            raise ValueError("Google reader requires the refreshed bearer")
        return False

    def limit(self, query, maximum):
        value = int(query.get("limit", ["25"])[0])
        if not 1 <= value <= maximum:
            raise ValueError("invalid fixture page limit")
        return value

    def read_payload(self, limit=512 << 10):
        size = int(self.headers.get("Content-Length", "0"))
        if not 0 < size <= limit:
            raise ValueError("invalid fixture payload size")
        return self.rfile.read(size)

    def read_json(self):
        data = json.loads(self.read_payload())
        if not isinstance(data, dict):
            raise ValueError("fixture accepts a JSON object")
        return data

    def count(self, name):
        with self.server.lock:
            self.server.counters[name] += 1

    def respond_json(self, status, value):
        return self.respond(status, "application/json", json.dumps(value))

    def respond(self, status, media_type, value, retry_after=None):
        body = value.encode() if isinstance(value, str) else value
        self.send_response(status)
        self.send_header("Content-Type", media_type)
        self.send_header("Content-Length", str(len(body)))
        if retry_after is not None:
            self.send_header("Retry-After", str(retry_after))
        self.end_headers()
        if body and self.command != "HEAD":
            self.wfile.write(body)

    def log_message(self, *_):
        pass


class LocalExecution:
    def __enter__(self):
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), ExecutionHandler)
        self.server.lock = Lock()
        self.server.calls = Counter()
        self.server.counters = Counter()
        self.server.gsc_pages = Counter()
        self.server.gsc_failure = None
        self.server.requests = []
        self.server.indexnow_retry_times = []
        self.server.indexnow_retry_gate = Event()
        self.server.pages = {
            "baseline-1": {"id": "baseline-1", "resource": "post", "url": SITE + "/old-notes",
                           "slug": "old-notes", "status": "publish", "title": "Maintenance notes",
                           "body": "<p>Verify the filter is seated before using the vacuum.</p>"},
        }
        self.server.media = {}
        self.server.media_bytes = {}
        self.origin = f"http://127.0.0.1:{self.server.server_port}"
        self.thread = Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        return self

    @property
    def calls(self):
        with self.server.lock:
            return self.server.calls.copy()

    @property
    def counters(self):
        with self.server.lock:
            return self.server.counters.copy()

    @property
    def requests(self):
        with self.server.lock:
            return tuple(self.server.requests)

    @property
    def indexnow_retry_times(self):
        with self.server.lock:
            return tuple(self.server.indexnow_retry_times)

    def fail_gsc_page_once(self, day, offset):
        with self.server.lock:
            self.server.gsc_failure = (day, offset, False)

    @property
    def gsc_pages(self):
        with self.server.lock:
            return self.server.gsc_pages.copy()

    def allow_indexnow_retry(self):
        self.server.indexnow_retry_gate.set()

    def __exit__(self, *_):
        self.allow_indexnow_retry()
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()
