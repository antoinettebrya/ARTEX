#!/usr/bin/env python3
"""Run model acceptance through a standard, isolated Go API and its worker."""

import argparse
import datetime
import json
import os
import re
import stat
import time
import urllib.error
import urllib.parse
import urllib.request


VENDORS = {"openai", "anthropic", "gemini", "deepseek", "openai_compatible"}


class API:
    def __init__(self, origin, token):
        url = urllib.parse.urlsplit(origin)
        if (url.scheme != "http" or url.hostname not in {"127.0.0.1", "::1"} or
                url.port is None or url.path or url.query or url.fragment or url.username):
            raise ValueError("Use a dedicated API on a loopback address with an explicit port.")
        self.origin = origin
        self.token = token
        self.client = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def call(self, path, method="GET", body=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(self.origin + "/api/v1" + path, data=data, method=method,
                                     headers={"Authorization": f"Bearer {self.token}",
                                              "Content-Type": "application/json"})
        try:
            with self.client.open(req, timeout=10) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"{method} {path.split('?')[0]} returned HTTP {error.code}") from None


def wait_for_job(api, job):
    deadline = time.monotonic() + 75
    while job["state"] in ("queued", "running") and time.monotonic() < deadline:
        time.sleep(1)
        job = api.call("/jobs/" + job["id"])["job"]
    return job


def verify_model(api, site_id, run_id, spec):
    kind = spec["kind"]
    name = f"Phase 4A {run_id} {kind}"
    listed = api.call("/providers?category=model&limit=200")["providers"]
    provider = next((row for row in listed if row["name"] == name), None)
    if provider is None:
        provider = api.call("/providers", "POST", {
            "kind": kind, "name": name, "model": spec["model"], "api_key": spec["api_key"],
            "max_output_tokens": spec.get("max_output_tokens", 512),
            "model_options": spec.get("model_options", {"temperature": "off"}),
            "enabled": True, "timeout_seconds": 25, "requests_per_minute": 1, "requests_per_day": 1,
        })["provider"]
    elif provider["model"] != spec["model"] or provider["kind"] != kind or provider["requests_per_day"] != 1:
        raise ValueError("The existing acceptance provider has different settings. Use another run ID.")
    provider_id = provider["id"]
    api.call("/content/models", "PUT", {
        "provider_id": provider_id, "role": "any", "system_prompt": "", "temperature": 0,
        "max_input_tokens": 2000, "daily_tokens": 6100, "daily_cost_micros": spec["budget_micros"],
        "input_micros_per_million": spec["input_micros_per_million"],
        "output_micros_per_million": spec["output_micros_per_million"],
        "max_retries": 0, "retry_seconds": 1,
    })
    job = wait_for_job(api, api.call("/jobs", "POST", {
        "site_id": site_id, "provider_id": provider_id, "kind": "site_brief",
        "idempotency_key": f"phase4a-live-{run_id}-{kind}",
    })["job"])
    rows = api.call(f"/providers/receipts?provider_id={provider_id}&limit=20")["items"]
    fields = ("http_status", "request_id", "category", "code", "input_tokens", "output_tokens",
              "cached_tokens", "cost_micros", "cost_source", "may_be_charged")
    calls = [{field: row["diagnostic"].get(field) for field in fields}
             for row in rows if row.get("job_id") == job["id"]]
    result = {"kind": kind, "model": provider["model"], "state": job["state"],
              "error_code": job.get("error_code"), "receipts": calls, "reconciliation": "vendor_bill_unavailable"}
    if spec.get("vendor_micros") is not None:
        day = spec.get("vendor_day", datetime.date.fromisoformat(job["created_at"][:10]).isoformat())
        query = urllib.parse.urlencode({"day": day, "vendor_micros": spec["vendor_micros"]})
        result["reconciliation"] = api.call(f"/providers/{provider_id}/reconcile?{query}")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("config", help="Private JSON configuration for a disposable test organization")
    args = parser.parse_args()
    with open(args.config, encoding="utf-8") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077:
            raise ValueError("Use a private regular configuration file, such as mode 0600.")
        settings = json.load(source)
    run_id = settings["run_id"]
    models = settings["models"]
    kinds = [spec["kind"] for spec in models]
    if not re.fullmatch(r"[a-z0-9-]{8,32}", run_id) or not models or set(kinds) - VENDORS or len(set(kinds)) != len(kinds):
        raise ValueError("Use one configuration per provider and a unique 8–32 character run ID.")
    if any(type(spec["budget_micros"]) is not int or spec["budget_micros"] <= 0 for spec in models) or \
            sum(spec["budget_micros"] for spec in models) > settings["maximum_cost_micros"]:
        raise ValueError("Individual reservations must fit the approved USD budget.")
    api = API(settings["api_origin"], settings["api_token"])
    site, _ = settings["site_id"], api.call("/me")
    api.call("/sites/" + site)
    results = [verify_model(api, site, run_id, spec) for spec in models]
    print(json.dumps({"mode": "live", "results": results}, indent=2))
    return 0 if all(row["state"] == "succeeded" for row in results) else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, KeyError, TypeError) as error:
        # Only purpose-built validation messages are printed. Inputs and provider response bodies stay private.
        detail = str(error) if isinstance(error, RuntimeError) else error.__class__.__name__
        raise SystemExit("Live acceptance could not finish: " + detail) from None
