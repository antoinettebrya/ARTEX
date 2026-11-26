#!/usr/bin/env bash
set -euo pipefail

release=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
install_root=$(dirname "$(dirname "$release")")
revision=${1:?Provide the full Git commit SHA}
[[ "$revision" =~ ^[a-f0-9]{40}$ && "${release##*/}" = "$revision" && "$(dirname "$release")" = "$install_root/releases" ]]
config=${ARTEX_STAGE_CONFIG:-/etc/artex-seoos}
umask 077
test -s "$config/staging.env"
exec 9>"$install_root/deploy.lock"
flock -n 9
previous=$(readlink -f "$install_root/current" || true)
if [ -L "$install_root/current" ] && [ "$previous" = "$release" ]; then
    printf '%s\n' 'This release is already active.'
    exit 1
fi
api="artex-seoos-api:$revision"
worker="$api"
web="artex-seoos-web:$revision"
label="org.opencontainers.image.revision=$revision"
docker build --label "$label" --tag "$api" "$release/seoos"
docker build --label "$label" --tag "$web" "$release/web"
if [ "${ARTEX_STAGE_BROWSER:-0}" = 1 ]; then
    test -s "$config/playwright-seccomp.json"
    worker="artex-seoos-worker:$revision"
    docker build --target browser --label "$label" --tag "$worker" "$release/seoos"
    touch "$release/.browser-worker"
else
    rm -f "$release/.browser-worker"
fi
printf 'ARTEX_API_IMAGE=%s\nARTEX_WORKER_IMAGE=%s\nARTEX_WEB_IMAGE=%s\nARTEX_PLAYWRIGHT_SECCOMP=%s\n' \
    "$api" "$worker" "$web" "$config/playwright-seccomp.json" > "$release/release.env"
compose=(bash "$release/seoos/deploy/compose.sh")
"${compose[@]}" config --quiet
"${compose[@]}" up -d --wait --wait-timeout 90 postgres
if [ -L "$install_root/current" ]; then
    test -f "$previous/seoos/deploy/compose.sh"
    bash "$previous/seoos/deploy/compose.sh" stop web api worker
fi
backup=$(bash "$release/seoos/deploy/backup.sh")
printf 'Pre-migration backup %s\n' "$backup"
"${compose[@]}" run --rm migrate
if ! "${compose[@]}" up -d --wait --wait-timeout 120 api worker web; then
    "${compose[@]}" stop web api worker
    printf '%s\n' 'Release did not become healthy. Restore or reselect the previous release using the deployment runbook.' >&2
    exit 1
fi
if [ -L "$install_root/current" ]; then
    ln -sfn "$previous" "$install_root/previous"
fi
next="$install_root/.current-$revision"
ln -s "$release" "$next"
mv -Tf "$next" "$install_root/current"
"${compose[@]}" ps
