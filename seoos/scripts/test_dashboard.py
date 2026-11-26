#!/usr/bin/env python3
"""Run the production Next.js build and browser tests against the real API."""

import base64
from contextlib import ExitStack
import os
import secrets
import subprocess
import uuid

import smoke_test
from research_fixture import LocalProvider
from content_fixture import LocalContent
from execution_fixture import LocalExecution, HOST as CMS_HOST, CMS_PREFIX


def main():
    dsn = os.environ.get("SEOOS_TEST_DATABASE_URL")
    research = os.environ.get("SEOOS_E2E_RESEARCH") == "1"
    execution = os.environ.get("SEOOS_E2E_EXECUTION") == "1"
    content = os.environ.get("SEOOS_E2E_CONTENT") == "1" or execution
    if research or content:
        smoke_test.BINARY = smoke_test.ROOT / "bin" / "seoos-fixture"
    if not dsn or not smoke_test.BINARY.is_file():
        raise SystemExit(f"Build {smoke_test.BINARY.name} and set SEOOS_TEST_DATABASE_URL to a disposable database.")
    os.environ["SEOOS_PROVIDER_KEY"] = base64.b64encode(secrets.token_bytes(32)).decode()
    token = secrets.token_hex(32)
    with ExitStack() as stack:
        upstream = stack.enter_context(LocalProvider()) if research or content else None
        editorial = stack.enter_context(LocalContent()) if content else None
        integration = stack.enter_context(LocalExecution()) if execution else None
        if upstream:
            os.environ["SEOOS_DATAFORSEO_TEST_URL"] = upstream.origin
        if editorial:
            os.environ["SEOOS_CONTENT_TEST_URL"] = editorial.origin
        if integration:
            os.environ["SEOOS_EXECUTION_TEST_URL"] = integration.origin
        process, origin = smoke_test.start(dsn, str(uuid.uuid4()), token)
        try:
            dashboard_env = dict(
                os.environ,
                SEOOS_API_ORIGIN=origin,
                SEOOS_E2E_TOKEN=token,
                SEOOS_E2E_WEB_PORT=smoke_test.address().split(":")[1],
                NEXT_TELEMETRY_DISABLED="1",
            )
            if content:
                dashboard_env["SEOOS_E2E_CONTENT"] = "1"
            if editorial:
                dashboard_env["SEOOS_E2E_CONTENT_FIXTURE"] = editorial.origin
            subprocess.run(["npm", "run", "test:e2e"], cwd=smoke_test.ROOT.parent / "web", env=dashboard_env, check=True)
            if integration:
                calls = integration.calls
                assert calls[(CMS_HOST, "POST", CMS_PREFIX + "/pages")] == 1, calls
                print(f"PASS: {sum(calls.values())} execution loopback requests and exactly one CMS draft write")
            if research:
                calls = upstream.server.calls
                assert calls["/v3/dataforseo_labs/google/keyword_suggestions/live"] == 1, "suggestions cache was not reused"
                expected = 13 if content else 10
                assert sum(calls.values()) == expected, f"unexpected loopback request counts: {calls}"
                print(f"PASS: {expected} DataForSEO loopback requests, including one shared suggestions response")
            if editorial:
                counts = editorial.server.calls
                assert counts == {"outline": 1, "section": 3, "review": 1}, counts
                status, usage = smoke_test.request(origin, "/api/v1/content/usage", token=token)
                assert status == 200, status
                items = usage["items"]
                tokens = sum(item["used_tokens"] for item in items)
                cost = sum(item["spent_micros"] for item in items)
                assert tokens == 1374 and cost > 0, (tokens, cost)
                assert all(item["reserved_tokens"] == 0 and item["reserved_micros"] == 0 for item in items)
                print(f"PASS: 5 editorial loopback requests, {tokens} measured fixture tokens, {cost} USD micros estimated")
        finally:
            smoke_test.stop(process)


if __name__ == "__main__":
    main()
