#!/usr/bin/env bash
# Enter the Auth0 Client Secret on the VPS without putting it in shell history.
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
read -r -s -p 'Auth0 Client Secret: ' auth0_secret </dev/tty
printf '\n' >/dev/tty
if [[ -z "$auth0_secret" ]]; then
  echo 'Client Secret cannot be empty.' >&2
  exit 1
fi
printf '%s' "$auth0_secret" | python3 -c '
import os
import pathlib
import sys
import tempfile

path = pathlib.Path(sys.argv[1])
secret = sys.stdin.read()
if any(c in secret for c in "\r\n\x00\047\\"):
    raise SystemExit("Client Secret contains a character unsupported by this installer; use a URL-safe Auth0 secret.")
data = path.read_text()
lines = data.splitlines(keepends=True)
matches = [i for i, line in enumerate(lines) if line.startswith("AUTH0_CLIENT_SECRET=")]
if len(matches) != 1:
    raise SystemExit("Expected exactly one AUTH0_CLIENT_SECRET entry in .env")
lines[matches[0]] = "AUTH0_CLIENT_SECRET=\047" + secret + "\047\n"
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
unset auth0_secret
echo 'Auth0 Client Secret saved to .env (mode 600).'
