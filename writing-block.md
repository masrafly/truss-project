# PRODUCT REQUIREMENTS DOCUMENT (PRD)
## TruSS — MVP 1: Task Management

**Versi:** 1.0  
**Status:** Draft  
**Jenis Produk:** Web-based Project Management Application  
**Prioritas:** MVP  
**Target Pengguna:** Individu, tim kecil, dan tim pengembangan software

---

## 1. Product Overview

### 1.1. Latar Belakang

Tim yang mengerjakan proyek sering menggunakan beberapa aplikasi berbeda untuk mengelola tugas, memantau progres, menentukan penanggung jawab, dan mencatat deadline. Kondisi ini dapat menyebabkan informasi tersebar, status pekerjaan tidak transparan, serta kesulitan mengetahui hambatan dalam penyelesaian proyek.

TruSS dikembangkan sebagai platform manajemen proyek yang nantinya dapat menghubungkan pengelolaan pekerjaan dengan proses pengembangan software, termasuk version control, automated testing, CI/CD, deployment, dan AI-assisted analysis.

MVP pertama berfokus pada Task Management sebagai fondasi operasional dan data bagi pengembangan fitur berikutnya.

### 1.2. Product Vision

Membangun platform yang membantu tim mengelola pekerjaan secara terstruktur dan nantinya memahami hubungan antara pekerjaan, kualitas software, serta proses delivery.

### 1.3. Problem Statement

Pengguna membutuhkan sistem yang memungkinkan mereka untuk:
- Mengelola task dalam konteks proyek.
- Mengetahui siapa yang bertanggung jawab atas setiap task.
- Memantau status, prioritas, dan deadline.
- Melihat progres proyek tanpa memeriksa setiap task secara manual.
- Menyimpan informasi dan riwayat pekerjaan di satu tempat.

### 1.4. Product Value Proposition

TruSS menyediakan fondasi manajemen pekerjaan yang terstruktur dengan model data yang disiapkan untuk integrasi engineering workflow di masa depan.

Pada MVP ini, TruSS belum mengklaim memiliki fitur otomatisasi delivery atau AI. Nilai tersebut akan dikembangkan dan divalidasi pada fase berikutnya.

---

## 2. Product Goals and Non-Goals

### 2.1. Product Goals

1. Memungkinkan pengguna membuat dan mengelola proyek.
2. Memungkinkan pengguna membuat, menugaskan, dan memperbarui task.
3. Menyediakan visibilitas progres pekerjaan melalui List View dan Kanban Board.
4. Menjaga keamanan akses berdasarkan keanggotaan proyek.
5. Menyediakan API dan database yang dapat dikembangkan untuk integrasi lanjutan.
6. Memastikan seluruh operasi inti dapat diuji secara otomatis.

### 2.2. Success Metrics

Target berikut merupakan sasaran awal yang perlu divalidasi melalui pengujian dan penggunaan nyata.

| Metric | Target MVP |
|---|---|
| Task creation success rate | ≥ 98% pada pengujian terkontrol |
| API test coverage untuk business logic inti | ≥ 80% |
| Task CRUD berhasil | 100% pada skenario pengujian wajib |
| Unauthorized access | 0 akses tidak sah pada pengujian keamanan wajib |
| Task dapat ditemukan melalui filter | 100% pada skenario pengujian wajib |
| Penyelesaian alur task pertama | ≤ 3 menit pada usability test awal |
| Pengguna uji berhasil menyelesaikan alur inti | ≥ 80% |

Persentase keberhasilan pada tabel bukan hasil pengukuran aktual. Target operasional seperti waktu respons dan ketersediaan akan ditentukan melalui pengujian performa dan lingkungan deployment.

### 2.3. Non-Goals

Fitur berikut tidak termasuk dalam MVP 1:

