# TruSS — Task Management System

## 1. Overview

TruSS adalah aplikasi task/project management yang dikembangkan menggunakan Go. Dokumentasi ini menjelaskan rancangan proyek, cara menjalankan backend, API yang sudah tersedia, serta fitur yang direncanakan.

> **Status dokumentasi:** Autentikasi (register, login, JWT), workspace beserta anggotanya, project beserta anggotanya, task dengan riwayat aktivitas dan komentar, serta ringkasan project (dashboard) telah diimplementasikan, sehingga seluruh cakupan backend MVP di PRD terpenuhi dengan Clean Architecture dan diuji (unit test + integration test PostgreSQL). Refresh token, logout, rate limiting, dan fitur lain masih merupakan rencana sampai benar-benar diimplementasikan.

## 2. Tech Stack

| Komponen | Teknologi |
|---|---|
| Bahasa | Go |
| HTTP framework | Gin |
| ORM | GORM |
| Database | PostgreSQL 18 |
| Database driver | GORM PostgreSQL driver |
| Environment configuration | godotenv |
| Logging | `log/slog` (standar Go) |
| Arsitektur | Clean Architecture, modular monolith |
| ID | UUID |
| Password hashing | bcrypt (`golang.org/x/crypto/bcrypt`) |
| Authentication token | JWT (`github.com/golang-jwt/jwt/v5`) |

## 3. Prerequisites

Pastikan perangkat pengembangan memiliki:
- Go versi yang kompatibel dengan `go.mod`.
- PostgreSQL 18.
- Git.
- Postman atau HTTP client sejenis untuk menguji API.

## 4. Project Structure

Struktur berikut adalah target struktur yang disarankan. Sesuaikan dengan file yang benar-benar ada di repository.

Proyek menggunakan **Clean Architecture** dengan pendekatan modular monolith. Detail ada di [ARCHITECTURE.md](docs/ARCHITECTURE.md).

```text
truss-project/
├── cmd/
│   └── api/
│       └── main.go                 # composition root
├── internal/
│   ├── config/
│   │   └── config.go               # membaca environment variable
│   ├── middleware/
│   │   ├── auth_middleware.go      # validasi Bearer token
│   │   ├── request_id.go           # X-Request-ID
│   │   └── logging.go              # access log + panic recovery
│   ├── platform/
│   │   ├── database/               # koneksi PostgreSQL + log GORM
│   │   ├── logger/                 # logger terstruktur (slog)
│   │   └── security/               # bcrypt, JWT
│   ├── modules/
│   │   ├── auth/
│   │   │   ├── domain/             # entity + error domain
│   │   │   ├── usecase/            # logika bisnis + ports
│   │   │   ├── repository/         # implementasi GORM
│   │   │   └── handler/            # Gin handler + DTO + routes
│   │   ├── workspaces/             # struktur sama: domain, usecase, repository, handler
│   │   ├── projects/               # struktur sama: domain, usecase, repository, handler
│   │   └── tasks/                  # struktur sama: domain, usecase, repository, handler
│   ├── testutil/pgtest/            # helper integration test
│   └── model/                      # (legacy) model GORM modul lain
├── migrations/
├── docs/
├── .env
├── .env.example
├── .gitignore
├── go.mod
└── go.sum
```

## 5. Environment Configuration

Buat `.env` pada root project:

```dotenv
APP_ENV=development
APP_PORT=8080
LOG_LEVEL=info

DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=change_me
DB_NAME=truss_db
DB_SSLMODE=disable

JWT_SECRET=ganti_dengan_secret_acak_minimal_32_byte
# Opsional: IP/CIDR reverse proxy yang dipercaya (kosong = tidak ada)
TRUSTED_PROXIES=
```

Buat `.env.example` dengan nama variabel yang sama tetapi tanpa secret atau password asli. Jangan commit `.env` ke Git.

## 6. Install Dependencies

Dari root project:

```powershell
go mod tidy
```

Jika dependency belum tercatat di `go.mod`, tambahkan sesuai kebutuhan:

```powershell
go get github.com/gin-gonic/gin
go get github.com/joho/godotenv
go get github.com/google/uuid
go get gorm.io/gorm
go get gorm.io/driver/postgres
go get golang.org/x/crypto/bcrypt
go get github.com/golang-jwt/jwt/v5
```

## 7. Run Application

```powershell
go run ./cmd/api
```

