#!/usr/bin/env bash
set -euo pipefail

release=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
config=${ARTEX_STAGE_CONFIG:-/etc/artex-seoos}
files=(-f "$release/seoos/deploy/compose.staging.yaml")
if [ -f "$release/.browser-worker" ]; then
    files+=(-f "$release/seoos/deploy/compose.browser.yaml")
fi
exec docker compose --env-file "$config/staging.env" --env-file "$release/release.env" "${files[@]}" "$@"
