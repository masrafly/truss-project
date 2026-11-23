# TruSS — Development Guide

## 1. Local Setup

1. Clone repository.
2. Pastikan Go dan PostgreSQL terpasang.
3. Buat database development.
4. Salin `.env.example` menjadi `.env`.
5. Isi konfigurasi database dan JWT secret lokal.
6. Unduh dependency dengan `go mod tidy`.
7. Jalankan migration yang memang sudah tersedia dan ditinjau.
8. Jalankan server dengan `go run ./cmd/api`.

Jangan menaruh password database atau JWT secret asli di dokumentasi atau repository.

## 2. Configuration

Seluruh konfigurasi dibaca sekali oleh `internal/config/config.go`.

| Variable | Wajib | Description |
|---|---|---|
| `APP_ENV` | Tidak (default `development`) | `development` (log teks) atau `production` (log JSON, Gin release mode) |
| `APP_PORT` | Tidak (default `8080`) | Port HTTP server |
| `LOG_LEVEL` | Tidak (default `info`) | `debug`, `info`, `warn`, atau `error` |
| `DB_HOST` | Ya | Host PostgreSQL |
| `DB_PORT` | Ya | Port PostgreSQL (target: server PostgreSQL 18) |
| `DB_USER` | Ya | Database username |
| `DB_PASSWORD` | Ya | Database password |
| `DB_NAME` | Ya | Nama database |
| `DB_SSLMODE` | Ya | Mode SSL PostgreSQL |
| `JWT_SECRET` | Ya | Secret penandatanganan JWT, **minimal 32 byte**; aplikasi berhenti saat start jika kosong atau terlalu pendek |
| `TRUSTED_PROXIES` | Tidak | IP/CIDR reverse proxy yang dipercaya untuk `X-Forwarded-For`, dipisah koma. Kosong = tidak ada (aman untuk lokal) |
| `ACCESS_TOKEN_TTL` | Tidak | Masa berlaku access token, format durasi (`15m`, `1h`). Default `15m`, batas 1 menit sampai 24 jam |
| `REFRESH_TOKEN_TTL` | Tidak | Masa berlaku refresh token (`720h` = 30 hari). Default `720h`, batas 1 jam sampai 2160h, harus lebih lama daripada `ACCESS_TOKEN_TTL` |

Nilai TTL yang salah format atau di luar batas membuat aplikasi menolak start dengan pesan yang menyebut nama variabelnya. Variabel `JWT_EXPIRES_IN` yang mungkin masih ada di `.env` lama **tidak dipakai lagi**; hapus saja.

## 3. Clean Architecture Rules

Arah dependensi: `handler → usecase → domain`, dan `repository`/`platform` mengimplementasikan interface milik `usecase`.

| Layer | Do | Avoid |
|---|---|---|
| Domain | Entity dan error bisnis | Mengimpor Gin, GORM, JWT, bcrypt, atau package proyek lain |
| Use case | Aturan bisnis, otorisasi, orkestrasi, log peristiwa bisnis | Mengimpor Gin/GORM, mengatur status HTTP, memakai tipe konkret repository |
| Repository | Query GORM, mapping `record ↔ domain`, menerjemahkan error ke error domain | Aturan bisnis, status HTTP, logging |
| Handler | Bind request ke DTO, memanggil use case, mapping ke DTO response dan status HTTP | Query database, mengembalikan entity domain langsung |
| Platform | Implementasi teknis (bcrypt, JWT, DB, logger) | Aturan bisnis |
| Middleware | Pemeriksaan lintas endpoint (auth, request ID, log, recovery) | Menjalankan business flow |
| `cmd/api/main.go` | Merakit semua dependency | Logika bisnis |

## 4. Adding a New Module

Contoh: `workspaces`.

1. Buat `internal/modules/workspaces/{domain,usecase,repository,handler}`.
2. `domain`: entity dan error bisnis.
3. `usecase/ports.go`: interface yang dibutuhkan; `usecase/*.go`: logika bisnis. Terima `*slog.Logger` pada konstruktor.
4. `repository`: model persistence, mapper ke domain, dan penerjemahan error.
5. `handler`: DTO, handler Gin, dan `RegisterRoutes`.
6. `cmd/api/main.go`: rakit `repository → usecase → handler` lalu panggil `RegisterRoutes`.
7. Ambil `userID` dari `middleware.UserIDFromContext`; use case memeriksa hak akses user atas resource.