- AI task generation atau AI project assistant.
- Integrasi GitHub, GitLab, Jira, dan CI/CD.
- Real-time collaboration menggunakan WebSocket.
- Gantt chart dan resource allocation.
- Custom workflow dan custom status.
- Advanced reporting dan predictive analytics.
- Aplikasi mobile native.
- Billing, subscription, dan marketplace.
- Multi-tenant enterprise administration yang kompleks.

---

## 3. Target Users and Personas

### 3.1. Project Owner

Pengguna yang membuat proyek dan mengelola anggotanya.

**Kebutuhan:**
- Membuat proyek.
- Mengundang atau menambahkan anggota.
- Mengatur pekerjaan proyek.
- Melihat progres keseluruhan.

### 3.2. Team Member

Pengguna yang terlibat dalam proyek dan mengerjakan task.

**Kebutuhan:**
- Melihat task yang menjadi tanggung jawabnya.
- Memahami deskripsi dan deadline pekerjaan.
- Memperbarui status task.
- Berkomunikasi melalui komentar.

### 3.3. Product Administrator

Dalam MVP, pengelolaan akun dan kontrol sistem dibatasi pada kebutuhan dasar. Dashboard administrasi tingkat organisasi bukan prioritas MVP.

---

## 4. Scope and Prioritization

Prioritas menggunakan metode MoSCoW.

| Priority | Fitur |
|---|---|
| Must Have | Authentication, workspace, project, membership, task CRUD, assignment, status, priority, deadline, authorization |
| Should Have | Kanban Board, filter, search, komentar, activity history, dashboard ringkas |
| Could Have | Label task, avatar, quick status update |
| Won't Have in MVP | AI, integrasi repository, CI/CD, real-time collaboration, Gantt chart |

**Release criteria:** seluruh fitur Must Have harus lolos acceptance criteria. Fitur Should Have dapat dirilis jika tidak menghambat keamanan, stabilitas, dan penyelesaian alur inti.

---

## 5. Functional Requirements

### FR-01 — Authentication

**Deskripsi:** Sistem harus memungkinkan pengguna membuat akun dan melakukan login.

**Requirements:**
- Pengguna dapat mendaftar dengan nama, email, dan password.
- Email harus unik.
- Password disimpan menggunakan password hashing yang aman, bukan plaintext.
- Pengguna dapat login dengan kredensial yang valid.
- Pengguna tidak dapat login menggunakan kredensial yang tidak valid.
- Endpoint yang membutuhkan autentikasi harus menolak request tanpa kredensial yang valid.

**Acceptance Criteria:**
1. Registrasi dengan data valid menghasilkan akun baru.
2. Registrasi menggunakan email yang sudah terdaftar ditolak.
3. Password yang salah menghasilkan respons autentikasi gagal.
4. Password tidak pernah dikembalikan dalam respons API.
5. Pengguna yang belum terautentikasi tidak dapat mengakses resource privat.

### FR-02 — Workspace

**Deskripsi:** Workspace menjadi ruang organisasi untuk mengelompokkan proyek dan anggotanya.

**Requirements:**
- Pengguna dapat membuat workspace.
- Pembuat workspace otomatis menjadi Owner.
- Workspace memiliki nama dan identitas unik.
- Pengguna hanya dapat mengakses workspace yang menjadi anggotanya.

**Acceptance Criteria:**
1. Workspace berhasil dibuat dengan nama valid.
2. Pembuat workspace memiliki role Owner.
3. Pengguna di luar workspace tidak dapat membaca data privat workspace.
4. Workspace tidak dapat dihapus oleh Member biasa.

### FR-03 — Project Management

**Deskripsi:** Pengguna dapat mengelola proyek dalam workspace.

**Requirements:**
- Membuat proyek dengan nama dan deskripsi.
- Melihat daftar proyek yang dapat diakses.
- Melihat detail proyek.
- Memperbarui informasi proyek.
- Mengarsipkan atau menghapus proyek sesuai kebijakan akses.
- Membatasi operasi proyek berdasarkan role dan membership.

