package database

import (
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"truss-project/internal/config"
)

// buildDSN merakit DSN format key='value' (libpq). SETIAP nilai dikutip dan di-escape: tanpa itu,
// password kosong membuat `password= dbname=x` terbaca sebagai password "dbname=x" (dbname hilang),
// password yang memuat spasi/kutip/backslash rusak, dan password berisi teks seperti `host=evil`
// dapat menyuntikkan parameter koneksi (lihat dsn_test.go).
func buildDSN(cfg config.DatabaseConfig) string {
	parts := []string{
		dsnPair("host", cfg.Host),
		dsnPair("port", cfg.Port),
		dsnPair("user", cfg.User),
		dsnPair("password", cfg.Password),
		dsnPair("dbname", cfg.Name),
	}
	// sslmode kosong dihilangkan agar memakai default driver (prefer), bukan nilai kosong.
	if cfg.SSLMode != "" {
		parts = append(parts, dsnPair("sslmode", cfg.SSLMode))
	}
	return strings.Join(parts, " ")
}

// dsnPair menghasilkan key='value' dengan backslash dan tanda kutip tunggal di-escape.
func dsnPair(key, value string) string {
	return key + "='" + dsnEscaper.Replace(value) + "'"
}

// Urutan pasangan penting: backslash di-escape dalam satu lintasan bersama kutip, sehingga
// hasil escape tidak di-escape ulang.
var dsnEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

func Connect(cfg config.DatabaseConfig, log *slog.Logger) (*gorm.DB, error) {
	dsn := buildDSN(cfg)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// Error unique-violation menjadi gorm.ErrDuplicatedKey.
		TranslateError: true,
		Logger:         newGormLogger(log),
	})
	if err != nil {
		return nil, fmt.Errorf("gagal membuka database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("gagal ping database: %w", err)
	}

	return db, nil
}
