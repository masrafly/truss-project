package config

import (
	"strings"
	"testing"
)

const validSecret = "0123456789abcdef0123456789abcdef" // 32 byte

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"APP_ENV", "APP_PORT", "LOG_LEVEL", "JWT_SECRET", "TRUSTED_PROXIES"} {
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
