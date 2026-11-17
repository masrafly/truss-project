# TruSS — Security Notes

## 1. Scope

Dokumen ini mencatat kontrol keamanan yang telah dibahas dan kontrol yang perlu diterapkan sebelum deployment produksi.

## 2. Password

Current approach:
- Password tidak disimpan dalam bentuk plaintext.
- Password di-hash menggunakan bcrypt.
- Password hash tidak boleh dikembalikan pada response API (`json:"-"` pada model).
- Hashing dilakukan melalui port `PasswordHasher` (implementasi `security.BcryptHasher`).
- Bcrypt memiliki batas input 72 byte; use case menolak input yang melewati batas tersebut.
- Aturan panjang password (min 8 karakter, maks 72 byte) ditegakkan di use case, bukan hanya di handler.
- Proteksi user enumeration lewat timing: login dengan email tidak terdaftar tetap menjalankan perbandingan bcrypt terhadap hash dummy, sehingga waktu responsnya setara dengan password salah.
- Response API memakai DTO (`userResponse`) yang tidak memiliki field password hash, sehingga hash tidak bisa bocor karena kelalaian tag JSON.

Recommendations:
- Jangan pernah mencatat password ke log.
- Jangan mengembalikan password hash kepada client.
- Rate limiting login/register sudah diterapkan (lihat bagian 6.1).
- Pertimbangkan kebijakan password dan mekanisme reset password.

## 3. JWT

Konfigurasi awal yang dibahas:
- Signing algorithm: HS256.
- Issuer: `truss`.
- Subject: `internal_id` user.
- Expiration: 24 jam.
- Secret dibaca dari environment variable `JWT_SECRET` oleh `config`, lalu disuntikkan ke `security.JWTManager`. Aplikasi menolak start jika secret kosong atau **kurang dari 32 byte**: secret HS256 yang pendek dapat ditebak offline dari satu token yang bocor, lalu dipakai memalsukan token untuk user mana pun. Buat secret dengan `go run ./cmd/genjwtsecret` (memakai `crypto/rand`, lihat DEVELOPMENT.md). Mengganti secret membatalkan semua token lama (user perlu login ulang).
- Penerbitan token dilakukan use case melalui port `TokenIssuer`; validasi dilakukan middleware melalui port `TokenValidator`.

Security requirements:
- Gunakan secret acak yang kuat dan rahasia.
- Jangan commit secret ke Git.
- Jangan mengirim secret ke client.
- Validasi signature, algoritma, issuer, dan expiration.
- Jangan menaruh password atau data sensitif di JWT payload.
- Gunakan HTTPS pada deployment.
- Pertimbangkan access token berumur lebih pendek dan refresh-token flow jika kebutuhan produk memerlukannya.

JWT yang ditandatangani bukan token terenkripsi. Payload dapat dibaca oleh pemegang token.

## 4. Authentication vs Authorization

Authentication menjawab: siapa user yang mengirim request?

Authorization menjawab: apakah user tersebut memiliki izin atas resource yang diminta?

JWT middleware saat ini memeriksa token dan menaruh subject ke Gin context. Ini belum membuktikan bahwa user berhak mengakses workspace, project, atau task tertentu.

Setiap operasi resource harus memeriksa:
- User merupakan anggota workspace/project yang relevan.
- Role user mengizinkan aksi yang diminta.
- Resource berada dalam scope yang benar.
- User atau resource belum dinonaktifkan/dihapus sesuai kebijakan aplikasi.

## 5. Logging

Logging memakai `log/slog` terstruktur (teks di development, JSON di production).

Dicatat:
- Access log per request: `request_id`, method, path (template route), status, latency, client IP, `user_id`.
- Peristiwa auth: user terdaftar, login berhasil, login gagal (dengan alasan umum).
- Error tak terduga, panic beserta stack trace, error query dan query lambat.

**Tidak boleh dicatat:**
- Password dan password hash.
- Token JWT dan header `Authorization`.
- Body request dan query string.
- Email lengkap (gunakan `user_id`).

Kontrol yang sudah diterapkan:
- `RequestLogger` hanya mencatat path template, bukan query string.
- Log SQL GORM tidak memuat nilai parameter (`ParamsFilter`), sehingga INSERT user tidak membocorkan hash.
- `X-Request-ID` dari client divalidasi (panjang dan karakter) untuk mencegah log injection.
- `Recovery` mencatat stack trace hanya di log; client menerima pesan generik.
- Gagal login dengan email tidak terdaftar dan password salah menghasilkan respons yang sama ke client; perbedaannya hanya ada di log internal.

Catatan: log tetap berisi `user_id` dan IP client, yang dapat dianggap data pribadi. Tentukan kebijakan retensi log sebelum produksi.

## 6. HTTP and API Security

Sudah diterapkan:
- **Batas ukuran body 1 MiB** (`middleware.BodyLimit`): `Content-Length` lebih besar langsung `413` tanpa membaca body; body chunked dihentikan `http.MaxBytesReader`.
- **Timeout server** (`http.Server` di `main.go`): `ReadHeaderTimeout` 5s, `ReadTimeout` 15s, `WriteTimeout` 30s, `IdleTimeout` 60s, header maks 1 MiB. Ini melawan koneksi lambat (Slowloris).
- **Proxy tepercaya** (`TRUSTED_PROXIES`): default kosong, sehingga `X-Forwarded-For` diabaikan dan IP client = alamat koneksi (tidak bisa dipalsukan). Isi dengan IP/CIDR reverse proxy bila aplikasi berada di belakang proxy atau load balancer; jika tidak, semua client tampak ber-IP sama dan rate limit per IP berlaku untuk semuanya sekaligus.
- **Validasi panjang input** sesuai kolom database (mis. nama user ≤ 100, email ≤ 255), agar input panjang dibalas 400, bukan 500.
- Semua query memakai parameter; tidak ada SQL yang dirangkai dari input user.

