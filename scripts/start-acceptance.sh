#!/usr/bin/env bash
# Start isolated acceptance services. The HTTPS ingress is managed by the VPS owner.
set -euo pipefail
umask 077
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
for tool in docker openssl; do
  command -v "$tool" >/dev/null || { printf 'Missing: %s\n' "$tool" >&2; exit 1; }
done

if [[ -f .env.acceptance ]]; then
  acceptance_password="$(sed -n 's/^ACCEPTANCE_PASSWORD=//p' .env.acceptance)"
  database_password="$(sed -n 's/^POSTGRES_PASSWORD=//p' .env.acceptance)"
  configured_url="$(sed -n 's/^APP_URL=//p' .env.acceptance)"
  if [[ ! "$acceptance_password" =~ ^[a-f0-9]{48}$ || ! "$database_password" =~ ^[a-f0-9]{64}$ ]]; then
    printf 'Unexpected .env.acceptance format. File left unchanged.\n' >&2
    exit 1
  fi
else
  acceptance_password="$(openssl rand -hex 24)"
  database_password="$(openssl rand -hex 32)"
  configured_url=https://acceptance.invalid
fi
acceptance_url="${APP_URL:-$configured_url}"
if [[ -z "$acceptance_url" ]]; then acceptance_url=https://acceptance.invalid; fi
if [[ ! "$acceptance_url" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]]; then
  printf 'APP_URL must be an HTTPS origin without a path.\n' >&2
  exit 1
fi
printf 'APP_URL=%s\nACCEPTANCE_PASSWORD=%s\nPOSTGRES_PASSWORD=%s\n' "$acceptance_url" "$acceptance_password" "$database_password" > .env.acceptance.next
mv .env.acceptance.next .env.acceptance
chmod 600 .env.acceptance
compose=(docker compose -p supportcenter-acceptance -f compose.acceptance.yaml --env-file .env.acceptance)
"${compose[@]}" config --quiet
"${compose[@]}" build app
"${compose[@]}" --profile verify run --rm verify

if [[ "$acceptance_url" == https://acceptance.invalid ]]; then
  printf 'Database checks passed. Configure APP_URL in .env.acceptance, then rerun this script.\n'
  printf 'Point your own HTTPS proxy or tunnel to http://127.0.0.1:8082.\n'
  exit 0
fi
"${compose[@]}" up -d --no-build --wait --wait-timeout 180 app
printf 'Acceptance app is available locally at http://127.0.0.1:8082.\n'
printf 'External origin: %s\n' "$acceptance_url"
if ! command -v curl >/dev/null; then
  printf 'Install curl and run npm run test:acceptance after HTTPS is reachable.\n'
  exit 0
fi
public_ready=false
for ((attempt=0; attempt<10; attempt++)); do
  if curl -fsS --max-time 5 "$acceptance_url/login" 2>/dev/null | grep '功能驗收登入' >/dev/null; then
    public_ready=true
    break
  fi
  sleep 2
done
if [[ "$public_ready" != true ]]; then
  printf 'The external HTTPS URL is not reachable yet; services remain running.\n' >&2
  printf 'Once your proxy or tunnel is ready, run npm run test:acceptance.\n' >&2
  exit 1
fi
for tool in npm npx; do
  command -v "$tool" >/dev/null || { printf 'Missing: %s\n' "$tool" >&2; exit 1; }
done
npm ci
npx playwright install chromium
npm run test:acceptance
printf 'PostgreSQL integration and browser workflow checks passed.\n'
printf 'Acceptance password is stored only in .env.acceptance (mode 600).\n'
