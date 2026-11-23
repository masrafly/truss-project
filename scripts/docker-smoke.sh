#!/usr/bin/env bash
# Smoke test image Docker TruSS: image dijalankan sungguhan bersama PostgreSQL 18 sementara.
#
# Pemakaian:  bash scripts/docker-smoke.sh [nama-image]     (default: truss-api:smoke)
# Prasyarat:  docker (engine berjalan) dan curl. Image dibangun lebih dulu: docker build -t truss-api:smoke .
#
# Yang diperiksa: image tidak memuat .env/.git/docs/test; HEALTHCHECK menjadi healthy; /healthz dan
# /readyz 200; production menolak DB tanpa TLS; header keamanan dan CORS; alur register -> login -> workspace -> project -> task berjalan; refresh token
# (rotasi, pemakaian ulang ditolak, logout); ubah nama dan ganti password; image berjalan
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
  # Di GitHub Actions, tampilkan juga sebagai anotasi error di halaman ringkasan run.
  if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::error title=GAGAL::$*"; fi
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

# ---------- hardening produksi: image (APP_ENV=production) menolak database tanpa TLS ----------
REFUSE="$(docker run --rm --network none -e JWT_SECRET="$JWT_SECRET" -e DB_HOST=db.invalid -e DB_USER=u \
  -e DB_PASSWORD=p -e DB_NAME=d -e DB_SSLMODE=disable "$IMAGE" 2>&1)" && REFUSE_CODE=0 || REFUSE_CODE=$?
[ "$REFUSE_CODE" != "0" ] || fail "production dengan DB_SSLMODE=disable seharusnya GAGAL start"
grep -q "DB_SSLMODE" <<<"$REFUSE" || fail "pesan penolakan tidak menyebut DB_SSLMODE: $REFUSE"
ok "production menolak DB_SSLMODE tanpa TLS (exit $REFUSE_CODE)"

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
  -e DB_NAME=truss -e DB_SSLMODE=disable -e ALLOW_INSECURE_DB=true \
  -e CORS_ALLOWED_ORIGINS=https://app.example.com -e JWT_SECRET="$JWT_SECRET" \
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

# ---------- header keamanan dan CORS ----------
HDRS="$(curl -s -D - -o /dev/null "$BASE/healthz" -H 'Origin: https://app.example.com' | tr -d '\r')"
for want in 'x-content-type-options: nosniff' 'x-frame-options: DENY' 'cache-control: no-store' \
  'access-control-allow-origin: https://app.example.com'; do
  grep -qi "^$want" <<<"$HDRS" || fail "header '$want' tidak ada pada respons: $HDRS"
done
if grep -qi '^strict-transport-security' <<<"$HDRS"; then fail "HSTS tidak boleh dikirim pada HTTP biasa"; fi
[ "$(curl -s -o /dev/null -w '%{http_code}' -X OPTIONS "$BASE/workspaces" -H 'Origin: https://app.example.com' -H 'Access-Control-Request-Method: GET')" = "204" ] \
  || fail "preflight dari origin terdaftar harus 204 (tanpa Authorization)"
[ "$(curl -s -o /dev/null -w '%{http_code}' -X OPTIONS "$BASE/workspaces" -H 'Origin: https://evil.example.com' -H 'Access-Control-Request-Method: GET')" = "403" ] \
  || fail "preflight dari origin tidak terdaftar harus 403"
EVIL="$(curl -s -D - -o /dev/null "$BASE/healthz" -H 'Origin: https://evil.example.com' | tr -d '\r')"
if grep -qi '^access-control-allow-origin' <<<"$EVIL"; then fail "origin tidak terdaftar tidak boleh mendapat Access-Control-Allow-Origin"; fi
ok "header keamanan, CORS origin terdaftar diizinkan, origin lain ditolak"

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