## 5. Error Handling Convention

- Definisikan error bisnis di `domain`.
- Repository menerjemahkan error teknis menjadi error domain.
- Gunakan `errors.Is` untuk mengenali sentinel errors.
- Handler memetakan error domain ke status HTTP.
- Untuk error tak terduga, handler mencatat detail di log (dengan `request_id`) dan mengirim pesan generik ke client.

Contoh kategori:
- Invalid input → 400.
- Duplicate email → 409.
- Invalid credentials/token → 401.
- Authenticated but forbidden → 403.
- Unexpected internal error → 500.

## 6. Logging Convention

- Gunakan `*slog.Logger` yang disuntikkan lewat konstruktor; jangan memakai `log.Println` atau `fmt.Println`.
- Gunakan field terstruktur: `log.InfoContext(ctx, "user terdaftar", "user_id", id)`.
- Catat error **satu kali**, di lapisan yang menanganinya (handler untuk error tak terduga). Repository tidak mencatat.
- Gunakan `request_id` agar log satu request dapat dilacak; header respons `X-Request-ID` dikirim ke client.
- Level: `Debug` detail teknis, `Info` peristiwa normal, `Warn` kegagalan yang diharapkan (login gagal, 4xx), `Error` kegagalan tak terduga.
- **Jangan** mencatat password, password hash, token, header `Authorization`, body request, atau email lengkap. Gunakan `user_id`.

## 7. Testing

Struktur Clean Architecture memudahkan pengujian per lapisan:

- **Use case**: unit test dengan fake `UserRepository`, `PasswordHasher`, dan `TokenIssuer`; tanpa database dan HTTP. Logger boleh `nil`.
- **Handler**: unit test dengan fake use case melalui `httptest`.
- **Repository**: integration test terhadap PostgreSQL.
- **Middleware**: test token valid, invalid, expired, dan hilang dengan fake `TokenValidator`.
- **Authorization**: test akses lintas workspace/project.

Manual API tests dapat dilakukan menggunakan Postman.

### Menjalankan test

```powershell
go test ./...
```

| Paket | Jenis | Catatan |
|---|---|---|
| `modules/auth/usecase` | Unit | Fake port, tanpa DB |
| `platform/security` | Unit | JWT: round-trip, claims, token kedaluwarsa/dipalsukan, algoritma `none`/HS512, secret berbeda |
| `middleware` | Unit | `AuthMiddleware`, `RequestID`, `RequestLogger`, `Recovery` (via `httptest`) |
| `modules/auth/handler` | Unit | Fake use case + `httptest`: status, DTO, pemetaan error, tidak ada kebocoran data |
| `modules/workspaces/usecase`, `handler` | Unit | Fake port; otorisasi keanggotaan, kebijakan 404, DTO, pemetaan error |
| `modules/workspaces/repository` | Integration | Menjalankan **semua** migration di schema sementara (`internal/testutil/pgtest`); di-skip tanpa `TEST_DB_HOST` |
| `modules/auth/repository` | Integration | PostgreSQL asli; **di-skip** bila `TEST_DB_HOST` kosong |

### Integration test repository

Test repository menjalankan `migrations/000001_create_users.up.sql` di schema sementara (`truss_test_<acak>`) yang dihapus setelah selesai, sehingga tabel yang sudah ada tidak tersentuh. Tetap gunakan database khusus test, bukan database production.

```powershell
$env:TEST_DB_HOST = "localhost"
$env:TEST_DB_PORT = "5432"          # default 5432
$env:TEST_DB_USER = "postgres"      # default postgres
$env:TEST_DB_PASSWORD = "change_me"
$env:TEST_DB_NAME = "truss_test"    # default postgres
$env:TEST_DB_SSLMODE = "disable"    # default disable
go test -v ./internal/modules/auth/repository
```

