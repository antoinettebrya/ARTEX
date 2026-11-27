"""Fixed, loopback-only search and provider-check responses for the browser suite."""

from collections import Counter
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import base64
import json
from threading import Lock, Thread

from content_fixture import SITE as CONTENT_SITE, SOURCE as CONTENT_SOURCE, TERM as CONTENT_TERM


LOGIN = "phase-two-fixture@example.test"
PASSWORD = "test-fixture-password-never-used-live"
SEED = "robot vacuum"
RELATED = "best robot vacuum"
OWNED = "https://research.example.test/reviews/robot-vacuums"
COMPETITOR = "https://competitor.example.com/reviews/robot-vacuums"
INSTANT_TASK = "01161532-1535-0216-0000-9b351e1ad02c"


def keyword_record(term, market):
    volume = 2400 if term == SEED else 720
    return dict(market, keyword=term, keyword_info={"search_volume": volume, "cpc": 1.25, "competition": 0.64},
                keyword_properties={"keyword_difficulty": 37}, search_intent_info={"main_intent": "informational" if term == CONTENT_TERM else "commercial"})


def response_for(path, params):
    method = path.split("/")[-2]
    engine = path.split("/")[-3]
    if path == "/v3/on_page/instant_pages" and params["url"] == COMPETITOR and params.get("validate_micromarkup"):
        return 0.00015, [{"items": [{"resource_type": "html", "status_code": 200, "url": COMPETITOR,
            "meta": {"title": "Robot vacuum reviews", "description": "Product comparisons and buying advice",
                     "canonical": "/reviews/robot-vacuums", "meta_keywords": "battery life, pet hair",
                     "htags": {"h1": ["Robot vacuum reviews"], "h2": ["Comparison", "Dustbin capacity"]}},
            "checks": {"has_micromarkup": True}, "last_modified": {"header": "2025-03-01 10:30:00 +00:00"},
        }]}]
    if path == "/v3/on_page/content_parsing/live" and params["url"] == COMPETITOR:
        return 0.00015, content_results()
    if path == "/v3/on_page/microdata" and params["url"] == COMPETITOR and params.get("id") == INSTANT_TASK:
        return 0, microdata_results()

    term = params.get("keyword") or params["keywords"][0]
    market = dict(location_code=params["location_code"], language_code=params["language_code"])
    if method in {"related_keywords", "keyword_suggestions", "keyword_overview", "bulk_keyword_difficulty"}:
        market["se_type"] = engine
        if method == "related_keywords":
            items = [{"keyword_data": keyword_record(t, market)} for t in (RELATED, "robot vacuum reviews")]
        elif method == "keyword_suggestions":
            items = [keyword_record(t, market) for t in (RELATED, "robot vacuum for pet hair")]
        elif method == "bulk_keyword_difficulty":
            items = [dict(market, keyword=term, keyword_difficulty=37)]
        else:
            items = [keyword_record(term, market)]
        return 0.0102, [dict(market, items=items)]
    if method in {"search_volume", "keywords_for_keywords"}:
        if "device" in params:
            market["device"] = params["device"]
        if method == "search_volume":
            return 0.09, [dict(market, keyword=term, search_volume=2400, cpc=1.24, competition_index=64)]
        return 0.09, [dict(market, keyword=RELATED, search_volume=720, cpc=1.1, competition=0.6)]
    if path in {"/v3/serp/google/organic/live/advanced", "/v3/serp/bing/organic/live/advanced"}:
        urls = [COMPETITOR, OWNED] + [f"https://publication-{i}.example.com/best-robot-vacuum" for i in range(3, 11)]
        title, description = "Best robot vacuum comparison", "Robot vacuum reviews and comparisons"
        if term == CONTENT_TERM:
            urls[:2] = [CONTENT_SOURCE, CONTENT_SITE + "/existing"]
            title, description = "How to do robot vacuum maintenance", "Check the housing and clean the filter."
        if params["device"] == "mobile":
            urls[0], urls[1] = urls[1], urls[0]
        items = [dict(type="paid", rank_group=1, url="https://ads.example.com/robot-vacuum")]
        items.extend(dict(type="organic", rank_group=i, rank_absolute=i+1, url=url,
                          title=f"{title} {i}", description=description)
                     for i, url in enumerate(urls, 1))
        return 0.0035, [dict(market, se_type=path.split("/")[3], device=params["device"], items=items)]
    raise ValueError("unrecognized fixture request")


def content_results():
    return [{"crawl_progress": "finished", "items": [{"type": "content_parsing_element",
        "fetch_time": "2025-03-01 10:30:00 +00:00", "status_code": 200,
        "page_content": {"main_topic": [
            {"h_title": "Robot vacuum battery life", "level": 2, "primary_content": [{"text": "Raw fixture sentence"}]},
            {"h_title": "Frequently asked questions", "level": 2},
            {"h_title": "How long does it last?", "level": 3},
        ]}}]}]


def microdata_results():
    return [{"crawl_progress": "finished", "items": [{"type": "json_ld", "inspection_info": {"types": ["FAQPage"],
        "fields": [{"name": "mainEntity", "fields": [{"name": "@type", "value": "Question"},
                                                      {"name": "name", "value": "How long does it last?"}]}]}}]}]


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        host = self.headers.get("X-SEOOS-Fixture-Host")
        auth = self.headers.get("Authorization")
        with self.server.call_lock:
            self.server.checks[(host, self.path)] += 1
        if host == "api.dataforseo.com" and self.path == "/v3/appendix/user_data":
            encoded = base64.b64encode(f"{LOGIN}:{PASSWORD}".encode()).decode()
            if auth != f"Basic {encoded}":
                return self.write_json(200, {"status_code": 40100})
            return self.write_json(200, {"status_code": 20000, "cost": 0, "tasks": [
                {"status_code": 20000, "result_count": 1, "result": [{"login": LOGIN}]},
            ]})
        if host == "api.openai.com" and self.path.startswith("/v1/models/"):
            if auth != "Bearer dashboard-no-network-test-key":
                return self.write_json(401, {"error": {"message": "fixture credential rejected"}})
            if self.path == "/v1/models/test-fixture-model":
                return self.write_json(200, {"object": "model", "id": "test-fixture-model"})
            return self.write_json(404, {"error": {"message": "fixture model missing"}})
        self.write_json(404, {})

    def do_POST(self):
        auth = base64.b64encode(f"{LOGIN}:{PASSWORD}".encode()).decode()
        if self.headers.get("Authorization") != f"Basic {auth}":
            return self.write_json(401, {"status_code": 40100, "cost": 0})
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > 65536:
                raise ValueError("invalid fixture request size")
            params, = json.loads(self.rfile.read(length))
            cost, result = response_for(self.path, params)
        except (ValueError, KeyError, TypeError):
            return self.write_json(400, {"status_code": 40501, "cost": 0})
        with self.server.call_lock:
            self.server.calls[self.path] += 1
        task = {"id": INSTANT_TASK if self.path.endswith("/instant_pages") else params.get("tag", "fixture-onpage"),
                "status_code": 20000, "cost": cost, "data": params, "result": result}
        self.write_json(200, {"status_code": 20000, "cost": cost, "tasks": [task]})

    def write_json(self, status, body):
        payload = json.dumps(body, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *_):
        pass


class LocalProvider:
    def __enter__(self):
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.calls = Counter()
        self.server.checks = Counter()
        self.server.call_lock = Lock()
        self.origin = f"http://127.0.0.1:{self.server.server_port}"
        self.thread = Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        return self

    def __exit__(self, *_):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()
