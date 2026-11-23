# TruSS — Development Roadmap

## Status Legend

- **Done**: telah diimplementasikan dan/atau diuji berdasarkan progres proyek yang diketahui.
- **Next**: kandidat pekerjaan berikutnya.
- **Planned**: belum dianggap tersedia sampai implementasi dan pengujian selesai.

## Phase 1 — Backend Foundation

- [x] Go project and module setup.
- [x] Gin HTTP server.
- [x] PostgreSQL connection through GORM.
- [x] User model and repository discussed/created.
- [x] Refactor ke Clean Architecture (`domain`, `usecase`, `repository`, `handler`) untuk modul auth.
- [x] Composition root di `cmd/api/main.go` dan konfigurasi terpusat (`internal/config`).
- [x] Structured logging (`slog`), request ID, access log, dan panic recovery.
- [ ] Pindahkan model GORM lama (`internal/model`) ke modul masing-masing saat fiturnya dikerjakan.
- [x] Verify all SQL migrations match `internal_id` convention and actual models (lihat DATABASE.md bagian 7).
- [x] Migration `000001`–`000008` diterapkan di PostgreSQL 18 (`schema_migrations`: version 8, dirty = false).
- [x] Migration `000009` (nama tabel jamak) dan `000010` (role owner project, title task 200) diterapkan di PostgreSQL 18.
- [ ] Add automated tests for database and repository behavior.

## Phase 2 — Authentication

- [x] Register endpoint.
- [x] Duplicate email handling.
- [x] Input validation for register/login.
- [x] bcrypt password hashing and verification.
- [x] Login endpoint.
- [x] JWT generation.
- [x] JWT validation helper.
- [x] JWT middleware.
- [x] Protected `/auth/me` endpoint (mengembalikan profil user).
- [x] Refresh token (opaque, tersimpan ter-hash, rotasi sekali pakai, deteksi pemakaian ulang), `POST /auth/refresh`, `/auth/logout`, `/auth/logout-all`; access token 15 menit (`ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL`).
- [ ] Pembersihan terjadwal refresh token kedaluwarsa dari user yang tidak pernah login lagi (saat ini dibersihkan per user ketika login).
- [x] Unit test use case auth dengan fake port (`go test ./internal/modules/auth/usecase`).
- [x] Validasi password (min 8 karakter, maks 72 byte) dipindah ke use case.
- [x] Proteksi timing login (email tak terdaftar tetap menjalankan perbandingan bcrypt).
- [x] Unit test JWT manager dan middleware (auth, request ID, logging, recovery).
- [x] Integration test repository (skip otomatis tanpa `TEST_DB_HOST`; sudah dijalankan terhadap PostgreSQL lokal dan lulus).
- [x] Unit test handler auth (register, login, `/auth/me`) dengan fake use case.
- [x] Rate limiting and login abuse protection (per IP 20/menit + per akun 10 gagal/15 menit; in-memory per instance).
- [ ] Decide access-token lifetime and refresh-token strategy.
- [ ] Decide logout/revocation strategy if required.

## Phase 3 — User Profile

- [x] Lihat profil: `GET /auth/me` (dipakai sebagai pengganti `GET /users/me`).
- [x] Ubah nama: `PATCH /auth/me` (validasi trim, 1 sampai 100 karakter).
- [x] Ganti password: `POST /auth/change-password` (verifikasi password lama, dibatasi per user, mencabut semua refresh token).
- [x] Test use case, handler, dan repository (PostgreSQL 18); 6 uji mutasi tertangkap.
- [ ] Ubah email (butuh verifikasi email; belum ada infrastruktur pengiriman email).

## Phase 4 — Workspace

- [x] Workspace model and migration.
- [x] Modul `workspaces` (`domain`, `usecase`, `repository`, `handler`).
- [x] Create workspace (pembuat otomatis Owner, atomik), list, dan get (non-anggota = 404).
- [x] Unit test use case dan handler; integration test repository (menjalankan semua migration) sudah dijalankan terhadap PostgreSQL dan lulus.
- [x] Tests for workspace isolation (user hanya melihat workspace miliknya).
- [x] Migration `000001`–`000010` teruji menyala bersih dari nol di schema sementara (lewat `pgtest`).
- [x] Manajemen anggota workspace: tambah (via email), daftar, hapus/keluar.
- [x] Role and permission rules (Owner/Admin/Member) dengan matriks izin teruji; 403 untuk anggota tanpa izin, 404 untuk non-anggota.
- [x] Unit test (domain, use case, handler) dan integration test repository anggota + direktori user (dijalankan terhadap PostgreSQL 18 dan lulus).
- [ ] Undangan untuk email yang belum terdaftar (saat ini hanya user terdaftar).
- [ ] Ubah role anggota dan transfer kepemilikan (diperlukan agar Owner dapat keluar dari workspace).
- [ ] Hapus workspace oleh Owner.
- [x] Keluarnya anggota dari workspace ikut menghapus keanggotaan project (atomik); Owner project ditolak dengan 409.
- [x] Pagination untuk `GET /workspaces` dan daftar anggota workspace.

## Phase 5 — Project

