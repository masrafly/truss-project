// Package pgtest menyiapkan database PostgreSQL sementara untuk integration test.
//
// Setiap pemanggilan Setup membuat schema khusus (truss_test_<acak>), menjalankan
// SEMUA file migrations/*.up.sql secara berurutan, lalu menghapus schema tersebut
// saat test selesai. Tabel dan data yang sudah ada tidak tersentuh.
//
// Test DI-SKIP jika TEST_DB_HOST tidak diisi. Environment variable:
//
//	TEST_DB_HOST      (wajib; jika kosong test di-skip)
//	TEST_DB_PORT      (default 5432)
//	TEST_DB_USER      (default postgres)
//	TEST_DB_PASSWORD
//	TEST_DB_NAME      (default postgres) gunakan database KHUSUS TEST
//	TEST_DB_SSLMODE   (default disable)
//	TEST_DB_REQUIRED  (isi 1 di CI: test GAGAL, bukan skip, bila TEST_DB_HOST kosong)
package pgtest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"

	"truss-project/internal/config"
	"truss-project/internal/platform/database"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// migrationsDir menghitung lokasi folder migrations dari lokasi file ini,
// sehingga tidak bergantung pada paket tempat test dijalankan.
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	// .../internal/testutil/pgtest/pgtest.go -> root proyek 3 tingkat di atas direktori file
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	return filepath.Join(root, "migrations")
}

// Setup mengembalikan koneksi GORM yang search_path-nya diarahkan ke schema sementara
// yang sudah berisi seluruh tabel hasil migration.
func Setup(t *testing.T) *gorm.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		// Di CI, skip diam-diam berbahaya: test tampak "ok" padahal tidak menyentuh
		// database. TEST_DB_REQUIRED=1 mengubah skip menjadi kegagalan.
		if os.Getenv("TEST_DB_REQUIRED") == "1" {
			t.Fatal("TEST_DB_REQUIRED=1 tetapi TEST_DB_HOST kosong: integration test tidak boleh di-skip")
		}
		t.Skip("TEST_DB_HOST tidak diisi; integration test di-skip")
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

	Exec(t, db, fmt.Sprintf("CREATE SCHEMA %s", schema))
	t.Cleanup(func() {
		db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		sqlDB.Close()
	})
	Exec(t, db, fmt.Sprintf("SET search_path TO %s", schema))

	applyMigrations(t, db)
	return db
}

func applyMigrations(t *testing.T, db *gorm.DB) {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(migrationsDir(), "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("file migration tidak ditemukan di %s (err: %v)", migrationsDir(), err)
	}
	sort.Strings(files) // nama berawalan 000001_, 000002_, ... sehingga urut

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("gagal membaca %s: %v", f, err)
		}
		for _, stmt := range splitStatements(string(content)) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("migration %s gagal: %v\nSQL: %s", filepath.Base(f), err, stmt)
			}
		}
	}
}

// splitStatements membuang komentar baris (-- ...) lalu memecah pada ';'.
func splitStatements(sql string) []string {
	var lines []string
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		lines = append(lines, line)
	}

	var out []string
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Exec menjalankan SQL dan menggagalkan test jika error.
func Exec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec gagal: %v\nSQL: %s", err, sql)
	}
}
