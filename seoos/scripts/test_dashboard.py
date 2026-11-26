#!/usr/bin/env python3
"""Run the production Next.js build and browser tests against the real API."""

import base64
from contextlib import nullcontext
import os
import secrets
import subprocess
import uuid

import smoke_test
from research_fixture import LocalProvider


def main():
    dsn = os.environ.get("SEOOS_TEST_DATABASE_URL")
    research = os.environ.get("SEOOS_E2E_RESEARCH") == "1"
    if research:
        smoke_test.BINARY = smoke_test.ROOT / "bin" / "seoos-fixture"
    if not dsn or not smoke_test.BINARY.is_file():
        raise SystemExit(f"Build {smoke_test.BINARY.name} and set SEOOS_TEST_DATABASE_URL to a disposable database.")
    os.environ["SEOOS_PROVIDER_KEY"] = base64.b64encode(secrets.token_bytes(32)).decode()
    token = secrets.token_hex(32)
    with LocalProvider() if research else nullcontext() as upstream:
        if upstream:
            os.environ["SEOOS_DATAFORSEO_TEST_URL"] = upstream.origin
        process, origin = smoke_test.start(dsn, str(uuid.uuid4()), token)
        try:
            dashboard_env = dict(
                os.environ,
                SEOOS_API_ORIGIN=origin,
                SEOOS_E2E_TOKEN=token,
                SEOOS_E2E_WEB_PORT=smoke_test.address().split(":")[1],
                NEXT_TELEMETRY_DISABLED="1",
            )
            subprocess.run(["npm", "run", "test:e2e"], cwd=smoke_test.ROOT.parent / "web", env=dashboard_env, check=True)
            if upstream:
                calls = upstream.server.calls
                assert calls["/v3/dataforseo_labs/google/keyword_suggestions/live"] == 1, "suggestions cache was not reused"
                assert sum(calls.values()) == 10, f"unexpected loopback request counts: {calls}"
                print("PASS: 10 DataForSEO loopback requests, including one shared suggestions response")
        finally:
            smoke_test.stop(process)


if __name__ == "__main__":
    main()
