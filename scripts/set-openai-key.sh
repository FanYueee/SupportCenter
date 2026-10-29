#!/usr/bin/env bash
# Store the API key without adding it to shell history or printing it.
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
read -r -s -p 'OpenAI API key: ' openai_key </dev/tty
printf '\n' >/dev/tty
if [[ -z "$openai_key" ]]; then
  echo 'API key cannot be empty.' >&2
  exit 1
fi
printf '%s' "$openai_key" | python3 -c '
import os
import pathlib
import sys
import tempfile

path = pathlib.Path(sys.argv[1])
secret = sys.stdin.read()
if any(c in secret for c in "\r\n\x00\047\\"):
    raise SystemExit("API key contains a character unsupported by this installer.")
lines = path.read_text().splitlines(keepends=True)
for key, value in (("OPENAI_API_KEY", "\047" + secret + "\047"), ("AI_AUTO_REPLY_ENABLED", "true")):
    matches = [i for i, line in enumerate(lines) if line.startswith(key + "=")]
    if len(matches) > 1:
        raise SystemExit("Duplicate " + key + " entries in .env")
    replacement = key + "=" + value + "\n"
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
unset openai_key
echo 'OpenAI key saved; AI automatic replies enabled in .env.'
echo 'Restart the app container to apply the setting: docker compose up -d --no-deps app'
