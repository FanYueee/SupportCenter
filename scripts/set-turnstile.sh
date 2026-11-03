#!/usr/bin/env bash
# Store the Cloudflare Turnstile keys for the new ticket form without adding
# the secret to shell history or printing it.
set -euo pipefail
umask 077
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="$project_dir/.env"
if [[ ! -f "$env_file" ]]; then
  echo 'Missing .env; prepare the production environment first.' >&2
  exit 1
fi
if [[ ! -r /dev/tty ]]; then
  echo 'Run this script from an interactive VPS terminal.' >&2
  exit 1
fi
read -r -p 'Turnstile site key: ' site_key </dev/tty
read -r -s -p 'Turnstile secret key: ' secret_key </dev/tty
printf '\n' >/dev/tty
for value in "$site_key" "$secret_key"; do
  if [[ ! "$value" =~ ^[A-Za-z0-9_-]{10,200}$ ]]; then
    echo 'Keys may only contain letters, digits, "-" and "_"; copy them from the Cloudflare dashboard.' >&2
    exit 1
  fi
done
printf '%s\n%s' "$site_key" "$secret_key" | python3 -c '
import os
import pathlib
import sys
import tempfile

path = pathlib.Path(sys.argv[1])
site_key, secret_key = sys.stdin.read().split("\n")
lines = path.read_text().splitlines(keepends=True)
for key, value in (("TURNSTILE_SITE_KEY", site_key), ("TURNSTILE_SECRET_KEY", secret_key)):
    matches = [i for i, line in enumerate(lines) if line.startswith(key + "=")]
    if len(matches) > 1:
        raise SystemExit("Duplicate " + key + " entries in .env")
    replacement = key + "=\047" + value + "\047\n"
    if matches:
        lines[matches[0]] = replacement
    else:
        lines.append(replacement)
fd, name = tempfile.mkstemp(prefix=".env.", dir=path.parent)
try:
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w") as out:
        out.writelines(lines)
    os.replace(name, path)
except BaseException:
    os.unlink(name)
    raise
' "$env_file"
unset secret_key
echo 'Turnstile keys saved in .env.'
echo 'Restart the app container to apply them: docker compose up -d --no-deps app'
