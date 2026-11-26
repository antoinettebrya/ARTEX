#!/usr/bin/env python3
"""Run Phase 4 HTTP, queue and PostgreSQL checks with local provider fixtures."""

import base64
import json
import os
from pathlib import Path
import secrets
import subprocess
import time
from urllib.parse import parse_qs, urlencode, urlsplit
import uuid

from execution_fixture import (
    BING_KEY, GA_PROPERTY, GOOGLE_CLIENTS, GOOGLE_SCOPES, HOST, INDEXNOW_KEY, INDEXNOW_RETRY_AFTER,
    INDEXNOW_RETRY_PAGE, LocalExecution, PAGE, REPORT_DAY, SITE,
)
from research_fixture import LocalProvider
import smoke_test

API = "/api/v1"
EXECUTION = API + "/execution"
CALLBACK = "https://dashboard.example.test/seo/execution"


def checked(base, path, token, method="GET", data=None, expected=200):
    status, result = smoke_test.request(base, path, token=token, method=method, data=data)
    assert status == expected, (method, path, status, (result or {}).get("error", {}).get("code"))
    return result


def rows(base, token, table, site, **filters):
    query = urlencode(dict(site_id=site, **filters))
    result = checked(base, EXECUTION + "/" + table + "?" + query, token)
    return result["items"]


def wait_job(base, token, job_id):
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        job = checked(base, API + "/jobs/" + job_id, token)["job"]
        state = job["state"]
        if state in ("succeeded", "failed", "cancelled"):
            assert state == "succeeded", (job["kind"], state, job.get("error_code"))
            return job
        time.sleep(0.15)
    raise AssertionError("execution job exceeded its smoke test deadline")


def connect(base, token, site, kind, credential, **settings):
    output = checked(base, EXECUTION + "/connections", token, "POST", {
        "site_id": site, "name": "Phase 4 fixture " + kind, "kind": kind, "enabled": True,
        "credential": credential, **settings,
    }, expected=201)["connection"]
    assert "credential" not in output
    return output["id"]


def connect_google(base, token, site, kind, property_id):
    client, client_secret = GOOGLE_CLIENTS[kind]
    connection = connect(base, token, site, kind, {"client_id": client, "client_secret": client_secret},
                         property=property_id, redirect_uri=CALLBACK)
    params = {"site_id": site, "connection_id": connection}
    setup = checked(base, EXECUTION + "/oauth/start", token, "POST", params)
    target = urlsplit(setup["authorization_url"])
    proof = parse_qs(target.query)
    assert target.scheme == "https" and target.netloc == "accounts.google.com"
    assert proof["state"] == [setup["state"]] and len(proof["code_challenge"][0]) == 43
    assert proof["code_challenge_method"] == ["S256"] and proof["scope"] == [GOOGLE_SCOPES[kind]]
    assert proof["redirect_uri"] == [CALLBACK] and proof["client_id"] == [client]
    assert "client_secret" not in proof
    exchange = dict(params, state=setup["state"], code=f"phase4-{kind}-code")
    assert checked(base, EXECUTION + "/oauth/exchange", token, "POST", exchange)["connected"]
    checked(base, EXECUTION + "/oauth/exchange", token, "POST", exchange, expected=403)
    return connection


def submit_job(base, token, site, connection, kind, key, *, replay=False, **values):
    request = {"site_id": site, "connection_id": connection, "kind": kind,
               "idempotency_key": key, **values}
    job = checked(base, EXECUTION + "/jobs", token, "POST", request, expected=201)["job"]
    final = wait_job(base, token, job["id"])
    if replay:
        again = checked(base, EXECUTION + "/jobs", token, "POST", request)["job"]
        assert again["id"] == job["id"]
    return final


def other_organization(dsn, org, token):
    result = subprocess.run(
        [str(smoke_test.BINARY), "provision-org", "Phase 4 smoke isolation",
         f"phase4-smoke-{uuid.uuid4()}@example.test"],
        cwd=smoke_test.ROOT, text=True, capture_output=True, check=True, timeout=15,
        env=dict(os.environ, SEOOS_DATABASE_URL=dsn, SEOOS_ORGANIZATION_ID=org, SEOOS_API_TOKEN=token),
    )
    return json.loads(result.stdout)["credential"]["token"]