- [x] Project model and migration.
- [x] Modul `projects` (`domain`, `usecase`, `repository`, `handler`).
- [x] Endpoint: create (Owner/Admin workspace), list per workspace dan semua, get, update/arsip, delete.
- [x] Matriks izin teruji (peran workspace × peran project); tak berhak melihat = 404, tak berhak bertindak = 403.
- [x] Unit test domain, use case, handler; 13 uji mutasi tertangkap.
- [x] Integration test repository projects dijalankan terhadap PostgreSQL 18 dan lulus.
- [x] Integration test pembersihan anggota (`workspaces/repository`) dijalankan terhadap PostgreSQL 18 dan lulus.
- [x] Project membership: tambah (anggota workspace saja, via email), daftar, hapus/keluar; Owner project tidak bisa dihapus.
- [x] Matriks izin anggota project teruji (pengelola / Admin project / Member project); 14 uji mutasi tertangkap.
- [ ] Jalankan integration test anggota project (termasuk uji kunci AddMember vs keluar workspace) terhadap PostgreSQL 18.
- [x] Menghapus anggota project / keluar workspace melepas assignee task-nya (atomik, tercatat di riwayat).
- [ ] Transfer kepemilikan project (agar Owner project dapat keluar dari workspace).
- [x] Pagination untuk daftar project dan anggota project; riwayat aktivitas task juga dipaginasi.
- [x] Project `archived` menjadi read-only untuk task (409); `completed` tetap dapat diubah.

## Phase 6 — Task Management

- [x] Modul `tasks` (`domain`, `usecase`, `repository`, `handler`) memakai tabel yang sudah ada.
- [x] Create/read/update/delete task; `created_by` dari token; `updated_at` selalu diperbarui.
- [x] Assignee wajib anggota project (dikunci `FOR SHARE` terhadap penghapusan anggota) dan deadline RFC3339 (disimpan UTC).
- [x] Validasi status dan prioritas.
- [x] Filter status/prioritas/assignee, pencarian judul, pagination dengan `total`.
- [x] Activity history: created, status, assignee, prioritas, deadline, dicatat dalam transaksi yang sama; `GET /tasks/:id/activities`.
- [x] Izin: pengelola semua; pembuat ubah & hapus; assignee ubah; lainnya 403. Izin dicek ulang pada baris terkunci.
- [x] Unit test domain, use case, handler; 15 uji mutasi tertangkap.
- [x] Integration test pelepasan assignee di `projects/repository` dijalankan terhadap PostgreSQL 18 dan lulus.
- [x] Integration test `tasks/repository` (task, komentar, ringkasan, kunci assignee) lulus di PostgreSQL 18: 102 test di empat paket repository, 0 gagal, 0 skip. Run CI pertama menemukan bug nyata (struct sematan tidak diekspor sehingga task terbaca kosong) yang kini diperbaiki dan dijaga `schema_test.go`.
- [x] Task comments (FR-07): tulis/baca (yang bisa melihat task), ubah (penulis), hapus (penulis/pengelola), pagination, flag `edited`; 12 uji mutasi tertangkap.
- [x] Project summary / dashboard (FR-09): satu query agregat; overdue mengecualikan task selesai; filter `overdue=true` konsisten dengan ringkasan; 11 uji mutasi tertangkap.
- [ ] Index `tasks(project_id, due_date)` bila data per project besar (saat ini ringkasan memakai index `project_id`).
- [ ] Pertimbangkan index trigram untuk pencarian judul bila data besar.

## Phase 7 — Production Readiness

- [x] Structured logging (dasar).
- [x] Request ID.
- [ ] Observability lanjutan (metrics, tracing, log shipping, retensi log).
- [x] Rate limiting (login/register).
- [x] Batas ukuran body (1 MiB), timeout server, `TRUSTED_PROXIES`, `JWT_SECRET` minimal 32 byte.
- [x] Security review manual (lihat SECURITY.md bagian 10); tidak ada temuan Kritis/Tinggi.
- [ ] Rate limit dengan penyimpanan bersama (Redis) bila lebih dari satu instance.
- [x] `DB_SSLMODE` aman dipaksakan di produksi (override eksplisit `ALLOW_INSECURE_DB`).
- [x] CORS (`CORS_ALLOWED_ORIGINS`) dan header keamanan.
- [ ] HTTPS deployment.
- [ ] Database backup and restore procedure.
- [x] CI pipeline (GitHub Actions: gofmt, vet, build, test -race + integration PostgreSQL 18, govulncheck). Repository git diinisialisasi (branch `main`).
- [ ] Commit awal, push ke GitHub, dan pastikan run CI pertama hijau.
- [x] Dockerfile (multi-stage, distroless, non-root, HEALTHCHECK, shutdown rapi), endpoint `/healthz` dan `/readyz`, smoke test image di CI.
- [x] `docker-compose.yml` (API + PostgreSQL 18 + migration golang-migrate, port loopback, volume PG 18, container API read-only) dengan smoke test di CI.
- [ ] Konfigurasi deployment target (belum dipilih): hosting, secret manager, backup database.
- [x] Wajibkan `DB_SSLMODE` terenkripsi saat `APP_ENV=production` (image memakai `production` sebagai default); smoke test image memeriksa penolakannya.
- [ ] Load and security testing.

## Current Recommended Next Step

Fitur backend MVP (PRD) lengkap dan temuan security review Sedang sudah diperbaiki. Ganti `JWT_SECRET` lokal (minimal 32 byte), jalankan seluruh integration test terhadap PostgreSQL 18, lalu: git + CI, CORS/security headers, transfer kepemilikan, dan konfigurasi deployment.