Gunakan PostgreSQL 18. `gen_random_uuid()` yang dipakai migration tersedia bawaan di PostgreSQL 13+.

Jika beberapa versi PostgreSQL terpasang bersamaan, pastikan `DB_PORT` (dan `TEST_DB_PORT`) mengarah ke server versi 18. Periksa dengan `SELECT version();`.

## 8. Git Practices

- Jangan commit `.env`, secret, token, atau credential.
- Gunakan `.env.example` untuk mendokumentasikan nama variabel.
- Buat commit kecil dan deskriptif.
- Tinjau migration sebelum diterapkan.
- Jangan mengubah migration yang sudah dipakai bersama secara sembarangan; buat migration baru untuk perubahan skema.

## 9. Suggested Commit Examples

```text
feat(auth): add register and login endpoints
refactor(auth): apply clean architecture
feat(logging): add structured logging with slog
feat(middleware): validate bearer token
feat(workspace): add workspace module
fix(auth): handle duplicate email
docs: update architecture documentation
```

## 10. Before Opening a Pull Request

- [ ] `gofmt` dijalankan.
- [ ] `go build ./...` dan `go vet ./...` berhasil.
- [ ] `go test ./...` berhasil.
- [ ] `domain` dan `usecase` tidak mengimpor `gin`, `gorm`, atau `jwt`.
- [ ] Tidak ada secret atau data sensitif di diff maupun di log.
- [ ] Error handling diperiksa.
- [ ] Migration ditinjau.
- [ ] API docs diperbarui.
- [ ] Authorization diperiksa untuk resource yang dilindungi.

## Membuat JWT_SECRET

`JWT_SECRET` wajib minimal 32 byte; aplikasi menolak start bila lebih pendek. Buat nilai acak yang kuat dengan generator bawaan proyek (memakai `crypto/rand`):

```powershell
go run ./cmd/genjwtsecret          # 48 byte acak -> 64 karakter
go run ./cmd/genjwtsecret -line    # langsung dalam bentuk JWT_SECRET=...
```

Keluarannya hanya berisi `A-Z a-z 0-9 - _`, jadi aman ditempel ke `.env` tanpa tanda kutip. Tempel hasilnya ke `JWT_SECRET=` di `.env` (pastikan hanya ada **satu** baris `JWT_SECRET`, lalu simpan). Mengganti secret membatalkan semua token lama (user perlu login ulang). Jangan memakai `Get-Random`, karena tidak dirancang untuk kriptografi.

## CI (GitHub Actions)

Workflow: `.github/workflows/ci.yml`, berjalan pada setiap push ke `main` dan setiap pull request.

| Job | Isi |
|---|---|
| `test` | `gofmt` (gagal bila ada file belum rapi), `go mod verify`, `go vet`, `go build`, lalu `go test -race` untuk **semua** test, termasuk integration test terhadap container PostgreSQL 18 |
| `vulncheck` | `govulncheck`: kerentanan yang diketahui pada dependensi dan pustaka standar Go |

Di CI, `TEST_DB_REQUIRED=1` membuat integration test **gagal** (bukan skip) bila database tidak tersedia, sehingga hasil hijau berarti test benar-benar menyentuh PostgreSQL. Di lokal tanpa variabel ini, integration test tetap di-skip seperti biasa.

Menjalankan pemeriksaan yang sama secara lokal (tanpa `-race`, yang butuh cgo):

```powershell
gofmt -l .        # harus kosong
go vet ./...
go test -count=1 ./...
```

## Versi Go dan pemeriksaan kerentanan

`go.mod` mensyaratkan **Go ≥ 1.27.4** (memuat perbaikan keamanan `net/http` dan `crypto/x509` yang ditemukan `govulncheck`). Dengan `GOTOOLCHAIN=auto` (default), Go yang lebih lama mengunduh toolchain yang sesuai secara otomatis.

Jalankan `govulncheck` secara lokal sebelum rilis:

