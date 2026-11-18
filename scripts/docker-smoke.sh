#!/usr/bin/env bash
# Smoke test image Docker TruSS: image dijalankan sungguhan bersama PostgreSQL 18 sementara.
#
# Pemakaian:  bash scripts/docker-smoke.sh [nama-image]     (default: truss-api:smoke)
# Prasyarat:  docker (engine berjalan) dan curl. Image dibangun lebih dulu: docker build -t truss-api:smoke .
#
# Yang diperiksa: image tidak memuat .env/.git/docs/test; HEALTHCHECK menjadi healthy; /healthz dan
# /readyz 200; alur register -> login -> workspace -> project -> task berjalan; image berjalan
# sebagai non-root; `docker stop` (SIGTERM) berhenti rapi dengan exit code 0.
# Semua kredensial dibuat acak per-run dan dibuang bersama container.
set -euo pipefail

IMAGE="${1:-truss-api:smoke}"
SUFFIX="$$"
NET="truss-smoke-$SUFFIX"
PG="truss-smoke-pg-$SUFFIX"
API="truss-smoke-api-$SUFFIX"
PORT="${SMOKE_PORT:-18090}"
BASE="http://127.0.0.1:$PORT"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cleanup() {
  docker rm -f "$API" "$PG" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "GAGAL: $*" >&2
  echo "--- log api (30 baris terakhir):" >&2
  docker logs "$API" 2>&1 | tail -30 >&2 || true
  exit 1
}
ok() { echo "OK    $*"; }

