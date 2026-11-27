#!/usr/bin/env bash
set -euo pipefail

deploy=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
config=${ARTEX_STAGE_CONFIG:-/etc/artex-seoos}
backup=${ARTEX_STAGE_BACKUPS:-/var/backups/artex-seoos}
umask 077
database=$(awk -F= '$1=="ARTEX_DATABASE_NAME" {print $2}' "$deploy/../../release.env")
database=${database:-seoos}
[[ "$database" =~ ^[a-z][a-z0-9_]{0,62}$ ]]
test -s "$config/backup-key"
install -d -m 0700 "$backup"
install -d -m 0700 "$config/backup-gpg"
timestamp=$(date -u +%Y%m%dT%H%M%S)
temp=$(mktemp "$backup/.seoos-$timestamp-XXXXXXXX")
trap 'rm -f "$temp"' EXIT
bash "$deploy/compose.sh" exec -T postgres pg_dump -U seoos -d "$database" -Fc </dev/null |
    gpg --homedir "$config/backup-gpg" --batch --yes --pinentry-mode loopback --passphrase-file "$config/backup-key" \
        --cipher-algo AES256 --compress-algo none --symmetric --output "$temp"
test -s "$temp"
final="$backup/seoos-$timestamp-${temp##*-}.dump.gpg"
mv "$temp" "$final"
(cd "$backup" && sha256sum "${final##*/}" > "${final##*/}.sha256")
trap - EXIT
if [[ -n "${ARTEX_BACKUP_REMOTE:-}" ]]; then
    [[ "$ARTEX_BACKUP_REMOTE" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*:/[a-zA-Z0-9_./-]+$ ]]
    remote_host=${ARTEX_BACKUP_REMOTE%%:*}
    remote_path=${ARTEX_BACKUP_REMOTE#*:}
    scp -B -o StrictHostKeyChecking=yes -- "$final" "$final.sha256" "$ARTEX_BACKUP_REMOTE/" >&2
    ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -- "$remote_host"         "cd '$remote_path' && sha256sum -c '${final##*/}.sha256'" >&2
fi
printf '%s\n' "$final"
