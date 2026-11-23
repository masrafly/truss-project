package config

import (
	"strings"
	"testing"
	"time"
)

const validSecret = "0123456789abcdef0123456789abcdef" // 32 byte

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"APP_ENV", "APP_PORT", "LOG_LEVEL", "JWT_SECRET", "TRUSTED_PROXIES", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL"} {
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
