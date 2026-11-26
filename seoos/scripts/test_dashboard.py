#!/usr/bin/env python3
"""Run the production Next.js build and browser tests against the real API."""

import base64
import os
import secrets
import subprocess
import uuid

from smoke_test import BINARY, ROOT, address, start, stop


def main():
    dsn = os.environ.get("SEOOS_TEST_DATABASE_URL")
    if not dsn or not BINARY.is_file():
        raise SystemExit("Build bin/seoos and set SEOOS_TEST_DATABASE_URL to a disposable database.")
    os.environ["SEOOS_PROVIDER_KEY"] = base64.b64encode(secrets.token_bytes(32)).decode()
    token = secrets.token_hex(32)
    process, origin = start(dsn, str(uuid.uuid4()), token)
    try:
        dashboard_env = dict(
            os.environ,
            SEOOS_API_ORIGIN=origin,
            SEOOS_E2E_TOKEN=token,
            SEOOS_E2E_WEB_PORT=address().split(":")[1],
            NEXT_TELEMETRY_DISABLED="1",
        )
        subprocess.run(["npm", "run", "test:e2e"], cwd=ROOT / "web", env=dashboard_env, check=True)
    finally:
        stop(process)


if __name__ == "__main__":
    main()