rand() { head -c 96 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
PG_PASSWORD="$(rand 24)"
JWT_SECRET="$(rand 48)"

# json_field <nama-field> : ambil nilai string PERTAMA dari JSON di stdin (cukup untuk respons kita).
json_field() { sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" | head -1; }

# ---------- isi image ----------
docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "image $IMAGE tidak ada; bangun dulu dengan docker build"
USER_CFG="$(docker image inspect -f '{{.Config.User}}' "$IMAGE")"
[ "$USER_CFG" = "nonroot:nonroot" ] || fail "image harus berjalan sebagai nonroot:nonroot, dapat '$USER_CFG'"
ok "image berjalan sebagai non-root ($USER_CFG)"

TMPC="$(docker create "$IMAGE")"
LISTING="$(docker export "$TMPC" | tar -t 2>/dev/null || true)"
docker rm -f "$TMPC" >/dev/null 2>&1 || true
# Here-string (<<<), bukan `echo | grep -q`: di bawah pipefail, grep -q yang keluar lebih awal
# bisa membuat echo mati karena SIGPIPE dan pemeriksaan gagal palsu.
grep -Eq '^app/api$' <<<"$LISTING" || fail "binary /app/api tidak ada di image"
# Hanya binary yang boleh ada di /app; dan di mana pun tidak boleh ada .env, .git, atau berkas test.
EXTRA="$(grep -E '^app/' <<<"$LISTING" | grep -vE '^app/(api)?$' || true)"
[ -z "$EXTRA" ] || fail "/app memuat berkas selain binary: $EXTRA"
if grep -Eiq '(^|/)(\.env(\..*)?|\.git)(/|$)|_test\.go$' <<<"$LISTING"; then
  fail "image memuat .env, .git, atau berkas test"
fi
ok "image hanya memuat binary di /app; tidak ada .env, .git, atau test"

# ---------- infrastruktur sementara ----------
docker network create "$NET" >/dev/null
docker run -d --name "$PG" --network "$NET" \
  -e POSTGRES_PASSWORD="$PG_PASSWORD" -e POSTGRES_DB=truss postgres:18 >/dev/null

# -h 127.0.0.1: server sementara saat inisialisasi hanya mendengarkan socket, jadi cek TCP
# memastikan server yang sebenarnya sudah siap.
for i in $(seq 1 60); do
  docker exec "$PG" pg_isready -h 127.0.0.1 -U postgres -d truss >/dev/null 2>&1 && break
  [ "$i" = 60 ] && fail "PostgreSQL tidak siap dalam 60 detik"
  sleep 1
done
for f in $(ls "$ROOT"/migrations/*.up.sql | sort); do
  docker exec -i "$PG" psql -U postgres -d truss -v ON_ERROR_STOP=1 -q < "$f" >/dev/null \
    || fail "migration gagal: $(basename "$f")"
done
ok "PostgreSQL 18 siap, semua migration diterapkan"

# ---------- jalankan image ----------
# Interval health dipercepat agar tidak menunggu 30 detik; nilai Dockerfile tetap yang asli.
docker run -d --name "$API" --network "$NET" -p "127.0.0.1:$PORT:8080" \
  --health-interval=2s --health-start-period=1s --health-retries=10 \
  -e DB_HOST="$PG" -e DB_PORT=5432 -e DB_USER=postgres -e DB_PASSWORD="$PG_PASSWORD" \
  -e DB_NAME=truss -e DB_SSLMODE=disable -e JWT_SECRET="$JWT_SECRET" \
  "$IMAGE" >/dev/null

STATUS=""
for _ in $(seq 1 60); do
  STATUS="$(docker inspect -f '{{.State.Health.Status}}' "$API" 2>/dev/null || echo "?")"
  [ "$STATUS" = "healthy" ] && break
  [ "$(docker inspect -f '{{.State.Running}}' "$API")" = "true" ] || fail "container berhenti sebelum healthy"
  sleep 1
done
[ "$STATUS" = "healthy" ] || fail "HEALTHCHECK tidak menjadi healthy (status: $STATUS)"
ok "HEALTHCHECK Docker: healthy"

[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" = "200" ] || fail "/healthz bukan 200"
[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/readyz")" = "200" ] || fail "/readyz bukan 200"
ok "/healthz dan /readyz mengembalikan 200"

# ---------- alur nyata ----------
JSON='Content-Type: application/json'
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

[ "$(code -X POST "$BASE/auth/register" -H "$JSON" \
  -d '{"name":"Smoke","email":"smoke@example.com","password":"password123"}')" = "201" ] || fail "register bukan 201"
TOKEN="$(curl -s -X POST "$BASE/auth/login" -H "$JSON" \
  -d '{"email":"smoke@example.com","password":"password123"}' | json_field access_token)"
[ -n "$TOKEN" ] || fail "login tidak menghasilkan access_token"
AUTH="Authorization: Bearer $TOKEN"

WS="$(curl -s -X POST "$BASE/workspaces" -H "$JSON" -H "$AUTH" -d '{"name":"WS Smoke"}' | json_field internal_id)"
[ -n "$WS" ] || fail "workspace tidak terbuat"
PR="$(curl -s -X POST "$BASE/workspaces/$WS/projects" -H "$JSON" -H "$AUTH" -d '{"name":"Proyek Smoke"}' | json_field internal_id)"
[ -n "$PR" ] || fail "project tidak terbuat"
TK_JSON="$(curl -s -X POST "$BASE/projects/$PR/tasks" -H "$JSON" -H "$AUTH" -d '{"title":"Task smoke","priority":"high"}')"
TK="$(echo "$TK_JSON" | json_field internal_id)"
{ [ -n "$TK" ] && [ "$TK" != "00000000-0000-0000-0000-000000000000" ]; } || fail "task terbuat dengan ID kosong: $TK_JSON"
GOT_TITLE="$(curl -s "$BASE/tasks/$TK" -H "$AUTH" | json_field title)"
[ "$GOT_TITLE" = "Task smoke" ] || fail "judul task terbaca '$GOT_TITLE', harapannya 'Task smoke'"
ok "register, login, workspace, project, task, dan baca task berjalan"

[ "$(code "$BASE/workspaces")" = "401" ] || fail "route privat tanpa token harus 401"
ok "route privat menolak request tanpa token"

# ---------- shutdown rapi ----------
docker stop -t 10 "$API" >/dev/null
EXIT_CODE="$(docker inspect -f '{{.State.ExitCode}}' "$API")"
[ "$EXIT_CODE" = "0" ] || fail "docker stop (SIGTERM) menghasilkan exit code $EXIT_CODE, harapannya 0 (shutdown rapi)"
LOGS="$(docker logs "$API" 2>&1)"
grep -q "server berhenti" <<<"$LOGS" || fail "log tidak memuat 'server berhenti'"
if grep -Eq "$JWT_SECRET|$PG_PASSWORD|password123" <<<"$LOGS"; then
  fail "log memuat rahasia (secret JWT, password database, atau password user)"
fi
if grep -q '/healthz\|/readyz' <<<"$LOGS"; then
  fail "probe kesehatan yang sukses tidak boleh memenuhi access log"
fi
ok "docker stop berhenti rapi (exit code 0)"

echo
echo "SMOKE TEST IMAGE LULUS"