def exercise(base, token, fixture, other_owner):
    site = checked(base, API + "/sites", token, "POST", {
        "name": "Phase 4 search smoke site", "base_url": SITE, "cms": "generic",
        "country_code": "US", "language_tag": "en-US",
    }, expected=201)["site"]["id"]

    gsc = connect_google(base, token, site, "gsc", "sc-domain:" + HOST)
    ga4 = connect_google(base, token, site, "ga4", GA_PROPERTY)
    bing = connect(base, token, site, "bing", {"secret": BING_KEY},
                   property=SITE + "/", allow_writes=True)
    indexnow = connect(base, token, site, "indexnow", {"secret": INDEXNOW_KEY},
                       key_location=SITE + "/" + INDEXNOW_KEY + ".txt", allow_writes=True)
    for kind, connection in (("gsc", gsc), ("ga4", ga4), ("bing", bing)):
        submit_job(base, token, site, connection, kind + "_sync", f"phase4-smoke-{kind}-sync",
                   **{"from": REPORT_DAY, "to": REPORT_DAY})
        assert rows(base, token, "properties", site, kind=kind, connection_id=connection)

    gsc_last = rows(base, token, "metrics", site, kind="gsc", page=PAGE, country="USA", limit=1, offset=1000,
                    **{"from": REPORT_DAY, "to": REPORT_DAY})
    assert len(gsc_last) == 1 and gsc_last[0]["query"] == "phase4 organic query 1000"
    assert gsc_last[0]["date"] == REPORT_DAY and gsc_last[0]["values"]["position"] == 6.5
    ga4_metrics = rows(base, token, "metrics", site, kind="ga4", page=PAGE)
    assert len(ga4_metrics) == 1 and ga4_metrics[0]["values"]["sessions"] == 17
    assert ga4_metrics[0]["language"] == "English" and ga4_metrics[0]["values"]["key_events"] == 4
    bing_metrics = rows(base, token, "metrics", site, kind="bing")
    assert len(bing_metrics) == 3 and sum("page" in item for item in bing_metrics) == 1
    assert all(item["values"]["impressions"] == 91 for item in bing_metrics)
    sitemap = {row["provider"]: row for row in rows(base, token, "sitemaps", site)}
    assert sitemap["gsc"]["status"] == "processed" and sitemap["bing"]["status"] == "Success"
    assert all(sitemap[kind]["url"] == SITE + "/sitemap.xml" for kind in ("gsc", "bing"))

    checked(base, EXECUTION + "/jobs", token, "POST", {
        "site_id": site, "connection_id": gsc, "kind": "url_inspection",
        "idempotency_key": "phase4-smoke-outside", "url": "https://outside.example.test/resource",
    }, expected=403)
    checked(base, EXECUTION + "/metrics?" + urlencode({"site_id": site, "kind": "ga4"}),
            other_owner, expected=404)
    editor = checked(base, API + "/members", token, "POST", {
        "email": f"phase4-editor-{uuid.uuid4()}@example.test", "role": "editor",
    }, expected=201)["token"]
    checked(base, EXECUTION + "/jobs", editor, "POST", {
        "site_id": site, "connection_id": indexnow, "kind": "indexnow_submit",
        "idempotency_key": "phase4-smoke-editor", "url": PAGE,
    }, expected=403)

    submit_job(base, token, site, indexnow, "indexnow_submit", "phase4-smoke-indexnow", replay=True, url=PAGE)
    submit_job(base, token, site, bing, "indexnow_submit", "phase4-smoke-submit-bing", url=PAGE)
    submit_job(base, token, site, gsc, "url_inspection", "phase4-smoke-inspect-gsc", url=PAGE)
    submit_job(base, token, site, bing, "url_inspection", "phase4-smoke-inspect-bing", url=PAGE)
    indexes = {item["provider"]: item for item in rows(base, token, "index", site)}
    assert indexes["gsc"]["status"] == "INDEXED" and indexes["gsc"]["last_crawled"]
    assert indexes["bing"]["status"] == "CRAWLED" and indexes["bing"]["last_crawled"]
    assert indexes["indexnow"]["status"] == "SUBMITTED"
    receipts = {item["provider"]: item for item in rows(base, token, "submissions", site)}
    assert receipts["indexnow"]["http_status"] == 202 and receipts["indexnow"]["status"] == "pending_key_validation"
    assert receipts["bing"]["http_status"] == 200
    usage = {item["connection_id"]: item["requests"] for item in rows(base, token, "usage", site)}
    assert usage == {gsc: 7, ga4: 4, bing: 8, indexnow: 2}, usage
    assert len(fixture.requests) == sum(usage.values())
    for kind in ("gsc", "ga4"):
        assert fixture.counters[f"oauth_{kind}_authorization_code"] == 1
        assert fixture.counters[f"oauth_{kind}_refresh_token"] == 1
    assert fixture.calls[("api.indexnow.org", "POST", "/indexnow")] == 1
    print("PASS: Google OAuth, two-page GSC import, GA4 organic report, Bing report and submission, "
          "URL inspection evidence, IndexNow replay protection, site/organization isolation, and 21 metered local requests")
    retry_indexnow(base, token, fixture, site, indexnow)
    return site


