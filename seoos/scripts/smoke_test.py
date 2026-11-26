#!/usr/bin/env python3
"""Exercise the built API against an explicitly selected disposable PostgreSQL DB.

Uses Python's standard library. Creates two random organizations and test sites;
the disposable database owns their lifecycle. Never point this at production.
"""

import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid


ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "bin" / "seoos"


def address():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return f"127.0.0.1:{sock.getsockname()[1]}"


def request(base, path, *, token=None, method="GET", data=None):
    headers = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    payload = None
    if data is not None:
        headers["Content-Type"] = "application/json"
        payload = json.dumps(data).encode()
    req = urllib.request.Request(base + path, data=payload, headers=headers, method=method)
    # Local smoke checks do not inherit a workstation's outbound proxy settings.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req, timeout=5) as response:
            return response.status, None if response.status == 204 else json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def start(dsn, organization, token):
    addr = address()
    env = dict(os.environ, SEOOS_DATABASE_URL=dsn, SEOOS_ORGANIZATION_ID=organization,
               SEOOS_ORGANIZATION_NAME="Disposable smoke test", SEOOS_API_TOKEN=token,
               SEOOS_ADDR=addr)
    process = subprocess.Popen([str(BINARY), "serve"], env=env, cwd=ROOT,
                               stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)
    base = "http://" + addr
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise AssertionError("service exited before readiness: " + process.stderr.read())
            try:
                code, _ = request(base, "/readyz")
                if code == 200:
                    return process, base
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.1)
        raise AssertionError("service did not become ready within 10 seconds")
    except BaseException:
        stop(process)
        raise


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=12)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)
            raise AssertionError("service failed graceful shutdown")
    if process.returncode != 0:
        raise AssertionError(f"service exited with code {process.returncode}")
    if process.stderr:
        process.stderr.close()


def main():
    dsn = os.environ.get("SEOOS_TEST_DATABASE_URL")
    if not dsn:
        raise SystemExit("SEOOS_TEST_DATABASE_URL must select a disposable database")
    if not BINARY.is_file():
        raise SystemExit("Build first: go build -o bin/seoos ./cmd/seoos")
    organizations = [str(uuid.uuid4()), str(uuid.uuid4())]
    tokens = [secrets.token_hex(32), secrets.token_hex(32)]
    processes = []
    try:
        process_a, a = start(dsn, organizations[0], tokens[0])
        processes.append(process_a)
        process_b, b = start(dsn, organizations[1], tokens[1])
        processes.append(process_b)

        assert request(a, "/healthz")[0] == 200
        assert request(a, "/api/v1/sites")[0] == 401
        assert request(a, "/api/v1/sites", token=tokens[1])[0] == 401
        payload = {"name": "Smoke test site", "base_url": "https://EXAMPLE.com:443/",
                   "cms": "wordpress", "country_code": "us", "language_tag": "en-us"}
        code, created = request(a, "/api/v1/sites", token=tokens[0], method="POST", data=payload)
        assert code == 201, (code, created)
        site = created["site"]
        assert site["base_url"] == "https://example.com" and site["version"] == 1
        assert site["organization_id"] == organizations[0]
        path = "/api/v1/sites/" + site["id"]
        assert request(a, "/api/v1/sites", token=tokens[0], method="POST", data=payload)[0] == 409
        assert request(b, path, token=tokens[1])[0] == 404
        assert request(b, "/api/v1/sites", token=tokens[1])[1]["sites"] == []
        assert request(b, "/api/v1/sites", token=tokens[1], method="POST", data=payload)[0] == 201

        update = dict(payload, name="Updated smoke site", status="paused", version=1)
        code, updated = request(a, path, token=tokens[0], method="PUT", data=update)
        assert code == 200 and updated["site"]["version"] == 2
        assert request(a, path, token=tokens[0], method="PUT", data=update)[0] == 409
        assert request(b, path, token=tokens[1], method="PUT", data=update)[0] == 404

        cli_env = dict(os.environ, SEOOS_DATABASE_URL=dsn, SEOOS_ORGANIZATION_ID=organizations[0],
                       SEOOS_API_TOKEN=tokens[0])
        owner_email = str(uuid.uuid4()) + "@example.test"
        result = subprocess.run([str(BINARY), "provision-org", "Provisioned smoke organization", owner_email],
                                env=cli_env, text=True, capture_output=True, check=True, timeout=15)
        provisioned = json.loads(result.stdout)
        owner = provisioned["credential"]["token"]
        org = provisioned["organization_id"]
        assert org not in organizations
        assert request(a, "/api/v1/me", token=owner)[1]["organization_id"] == org
        assert request(a, path, token=owner)[0] == 404
        assert request(a, "/api/v1/sites", token=owner)[0] == 200
        code, added = request(a, "/api/v1/members", token=owner, method="POST",
                              data={"email": "editor-" + owner_email, "role": "viewer"})
        assert code == 201
        member = added["member"]["user_id"]
        viewer = added["token"]
        assert request(a, "/api/v1/sites", token=viewer, method="POST", data=payload)[0] == 403
        assert request(a, "/api/v1/audit", token=viewer)[0] == 403
        member_path = "/api/v1/members/" + member
        assert request(a, member_path + "/role", token=owner, method="PUT", data={"role": "editor"})[0] == 200
        assert request(a, "/api/v1/sites", token=viewer, method="POST", data=payload)[0] == 201
        assert request(a, member_path + "/token", token=owner, method="DELETE")[0] == 204
        assert request(a, "/api/v1/me", token=viewer)[0] == 401
        code, data = request(a, "/api/v1/audit", token=owner)
        assert code == 200 and {item["action"] for item in data["events"]} >= {"site.create", "member.token.revoke"}
        print("PASS: CLI organization provisioning, shared-service tenancy, role changes, credential revocation, and transactional audit")

        stop(process_a)
        processes.remove(process_a)
        restarted, a = start(dsn, organizations[0], tokens[0])
        processes.append(restarted)
        code, persisted = request(a, path, token=tokens[0])
        assert code == 200 and persisted["site"]["name"] == "Updated smoke site"
        assert persisted["site"]["version"] == 2 and persisted["site"]["status"] == "paused"
        print("PASS: real HTTP + PostgreSQL startup, auth, organization isolation, duplicate protection, version conflicts, restart persistence")
    finally:
        for process in reversed(processes):
            stop(process)
    print("PASS: graceful shutdown")


if __name__ == "__main__":
    main()