**Acceptance Criteria:**
1. Proyek harus memiliki workspace yang valid.
2. Pengguna hanya dapat mengakses proyek yang diizinkan.
3. Proyek yang tidak ditemukan menghasilkan respons yang sesuai.
4. Operasi yang tidak diizinkan ditolak oleh backend.

### FR-04 — Project Membership

**Deskripsi:** Project Owner dapat mengelola anggota proyek.

**Requirements:**
- Menambahkan anggota yang sudah terdaftar.
- Melihat daftar anggota.
- Menghapus anggota dari proyek.
- Membedakan Owner dan Member.
- Membatasi penugasan task kepada pengguna yang menjadi anggota proyek.

**Acceptance Criteria:**
1. Anggota tidak dapat ditambahkan dua kali ke proyek yang sama.
2. Assignee task harus menjadi anggota proyek.
3. Member tidak dapat mengubah keanggotaan jika tidak memiliki izin.
4. Sistem mencegah penghapusan Owner terakhir apabila hal tersebut menyebabkan proyek tidak memiliki pengelola.

### FR-05 — Task Management

**Deskripsi:** Pengguna dapat membuat dan mengelola task dalam proyek.

**Field utama:**

| Field | Requirement |
|---|---|
| ID | Dibuat otomatis |
| Project ID | Wajib |
| Title | Wajib, maksimal 200 karakter |
| Description | Opsional |
| Status | `todo`, `in_progress`, `done` |
| Priority | `low`, `medium`, `high`, `urgent` |
| Assignee ID | Opsional |
| Created By | Diambil dari identitas pengguna terautentikasi |
| Due Date | Opsional |
| Created At | Dibuat otomatis |
| Updated At | Diperbarui otomatis |

**Requirements:**
- Membuat task.
- Melihat daftar dan detail task.
- Memperbarui informasi task.
- Mengubah status dan prioritas.
- Menghapus task sesuai izin.
- Melakukan pencarian dan filtering.
- Menggunakan pagination untuk daftar task.

**Acceptance Criteria:**
1. Task tidak dapat dibuat tanpa judul atau project yang valid.
2. Status dan prioritas hanya menerima nilai yang diizinkan.
3. Task tidak dapat ditugaskan kepada pengguna di luar keanggotaan proyek.
4. `created_by` tidak dapat dipalsukan melalui request body.
5. `updated_at` berubah ketika task diperbarui.
6. Pengguna yang tidak memiliki akses tidak dapat membaca atau mengubah task.
7. Penghapusan task hanya dapat dilakukan oleh role yang berwenang.

### FR-06 — Task List and Kanban Board

**Deskripsi:** Sistem menyediakan dua cara untuk melihat task.

**List View:**
- Menampilkan judul, status, prioritas, assignee, dan deadline.
- Mendukung filter status dan prioritas.
- Mendukung pencarian judul.
- Mendukung pagination.

**Kanban Board:**
- Mengelompokkan task berdasarkan status.
- Menampilkan jumlah task per status.
- Memungkinkan perubahan status melalui kontrol UI.
- Memperbarui data berdasarkan respons backend.

**Acceptance Criteria:**
1. Data pada List View dan Kanban berasal dari sumber data yang sama.
2. Perubahan status tersimpan di backend.
3. Filter dapat dikombinasikan.
4. Task tidak hilang dari sistem hanya karena tidak ditampilkan pada filter aktif.
5. Perubahan status yang gagal harus menampilkan pesan error dan tidak mengklaim perubahan berhasil.

### FR-07 — Task Comments

**Deskripsi:** Anggota proyek dapat menambahkan komentar pada task.

**Requirements:**
- Membuat komentar.
- Melihat komentar pada task.
- Menampilkan penulis dan waktu pembuatan.
- Membatasi akses komentar berdasarkan akses task.

