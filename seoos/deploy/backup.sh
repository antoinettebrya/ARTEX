#!/usr/bin/env bash
set -euo pipefail

deploy=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
config=${ARTEX_STAGE_CONFIG:-/etc/artex-seoos}
backup=${ARTEX_STAGE_BACKUPS:-/var/backups/artex-seoos}
umask 077
test -s "$config/backup-key"
install -d -m 0700 "$backup"
install -d -m 0700 "$config/backup-gpg"
timestamp=$(date -u +%Y%m%dT%H%M%S)
temp=$(mktemp "$backup/.seoos-$timestamp-XXXXXXXX")
trap 'rm -f "$temp"' EXIT
bash "$deploy/compose.sh" exec -T postgres pg_dump -U seoos -d seoos -Fc |
    gpg --homedir "$config/backup-gpg" --batch --yes --pinentry-mode loopback --passphrase-file "$config/backup-key" \
        --cipher-algo AES256 --compress-algo none --symmetric --output "$temp"
test -s "$temp"
final="$backup/seoos-$timestamp-${temp##*-}.dump.gpg"
mv "$temp" "$final"
(cd "$backup" && sha256sum "${final##*/}" > "${final##*/}.sha256")
trap - EXIT
printf '%s\n' "$final"