### 6.1 Rate limiting

| Lapis | Kunci | Batas | Respons |
|---|---|---|---|
| Per IP (`middleware.RateLimitByIP`) | IP client | 20 request / menit, gabungan `POST /auth/register` dan `/auth/login` | `429` + `Retry-After` |
| Per akun (use case login) | email ternormalisasi | 10 login **gagal** / 15 menit; login berhasil mereset hitungan | `429` + `Retry-After` |

- Akun yang terblokir ditolak **sebelum** bcrypt, sehingga tidak membebani CPU.
- Email terdaftar dan tidak terdaftar dihitung sama, jadi blokir tidak membocorkan keberadaan akun. Kegagalan teknis (error database) tidak dihitung.
- Trade-off: siapa pun yang tahu email korban dapat memicu blokir sementara (maks. 15 menit). Batas sengaja longgar; pertimbangkan CAPTCHA atau notifikasi bila ini menjadi masalah.
- State disimpan **di memori per instance** (`platform/ratelimit`) dan hilang saat restart. Untuk lebih dari satu instance, ganti implementasinya dengan penyimpanan bersama (mis. Redis) tanpa mengubah pemakainya.

Recommended controls:
- Gunakan HTTPS di luar local development.
- Atur CORS berdasarkan origin frontend yang diizinkan.
- Jangan mengirim detail internal error, SQL, stack trace, atau secret ke client.
- Jangan mencatat Authorization header atau token utuh.
- Pertimbangkan security headers dan audit logging.

## 7. Environment and Secrets

- `.env` hanya untuk local development.
- `.env` harus masuk `.gitignore`.
- Sediakan `.env.example` tanpa kredensial asli.
- Untuk production, gunakan environment secret atau secret manager.
- Rotasi secret jika terungkap.
- Pisahkan credential database development, staging, dan production.

## 8. Data and Database

- Terapkan least-privilege pada database user.
- Gunakan backup dan uji pemulihan backup.
- Tentukan kebijakan retensi dan penghapusan data.
- Gunakan transaksi untuk perubahan multi-tabel.
- Evaluasi enkripsi backup dan data sensitif sesuai kebutuhan.

## 9. Production Readiness Checklist

- [ ] HTTPS aktif.
- [ ] Secret kuat dan tidak tersimpan di repository (panjang minimal 32 byte kini dipaksakan saat start).
- [x] Rate limiting login/register (in-memory, per instance).
- [x] Batas ukuran body dan timeout server.
- [ ] `TRUSTED_PROXIES` diisi sesuai reverse proxy produksi.
- [x] Authorization membership/role pada setiap resource (ditinjau manual, lihat bagian 10).
- [x] Error response tidak membocorkan detail internal.
- [x] Logging terstruktur tidak menyimpan password/token (perlu diverifikasi ulang pada setiap fitur baru).
- [ ] Kebijakan retensi log ditentukan.
- [ ] CORS dibatasi.
- [ ] Database least privilege.
- [ ] Backup dan restore diuji.
- [ ] Dependency dan image deployment diperbarui.
- [ ] Security tests dijalankan.

## 10. Security Review (2026-11-17, manual)

Review dilakukan dengan membaca kode seluruh modul (auth, workspaces, projects, tasks), tanpa uji penetrasi. Tidak ada temuan Kritis atau Tinggi.

| # | Tingkat | Temuan | Status |
|---|---|---|---|
| 1 | Sedang | Tidak ada pembatasan percobaan login | **Diperbaiki**: rate limit per IP + per akun (6.1) |
| 2 | Sedang | Kekuatan `JWT_SECRET` tidak diperiksa | **Diperbaiki**: minimal 32 byte. Secret lokal yang ada ternyata terlalu pendek dan harus diganti |
| 3 | Sedang | Server tanpa timeout dan tanpa batas body | **Diperbaiki**: `http.Server` dengan timeout, body maks. 1 MiB |
| 4 | Rendah | Gin mempercayai semua proxy (`X-Forwarded-For` bisa dipalsukan) | **Diperbaiki**: `TRUSTED_PROXIES`, default tidak ada |
| 5 | Rendah | Register tanpa batas panjang nama/email (500 alih-alih 400) | **Diperbaiki** |
| 6 | Rendah | Anggota ditambahkan langsung tanpa persetujuan; respons mengungkap email terdaftar | Terbuka: fitur undangan (ROADMAP) |
| 7 | Rendah | Daftar workspace/project/anggota/aktivitas tanpa pagination | Terbuka (ROADMAP) |
| 8 | Rendah | Token tidak dapat dicabut (24 jam) | Terbuka: refresh token/logout (ROADMAP). Dampak terbatas karena otorisasi selalu membaca database |
| 9 | Info | `DB_SSLMODE` kosong = `prefer` | Terbuka: wajibkan `require`/`verify-full` di produksi |

Yang diverifikasi aman: otorisasi selalu dari database (bukan token); tidak ada IDOR antar-task/komentar/project; semua SQL berparameter; JWT dikunci ke HS256 dengan issuer dan expiry wajib; log tanpa password, token, body, query string, atau nilai parameter SQL.