**Acceptance Criteria:**
1. Komentar harus terhubung dengan task yang valid.
2. Identitas penulis berasal dari pengguna terautentikasi.
3. Pengguna di luar proyek tidak dapat membaca atau menambahkan komentar.
4. Komentar kosong ditolak.

### FR-08 — Activity History

**Deskripsi:** Sistem mencatat perubahan penting pada task.

**Aktivitas minimum:**
- Task dibuat.
- Status berubah.
- Assignee berubah.
- Prioritas berubah.
- Deadline berubah.

**Acceptance Criteria:**
1. Aktivitas menyimpan aktor, task, jenis aktivitas, dan waktu.
2. Aktivitas dihasilkan oleh backend.
3. Pengguna tidak dapat membuat atau mengubah riwayat aktivitas melalui endpoint publik.
4. Riwayat hanya dapat dilihat oleh pengguna yang berhak mengakses task.

### FR-09 — Project Dashboard

**Deskripsi:** Sistem menampilkan ringkasan kondisi proyek.

**Informasi minimum:**
- Total task.
- Task berdasarkan status.
- Task yang sudah selesai.
- Task yang melewati deadline.

**Acceptance Criteria:**
1. Perhitungan hanya mencakup task pada proyek yang diminta.
2. Task yang sudah selesai tidak dihitung sebagai task terlambat.
3. Data ringkasan konsisten dengan data task.
4. Pengguna tidak dapat melihat ringkasan proyek yang tidak dapat diaksesnya.

---

## 6. User Stories

### Epic A — Authentication and Workspace

**US-01: Register**

Sebagai pengguna baru, saya ingin membuat akun agar dapat menggunakan TruSS.

Acceptance:
- Data registrasi valid.
- Email unik.
- Password di-hash dengan aman.

**US-02: Login**

Sebagai pengguna terdaftar, saya ingin login agar dapat mengakses workspace saya.

Acceptance:
- Kredensial valid diterima.
- Kredensial tidak valid ditolak.
- Sesi atau token dikelola secara aman.

**US-03: Create Workspace**

Sebagai pengguna, saya ingin membuat workspace agar dapat mengelompokkan proyek.

Acceptance:
- Workspace berhasil dibuat.
- Pembuat menjadi Owner.

### Epic B — Project Management

**US-04: Create Project**

Sebagai Project Owner, saya ingin membuat proyek agar pekerjaan dapat dikelompokkan.

Acceptance:
- Nama proyek wajib.
- Proyek terhubung dengan workspace.
- Hanya pengguna yang berwenang yang dapat membuat proyek.

**US-05: Add Project Member**

Sebagai Project Owner, saya ingin menambahkan anggota agar pekerjaan dapat dikerjakan bersama.

Acceptance:
- Anggota harus memiliki akun valid.
- Keanggotaan tidak boleh duplikat.
- Penambahan anggota dicatat.

### Epic C — Task Management

**US-06: Create Task**

Sebagai anggota proyek yang berwenang, saya ingin membuat task agar pekerjaan dapat didefinisikan dengan jelas.

Acceptance:
- Judul wajib.
- Task terhubung ke proyek.
- Pembuat diambil dari konteks autentikasi.

**US-07: Assign Task**

Sebagai Project Owner atau anggota yang memiliki izin, saya ingin menetapkan assignee agar tanggung jawab jelas.

Acceptance:
- Assignee merupakan anggota proyek.
- Assignee dapat dikosongkan.
- Perubahan tercatat dalam activity history.

**US-08: Update Task Status**

Sebagai anggota proyek yang berwenang, saya ingin memperbarui status agar progres pekerjaan terlihat.

Acceptance:
- Status mengikuti nilai yang diizinkan.
- Perubahan tersimpan.
- Aktivitas perubahan tercatat.

**US-09: Filter Tasks**

Sebagai anggota proyek, saya ingin memfilter task agar dapat menemukan pekerjaan tertentu.

Acceptance:
- Filter status dan prioritas berfungsi.
- Search dan pagination berfungsi.
- Filter tidak mengubah data asli.

