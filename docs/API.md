# TruSS — API Reference

## 1. General

Base URL lokal:

```text
http://localhost:8080
```

Request body menggunakan JSON kecuali dinyatakan lain.

Setiap respons menyertakan header `X-Request-ID`. Client boleh mengirim header yang sama (maks. 64 karakter; huruf, angka, `-`, `_`); jika tidak valid atau kosong, server membuat ID baru. Sertakan ID ini saat melaporkan masalah agar log server mudah ditelusuri.

Respons memakai DTO terpisah dari entity domain, sehingga perubahan skema database tidak otomatis mengubah kontrak API.

## 2. Authentication

### 2.1 Register

`POST /auth/register`

Membuat akun baru. Register tidak otomatis membuat sesi login.

Request:

```json
{
  "name": "Budi",
  "email": "budi@example.com",
  "password": "password123"
}
```

Validasi yang diketahui dari implementasi:
- `name` wajib diisi.
- `email` wajib diisi dan harus berformat email.
- `password` wajib diisi dan minimal 8 karakter pada binding handler.
- Service membatasi password hingga 72 byte karena batas bcrypt.

Success — `201 Created`:

```json
{
  "message": "Akun berhasil dibuat",
  "data": {
    "internal_id": "UUID",
    "name": "Budi",
    "email": "budi@example.com",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }
}
```

`UUID` di atas adalah placeholder, bukan nilai literal. Field respons ditentukan oleh `userResponse` pada DTO handler.

Possible errors:
- `400 Bad Request`: JSON atau input tidak valid.
- `409 Conflict`: email sudah digunakan.
- `500 Internal Server Error`: kegagalan server yang tidak terduga.

### 2.2 Login

`POST /auth/login`

Memverifikasi email dan password. Jika berhasil, use case menerbitkan JWT melalui port `TokenIssuer`.

Request:

```json
{
  "email": "budi@example.com",
  "password": "password123"
}
```

Success — `200 OK`:

```json
{
  "message": "Login berhasil",
  "access_token": "<JWT>",
  "token_type": "Bearer",
  "expires_in": 86400,
  "data": {
    "internal_id": "UUID",
    "name": "Budi",
    "email": "budi@example.com",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }
}
```

`expires_in` dihitung dari masa berlaku token (24 jam = 86400 detik pada konfigurasi saat ini).

Possible errors:
- `400 Bad Request`: request tidak valid.
- `401 Unauthorized`: email atau password salah.
- `500 Internal Server Error`: kegagalan server yang tidak terduga.

### 2.3 Current user

`GET /auth/me`

Endpoint terlindungi JWT. Middleware mengambil `user_id` dari token, lalu use case `GetProfile` membaca data terbaru user dari database.

Header:

```http
Authorization: Bearer <JWT>
```

Success — `200 OK`:

```json
{
  "message": "Token valid",
  "data": {
    "internal_id": "UUID",
    "name": "Budi",
    "email": "budi@example.com",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }
}
```

Possible errors:
- `401 Unauthorized`: token tidak ada, format salah, signature tidak valid, atau token kedaluwarsa.
- `404 Not Found`: user pada token tidak lagi ada.

## 3. Workspaces

Semua endpoint workspace membutuhkan header `Authorization: Bearer <token>`. `created_by` dan `role` selalu ditentukan server dari identitas terautentikasi; nilai yang dikirim client di body diabaikan.

Objek workspace pada respons:

