#!/usr/bin/env bash
# Smoke test docker-compose.yml: seluruh stack (PostgreSQL 18 + migration + API) dijalankan
# sungguhan dengan kredensial acak dan project Compose terisolasi, lalu dibuang.
#
# Pemakaian:  bash scripts/compose-smoke.sh
# Prasyarat:  docker (engine berjalan), plugin docker compose, curl.
#
# Yang diperiksa: migration selesai (versi 10, tidak dirty) dan idempoten; API sehat; alur
# register -> login -> workspace -> project -> task; port hanya di loopback; data database
# BERTAHAN setelah `down` lalu `up` (membuktikan mount volume PostgreSQL 18 benar); `stop`
# berhenti rapi (exit code 0). .env milik developer TIDAK dibaca (env-file kosong).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PROJECT="truss-smoke-$$"
EMPTY_ENV="$(mktemp)"
rand() { head -c 96 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "$1"; }

# Variabel untuk docker-compose.yml. Password HANYA huruf/angka (dipakai di URL migration).
export DB_USER=truss DB_NAME=truss_db
export DB_PASSWORD="$(rand 24)"
export JWT_SECRET="$(rand 48)"
export APP_PORT="${SMOKE_PORT:-18091}"
export DB_HOST_PORT="${SMOKE_DB_PORT:-55499}"
export APP_ENV=production
BASE="http://127.0.0.1:$APP_PORT"

dc() { docker compose -p "$PROJECT" --env-file "$EMPTY_ENV" "$@"; }

cleanup() {
  dc down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$EMPTY_ENV"
}
trap cleanup EXIT

fail() {
  echo "GAGAL: $*" >&2
  echo "--- status service:" >&2
  dc ps -a >&2 || true
  echo "--- log (40 baris terakhir per service):" >&2
  dc logs --tail 40 >&2 || true
  exit 1
}
ok() { echo "OK    $*"; }

json_field() { sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" | head -1; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

wait_ready() { # wait_ready <detik>
  for _ in $(seq 1 "$1"); do
    [ "$(code --max-time 3 "$BASE/readyz" || true)" = "200" ] && return 0
    sleep 1
  done
  return 1
}

# ---------- naik ----------
dc up -d --build >/dev/null || fail "docker compose up gagal"
wait_ready 120 || fail "API tidak siap (/readyz bukan 200) dalam 120 detik"
ok "stack naik; /readyz 200"

# ---------- migration ----------
MIG_ID="$(dc ps -a -q migrate)"
[ -n "$MIG_ID" ] || fail "service migrate tidak ditemukan"
[ "$(docker inspect -f '{{.State.ExitCode}}' "$MIG_ID")" = "0" ] || fail "migration tidak selesai dengan exit code 0"
VERSION="$(dc exec -T db psql -U "$DB_USER" -d "$DB_NAME" -tAc 'select version || chr(124) || dirty from schema_migrations')"
[ "$VERSION" = "10|f" ] || fail "schema_migrations = '$VERSION', harapannya '10|f' (versi 10, tidak dirty)"
ok "migration selesai: versi 10, tidak dirty"

RERUN="$(dc run --rm migrate 2>&1)" || fail "menjalankan migration ulang gagal: $RERUN"
grep -q "no change" <<<"$RERUN" || fail "migration ulang harus 'no change' (idempoten), dapat: $RERUN"
ok "migration idempoten (tidak ada perubahan saat diulang)"

# ---------- port hanya loopback ----------
case "$(dc port api 8080)" in
  127.0.0.1:*) ok "API hanya terikat ke 127.0.0.1" ;;
  *) fail "port API tidak terikat ke loopback: $(dc port api 8080)" ;;
esac
case "$(dc port db 5432)" in
  127.0.0.1:*) ok "database hanya terikat ke 127.0.0.1" ;;
  *) fail "port database tidak terikat ke loopback: $(dc port db 5432)" ;;
esac

# ---------- alur nyata (container API: read-only, tanpa capability) ----------
JSON='Content-Type: application/json'
[ "$(code -X POST "$BASE/auth/register" -H "$JSON" \
  -d '{"name":"Compose","email":"compose@example.com","password":"password123"}')" = "201" ] || fail "register bukan 201"
login() {
  curl -s -X POST "$BASE/auth/login" -H "$JSON" -d '{"email":"compose@example.com","password":"password123"}' | json_field access_token
}
TOKEN="$(login)"
[ -n "$TOKEN" ] || fail "login tidak menghasilkan token"
AUTH="Authorization: Bearer $TOKEN"
WS="$(curl -s -X POST "$BASE/workspaces" -H "$JSON" -H "$AUTH" -d '{"name":"WS Compose"}' | json_field internal_id)"
PR="$(curl -s -X POST "$BASE/workspaces/$WS/projects" -H "$JSON" -H "$AUTH" -d '{"name":"Proyek Compose"}' | json_field internal_id)"
TK="$(curl -s -X POST "$BASE/projects/$PR/tasks" -H "$JSON" -H "$AUTH" -d '{"title":"Task compose"}' | json_field internal_id)"
{ [ -n "$WS" ] && [ -n "$PR" ] && [ -n "$TK" ]; } || fail "workspace/project/task tidak terbuat"
[ "$(curl -s "$BASE/tasks/$TK" -H "$AUTH" | json_field title)" = "Task compose" ] || fail "judul task tidak terbaca benar"
ok "alur register -> login -> workspace -> project -> task berjalan"

# ---------- data bertahan setelah down lalu up (volume PostgreSQL 18) ----------
dc down >/dev/null || fail "docker compose down gagal"      # TANPA -v: volume dipertahankan
dc up -d >/dev/null || fail "docker compose up (kedua) gagal"
wait_ready 120 || fail "API tidak siap setelah down/up"
TOKEN2="$(login)"
[ -n "$TOKEN2" ] || fail "user hilang setelah down/up: data database tidak bertahan (cek mount volume PostgreSQL 18)"
[ "$(curl -s "$BASE/tasks/$TK" -H "Authorization: Bearer $TOKEN2" | json_field title)" = "Task compose" ] \
  || fail "task hilang setelah down/up"
ok "data bertahan setelah down lalu up (volume benar)"

# ---------- shutdown rapi ----------
API_ID="$(dc ps -q api)"
dc stop api >/dev/null
[ "$(docker inspect -f '{{.State.ExitCode}}' "$API_ID")" = "0" ] || fail "API berhenti dengan exit code bukan 0 (shutdown tidak rapi)"
ok "stop berhenti rapi (exit code 0)"

echo
echo "SMOKE TEST COMPOSE LULUS"
