package repository_test

// Integration test: memakai PostgreSQL asli dan menjalankan migration
// migrations/000001_create_users.up.sql, sehingga sekaligus memverifikasi bahwa
// skema SQL cocok dengan model repository.
//
// Test DI-SKIP jika TEST_DB_HOST tidak diisi. Environment variable:
//
//	TEST_DB_HOST      (wajib; jika kosong semua test di-skip)
//	TEST_DB_PORT      (default 5432)
//	TEST_DB_USER      (default postgres)
//	TEST_DB_PASSWORD
//	TEST_DB_NAME      (default postgres) gunakan database KHUSUS TEST
//	TEST_DB_SSLMODE   (default disable)
//
// Setiap test membuat schema sementara (truss_test_<acak>) dan menghapusnya
// setelah selesai, sehingga tabel/data yang sudah ada tidak tersentuh.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/config"
	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/repository"
	"truss-project/internal/platform/database"
)

const migrationFile = "../../../../migrations/000001_create_users.up.sql"

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// setup menyiapkan schema sementara berisi tabel users dan mengembalikan
// repository + koneksi gorm (untuk pemeriksaan/manipulasi langsung).
func setup(t *testing.T) (*repository.UserGormRepository, *gorm.DB) {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		t.Skip("TEST_DB_HOST tidak diisi; integration test repository di-skip")
	}

	cfg := config.DatabaseConfig{
		Host:     host,
		Port:     envOr("TEST_DB_PORT", "5432"),
		User:     envOr("TEST_DB_USER", "postgres"),
		Password: os.Getenv("TEST_DB_PASSWORD"),
		Name:     envOr("TEST_DB_NAME", "postgres"),
		SSLMode:  envOr("TEST_DB_SSLMODE", "disable"),
	}

	// Memakai database.Connect agar opsi produksi (TranslateError, logger) ikut teruji.
	db, err := database.Connect(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("gagal terhubung ke database test: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Satu koneksi saja: SET search_path hanya berlaku per koneksi.
	sqlDB.SetMaxOpenConns(1)

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schema := "truss_test_" + hex.EncodeToString(b)

	mustExec(t, db, fmt.Sprintf("CREATE SCHEMA %s", schema))
	t.Cleanup(func() {
		// Cleanup memakai koneksi yang sama; schema dihapus beserta isinya.
		db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		sqlDB.Close()
	})
	mustExec(t, db, fmt.Sprintf("SET search_path TO %s", schema))

	migration, err := os.ReadFile(migrationFile)
	if err != nil {
		t.Fatalf("gagal membaca migration: %v", err)
	}
	for _, stmt := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		mustExec(t, db, stmt)
	}

	return repository.NewUserGormRepository(db), db
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec gagal: %v\nSQL: %s", err, sql)
	}
}

func newUser(email string) *domain.User {
	return &domain.User{Name: "Budi", Email: email, PasswordHash: "$2a$10$hash-contoh"}
}

// ---------- Create ----------

func TestCreate_PersistsAndFillsGeneratedFields(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()
	u := newUser("budi@example.com")

	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if u.ID == uuid.Nil {
		t.Error("ID harus terisi dari database (gen_random_uuid)")
	}
	if u.CreatedAt.IsZero() || u.UpdatedAt.IsZero() {
		t.Error("CreatedAt dan UpdatedAt harus terisi")
	}
	if time.Since(u.CreatedAt) > time.Minute {
		t.Errorf("CreatedAt tidak wajar: %v", u.CreatedAt)
	}

	var name, email, hash string
	err := db.Raw("SELECT name, email, password_hash FROM users WHERE internal_id = ?", u.ID).
		Row().Scan(&name, &email, &hash)
	if err != nil {
		t.Fatalf("baris tidak ditemukan di tabel users: %v", err)
	}
	if name != "Budi" || email != "budi@example.com" || hash != "$2a$10$hash-contoh" {
		t.Errorf("data tersimpan tidak sesuai: %q %q %q", name, email, hash)
	}
}

func TestCreate_GeneratesDistinctIDs(t *testing.T) {
	repo, _ := setup(t)
	ctx := context.Background()

	a, b := newUser("a@example.com"), newUser("b@example.com")
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Error("ID dua user harus berbeda")
	}
}

func TestCreate_DuplicateEmailMapsToDomainError(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()

	if err := repo.Create(ctx, newUser("budi@example.com")); err != nil {
		t.Fatal(err)
	}

	err := repo.Create(ctx, newUser("budi@example.com"))
	if !errors.Is(err, domain.ErrEmailAlreadyUsed) {
		t.Fatalf("harapannya ErrEmailAlreadyUsed, dapat %v", err)
	}

	var n int64
	db.Raw("SELECT COUNT(*) FROM users WHERE email = ?", "budi@example.com").Scan(&n)
	if n != 1 {
		t.Errorf("harus tetap 1 baris, dapat %d", n)
	}
}