```powershell
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Jika melaporkan kerentanan pada modul dependensi, naikkan modulnya (`go get <modul>@<versi-perbaikan>`, lalu `go mod tidy`). Kerentanan di pustaka standar diperbaiki dengan menaikkan versi `go` di `go.mod`.

## Menjalankan integration test lokal tanpa menyentuh database dev

Integration test membuat schema sementara, tetapi lebih aman memakai server PostgreSQL terpisah. Contoh klaster sementara (tanpa Docker; sesuaikan `PGBIN`), pada port 55432 dengan autentikasi `trust` yang hanya mendengarkan `127.0.0.1`:

```bash
PGBIN=/d/PostgreSQL18/bin; TMP=$(mktemp -d)
"$PGBIN/initdb.exe" -D "$TMP/data" -U postgres --auth=trust -E UTF8
"$PGBIN/pg_ctl.exe" -D "$TMP/data" -o "-p 55432 -c listen_addresses=127.0.0.1" -l "$TMP/pg.log" start
"$PGBIN/psql.exe" -h 127.0.0.1 -p 55432 -U postgres -c "create database truss_test"

export TEST_DB_HOST=127.0.0.1 TEST_DB_PORT=55432 TEST_DB_USER=postgres TEST_DB_NAME=truss_test TEST_DB_SSLMODE=disable
export TEST_DB_REQUIRED=1        # test GAGAL, bukan skip, bila database tidak tersedia
go test -count=1 -timeout 5m ./...

"$PGBIN/pg_ctl.exe" -D "$TMP/data" -m fast stop && rm -rf "$TMP"
```

Atau dengan Docker: `docker run -d -e POSTGRES_PASSWORD=x -e POSTGRES_DB=truss_test -p 127.0.0.1:55432:5432 postgres:18` (isi `TEST_DB_PASSWORD=x`).

Selalu isi `TEST_DB_REQUIRED=1` saat memverifikasi: tanpa itu, integration test yang tidak menemukan database di-skip dan `go test` tetap melaporkan `ok`.

## Pelajaran: GORM dan struct sematan

GORM hanya membaca field yang **diekspor**. Struct hasil query tidak boleh menyematkan tipe yang tidak diekspor (mis. `taskRecord`): semua kolomnya akan terbaca kosong **tanpa error**. `internal/modules/tasks/repository/schema_test.go` memeriksa ini tanpa database dan berjalan di setiap `go test`; tambahkan tipe baru ke test itu.

## Docker

`Dockerfile` membangun satu binary statis (multi-stage: `golang:1.27.4-alpine` → `gcr.io/distroless/static-debian12:nonroot`). Image berjalan sebagai user non-root, tanpa shell dan package manager, dan hanya berisi `/app/api`.

```powershell
docker build -t truss-api .
docker run --rm -p 8080:8080 --env-file .env -e DB_HOST=host.docker.internal truss-api
```

- **Konfigurasi hanya lewat environment variable** (lihat `.env.example`). `.env` tidak masuk image (`.dockerignore`). Dari dalam container, `localhost` adalah container itu sendiri: arahkan `DB_HOST` ke database yang dapat dijangkau (di Docker Desktop: `host.docker.internal`, atau nama service bila memakai jaringan Docker).
- **`APP_ENV=production` adalah default image** (log JSON, Gin release mode). Timpa dengan `-e APP_ENV=development` untuk log teks.
- **Skema database tidak dibuat oleh image.** Jalankan migration (`migrations/`) dengan golang-migrate sebelum aplikasi.
- **Endpoint health** (publik, tanpa autentikasi): `GET /healthz` (liveness, tidak menyentuh database) dan `GET /readyz` (readiness, memeriksa database; gagal = `503`). `HEALTHCHECK` Docker memakai `/app/api healthcheck` (binary itu sendiri sebagai klien HTTP, karena distroless tidak punya `curl`) ke `/healthz`. Sengaja bukan `/readyz`: gangguan database tidak boleh membuat container dianggap mati lalu di-restart terus; orkestrator memakai `/readyz` untuk mengalihkan trafik.
- **Shutdown rapi:** `docker stop` mengirim SIGTERM; server berhenti menerima koneksi baru dan memberi request berjalan waktu hingga 8 detik (`shutdownTimeout`). Naikkan `stop_grace_period` di orkestrator bila memperbesar nilai itu.
- **Versi Go di Dockerfile (`GO_VERSION`) harus sama dengan baris `go` di `go.mod`.** `GOTOOLCHAIN=local` membuat build gagal bila berbeda, bukan diam-diam memakai Go lain.

### Smoke test image

```powershell
docker build -t truss-api:smoke .
bash scripts/docker-smoke.sh truss-api:smoke
```

Skrip menjalankan image bersama PostgreSQL 18 sementara dengan kredensial acak, lalu memeriksa: isi image (tanpa `.env`, `.git`, test), non-root, `HEALTHCHECK` healthy, `/healthz` dan `/readyz`, alur register → login → workspace → project → task, route privat menolak tanpa token, log tanpa rahasia dan tanpa probe, serta `docker stop` berhenti rapi (exit code 0). Job `docker` di CI menjalankannya pada setiap push.

### Docker Compose (API + PostgreSQL 18 + migration)

Menjalankan seluruh stack lokal dengan satu perintah. Database yang dipakai adalah container baru berdata kosong, **terpisah** dari PostgreSQL lokal Anda (port 5432 tidak disentuh).

```powershell
copy .env.example .env                  # lalu isi nilainya (lihat di bawah)
go run ./cmd/genjwtsecret               # tempel hasilnya ke JWT_SECRET di .env
docker compose up -d --build            # db -> migration -> API
```

`.env` yang dibaca Compose:

| Variabel | Wajib | Keterangan |
|---|---|---|
| `DB_PASSWORD` | Ya | Password database baru. **Hanya huruf dan angka**: dipakai apa adanya di URL migration, sehingga karakter seperti `@`, `/`, `:`, atau `#` merusaknya |
| `JWT_SECRET` | Ya | Minimal 32 byte acak |
| `DB_USER`, `DB_NAME` | Tidak | Default `truss` dan `truss_db` |
| `APP_PORT` | Tidak | Port API di host (default 8080) |
| `DB_HOST_PORT` | Tidak | Port database di host untuk `psql`/DBeaver (default 55432) |
| `APP_ENV`, `LOG_LEVEL`, `TRUSTED_PROXIES` | Tidak | Diteruskan ke API (default `development`, `info`, kosong) |
| `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL` | Tidak | Diteruskan ke API (default `15m` dan `720h`) |

