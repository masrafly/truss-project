package database

// Test perakitan DSN memakai parser pgx yang SAMA dengan driver GORM, tanpa database.
//
// Latar belakang: DSN dulu dirakit sebagai `password=%s dbname=%s` tanpa tanda kutip. Menurut
// aturan libpq, spasi setelah "=" diabaikan, sehingga password KOSONG membuat
// `password= dbname=app` terbaca sebagai password "dbname=app" dan dbname hilang (aplikasi
// diam-diam tersambung ke database default). Password yang memuat spasi, kutip, atau backslash
// juga akan rusak. Nilai harus dikutip dan di-escape.

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"truss-project/internal/config"
)

func parsed(t *testing.T, cfg config.DatabaseConfig) *pgconn.Config {
	t.Helper()
	pc, err := pgconn.ParseConfig(buildDSN(cfg))
	if err != nil {
		t.Fatalf("DSN tidak dapat di-parse: %v\nDSN: %q", err, buildDSN(cfg))
	}
	return pc
}

func TestBuildDSN_RoundTripsEveryField(t *testing.T) {
	cases := map[string]config.DatabaseConfig{
		"biasa":              {Host: "db.internal", Port: "5432", User: "truss", Password: "s3cret", Name: "truss_db", SSLMode: "disable"},
		"password kosong":    {Host: "127.0.0.1", Port: "55433", User: "postgres", Password: "", Name: "truss_app", SSLMode: "disable"},
		"password spasi":     {Host: "h", Port: "5432", User: "u", Password: "kata sandi panjang", Name: "d", SSLMode: "disable"},
		"password kutip":     {Host: "h", Port: "5432", User: "u", Password: `it's "quoted"`, Name: "d", SSLMode: "disable"},
		"password backslash": {Host: "h", Port: "5432", User: "u", Password: `a\b\\c`, Name: "d", SSLMode: "disable"},
		"password mirip DSN": {Host: "h", Port: "5432", User: "u", Password: "x dbname=hack host=evil", Name: "d", SSLMode: "disable"},
		"password simbol":    {Host: "h", Port: "5432", User: "u", Password: "p@ss/w:rd?#&=%", Name: "d", SSLMode: "disable"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			pc := parsed(t, cfg)
			if pc.Host != cfg.Host || pc.User != cfg.User || pc.Password != cfg.Password || pc.Database != cfg.Name {
				t.Errorf("hasil parse: host=%q user=%q password=%q db=%q\nharapannya: host=%q user=%q password=%q db=%q",
					pc.Host, pc.User, pc.Password, pc.Database, cfg.Host, cfg.User, cfg.Password, cfg.Name)
			}
			if uint16(atoi(cfg.Port)) != pc.Port {
				t.Errorf("port = %d, harapannya %s", pc.Port, cfg.Port)
			}
		})
	}
}

// Password yang memuat teks seperti DSN tidak boleh menyuntikkan parameter koneksi
// (mis. mengganti host) dan tidak boleh menggeser field lain.
func TestBuildDSN_PasswordCannotInjectParameters(t *testing.T) {
	pc := parsed(t, config.DatabaseConfig{
		Host: "db.internal", Port: "5432", User: "u", Password: "x host=evil.example dbname=hack", Name: "truss_db", SSLMode: "disable",
	})
	if pc.Host != "db.internal" || pc.Database != "truss_db" {
		t.Errorf("injeksi berhasil: host=%q db=%q", pc.Host, pc.Database)
	}
}

func TestBuildDSN_EmptySSLModeUsesDriverDefaultInsteadOfBreaking(t *testing.T) {
	pc := parsed(t, config.DatabaseConfig{Host: "h", Port: "5432", User: "u", Password: "p", Name: "d", SSLMode: ""})
	if pc.Database != "d" || pc.Password != "p" {
		t.Errorf("sslmode kosong tidak boleh merusak field lain: db=%q password=%q", pc.Database, pc.Password)
	}
}

func TestBuildDSN_SSLModeIsHonored(t *testing.T) {
	disabled := parsed(t, config.DatabaseConfig{Host: "h", Port: "5432", User: "u", Password: "p", Name: "d", SSLMode: "disable"})
	if disabled.TLSConfig != nil {
		t.Error("sslmode=disable: TLS harus mati")
	}
	required := parsed(t, config.DatabaseConfig{Host: "h", Port: "5432", User: "u", Password: "p", Name: "d", SSLMode: "require"})
	if required.TLSConfig == nil {
		t.Error("sslmode=require: TLS harus aktif")
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
