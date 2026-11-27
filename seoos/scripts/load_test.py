#!/usr/bin/env python3
"""Exercise 160 registered sites and local inventory jobs on disposable PostgreSQL."""

from concurrent.futures import ThreadPoolExecutor
import math
import os
import secrets
import time
import uuid

from smoke_test import BINARY, request, start, stop


SITE_COUNT = 160
PARALLEL = 12
MARKETS = [("US", "en-US"), ("JP", "ja-JP"), ("DE", "de-DE"), ("CN", "zh-Hans")]


def measure(items, operation):
    def timed(item):
        before = time.perf_counter()
        value = operation(item)
        return value, (time.perf_counter() - before) * 1000

    before = time.perf_counter()
    with ThreadPoolExecutor(max_workers=PARALLEL) as threads:
        records = list(threads.map(timed, items))
    elapsed = time.perf_counter() - before
    times = sorted(timing for _, timing in records)
    p95 = times[math.ceil(0.95 * len(times)) - 1]
    return [value for value, _ in records], elapsed, p95


def main():
    dsn = os.environ.get("SEOOS_TEST_DATABASE_URL")
    if not dsn or not BINARY.is_file():
        raise SystemExit("Build bin/seoos and set SEOOS_TEST_DATABASE_URL to a disposable database.")
    token = secrets.token_hex(32)
    process, origin = start(dsn, str(uuid.uuid4()), token)
    try:
        def create_site(index):
            country, language = MARKETS[index % len(MARKETS)]
            code, body = request(origin, "/api/v1/sites", token=token, method="POST", data={
                "name": f"Load site {index}", "base_url": f"https://site-{index}.example.test",
                "country_code": country, "language_tag": language,
            })
            assert code == 201, (index, code)
            return body["site"]["id"]

        def enqueue(site_id):
            code, body = request(origin, "/api/v1/jobs", token=token, method="POST", data={
                "site_id": site_id, "kind": "site_inventory", "idempotency_key": site_id,
            })
            assert code == 201, (site_id, code)
            return body["job"]["id"]

        ids, sites_elapsed, sites_p95 = measure(range(SITE_COUNT), create_site)
        assert len(set(ids)) == SITE_COUNT
        code, listed = request(origin, "/api/v1/sites?limit=200", token=token)
        assert code == 200 and {site["id"] for site in listed["sites"]} == set(ids)

        before_jobs = time.perf_counter()
        jobs, enqueue_elapsed, enqueue_p95 = measure(ids, enqueue)
        expected = set(jobs)
        while time.perf_counter() - before_jobs < 30:
            code, result = request(origin, "/api/v1/jobs?limit=200", token=token)
            assert code == 200
            succeeded = {job["id"]: job["result"]["id"] for job in result["jobs"] if job["state"] == "succeeded"}
            if expected == set(succeeded):
                assert set(succeeded.values()) == set(ids)
                break
            time.sleep(0.1)
        else:
            raise AssertionError(f"Worker completed {len(succeeded)} of {SITE_COUNT} jobs")

        print(f"{SITE_COUNT} sites created with {PARALLEL} concurrent clients, 0 failures; {sites_elapsed:.2f}s, p95 {sites_p95:.1f} ms")
        print(f"{SITE_COUNT} jobs enqueued, 0 failures; {enqueue_elapsed:.2f}s, p95 {enqueue_p95:.1f} ms")
        print(f"{SITE_COUNT} inventory jobs completed, 0 failures; {time.perf_counter() - before_jobs:.2f}s from first enqueue")
    finally:
        stop(process)


if __name__ == "__main__":
    main()