# ---------- refresh token ----------
LOGIN_BODY="$(curl -s -X POST "$BASE/auth/login" -H "$JSON" -d '{"email":"smoke@example.com","password":"password123"}')"
RT1="$(echo "$LOGIN_BODY" | json_field refresh_token)"
[ -n "$RT1" ] || fail "login tidak menghasilkan refresh_token"
REFRESH_BODY="$(curl -s -X POST "$BASE/auth/refresh" -H "$JSON" -d "{\"refresh_token\":\"$RT1\"}")"
RT2="$(echo "$REFRESH_BODY" | json_field refresh_token)"
AT2="$(echo "$REFRESH_BODY" | json_field access_token)"
{ [ -n "$RT2" ] && [ "$RT2" != "$RT1" ] && [ -n "$AT2" ]; } || fail "refresh tidak menghasilkan pasangan token baru: $REFRESH_BODY"
[ "$(code "$BASE/auth/me" -H "Authorization: Bearer $AT2")" = "200" ] || fail "access token hasil refresh tidak diterima"
[ "$(code -X POST "$BASE/auth/refresh" -H "$JSON" -d "{\"refresh_token\":\"$RT1\"}")" = "401" ]   || fail "refresh token lama (sudah dirotasi) harus ditolak 401"
[ "$(code -X POST "$BASE/auth/refresh" -H "$JSON" -d "{\"refresh_token\":\"$RT2\"}")" = "401" ]   || fail "pemakaian ulang token lama harus mencabut seluruh rantai (token terbaru juga 401)"
RT3="$(curl -s -X POST "$BASE/auth/login" -H "$JSON" -d '{"email":"smoke@example.com","password":"password123"}' | json_field refresh_token)"
[ "$(code -X POST "$BASE/auth/logout" -H "$JSON" -d "{\"refresh_token\":\"$RT3\"}")" = "204" ] || fail "logout bukan 204"
[ "$(code -X POST "$BASE/auth/refresh" -H "$JSON" -d "{\"refresh_token\":\"$RT3\"}")" = "401" ]   || fail "refresh token harus mati setelah logout"
ok "refresh token: rotasi, pemakaian ulang ditolak, logout mencabut"

# ---------- profil dan ganti password ----------
[ "$(code -X PATCH "$BASE/auth/me" -H "$JSON" -H "$AUTH" -d '{"name":"Smoke Baru"}')" = "200" ] || fail "ubah nama bukan 200"
[ "$(curl -s "$BASE/auth/me" -H "$AUTH" | json_field name)" = "Smoke Baru" ] || fail "nama baru tidak terbaca di /auth/me"
SESSION="$(curl -s -X POST "$BASE/auth/login" -H "$JSON" -d '{"email":"smoke@example.com","password":"password123"}' | json_field refresh_token)"
[ -n "$SESSION" ] || fail "login untuk uji ganti password tidak menghasilkan refresh_token"
[ "$(code -X POST "$BASE/auth/change-password" -H "$JSON" -H "$AUTH" -d '{"current_password":"salahsalah","new_password":"passwordBaru1"}')" = "403" ]   || fail "password saat ini salah harus 403"
[ "$(code -X POST "$BASE/auth/change-password" -H "$JSON" -H "$AUTH" -d '{"current_password":"password123","new_password":"passwordBaru1"}')" = "204" ]   || fail "ganti password bukan 204"
[ "$(code -X POST "$BASE/auth/refresh" -H "$JSON" -d "{\"refresh_token\":\"$SESSION\"}")" = "401" ]   || fail "sesi lama harus mati setelah ganti password"
[ "$(code -X POST "$BASE/auth/login" -H "$JSON" -d '{"email":"smoke@example.com","password":"password123"}')" = "401" ]   || fail "password lama harus ditolak setelah diganti"
[ "$(code -X POST "$BASE/auth/login" -H "$JSON" -d '{"email":"smoke@example.com","password":"passwordBaru1"}')" = "200" ]   || fail "password baru harus bisa login"
ok "ubah nama, ganti password (403 bila salah), sesi lama dicabut, password baru berlaku"

# ---------- shutdown rapi ----------
docker stop -t 10 "$API" >/dev/null
EXIT_CODE="$(docker inspect -f '{{.State.ExitCode}}' "$API")"
[ "$EXIT_CODE" = "0" ] || fail "docker stop (SIGTERM) menghasilkan exit code $EXIT_CODE, harapannya 0 (shutdown rapi)"
LOGS="$(docker logs "$API" 2>&1)"
grep -q "server berhenti" <<<"$LOGS" || fail "log tidak memuat 'server berhenti'"
if grep -Eq "$JWT_SECRET|$PG_PASSWORD|password123|passwordBaru1" <<<"$LOGS"; then
  fail "log memuat rahasia (secret JWT, password database, atau password user)"
fi
if grep -q '/healthz\|/readyz' <<<"$LOGS"; then
  fail "probe kesehatan yang sukses tidak boleh memenuhi access log"
fi
ok "docker stop berhenti rapi (exit code 0)"

echo
echo "SMOKE TEST IMAGE LULUS"
