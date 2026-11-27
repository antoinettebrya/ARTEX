"""Local HTTP contracts for authorized pages and independent editorial models."""

from collections import Counter
from html import escape
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from threading import Lock, Thread

TERM = "robot vacuum maintenance"
SITE = "https://phase3.example.com"
SOURCE = "https://official.example.com/robot-vacuum-guide"
TOKEN = "phase3-model-fixture-token"
CLAIM = "Replace the robot vacuum filter every 90 days."
SECTIONS = [
    "Switch off the robot vacuum before opening the filter housing. Check the housing and lift out trapped dust with a dry cloth. Put a clean container nearby to catch debris as the dustbin comes free.",
    CLAIM + " Follow the product manual before washing removable parts. Seat the clean filter firmly and close the cover before reconnecting the vacuum.",
    "Check the result by looking for gaps around the filter. After closing the cover, run a short cleaning cycle and listen for changes. Pause maintenance if the housing is loose.",
]
OLD = """<!doctype html><html lang="en"><head>
<title>Robot vacuum maintenance reminders</title><meta name="description" content="Basic cleaning notes">
<link rel="canonical" href="/existing"><meta name="robots" content="index,follow"></head>
<body><main><h1>Robot vacuum maintenance</h1><h2>Previous notes</h2>
<p>Keep the dock clear and empty the dustbin after cleaning. Store replacement parts somewhere dry.</p>
<img src="/photos/dock.png" alt="Vacuum dock"></main></body></html>"""
FIRST_PARTY = f"""<!doctype html><html lang="en"><head><title>Product filter care</title>
<meta property="article:published_time" content="2026-08-01T10:00:00Z"></head>
<body><main><h1>Product filter care</h1><p>{CLAIM}</p></main></body></html>"""


def answer(request):
    stage, data = request["stage"], request["data"]
    if request["language"] != "en":
        raise ValueError("fixture expects the original English market")
    if stage == "outline":
        return {"headings": data["brief"]["headings"]}
    if stage == "section":
        ordinal = data["ordinal"]
        claims = []
        if ordinal == 1:
            fact = next(item for item in data["facts"] if item["statement"] == CLAIM)
            claims.append({"text": CLAIM, "fact_ids": [fact["id"]]})
        return {"body": SECTIONS[ordinal], "claims": claims}
    if stage == "review":
        return {"passed": True, "intent": True, "language": True, "original_value": True, "issues": []}
    raise ValueError("unknown editorial stage")


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        host = self.headers.get("X-SEOOS-Fixture-Host")
        if self.path == "/robots.txt" and host in {"phase3.example.com", "official.example.com"}:
            return self.respond(200, "text/plain", "User-agent: *\nAllow: /\n")
        if host == "phase3.example.com":
            if self.path == "/sitemap.xml":
                return self.respond(200, "application/xml",
                    '<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">'
                    f'<url><loc>{SITE}/existing</loc></url></urlset>')
            if self.path == "/existing":
                with self.server.lock:
                    published = self.server.published
                if published:
                    return self.respond(200, "text/html; charset=utf-8", published)
                if self.headers.get("If-None-Match") == '"inventory-v1"':
                    return self.respond(304, "text/html", "")
                return self.respond(200, "text/html; charset=utf-8", OLD, etag='"inventory-v1"')
        if host == "official.example.com" and self.path == "/robot-vacuum-guide":
            return self.respond(200, "text/html; charset=utf-8", FIRST_PARTY)
        self.respond(404, "text/plain", "outside fixed fixture")

    def do_POST(self):
        if self.path == "/test/published":
            return self.fake_cms_output()
        if self.headers.get("Authorization") != f"Bearer {TOKEN}":
            return self.respond(401, "application/json", "{}")
        host = self.headers.get("X-SEOOS-Fixture-Host")
        openai = host == "api.openai.com" and self.path == "/v1/responses"
        gemini = host == "generativelanguage.googleapis.com" and self.path == "/v1beta/openai/chat/completions"
        if not openai and not gemini:
            return self.respond(404, "application/json", "{}")
        try:
            length = int(self.headers["Content-Length"])
            if length < 1 or length > 100000:
                raise ValueError("request size")
            raw = json.loads(self.rfile.read(length))
            request = json.loads(raw["input"] if openai else raw["messages"][-1]["content"])
            payload = json.dumps(answer(request))
            stage = request["stage"]
            expected = "fixture-reviewer" if stage == "review" else "fixture-writer"
            if raw["model"] != expected or ((stage == "review") != gemini):
                raise ValueError("wrong independent model")
        except (ValueError, KeyError, IndexError, StopIteration, TypeError):
            return self.respond(400, "application/json", "{}")
        with self.server.lock:
            self.server.calls[stage] += 1
        if openai:
            body = {"status": "completed", "model": raw["model"],
                    "output": [{"type": "message", "content": [{"type": "output_text", "text": payload}]}],
                    "usage": {"input_tokens": 185, "output_tokens": 88}}
        else:
            body = {"model": raw["model"],
                    "choices": [{"finish_reason": "stop", "message": {"role": "assistant", "content": payload}}],
                    "usage": {"prompt_tokens": 240, "completion_tokens": 42}}
        self.respond(200, "application/json", json.dumps(body))

    def fake_cms_output(self):
        # The browser test supplies a released package as though a separate
        # fixture CMS had rendered it. The Go runtime makes no CMS write.
        try:
            length = int(self.headers["Content-Length"])
            if not 0 < length <= 64000:
                raise ValueError("size")
            article = json.loads(self.rfile.read(length))["article"]
            headings = []
            for section in article["sections"]:
                level = int(section["level"])
                if not 2 <= level <= 6:
                    raise ValueError("level")
                headings.append(f'<h{level}>{escape(section["heading"])}</h{level}><p>{escape(section["body"])}</p>')
            page = ('<!doctype html><html lang="en"><head>'
                    f'<title>{escape(article["title"])}</title></head><body><main>'
                    f'<h1>{escape(article["h1"])}</h1>{"".join(headings)}</main></body></html>')
        except (ValueError, KeyError, TypeError):
            return self.respond(400, "application/json", "{}")
        with self.server.lock:
            self.server.published = page
        return self.respond(200, "application/json", "{}")

    def respond(self, status, content_type, value, etag=None):
        body = value.encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        if etag:
            self.send_header("ETag", etag)
        self.end_headers()
        if body:
            self.wfile.write(body)

    def log_message(self, *_):
        pass


class LocalContent:
    def __enter__(self):
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.calls = Counter()
        self.server.published = None
        self.server.lock = Lock()
        self.thread = Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.origin = f"http://127.0.0.1:{self.server.server_port}"
        return self

    def __exit__(self, *_):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()
