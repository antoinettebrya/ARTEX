#!/usr/bin/env bash
set -euo pipefail

release=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
root=$(dirname "$(dirname "$release")")
exec 9>"$root/deploy.lock"
flock -n 9
current=$(readlink -f "$root/current")
previous=$(readlink -f "$root/previous")
test "$release" = "$current"
[[ "$previous" = "$root/releases/"* && "$previous" != "$current" ]]
test -f "$previous/release.env"

current_compose=(bash "$current/seoos/deploy/compose.sh")
previous_compose=(bash "$previous/seoos/deploy/compose.sh")
recover_current() {
    status=$?
    trap - EXIT
    if (( status != 0 )); then
        "${current_compose[@]}" up -d --wait --wait-timeout 150 api worker web >&2 || true
    fi
    exit "$status"
}
trap recover_current EXIT
"${current_compose[@]}" stop web api worker
archive=$(bash "$current/seoos/deploy/backup.sh")
printf 'Pre-rollback backup %s\n' "$archive"
"${previous_compose[@]}" up -d --wait --wait-timeout 150 api worker web
"${previous_compose[@]}" exec -T api /usr/local/bin/seoos health </dev/null
"${previous_compose[@]}" exec -T web node -e 'fetch("http://127.0.0.1:3000/seo/login",{signal:AbortSignal.timeout(5000)}).then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))' </dev/null

ln -s "$previous" "$root/.rollback-$$"
mv -Tf "$root/.rollback-$$" "$root/current"
trap - EXIT
ln -sfn "$current" "$root/previous"
"${previous_compose[@]}" ps