def retry_indexnow(base, token, fixture, site, connection):
    job = checked(base, EXECUTION + "/jobs", token, "POST", {
        "site_id": site, "connection_id": connection, "kind": "indexnow_submit",
        "idempotency_key": "phase4-smoke-indexnow-retry", "url": INDEXNOW_RETRY_PAGE,
    }, expected=201)["job"]
    try:
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            receipts = rows(base, token, "submissions", site)
            failed = next((item for item in receipts if item["job_id"] == job["id"]), None)
            if failed:
                break
            time.sleep(0.1)
        else:
            raise AssertionError("the throttled submission did not record a failure")
        assert failed["url"] == INDEXNOW_RETRY_PAGE and failed["status"] == "failed", failed
        assert failed["http_status"] == 429 and failed["error"] == "execution_upstream_quota", failed
        assert all(item["url"] != INDEXNOW_RETRY_PAGE for item in rows(base, token, "index", site))
    finally:
        fixture.allow_indexnow_retry()

    succeeded = wait_job(base, token, job["id"])
    assert succeeded["attempts"] == 2
    attempt_times = fixture.indexnow_retry_times
    assert len(attempt_times) == 2
    elapsed = attempt_times[1] - attempt_times[0]
    assert elapsed >= INDEXNOW_RETRY_AFTER - 0.05, elapsed
    assert_retry_receipt(base, token, site)

    events = checked(base, API + "/audit?limit=100", token)["events"]
    actions = {item["action"]: item for item in events if item["resource_id"] == job["id"]}
    failure_event = actions["execution.indexing.submit_fail"]
    retry_event = actions["job.retry"]
    assert failure_event["details"] == {"provider": "indexnow", "error": "execution_upstream_quota"}
    assert retry_event["details"]["state"] == "queued" and retry_event["details"]["attempts"] == 1
    assert failure_event["id"] < retry_event["id"] < actions["job.succeed"]["id"]

    usage = {item["connection_id"]: item["requests"] for item in rows(base, token, "usage", site)}
    assert usage[connection] == 6 and sum(usage.values()) == len(fixture.requests) == 25
    assert fixture.calls[("api.indexnow.org", "POST", "/indexnow")] == 3
    assert fixture.calls[(HOST, "GET", "/" + INDEXNOW_KEY + ".txt")] == 3
    print(f"PASS: IndexNow HTTP 429 → 200 after {elapsed:.2f}s (Retry-After: {INDEXNOW_RETRY_AFTER}s), "
          "failed receipt and retry audited, SUBMITTED only after acceptance, and 4 additional metered requests")


def assert_retry_receipt(base, token, site):
    receipt, = [item for item in rows(base, token, "submissions", site) if item["url"] == INDEXNOW_RETRY_PAGE]
    record, = [item for item in rows(base, token, "index", site) if item["url"] == INDEXNOW_RETRY_PAGE]
    assert receipt["http_status"] == 200 and receipt["status"] == "accepted"
    assert record["provider"] == "indexnow" and record["status"] == "SUBMITTED" and record["evidence"] == "accepted"


def main():
    dsn = os.environ.get("SEOOS_TEST_DATABASE_URL")
    binary = os.environ.get("SEOOS_TEST_EXECUTION_BINARY")
    smoke_test.BINARY = Path(binary).resolve() if binary else smoke_test.ROOT / "bin" / "seoos-fixture"
    if not dsn or not smoke_test.BINARY.is_file():
        raise SystemExit("Build seoos-fixture with -tags seoos_testfixture and set SEOOS_TEST_DATABASE_URL to a disposable database.")
    org, token = str(uuid.uuid4()), secrets.token_hex(32)
    os.environ["SEOOS_PROVIDER_KEY"] = base64.b64encode(secrets.token_bytes(32)).decode()
    with LocalExecution() as fixture, LocalProvider() as research:
        os.environ["SEOOS_EXECUTION_TEST_URL"] = fixture.origin
        os.environ["SEOOS_DATAFORSEO_TEST_URL"] = research.origin
        process, base = smoke_test.start(dsn, org, token)
        try:
            outsider = other_organization(dsn, org, token)
            site = exercise(base, token, fixture, outsider)
        finally:
            smoke_test.stop(process)

        process, base = smoke_test.start(dsn, org, token)
        try:
            stored = rows(base, token, "metrics", site, kind="ga4", page=PAGE)
            assert stored[0]["values"]["sessions"] == 17
            assert len(rows(base, token, "submissions", site)) == 3
            assert_retry_receipt(base, token, site)
            assert len(fixture.requests) == 25
        finally:
            smoke_test.stop(process)
    print("PASS: metrics and submission receipts survive an application restart")


if __name__ == "__main__":
    main()