**US-10: View Project Progress**

Sebagai Project Owner, saya ingin melihat ringkasan proyek agar dapat memantau penyelesaian pekerjaan.

Acceptance:
- Total task ditampilkan.
- Distribusi status benar.
- Task terlambat dihitung dengan benar.

---

## 7. Business Rules

1. Setiap task harus dimiliki oleh tepat satu proyek.
2. Setiap proyek harus berada dalam satu workspace.
3. Setiap workspace memiliki setidaknya satu Owner.
4. Assignee harus menjadi anggota proyek.
5. Status task hanya dapat bernilai `todo`, `in_progress`, atau `done`.
6. Prioritas task hanya dapat bernilai `low`, `medium`, `high`, atau `urgent`.
7. Pengguna yang tidak terautentikasi tidak boleh mengakses resource privat.
8. Seluruh pemeriksaan izin dilakukan di backend, bukan hanya di frontend.
9. Identitas pembuat dan aktor aktivitas diambil dari identitas terautentikasi.
10. Semua waktu disimpan sebagai timestamp yang konsisten dan ditampilkan sesuai zona waktu pengguna.
11. Task yang sudah selesai tidak dianggap overdue.
12. Penghapusan proyek harus mempertimbangkan integritas data task, komentar, dan activity history.
13. Operasi yang gagal tidak boleh meninggalkan data dalam keadaan parsial yang tidak konsisten.

---

## 8. UX and Screens

### 8.1. Authentication

- Register
- Login
- Validasi form
- Pesan error autentikasi

### 8.2. Workspace

- Workspace selector
- Workspace overview
- Daftar proyek
- Create project

### 8.3. Project Detail

- Nama dan deskripsi proyek
- Daftar anggota
- Ringkasan progres
- Navigasi List dan Board

### 8.4. Task List

- Search input
- Filter status
- Filter prioritas
- Pagination
- Tombol create task
- Task detail panel atau halaman detail

### 8.5. Kanban Board

Kolom:
- To Do
- In Progress
- Done

Setiap kartu menampilkan:
- Judul task
- Prioritas
- Assignee
- Deadline

### 8.6. Task Detail

- Judul dan deskripsi
- Status dan prioritas
- Assignee
- Deadline
- Komentar
- Activity history

**UX requirement:** UI harus menampilkan loading state, empty state, error state, dan konfirmasi untuk tindakan destruktif.

---

## 9. Technical Requirements

### 9.1. Proposed Technology Stack

| Layer | Technology |
|---|---|
| Backend | Go |
| HTTP Router | Chi atau Gin |
| Database | PostgreSQL |
| Database Access | pgx dengan SQL eksplisit atau GORM |
| Authentication | Secure session atau access token |
| Frontend | React + TypeScript |
| API Contract | REST + OpenAPI |
| Testing | Go testing, integration tests |
| Containerization | Docker |
| CI | GitHub Actions atau CI setara |

Pemilihan library final ditetapkan sebelum implementasi. Hindari penggunaan beberapa ORM atau router sekaligus tanpa kebutuhan yang jelas.

### 9.2. Architecture

Arsitektur awal menggunakan **modular monolith** dengan prinsip **Clean Architecture**. Setiap modul fitur (`auth`, `workspaces`, `projects`, `tasks`, dst.) dibagi menjadi empat lapisan, dan dependensi hanya boleh menunjuk ke dalam.

Komponen:
1. Handler (HTTP/API layer): Gin, DTO request/response, pemetaan error ke status HTTP.
2. Middleware: autentikasi, request ID, access logging, panic recovery.
3. Use case (application layer): business logic dan otorisasi; bergantung pada interface (port), bukan implementasi.
4. Domain: entity dan error bisnis tanpa ketergantungan framework.
5. Repository (data access layer): implementasi port menggunakan GORM; menerjemahkan error database ke error domain.
6. Platform: implementasi teknis (PostgreSQL, bcrypt, JWT, logger).
7. PostgreSQL.
8. Background worker hanya jika kebutuhan nyata muncul.

