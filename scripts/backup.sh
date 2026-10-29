#!/usr/bin/env bash
# Consistent, local production backup. Restart the app even when backup fails.
set -euo pipefail
umask 077
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
backup_dir="${1:-$project_dir/backup/$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$backup_dir"
if [[ -e "$backup_dir/database.dump" || -e "$backup_dir/attachments.tar.gz" ]]; then echo 'Refusing to overwrite an existing backup.' >&2; exit 1; fi
compose=(docker compose -f compose.yaml)
app_running="$("${compose[@]}" ps --status running -q app)"
resume() { if [[ -n "$app_running" ]]; then "${compose[@]}" start app; fi; }
trap resume EXIT
"${compose[@]}" stop app
"${compose[@]}" exec -T db pg_dump -U support -d support -Fc > "$backup_dir/database.dump.partial"
"${compose[@]}" run --rm -T --no-deps --entrypoint tar app -C /data/attachments -czf - . > "$backup_dir/attachments.tar.gz.partial"
"${compose[@]}" exec -T db pg_restore --list < "$backup_dir/database.dump.partial" >/dev/null
tar -tzf "$backup_dir/attachments.tar.gz.partial" >/dev/null
mv "$backup_dir/database.dump.partial" "$backup_dir/database.dump"
mv "$backup_dir/attachments.tar.gz.partial" "$backup_dir/attachments.tar.gz"
(cd "$backup_dir" && sha256sum database.dump attachments.tar.gz > SHA256SUMS)
printf 'Backup verified: %s\n' "$backup_dir"