func TestCreate_DuplicateEmailStillRejectedAfterSoftDelete(t *testing.T) {
	// Unique constraint pada tabel tidak mempertimbangkan deleted_at,
	// jadi email milik akun yang di-soft-delete tidak bisa dipakai ulang.
	// Test ini mendokumentasikan perilaku tersebut.
	repo, db := setup(t)
	ctx := context.Background()

	u := newUser("budi@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", u.ID)

	err := repo.Create(ctx, newUser("budi@example.com"))
	if !errors.Is(err, domain.ErrEmailAlreadyUsed) {
		t.Fatalf("harapannya ErrEmailAlreadyUsed, dapat %v", err)
	}
}

func TestCreate_ValueTooLongIsNotMappedToDuplicate(t *testing.T) {
	repo, _ := setup(t)

	u := newUser("budi@example.com")
	u.Name = strings.Repeat("x", 101) // kolom VARCHAR(100)

	err := repo.Create(context.Background(), u)
	if err == nil {
		t.Fatal("nama 101 karakter harus ditolak database")
	}
	if errors.Is(err, domain.ErrEmailAlreadyUsed) {
		t.Error("error panjang kolom tidak boleh dipetakan ke ErrEmailAlreadyUsed")
	}
}

// ---------- FindByEmail ----------

func TestFindByEmail_Found(t *testing.T) {
	repo, _ := setup(t)
	ctx := context.Background()
	created := newUser("budi@example.com")
	if err := repo.Create(ctx, created); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByEmail(ctx, "budi@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("ID = %v, harapannya %v", got.ID, created.ID)
	}
	if got.Name != "Budi" || got.Email != "budi@example.com" {
		t.Errorf("data tidak sesuai: %+v", got)
	}
	if got.PasswordHash != "$2a$10$hash-contoh" {
		t.Errorf("repository harus mengembalikan hash agar use case bisa membandingkan, dapat %q", got.PasswordHash)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("timestamp harus terisi")
	}
}

func TestFindByEmail_NotFoundMapsToDomainError(t *testing.T) {
	repo, _ := setup(t)

	got, err := repo.FindByEmail(context.Background(), "tidakada@example.com")

	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("harapannya ErrUserNotFound, dapat %v", err)
	}
	if got != nil {
		t.Errorf("hasil harus nil, dapat %+v", got)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t.Error("error GORM tidak boleh bocor keluar repository")
	}
}

func TestFindByEmail_IsExactMatch(t *testing.T) {
	// Normalisasi (lowercase) adalah tugas use case; repository mencocokkan apa adanya.
	repo, _ := setup(t)
	ctx := context.Background()
	if err := repo.Create(ctx, newUser("budi@example.com")); err != nil {
		t.Fatal(err)
	}

	_, err := repo.FindByEmail(ctx, "BUDI@example.com")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("pencarian harus case-sensitive di level repository, dapat %v", err)
	}
}

func TestFindByEmail_SQLInjectionAttemptIsHarmless(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()
	if err := repo.Create(ctx, newUser("budi@example.com")); err != nil {
		t.Fatal(err)
	}

	_, err := repo.FindByEmail(ctx, "' OR '1'='1")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("payload injeksi tidak boleh mengembalikan user, dapat %v", err)
	}

	_, _ = repo.FindByEmail(ctx, "x'; DROP TABLE users; --")
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM users").Scan(&n).Error; err != nil || n != 1 {
		t.Errorf("tabel users harus utuh dengan 1 baris, n=%d err=%v", n, err)
	}
}

// ---------- FindByID ----------

func TestFindByID_Found(t *testing.T) {
	repo, _ := setup(t)
	ctx := context.Background()
	created := newUser("budi@example.com")
	if err := repo.Create(ctx, created); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID != created.ID || got.Email != "budi@example.com" {
		t.Errorf("data tidak sesuai: %+v", got)
	}
}

func TestFindByID_NotFoundMapsToDomainError(t *testing.T) {
	repo, _ := setup(t)

	got, err := repo.FindByID(context.Background(), uuid.New())

	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("harapannya ErrUserNotFound, dapat %v", err)
	}
	if got != nil {
		t.Errorf("hasil harus nil, dapat %+v", got)
	}
}

// ---------- Soft delete ----------

