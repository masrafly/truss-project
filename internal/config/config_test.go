package config

import (
	"strings"
	"testing"
	"time"
)

const validSecret = "0123456789abcdef0123456789abcdef" // 32 byte

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"APP_ENV", "APP_PORT", "LOG_LEVEL", "JWT_SECRET", "TRUSTED_PROXIES", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL", "CORS_ALLOWED_ORIGINS", "ALLOW_INSECURE_DB", "DB_SSLMODE"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": validSecret})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "development" || cfg.AppPort != "8080" || cfg.LogLevel != "info" {
		t.Errorf("default salah: %+v", cfg)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("default tidak boleh mempercayai proxy apa pun: %v", cfg.TrustedProxies)
	}
	if cfg.AccessTokenTTL != 15*time.Minute || cfg.RefreshTokenTTL != 720*time.Hour {
		t.Errorf("TTL default = %v / %v", cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	}
}

func TestLoad_TokenTTLs(t *testing.T) {
	cases := []struct {
		name            string
		access, refresh string
		wantA, wantR    time.Duration
		wantErr         string // potongan pesan error; kosong = harus berhasil
	}{
		{"nilai kustom", "30m", "168h", 30 * time.Minute, 168 * time.Hour, ""},
		{"spasi diabaikan", " 10m ", "", 10 * time.Minute, 720 * time.Hour, ""},
		{"access tanpa satuan", "15", "", 0, 0, "ACCESS_TOKEN_TTL tidak valid"},
		{"refresh bukan durasi", "", "sebulan", 0, 0, "REFRESH_TOKEN_TTL tidak valid"},
		{"access terlalu pendek", "10s", "", 0, 0, "ACCESS_TOKEN_TTL harus antara"},
		{"access terlalu panjang", "48h", "2000h", 0, 0, "ACCESS_TOKEN_TTL harus antara"},
		{"refresh terlalu panjang", "", "9999h", 0, 0, "REFRESH_TOKEN_TTL harus antara"},
		{"refresh tidak lebih lama dari access", "2h", "1h", 0, 0, "harus lebih lama"},
		{"refresh sama dengan access", "2h", "2h", 0, 0, "harus lebih lama"},
		{"negatif", "-5m", "", 0, 0, "ACCESS_TOKEN_TTL harus antara"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, map[string]string{
				"JWT_SECRET": validSecret, "ACCESS_TOKEN_TTL": tc.access, "REFRESH_TOKEN_TTL": tc.refresh,
			})
			cfg, err := Load()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, harapannya memuat %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || cfg.AccessTokenTTL != tc.wantA || cfg.RefreshTokenTTL != tc.wantR {
				t.Fatalf("cfg = %+v err = %v", cfg, err)
			}
		})
	}
}

func TestLoad_JWTSecretLength(t *testing.T) {
	cases := map[string]bool{
		"":                      false,
		"rahasia":               false,
		validSecret[:31]:        false,
		validSecret:             true,
		strings.Repeat("x", 64): true,
	}
	for secret, ok := range cases {
		setEnv(t, map[string]string{"JWT_SECRET": secret})
		_, err := Load()
		if (err == nil) != ok {
			t.Errorf("secret %d byte: err=%v, harapannya ok=%v", len(secret), err, ok)
		}
		if err != nil && secret != "" && strings.Contains(err.Error(), secret) {
			t.Errorf("pesan error tidak boleh memuat secret: %v", err)
		}
	}
}

func TestLoad_TrustedProxies(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": validSecret, "TRUSTED_PROXIES": " 10.0.0.1, 172.16.0.0/12 ,,::1"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.1", "172.16.0.0/12", "::1"}
	if strings.Join(cfg.TrustedProxies, "|") != strings.Join(want, "|") {
		t.Errorf("TrustedProxies = %v, harapannya %v", cfg.TrustedProxies, want)
	}

	for _, bad := range []string{"localhost", "10.0.0.300", "10.0.0.0/33", "*"} {
		setEnv(t, map[string]string{"JWT_SECRET": validSecret, "TRUSTED_PROXIES": bad})
		if _, err := Load(); err == nil {
			t.Errorf("%q harus ditolak", bad)
		}
	}
}