Atau seluruh stack (API + PostgreSQL 18 + migration) dengan Docker: `docker compose up -d --build` (lihat bagian Docker di [DEVELOPMENT.md](docs/DEVELOPMENT.md)).

Secara default aplikasi menggunakan port `8080`, kecuali `APP_PORT` diatur di `.env`. `APP_ENV=production` mengaktifkan log JSON dan Gin release mode; `LOG_LEVEL` dapat diisi `debug`, `info`, `warn`, atau `error`.

## 8. Current API

| Method | Endpoint | Status |
|---|---|---|
| POST | `/auth/register` | Implemented |
| POST | `/auth/login` | Implemented |
| GET | `/auth/me` | Implemented; protected by JWT middleware, mengembalikan profil user |
| POST | `/workspaces` | Implemented; JWT; pembuat menjadi Owner |
| GET | `/workspaces` | Implemented; JWT; hanya workspace milik user |
| GET | `/workspaces/:id` | Implemented; JWT; non-anggota mendapat 404 |
| POST | `/workspaces/:id/members` | Implemented; JWT; Owner/Admin menambah anggota via email |
| GET | `/workspaces/:id/members` | Implemented; JWT; semua anggota |
| DELETE | `/workspaces/:id/members/:userId` | Implemented; JWT; hapus anggota atau keluar |
| POST | `/workspaces/:id/projects` | Implemented; JWT; Owner/Admin workspace; pembuat menjadi Owner project |
| GET | `/workspaces/:id/projects` | Implemented; JWT; project yang boleh dilihat di workspace |
| GET | `/projects` | Implemented; JWT; project yang boleh dilihat di semua workspace |
| GET | `/projects/:id` | Implemented; JWT; tak berhak melihat = 404 |
| PATCH | `/projects/:id` | Implemented; JWT; ubah nama/deskripsi/status (arsip) |
| DELETE | `/projects/:id` | Implemented; JWT; Owner project atau Owner workspace |
| POST | `/projects/:id/members` | Implemented; JWT; tambah anggota workspace ke project |
| GET | `/projects/:id/members` | Implemented; JWT; semua yang bisa melihat project |
| DELETE | `/projects/:id/members/:userId` | Implemented; JWT; hapus anggota atau keluar |
| POST | `/projects/:id/tasks` | Implemented; JWT; semua yang bisa melihat project |
| GET | `/projects/:id/tasks` | Implemented; JWT; filter status/prioritas/assignee, cari, pagination |
| GET | `/tasks/:id` | Implemented; JWT |
| PATCH | `/tasks/:id` | Implemented; JWT; pengelola, pembuat, atau assignee |
| DELETE | `/tasks/:id` | Implemented; JWT; pengelola atau pembuat |
| GET | `/tasks/:id/activities` | Implemented; JWT; riwayat perubahan task |
| POST | `/tasks/:id/comments` | Implemented; JWT; semua yang bisa melihat task |
| GET | `/tasks/:id/comments` | Implemented; JWT; terlama dulu, pagination |
| PATCH | `/tasks/:id/comments/:commentId` | Implemented; JWT; hanya penulis |
| DELETE | `/tasks/:id/comments/:commentId` | Implemented; JWT; penulis atau pengelola |
| GET | `/projects/:id/summary` | Implemented; JWT; ringkasan dashboard (total, status, terlambat) |
| GET | `/healthz`, `/readyz` | Implemented; publik; liveness dan readiness (database) untuk container |

Detail request dan response tersedia di [API.md](docs/API.md).

## 9. Development Principles

- Dependency hanya menunjuk ke dalam: `handler → usecase → domain`.
- Domain berisi entity dan error bisnis, tanpa framework.
- Use case berisi business logic dan hanya bergantung pada interface (port).
- Repository dan platform mengimplementasikan port; hanya repository yang mengenal GORM.
- Handler menangani HTTP dan DTO; entity domain tidak dikirim langsung ke client.
- Middleware menangani pemeriksaan request lintas endpoint (auth, request ID, log, recovery).
- Logging memakai `slog` terstruktur; jangan mencatat password, token, atau body request.
- Jangan mengembalikan password hash kepada client.
- Validasi akses ke resource harus dilakukan di sisi server.

## 10. Documentation Index

- [Architecture](docs/ARCHITECTURE.md)
- [API Reference](docs/API.md)
- [Database Design](docs/DATABASE.md)
- [Security](docs/SECURITY.md)
- [Development Guide](docs/DEVELOPMENT.md)
- [Roadmap](docs/ROADMAP.md)