func TestFind_SoftDeletedUserIsInvisible(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()
	u := newUser("budi@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	mustExec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", u.ID)

	if _, err := repo.FindByID(ctx, u.ID); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("FindByID: user terhapus harus ErrUserNotFound, dapat %v", err)
	}
	if _, err := repo.FindByEmail(ctx, u.Email); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("FindByEmail: user terhapus harus ErrUserNotFound, dapat %v", err)
	}
}

// ---------- Context ----------

func TestRepository_CancelledContextReturnsError(t *testing.T) {
	repo, _ := setup(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := repo.Create(ctx, newUser("budi@example.com")); err == nil {
		t.Error("Create dengan context dibatalkan harus error")
	}
	_, err := repo.FindByEmail(ctx, "budi@example.com")
	if err == nil {
		t.Fatal("FindByEmail dengan context dibatalkan harus error")
	}
	if errors.Is(err, domain.ErrUserNotFound) {
		t.Error("context dibatalkan tidak boleh dipetakan menjadi ErrUserNotFound")
	}
}

// ---------- UpdateName / UpdatePasswordHash ----------

func TestUpdateName_PersistsAndKeepsOtherFields(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()
	u, other := newUser("budi@example.com"), newUser("lain@example.com")
	for _, x := range []*domain.User{u, other} {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	// Pastikan updated_at yang baru jelas lebih besar dari nilai awal.
	mustExec(t, db, "UPDATE users SET updated_at = NOW() - interval '1 hour' WHERE internal_id = ?", u.ID)

	got, err := repo.UpdateName(ctx, u.ID, "Nama Baru")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID || got.Name != "Nama Baru" || got.Email != u.Email || got.PasswordHash != u.PasswordHash {
		t.Errorf("hasil = %+v", got)
	}
	if time.Since(got.UpdatedAt) > time.Minute {
		t.Errorf("updated_at harus maju: %v", got.UpdatedAt)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at harus terisi")
	}
	if o, _ := repo.FindByID(ctx, other.ID); o.Name != "Budi" {
		t.Errorf("user lain tidak boleh berubah: %+v", o)
	}
}

func TestUpdateName_UnknownAndSoftDeletedUsersNotFound(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()
	u := newUser("budi@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateName(ctx, uuid.New(), "X"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("user tidak ada: %v", err)
	}
	mustExec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", u.ID)
	if _, err := repo.UpdateName(ctx, u.ID, "X"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("user terhapus tidak boleh diubah: %v", err)
	}
}

func TestUpdateName_ValueTooLongIsNotMappedToNotFound(t *testing.T) {
	repo, _ := setup(t)
	ctx := context.Background()
	u := newUser("budi@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	_, err := repo.UpdateName(ctx, u.ID, strings.Repeat("a", 101))
	if err == nil || errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("harus error database biasa, dapat %v", err)
	}
}

func TestUpdatePasswordHash_PersistsOnlyForThatUser(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()
	u, other := newUser("budi@example.com"), newUser("lain@example.com")
	for _, x := range []*domain.User{u, other} {
		if err := repo.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.UpdatePasswordHash(ctx, u.ID, "$2a$10$hash-baru"); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.FindByID(ctx, u.ID); got.PasswordHash != "$2a$10$hash-baru" {
		t.Errorf("hash = %q", got.PasswordHash)
	}
	if got, _ := repo.FindByID(ctx, other.ID); got.PasswordHash != other.PasswordHash {
		t.Error("hash user lain tidak boleh berubah")
	}
	var recent bool
	if err := db.Raw("SELECT updated_at > NOW() - interval '1 minute' FROM users WHERE internal_id = ?", u.ID).Row().Scan(&recent); err != nil || !recent {
		t.Errorf("updated_at harus ikut diperbarui (err=%v)", err)
	}
}

func TestUpdatePasswordHash_UnknownAndSoftDeletedUsersNotFound(t *testing.T) {
	repo, db := setup(t)
	ctx := context.Background()
	u := newUser("budi@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdatePasswordHash(ctx, uuid.New(), "h"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("user tidak ada: %v", err)
	}
	mustExec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", u.ID)
	if err := repo.UpdatePasswordHash(ctx, u.ID, "h"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("user terhapus tidak boleh diubah: %v", err)
	}
}

func TestUpdate_CancelledContextReturnsError(t *testing.T) {
	repo, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.UpdateName(ctx, uuid.New(), "X"); err == nil || errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("UpdateName: %v", err)
	}
	if err := repo.UpdatePasswordHash(ctx, uuid.New(), "h"); err == nil || errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("UpdatePasswordHash: %v", err)
	}
}