func TestLoad_ProductionRequiresEncryptedDatabaseConnection(t *testing.T) {
	cases := []struct {
		name, env, sslmode, allow string
		wantErr                   bool
	}{
		{"production require", "production", "require", "", false},
		{"production verify-ca", "production", "verify-ca", "", false},
		{"production verify-full", "production", "verify-full", "", false},
		{"production disable", "production", "disable", "", true},
		{"production prefer", "production", "prefer", "", true},
		{"production allow", "production", "allow", "", true},
		{"production kosong (default driver = prefer)", "production", "", "", true},
		{"production disable + override", "production", "disable", "true", false},
		{"production kosong + override", "production", "", "1", false},
		{"override false tidak melonggarkan", "production", "disable", "false", true},
		{"development disable", "development", "disable", "", false},
		{"development kosong", "development", "", "", false},
		{"APP_ENV kosong = development", "", "disable", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, map[string]string{
				"JWT_SECRET": validSecret, "APP_ENV": tc.env, "DB_SSLMODE": tc.sslmode, "ALLOW_INSECURE_DB": tc.allow,
			})
			cfg, err := Load()
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "DB_SSLMODE") || !strings.Contains(err.Error(), "ALLOW_INSECURE_DB") {
					t.Fatalf("harus ditolak dengan pesan yang menyebut DB_SSLMODE dan ALLOW_INSECURE_DB, dapat %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("tidak boleh error: %v", err)
			}
			if got, want := cfg.InsecureDBInProduction(), tc.env == "production" && !secureSSLModes[tc.sslmode]; got != want {
				t.Errorf("InsecureDBInProduction = %v, harapannya %v", got, want)
			}
		})
	}
}

func TestLoad_AllowInsecureDBMustBeBoolean(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": validSecret, "ALLOW_INSECURE_DB": "mungkin"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ALLOW_INSECURE_DB") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoad_CORSAllowedOrigins(t *testing.T) {
	cases := []struct {
		name, env, in string
		want          []string
		wantErr       bool
	}{
		{"kosong", "development", "", nil, false},
		{"satu origin", "production", "https://app.example.com", []string{"https://app.example.com"}, false},
		{"beberapa + spasi + duplikat", "production", " https://a.example.com , https://b.example.com:8443,https://a.example.com ", []string{"https://a.example.com", "https://b.example.com:8443"}, false},
		{"localhost http di production", "production", "http://localhost:3000", []string{"http://localhost:3000"}, false},
		{"127.0.0.1 http di production", "production", "http://127.0.0.1:5173", []string{"http://127.0.0.1:5173"}, false},
		{"http biasa di development", "development", "http://dev.local:3000", []string{"http://dev.local:3000"}, false},
		{"http biasa di production", "production", "http://app.example.com", nil, true},
		{"wildcard", "development", "*", nil, true},
		{"wildcard di antara origin sah", "development", "https://a.example.com,*", nil, true},
		{"garis miring akhir", "development", "https://app.example.com/", nil, true},
		{"memuat path", "development", "https://app.example.com/app", nil, true},
		{"tanpa skema", "development", "app.example.com", nil, true},
		{"skema lain", "development", "ftp://app.example.com", nil, true},
		{"memuat userinfo", "development", "https://user@app.example.com", nil, true},
		{"memuat query", "development", "https://app.example.com?x=1", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCORSOrigins(tc.in, tc.env)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
					t.Fatalf("harus error yang menyebut CORS_ALLOWED_ORIGINS, dapat %v", err)
				}
				return
			}
			if err != nil || strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("got %v err %v, harapannya %v", got, err, tc.want)
			}
		})
	}
}

func TestLoad_CORSDefaultsToNone(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": validSecret})
	cfg, err := Load()
	if err != nil || len(cfg.CORSAllowedOrigins) != 0 {
		t.Fatalf("cfg = %+v err = %v", cfg, err)
	}
}
