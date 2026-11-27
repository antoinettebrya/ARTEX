#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
chmod 755 "$scratch"
arch=$(docker version --format '{{.Server.Arch}}')
profile="$scratch/seccomp.json"
curl --fail --silent --show-error --location --max-time 30 \
  https://raw.githubusercontent.com/microsoft/playwright/v1.65.1/utils/docker/seccomp_profile.json \
  --output "$profile"
printf 'cc3e61cabda6bbc1e53e54d27ba4d55a9d3be829b6dd1a596f4a7b31b1cc7849  %s\n' "$profile" | shasum -a 256 -c -
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go test -c -o "$scratch/executionextract.test" ./internal/executionextract
docker build --target browser --tag artex-seoos-browser:test .
docker run --rm --network none --init --shm-size=256m --cap-drop ALL --cap-add SYS_CHROOT \
  --security-opt no-new-privileges --security-opt "seccomp=$profile" \
  -e SEOOS_REQUIRE_POPPLER=1 -e SEOOS_REQUIRE_CHROMIUM=1 \
  -v "$scratch/executionextract.test:/opt/executionextract.test:ro" \
  --entrypoint /opt/executionextract.test artex-seoos-browser:test -test.v