Aturan dependensi:
- `domain` tidak mengimpor framework maupun package lain.
- `usecase` hanya mengimpor `domain`.
- `repository` dan `platform` mengimplementasikan interface milik `usecase`.
- Hanya composition root (`cmd/api/main.go`) yang mengenal semua lapisan.

Arsitektur modular monolith dipilih agar lebih sederhana untuk dikembangkan, diuji, dan di-deploy dibandingkan microservices pada tahap MVP. Clean Architecture dipilih agar business logic dapat diuji tanpa database dan HTTP, serta agar framework dan database dapat diganti dengan dampak minimal.

### 9.3. Database Entities

- `users`
- `workspaces`
- `workspace_members`
- `projects`
- `project_members`
- `tasks`
- `task_comments`
- `task_activities`

Relasi utama:
- User memiliki banyak workspace melalui `workspace_members`.
- Workspace memiliki banyak project.
- Project memiliki banyak member melalui `project_members`.
- Project memiliki banyak task.
- Task dapat memiliki satu assignee.
- Task memiliki banyak komentar dan aktivitas.

### 9.4. API Design

Base path: `/api/v1`

**Authentication**

| Method | Endpoint |
|---|---|
| POST | `/auth/register` |
| POST | `/auth/login` |
| POST | `/auth/logout` |

**Workspace and Project**

| Method | Endpoint |
|---|---|
| POST | `/workspaces` |
| GET | `/workspaces` |
| GET | `/workspaces/{workspaceId}` |
| POST | `/workspaces/{workspaceId}/projects` |
| GET | `/projects` |
| GET | `/projects/{projectId}` |
| PATCH | `/projects/{projectId}` |
| DELETE | `/projects/{projectId}` |

**Project Members**

| Method | Endpoint |
|---|---|
| GET | `/projects/{projectId}/members` |
| POST | `/projects/{projectId}/members` |
| DELETE | `/projects/{projectId}/members/{userId}` |

**Tasks**

| Method | Endpoint |
|---|---|
| GET | `/projects/{projectId}/tasks` |
| POST | `/projects/{projectId}/tasks` |
| GET | `/tasks/{taskId}` |
| PATCH | `/tasks/{taskId}` |
| DELETE | `/tasks/{taskId}` |
| GET | `/tasks/{taskId}/comments` |
| POST | `/tasks/{taskId}/comments` |
| GET | `/tasks/{taskId}/activities` |
| GET | `/projects/{projectId}/summary` |

Daftar endpoint merupakan rancangan awal. Detail payload, tipe data, autentikasi, error schema, dan status code harus ditentukan dalam OpenAPI sebelum frontend dan backend dikembangkan secara paralel.

### 9.5. Standard API Responses

Respons sukses mengembalikan data dan status HTTP yang sesuai.