```json
{
  "internal_id": "UUID",
  "name": "Tim Backend",
  "description": "Ruang kerja utama",
  "created_by": "UUID",
  "role": "owner",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

`role` adalah peran user yang meminta (`owner`, `admin`, atau `member`), bukan peran pembuat. `description` dapat `null`.

### 3.1 Create workspace

`POST /workspaces`

```json
{
  "name": "Tim Backend",
  "description": "Ruang kerja utama"
}
```

Aturan: `name` wajib, di-trim, 1–100 karakter. `description` opsional, di-trim, maksimal 2000 karakter; string kosong disimpan sebagai `null`. Pembuat otomatis menjadi **Owner**, dan workspace serta keanggotaan Owner disimpan dalam satu transaksi.

Success — `201 Created`:

```json
{ "message": "Workspace berhasil dibuat", "data": { "...objek workspace..." } }
```

Possible errors: `400` input tidak valid, `401` token tidak ada/tidak valid atau user sudah tidak ada, `500`.

### 3.2 List workspaces

`GET /workspaces`

Mengembalikan hanya workspace tempat user menjadi anggota, terbaru lebih dulu. Belum ada pagination.

Success — `200 OK`:

```json
{ "data": [ { "...objek workspace..." } ] }
```

`data` selalu berupa array (`[]` jika kosong, bukan `null`).

### 3.3 Get workspace

`GET /workspaces/{id}`

Success — `200 OK`: `{ "data": { "...objek workspace..." } }`

Possible errors:
- `401` belum terautentikasi.
- `404` workspace tidak ada, **atau user bukan anggotanya**, atau `id` bukan UUID. Respons sengaja identik agar orang luar tidak dapat mengetahui apakah sebuah workspace ada.

## 4. Workspace Members

Semua endpoint membutuhkan header `Authorization: Bearer <token>`.

**Kebijakan respons akses:**
- **404** — pemanggil **bukan anggota** workspace (atau workspace tidak ada / `id` bukan UUID). Respons identik agar keberadaan workspace tidak bocor.
- **403** — pemanggil **adalah anggota**, tetapi perannya tidak mengizinkan aksi tersebut.

**Matriks izin:**

| Aksi | Owner | Admin | Member |
|---|---|---|---|
| Lihat daftar anggota | ya | ya | ya |
| Tambah anggota dengan role `admin` | ya | **403** | **403** |
| Tambah anggota dengan role `member` | ya | ya | **403** |
| Hapus Admin | ya | **403** | **403** |
| Hapus Member | ya | ya | **403** |
| Keluar sendiri | **409** (Owner tidak dapat dihapus) | ya | ya |
| Hapus Owner | **409** | **403** | **403** |

Role `owner` tidak dapat diberikan melalui penambahan anggota. Workspace selalu memiliki Owner (pembuatnya), dan keanggotaan Owner tidak dapat dihapus.

Objek anggota pada respons:

```json
{
  "user_id": "UUID",
  "name": "Budi",
  "email": "budi@example.com",
  "role": "member",
  "joined_at": "2026-01-01T00:00:00Z"
}
```

### 4.1 Add member

`POST /workspaces/{id}/members`

```json
{ "email": "budi@example.com", "role": "member" }
```

`email` wajib (di-trim dan diubah ke huruf kecil; user harus sudah terdaftar). `role` opsional, nilai `admin` atau `member`, default `member`.

Success — `201 Created`: `{ "message": "Anggota berhasil ditambahkan", "data": { ...objek anggota... } }`

Possible errors:
- `400` input tidak valid (email kosong, role bukan `admin`/`member`, termasuk `owner`).
- `401` belum terautentikasi.
- `403` peran pemanggil tidak cukup untuk menambahkan role tersebut.
- `404` pemanggil bukan anggota workspace, **atau** email belum terdaftar sebagai user.
- `409` user sudah menjadi anggota.

### 4.2 List members

`GET /workspaces/{id}/members`

Dapat dilihat semua anggota. Urutan: Owner, lalu Admin, lalu Member; di dalam satu peran urut waktu bergabung. User yang akunnya telah dihapus tidak ditampilkan.

Success — `200 OK`: `{ "data": [ ...objek anggota... ] }` (selalu array).

### 4.3 Remove member / leave

`DELETE /workspaces/{id}/members/{userId}`

Jika `userId` adalah pemanggil sendiri, aksi ini berarti keluar dari workspace.

Success — `204 No Content` (tanpa body).

Possible errors:
- `401` belum terautentikasi.
- `403` peran pemanggil tidak mengizinkan penghapusan target (lihat matriks).
- `404` pemanggil bukan anggota, **atau** `userId` bukan anggota workspace ini.
- `409` target adalah Owner, **atau** target masih menjadi Owner suatu project di workspace ini (alihkan/hapus project tersebut lebih dulu).

Saat anggota keluar/dihapus, seluruh keanggotaannya pada project di workspace tersebut ikut terhapus dalam transaksi yang sama.

## 5. Projects

Semua endpoint membutuhkan header `Authorization: Bearer <token>`. Project selalu berada di dalam satu workspace.

**Siapa boleh apa:**

| Aksi | Owner ws | Admin ws | Member ws + Owner project | Member ws + Admin project | Member ws + Member project | Member ws (bukan anggota project) | Bukan anggota ws |
|---|---|---|---|---|---|---|---|
| Buat project | ya | ya | **403** | **403** | **403** | **403** | 404 |
| Lihat (daftar & detail) | ya | ya | ya | ya | ya | 404 / tidak tercantum | 404 |
| Ubah (`PATCH`) | ya | ya | ya | ya | **403** | 404 | 404 |
| Hapus | ya | **403** | ya | **403** | **403** | 404 | 404 |

Ringkasnya: Owner/Admin workspace adalah pengelola dan dapat melihat serta mengubah semua project di workspace-nya walau bukan anggota project. Yang tidak berhak **melihat** mendapat 404; yang boleh melihat tetapi tidak berhak bertindak mendapat 403. Penghapusan hanya untuk Owner project atau Owner workspace.

Objek project pada respons:

```json
{
  "internal_id": "UUID",
  "workspace_id": "UUID",
  "name": "Website Baru",
  "description": "Redesain landing page",
  "status": "active",
  "created_by": "UUID",
  "role": "owner",
  "workspace_role": "admin",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

`role` adalah peran pemanggil di project (`owner`, `admin`, `member`), atau `null` bila ia melihat sebagai pengelola workspace tanpa menjadi anggota project. `workspace_role` adalah peran pemanggil di workspace. `status`: `active`, `archived`, atau `completed`.

### 5.1 Create project

`POST /workspaces/{id}/projects`

```json
{ "name": "Website Baru", "description": "Redesain landing page" }
```

Aturan sama dengan workspace: `name` wajib, di-trim, 1–100 karakter; `description` opsional, maksimal 2000 karakter, string kosong disimpan sebagai `null`. Status awal selalu `active`. Pembuat otomatis menjadi **Owner project** (disimpan dalam satu transaksi). `workspace_id`, `created_by`, dan `status` di body diabaikan.

Success — `201 Created`: `{ "message": "Project berhasil dibuat", "data": { ...objek project... } }`

Possible errors: `400` input tidak valid, `401`, `403` peran workspace bukan Owner/Admin, `404` bukan anggota workspace.

### 5.2 List projects

`GET /workspaces/{id}/projects` — project yang boleh dilihat di satu workspace (`404` bila bukan anggota workspace).

`GET /projects` — project yang boleh dilihat di semua workspace pemanggil.

Urutan terbaru lebih dulu; `data` selalu array. Belum ada pagination.

### 5.3 Get project

`GET /projects/{id}` — `200` dengan `{ "data": { ...objek project... } }`, atau `404` bila project tidak ada, `id` bukan UUID, atau pemanggil tidak berhak melihatnya (respons identik).

### 5.4 Update project

`PATCH /projects/{id}`

```json
{ "name": "Nama Baru", "description": "", "status": "archived" }
```

Semua field opsional, tetapi minimal satu harus dikirim. Field yang tidak dikirim (atau `null`) tidak diubah; `description: ""` menghapus deskripsi. Mengarsipkan project = `status: "archived"`.

Success — `200 OK`: `{ "message": "Project berhasil diperbarui", "data": { ...objek project... } }`

Possible errors: `400` (tidak ada field, nama/deskripsi tidak valid, status tidak dikenal), `401`, `403`, `404`.

### 5.5 Delete project

`DELETE /projects/{id}` — `204 No Content`. **Permanen**: keanggotaan project, task, komentar, dan aktivitasnya ikut terhapus. Gunakan `status: "archived"` bila data ingin disimpan.

Possible errors: `401`, `403` (bukan Owner project/Owner workspace), `404`.

### 5.6 Project members

`POST /projects/{id}/members`, `GET /projects/{id}/members`, `DELETE /projects/{id}/members/{userId}`

**Matriks izin** (pengelola = Owner project, atau Owner/Admin workspace):

| Aksi | Pengelola | Admin project | Member project | Tidak bisa melihat project |
|---|---|---|---|---|
| Lihat daftar anggota | ya | ya | ya | 404 |
| Tambah `admin` | ya | **403** | **403** | 404 |
| Tambah `member` | ya | ya | **403** | 404 |
| Hapus Admin | ya | **403** | **403** | 404 |
| Hapus Member | ya | ya | **403** | 404 |
| Keluar sendiri | ya (bila bukan Owner) | ya | ya | 404 |
| Hapus Owner project | **409** | **403** | **403** | 404 |

Aturan:
- Yang dapat ditambahkan hanya **anggota workspace** project tersebut (PRD FR-04). Email yang tidak terdaftar dan email milik user di luar workspace sama-sama menghasilkan `404` dengan pesan yang identik, agar endpoint ini tidak bisa dipakai untuk menebak email terdaftar.
- Role `owner` tidak dapat diberikan. Owner project (pembuatnya) tidak dapat dihapus maupun keluar sampai fitur transfer kepemilikan tersedia.
- Ketika seseorang keluar dari workspace, keanggotaan project-nya ikut terhapus (lihat bagian 4.3).

Add — body `{ "email": "budi@example.com", "role": "member" }`. `email` wajib (di-trim, huruf kecil); `role` opsional (`admin`/`member`, default `member`). Success — `201`: `{ "message": "Anggota project berhasil ditambahkan", "data": { ...objek anggota... } }`. Objek anggota sama dengan anggota workspace (`user_id`, `name`, `email`, `role`, `joined_at`).

List — `200`: `{ "data": [ ... ] }`, urutan Owner, Admin, Member, lalu waktu bergabung. User yang akunnya dihapus tidak ditampilkan.

Remove — `204 No Content`.

Possible errors: `400` email kosong atau role tidak valid; `401`; `403` peran tidak cukup; `404` project tidak terlihat, user bukan anggota workspace, atau `userId` bukan anggota project; `409` sudah menjadi anggota, atau target adalah Owner project.

## 6. Tasks

Semua endpoint membutuhkan header `Authorization: Bearer <token>`. Siapa pun yang dapat **melihat project** (lihat bagian 5) dapat melihat task-nya; selain itu `404`.

**Matriks izin** (pengelola = Owner/Admin project, atau Owner/Admin workspace):

| Aksi | Pengelola | Pembuat task | Assignee task | Anggota project lain |
|---|---|---|---|---|
| Lihat, daftar, riwayat | ya | ya | ya | ya |
| Buat task | ya | ya | ya | ya |
| Ubah task (`PATCH`) | ya | ya | ya | **403** |
| Hapus task | ya | ya | **403** | **403** |

**Project diarsipkan** (`status: "archived"`): task read-only. Membuat, mengubah, atau menghapus task menghasilkan `409`. Project `completed` tetap dapat diubah.

**Assignee** harus anggota project (PRD FR-05 #3); jika tidak, `400`. Ketika seseorang dihapus dari project atau keluar dari workspace, task yang ditugaskan kepadanya otomatis dilepas (`assignee: null`) dan tercatat di riwayat atas nama orang yang menghapus.

Objek task pada respons:

```json
{
  "internal_id": "UUID",
  "project_id": "UUID",
  "title": "Tulis test login",
  "description": null,
  "status": "in_progress",
  "priority": "high",
  "assignee": { "user_id": "UUID", "name": "Budi" },
  "created_by": "UUID",
  "due_date": "2026-03-01T10:00:00Z",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

`assignee` bernilai `null` bila task belum ditugaskan. `status`: `todo`, `in_progress`, `done`. `priority`: `low`, `medium`, `high`, `urgent`. `due_date` selalu dikembalikan dalam UTC.

### 6.1 Create task

`POST /projects/{id}/tasks`

```json
{
  "title": "Tulis test login",
  "description": "Termasuk kasus password salah",
  "status": "todo",
  "priority": "high",
  "assignee_id": "UUID",
  "due_date": "2026-03-01T17:00:00+07:00"
}
```

- `title` wajib, di-trim, 1–200 karakter. `description` opsional, maksimal 10.000 karakter; kosong disimpan sebagai `null`.
- `status` default `todo`, `priority` default `medium`.
- `due_date` opsional, format **RFC3339** dengan zona waktu (contoh di atas); disimpan dalam UTC.
- `project_id` dan `created_by` di body diabaikan: project dari path, pembuat dari token (PRD FR-05 #4).

Success — `201`: `{ "message": "Task berhasil dibuat", "data": { ...objek task... } }`. Riwayat mencatat aktivitas `created`.

Possible errors: `400` input tidak valid atau assignee bukan anggota project, `401`, `404` project tidak terlihat, `409` project diarsipkan.

### 6.2 List tasks

`GET /projects/{id}/tasks`

| Query | Keterangan |
|---|---|
| `status` | Satu atau beberapa: `?status=todo,in_progress` atau `?status=todo&status=done` |
| `priority` | Sama seperti `status` |
| `assignee` | `me` (pemanggil), `none` (tanpa assignee), atau UUID user |
| `overdue` | `true` = hanya task terlambat (definisi sama dengan ringkasan, lihat 6.8) |
| `q` | Cari judul (mengandung, tidak peka huruf besar; `%` dan `_` dicari apa adanya). Maks. 100 karakter |
| `page` | Default 1 |
| `limit` | Default 20, maksimal 100 |

Semua filter dapat dikombinasikan. Urutan: terbaru lebih dulu.

Success — `200`:

```json
{
  "data": [ { "...objek task..." } ],
  "meta": { "page": 1, "limit": 20, "total": 57 }
}
```

`total` adalah jumlah semua task yang cocok dengan filter (bukan hanya halaman ini). Untuk Kanban, panggil endpoint ini per status, atau tanpa filter status lalu kelompokkan di client. Keduanya memakai sumber data yang sama (PRD FR-06).

Possible errors: `400` nilai filter, `page`, atau `limit` tidak valid; `401`; `404`.

### 6.3 Get task

`GET /tasks/{id}` — `200` dengan `{ "data": { ...objek task... } }`, atau `404` bila task tidak ada atau tidak terlihat.

### 6.4 Update task

`PATCH /tasks/{id}`

```json
{ "status": "done", "assignee_id": null, "due_date": "2026-03-05T09:00:00Z" }
```

Semua field opsional, tetapi minimal satu harus dikirim. Aturannya:
- **Tidak dikirim** = tidak diubah.
- `description`, `assignee_id`, dan `due_date` dapat **dikosongkan dengan `null`**.
- `title`, `status`, dan `priority` bernilai `null` diperlakukan sebagai tidak dikirim.

Untuk Kanban, cukup kirim `{ "status": "in_progress" }`. Jika gagal, respons berupa error dan task tidak berubah (PRD FR-06 #5).

Riwayat mencatat setiap perubahan **status, assignee, prioritas, dan deadline** (PRD FR-08). Perubahan judul/deskripsi tidak dicatat. `updated_at` selalu diperbarui.

Success — `200`: `{ "message": "Task berhasil diperbarui", "data": { ...objek task... } }`

Possible errors: `400`, `401`, `403`, `404`, `409` project diarsipkan.

### 6.5 Delete task

`DELETE /tasks/{id}` — `204 No Content`. Riwayat task ikut terhapus. Errors: `401`, `403`, `404`, `409`.

### 6.6 Activity history

`GET /tasks/{id}/activities` — riwayat task, terlama lebih dulu. Hanya dapat dibaca; tidak ada endpoint untuk membuat atau mengubah riwayat (PRD FR-08 #3).

```json
{
  "data": [
    {
      "internal_id": "UUID",
      "type": "status_changed",
      "actor": { "user_id": "UUID", "name": "Budi" },
      "old_value": "todo",
      "new_value": "in_progress",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

| `type` | `old_value` / `new_value` |
|---|---|
| `created` | `null` / judul task |
| `status_changed` | status lama / baru |
| `priority_changed` | prioritas lama / baru |
| `assignee_changed` | UUID user lama / baru (`null` = tanpa assignee) |
| `due_date_changed` | RFC3339 UTC lama / baru (`null` = tanpa deadline) |

### 6.7 Comments

`POST /tasks/{id}/comments`, `GET /tasks/{id}/comments`, `PATCH /tasks/{id}/comments/{commentId}`, `DELETE /tasks/{id}/comments/{commentId}`

| Aksi | Siapa |
|---|---|
| Baca & tulis komentar | Siapa pun yang dapat melihat task. Di luar project mendapat `404` (PRD FR-07 #3) |
| Ubah komentar | **Hanya penulis**. Pengelola pun tidak boleh mengubah kata-kata orang lain (`403`) |
| Hapus komentar | Penulis, atau pengelola (moderasi). Lainnya `403`, termasuk pembuat/assignee task |

Project diarsipkan: menulis, mengubah, dan menghapus komentar menghasilkan `409`.

Body create/update: `{ "content": "Sudah dicek, aman." }`. `content` di-trim, 1–5000 karakter; kosong ditolak (PRD FR-07 #4). Penulis selalu diambil dari token.

Objek komentar:

```json
{
  "internal_id": "UUID",
  "task_id": "UUID",
  "author": { "user_id": "UUID", "name": "Budi" },
  "content": "Sudah dicek, aman.",
  "edited": false,
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

`edited` bernilai `true` bila komentar pernah diubah.

List: terlama lebih dulu, `page` (default 1) dan `limit` (default 50, maks. 100); respons `{ "data": [...], "meta": { "page", "limit", "total" } }`.

Create `201`, update `200`, delete `204`. Errors: `400` isi tidak valid, `401`, `403`, `404` task tidak terlihat atau komentar tidak ada/bukan milik task tersebut, `409` project diarsipkan.

Komentar ikut terhapus bila task-nya dihapus.

### 6.8 Project summary (dashboard)

`GET /projects/{id}/summary`

Ringkasan kondisi project (PRD FR-09). Hanya untuk yang dapat melihat project; selain itu `404` (FR-09 #4). Project yang diarsipkan tetap dapat diringkas.

Success — `200`:

```json
{
  "data": {
    "total": 8,
    "by_status": { "todo": 3, "in_progress": 4, "done": 1 },
    "by_priority": { "low": 1, "medium": 2, "high": 3, "urgent": 2 },
    "completed": 1,
    "overdue": 2,
    "unassigned": 5,
    "completion_rate": 12.5,
    "as_of": "2026-03-01T05:00:00Z"
  }
}
```

- Semua angka hanya mencakup task project ini (FR-09 #1), dihitung dalam satu query agregat (satu snapshot data).
- `overdue`: task yang punya deadline, deadline-nya **sebelum** `as_of`, dan statusnya **bukan** `done`. Task selesai tidak pernah dihitung terlambat (FR-09 #2). Task tepat pada deadline belum terlambat.
- `completion_rate`: persentase `done / total`, satu desimal; `0` bila belum ada task.
- `as_of`: waktu acuan perhitungan (UTC).
- Semua status dan prioritas selalu ada di respons, bernilai `0` bila kosong.

**Konsistensi dengan daftar task (FR-09 #3):** setiap angka dapat "diklik" menjadi daftar dengan total yang sama:

| Angka | Daftar task |
|---|---|
| `by_status.todo` | `GET /projects/{id}/tasks?status=todo` |
| `unassigned` | `GET /projects/{id}/tasks?assignee=none` |
| `overdue` | `GET /projects/{id}/tasks?overdue=true` |

`overdue=true` memakai definisi yang sama persis (satu kondisi SQL yang dipakai bersama). Parameter ini hanya menerima `true` atau `false`.

## 7. Authorization Header

Untuk endpoint terlindungi, client mengirim:

```http
Authorization: Bearer <access_token>
```

Jangan mengirim JWT melalui query string atau mencatat token lengkap ke log.

## 8. Status Code Convention

| Status | Makna |
|---|---|
| 200 | Request berhasil |
| 201 | Resource berhasil dibuat |
| 400 | Request tidak valid |
| 401 | Belum terautentikasi atau kredensial/token tidak valid |
| 403 | Terautentikasi tetapi tidak memiliki izin |
| 404 | Resource tidak ditemukan |
| 409 | Konflik, misalnya email duplikat |
| 413 | Body request lebih dari 1 MiB |
| 429 | Terlalu banyak percobaan (rate limit); lihat header `Retry-After` (detik) |
| 500 | Kesalahan internal server |

## 9. Planned API — Not Implemented Yet

Daftar ini adalah rencana, bukan endpoint yang sudah tersedia.

| Method | Endpoint kandidat | Tujuan |
|---|---|---|
| PATCH | `/users/me` | Memperbarui profil (modul `users` terpisah dari `auth`) |

Path parameter, role, pagination, filter, dan response final harus ditetapkan ketika fitur terkait dirancang.