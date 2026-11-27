#!/usr/bin/env bash
set -euo pipefail

deploy=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
config=${ARTEX_STAGE_CONFIG:-/etc/artex-seoos}
archive=${1:?Provide a verified encrypted database archive}
database=${2:?Provide a new restore database name}
[[ "$database" =~ ^seoos_[a-z0-9_]{1,55}$ ]]
test -s "$config/backup-key"
test -s "$archive"
test -s "$archive.sha256"
read -r recorded _ < "$archive.sha256"
calculated=$(sha256sum "$archive")
test "${calculated%% *}" = "$recorded"

compose=(bash "$deploy/compose.sh")
"${compose[@]}" exec -T postgres createdb -U seoos --template=template0 "$database" </dev/null
cleanup() {
    status=$?
    if (( status != 0 )); then
        "${compose[@]}" exec -T postgres dropdb -U seoos "$database" </dev/null || true
    fi
}
trap cleanup EXIT
gpg --homedir "$config/backup-gpg" --batch --quiet --pinentry-mode loopback --passphrase-file "$config/backup-key" \
    --decrypt "$archive" |
    "${compose[@]}" exec -T postgres pg_restore -U seoos -d "$database" --exit-on-error --single-transaction
"${compose[@]}" exec -T postgres psql -U seoos -d "$database" -X -qAt -v ON_ERROR_STOP=1 \
    -c 'select current_database()' </dev/null | grep -Fx "$database"
trap - EXIT