Respons error memiliki format konsisten, misalnya:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Title is required"
  }
}
```

Status HTTP yang digunakan:
- `200 OK`: request berhasil.
- `201 Created`: resource berhasil dibuat.
- `204 No Content`: operasi berhasil tanpa body.
- `400 Bad Request`: request tidak valid.
- `401 Unauthorized`: autentikasi tidak valid atau tidak tersedia.
- `403 Forbidden`: akses ditolak.
- `404 Not Found`: resource tidak ditemukan atau disembunyikan sesuai kebijakan keamanan.
- `409 Conflict`: konflik data, misalnya email duplikat.
- `422 Unprocessable Content`: validasi semantik gagal, jika dipilih sebagai konvensi API.
- `500 Internal Server Error`: kesalahan internal tanpa membocorkan detail sensitif.

---

## 10. Non-Functional Requirements

### 10.1. Security

- Password menggunakan algoritma hashing yang sesuai, seperti Argon2id atau bcrypt.
- Seluruh endpoint privat memerlukan autentikasi.
- Authorization diperiksa pada setiap akses resource.
- Validasi input dilakukan di backend.
- Query database menggunakan parameterisasi.
- Secret dan kredensial database tidak disimpan dalam source code.
- Rate limiting diterapkan pada endpoint sensitif, terutama login.
- CORS dikonfigurasi berdasarkan origin yang diizinkan.
- Log tidak boleh menyimpan password, token, atau informasi rahasia.
- Jika menggunakan cookie session, terapkan `HttpOnly`, `Secure` pada HTTPS, dan kebijakan `SameSite` yang sesuai.
- Jika menggunakan bearer token, terapkan masa berlaku, penyimpanan aman, dan strategi pencabutan token yang sesuai.

### 10.2. Performance

Target awal:
- API read operation: p95 ≤ 500 ms pada lingkungan uji yang ditentukan.
- API write operation: p95 ≤ 800 ms pada lingkungan uji yang ditentukan.
- List endpoint mendukung pagination.
- Query yang sering digunakan memiliki indeks yang sesuai.
- Dashboard menggunakan agregasi database yang efisien.

Target ini perlu divalidasi dengan dataset, perangkat keras, dan jumlah pengguna uji yang terdokumentasi.

### 10.3. Reliability

- Operasi database penting menggunakan transaksi bila diperlukan.
- Error ditangani tanpa menyebabkan aplikasi crash.
- Migration dapat dilacak dan dijalankan secara konsisten.
- Backup database dan prosedur pemulihan didokumentasikan.
- Health check tersedia untuk aplikasi dan dependensi utama.

### 10.4. Maintainability

- Struktur kode modular dengan Clean Architecture (`domain`, `usecase`, `repository`, `handler` per modul).
- Business logic dipisahkan dari HTTP handler dan dari akses database melalui interface (port).
- Konfigurasi menggunakan environment variables dan dibaca terpusat.
- Unit test untuk business logic menggunakan fake port, tanpa database.
- Integration test untuk operasi database dan authorization.
- API contract didokumentasikan.
- Logging terstruktur (`slog`) dengan request ID, mudah ditelusuri, dan tanpa data rahasia.

---

## 11. Definition of Done (DoD)

Sebuah fitur dianggap selesai jika:

1. Implementasi sesuai user story dan acceptance criteria.
2. Validasi request telah dilakukan.
3. Authorization telah diuji.
4. Unit test untuk business logic relevan telah lulus.
5. Integration test yang relevan telah lulus.
6. Error response mengikuti kontrak API.
7. Database migration tersedia jika diperlukan.
8. Dokumentasi API diperbarui.
9. Tidak ada secret yang ter-commit.
10. Pull request telah direview.
11. CI pipeline lulus.
12. Fitur dapat diuji pada lingkungan staging.

### MVP Release Gate

MVP dapat dirilis jika:
- Pengguna dapat mendaftar dan login.
- Pengguna dapat membuat workspace dan project.
- Owner dapat mengelola anggota.
- Anggota yang berwenang dapat melakukan task CRUD.
- Assignment, status, prioritas, dan deadline berfungsi.
- List View dan filter berfungsi.
- Kanban Board berfungsi apabila masuk dalam release.
- Authorization tidak memiliki temuan kritis atau tinggi yang belum ditangani.
- Backup, konfigurasi deployment, dan prosedur rollback tersedia.

---

## 12. Risks and Mitigation

| Risiko | Mitigasi |
|---|---|
| Scope creep | Terapkan prioritas MoSCoW dan change control |
| Authorization salah | Gunakan centralized policy dan integration tests |
| Database schema berubah berulang | Tetapkan domain model dan migration review |
| API frontend/backend tidak konsisten | Gunakan OpenAPI sebagai kontrak bersama |
| Fitur terlalu kompleks untuk MVP | Gunakan modular monolith dan workflow sederhana |
| Performa menurun saat data bertambah | Pagination, indexing, dan load testing |
| Data hilang saat deployment | Backup dan uji pemulihan database |
| Nilai produk kurang berbeda dari kompetitor | Validasi kebutuhan pengguna dan prioritaskan integrasi engineering pada fase berikutnya |

---

## 13. Development Roadmap

### Phase 1 — Foundation
- Repository setup.
- Konfigurasi environment.
- PostgreSQL dan migration.
- Struktur modular backend dengan Clean Architecture.
- Logging terstruktur dan error handling.

### Phase 2 — Authentication and Access Control
- Register dan login.
- Workspace.
- Workspace membership.
- Project membership.
- Authorization middleware.

### Phase 3 — Core Task Management
- Project CRUD.
- Task CRUD.
- Assignee.
- Status dan priority.
- Due date.
- Filtering dan pagination.

### Phase 4 — Collaboration and Monitoring
- Task comments.
- Activity history.
- List View.
- Kanban Board.
- Project summary.

### Phase 5 — Testing and Release
- Integration tests.
- Security tests.
- Performance baseline.
- Docker dan CI.
- Staging deployment.
- User acceptance testing.

Urutan ini dapat disesuaikan dengan hasil validasi, tetapi fitur autentikasi dan authorization harus tersedia sebelum resource proyek digunakan oleh banyak pengguna.

---

## 14. Future Product Direction

Setelah MVP stabil dan kebutuhan pengguna tervalidasi, TruSS dapat dikembangkan melalui beberapa fase.

**MVP 2 — Engineering Integration**
- GitHub/GitLab integration.
- Hubungan task dengan branch dan pull request.
- Commit dan pull request references.
- Webhook event processing.

**MVP 3 — Delivery Intelligence**
- Integrasi CI/CD.
- Hasil automated testing.
- Deployment status.
- Relasi task, perubahan kode, dan hasil delivery.

**MVP 4 — AI-Assisted Project Intelligence**
- Ringkasan progres berbasis data.
- Identifikasi task yang berisiko terlambat.
- Analisis hambatan delivery.
- Rekomendasi prioritas berdasarkan konteks dan data yang tersedia.

Fitur lanjutan tersebut memerlukan validasi teknis, keamanan integrasi, serta bukti bahwa hasilnya benar-benar membantu pengguna.

---

## 15. Open Questions

Hal-hal berikut harus diputuskan sebelum implementasi:

1. Apakah MVP ditujukan untuk satu tim kecil atau banyak organisasi sejak awal?
2. Apakah Owner boleh mengelola semua task, atau hanya task tertentu?
3. Apakah task harus memiliki assignee, atau boleh tidak ditugaskan?
4. Apakah task dapat dihapus permanen, atau harus diarsipkan?
5. Apakah komentar dan activity history wajib dirilis bersama task CRUD?
6. Apakah autentikasi menggunakan cookie session atau bearer token?
7. Apakah Kanban Board wajib untuk rilis pertama?
8. Berapa jumlah pengguna dan ukuran dataset yang akan menjadi target pengujian?

Keputusan tersebut harus dicatat agar implementasi backend, database, dan frontend menggunakan asumsi yang sama.

---

## 16. Final Acceptance

PRD ini menjadi acuan awal untuk pengembangan TruSS MVP 1. Sebelum implementasi dimulai, tim perlu menyetujui scope, role matrix, schema database, API contract, dan acceptance criteria.

**Deliverables sebelum coding fitur utama:**
- Finalized PRD.
- ERD dan database migration plan.
- OpenAPI specification.
- Role-permission matrix.
- Repository structure.
- Test plan.
- Deployment plan.

**Definition of MVP success:** pengguna dapat mengelola proyek dan task secara aman, konsisten, dan terukur, dengan fondasi teknis yang memungkinkan TruSS berkembang menjadi platform engineering delivery intelligence.
