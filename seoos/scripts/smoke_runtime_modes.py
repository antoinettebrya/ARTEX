#!/usr/bin/env python3
"""Check dedicated runtime modes against disposable PostgreSQL and real HTTP."""

import os
import secrets
import subprocess
import time
import uuid

from smoke_test import BINARY, ROOT, request, start, stop


def main():
    dsn = os.environ.get("SEOOS_TEST_DATABASE_URL")
    if not dsn or not BINARY.is_file():
        raise SystemExit("Set SEOOS_TEST_DATABASE_URL to a disposable database and build bin/seoos.")
    organization, token = str(uuid.uuid4()), secrets.token_hex(32)
    subprocess.run([BINARY, "migrate"], cwd=ROOT, check=True, timeout=30, env=dict(
        os.environ, SEOOS_DATABASE_URL=dsn, SEOOS_ORGANIZATION_ID=organization, SEOOS_API_TOKEN=token,
    ))
    processes = []
    try:
        for _ in range(2):
            process, origin = start(dsn, organization, token, command="api")
            processes.append(process)
        code, created = request(origin, "/api/v1/sites", token=token, method="POST", data={
            "name": "Separate runtime smoke fixture", "base_url": "https://runtime-modes.example.test",
            "cms": "generic", "country_code": "US", "language_tag": "en-US",
        })
        assert code == 201, code
        jobs = []
        for _ in range(12):
            code, result = request(origin, "/api/v1/jobs", token=token, method="POST", data={
                "site_id": created["site"]["id"], "kind": "site_inventory", "idempotency_key": str(uuid.uuid4()),
            })
            assert code == 201, code
            jobs.append("/api/v1/jobs/" + result["job"]["id"])

        def states():
            return [request(origin, path, token=token)[1]["job"]["state"] for path in jobs]

        time.sleep(2.2)
        assert set(states()) == {"queued"}, "API replicas executed queued jobs"
        worker_env = dict(os.environ, SEOOS_DATABASE_URL=dsn)
        worker_env.pop("SEOOS_API_TOKEN", None)
        worker_env.pop("SEOOS_ORGANIZATION_ID", None)
        for _ in range(2):
            processes.append(subprocess.Popen([BINARY, "worker"], env=worker_env, cwd=ROOT,
                                              stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True))
        for _ in range(40):
            assert all(proc.poll() is None for proc in processes), "a runtime process exited early"
            if set(states()) == {"succeeded"}:
                break
            time.sleep(0.25)
        else:
            raise AssertionError(f"worker processes did not complete the queued jobs: {states()}")
    finally:
        for process in reversed(processes):
            stop(process)
    print("PASS: one-shot migration, two API-only replicas, two workers without API credentials, 12 durable jobs and graceful shutdown")


if __name__ == "__main__":
    main()
