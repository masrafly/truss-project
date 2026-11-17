package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// MinJWTSecretLength: secret HS256 yang pendek dapat ditebak secara offline dari satu
// token yang bocor, lalu dipakai memalsukan token untuk user mana pun. 32 byte = 256 bit,
// sama dengan ukuran output HMAC-SHA256.
const MinJWTSecretLength = 32

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

type Config struct {
	AppEnv    string // development | production
	AppPort   string
	LogLevel  string // debug | info | warn | error
	JWTSecret string
	DB        DatabaseConfig

	// TrustedProxies: IP/CIDR reverse proxy yang boleh menentukan IP client lewat
	// X-Forwarded-For. Kosong = tidak ada (IP client = alamat koneksi langsung).
	// Penting untuk log dan rate limiting per IP agar tidak bisa dipalsukan.
	TrustedProxies []string
}

// Load membaca environment variable sekali, di awal aplikasi.
func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:    os.Getenv("APP_ENV"),
		AppPort:   os.Getenv("APP_PORT"),
		LogLevel:  os.Getenv("LOG_LEVEL"),
		JWTSecret: os.Getenv("JWT_SECRET"),
		DB: DatabaseConfig{
			Host:     os.Getenv("DB_HOST"),
			Port:     os.Getenv("DB_PORT"),
			User:     os.Getenv("DB_USER"),
			Password: os.Getenv("DB_PASSWORD"),
			Name:     os.Getenv("DB_NAME"),
			SSLMode:  os.Getenv("DB_SSLMODE"),
		},
	}

	if cfg.AppEnv == "" {
		cfg.AppEnv = "development"
	}
	if cfg.AppPort == "" {
		cfg.AppPort = "8080"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET belum dikonfigurasi")
	}
	if len(cfg.JWTSecret) < MinJWTSecretLength {
		// Nilai secret sengaja tidak disertakan di pesan error.
		return nil, fmt.Errorf("JWT_SECRET terlalu pendek: minimal %d byte (buat dengan: go run ./cmd/genjwtsecret)", MinJWTSecretLength)
	}

	proxies, err := parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return nil, err
	}
	cfg.TrustedProxies = proxies

	return cfg, nil
}

// parseTrustedProxies menerima daftar IP/CIDR dipisah koma. Nilai tidak valid ditolak
// saat start, bukan diabaikan diam-diam.
func parseTrustedProxies(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES berisi nilai tidak valid: %q", p)
			}
		}
		out = append(out, p)
	}
	return out, nil
}