`DB_HOST` dan `DB_PORT` di `.env` **diabaikan**: di dalam jaringan Compose, database selalu `db:5432`. Bila `compose up` gagal di langkah migration dengan error koneksi/URL, periksa dulu apakah `DB_PASSWORD` memuat karakter selain huruf dan angka.

Perintah sehari-hari:

```powershell
docker compose logs -f api                      # log API
docker compose ps                               # status service
docker compose down                             # berhenti; data TETAP (volume pgdata)
docker compose down -v                          # berhenti DAN hapus data database
docker compose run --rm migrate                 # terapkan migration baru (aman diulang: "no change")
psql -h 127.0.0.1 -p 55432 -U truss truss_db    # masuk ke database (sesuaikan user/nama)
```

Catatan:
- Port API dan database hanya terikat ke `127.0.0.1`, tidak terbuka ke jaringan. Hapus awalan `127.0.0.1:` di `docker-compose.yml` bila memang perlu.
- **PostgreSQL 18 memasang volume di `/var/lib/postgresql`** (bukan `.../data` seperti versi lama); `PGDATA` berada di subfolder versi. Jangan mengubahnya ke path lama.
- Penambahan migration baru: letakkan berkasnya di `migrations/`, lalu `docker compose run --rm migrate`. Container `api` tidak perlu dibangun ulang.
- Perubahan kode: `docker compose up -d --build api`.
- Ini konfigurasi **lokal/staging sederhana**, bukan produksi: database satu node tanpa replikasi dan backup, SSL internal dimatikan (`DB_SSLMODE=disable`).
- `scripts/compose-smoke.sh` menguji seluruh stack, termasuk data yang bertahan setelah `down` lalu `up`. Job `compose` di CI menjalankannya.
