#!/usr/bin/env bash
# Store the SMTP settings in .env without echoing the password or adding it to shell history.
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
ask() { # prompt, default
  local answer
  read -r -p "$1 [$2]: " answer </dev/tty
  printf '%s' "${answer:-$2}"
}
smtp_host="$(ask 'SMTP host' '')"
smtp_port="$(ask 'SMTP port (587 = STARTTLS, 465 = TLS)' '587')"
default_mode=starttls
[[ "$smtp_port" == 465 ]] && default_mode=implicit
smtp_mode="$(ask 'Encryption: starttls or implicit' "$default_mode")"
smtp_from="$(ask 'Sender address' 'noreply-support@vproxy.cloud')"
smtp_user="$(ask 'SMTP username' "$smtp_from")"
read -r -s -p 'SMTP password: ' smtp_password </dev/tty
printf '\n' >/dev/tty
if [[ -z "$smtp_host" || -z "$smtp_password" ]]; then
  echo 'Host and password cannot be empty.' >&2
  exit 1
fi
printf '%s' "$smtp_password" | SMTP_HOST="$smtp_host" SMTP_PORT="$smtp_port" SMTP_TLS_MODE="$smtp_mode" SMTP_FROM="$smtp_from" SMTP_USERNAME="$smtp_user" python3 -c '
import os
import pathlib
import re
import sys
import tempfile

path = pathlib.Path(sys.argv[1])
values = {key: os.environ[key] for key in ("SMTP_HOST", "SMTP_PORT", "SMTP_TLS_MODE", "SMTP_FROM", "SMTP_USERNAME")}
values["SMTP_PASSWORD"] = sys.stdin.read()
if not re.fullmatch(r"[A-Za-z0-9.-]+", values["SMTP_HOST"]):
    raise SystemExit("SMTP host must be a host name.")
if not values["SMTP_PORT"].isdigit() or not 0 < int(values["SMTP_PORT"]) < 65536:
    raise SystemExit("SMTP port must be a number.")
if values["SMTP_TLS_MODE"] not in ("starttls", "implicit"):
    raise SystemExit("Encryption must be starttls or implicit.")
if not re.fullmatch(r"[^@\s]+@[^@\s]+\.[^@\s]+", values["SMTP_FROM"]):
    raise SystemExit("Sender must be one email address.")
for key, value in values.items():
    # Values are single-quoted in .env, so they cannot contain a single quote.
    if any(c in value for c in "\r\n\x00\047"):
        raise SystemExit(key + " contains a character unsupported by this installer.")
lines = path.read_text().splitlines(keepends=True)
for key, value in values.items():
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
unset smtp_password
echo 'SMTP settings saved in .env.'
echo 'Apply them with: docker compose up -d --no-build app'
echo 'Then send a test email from 管理設定 → Email 通知.'
