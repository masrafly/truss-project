package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// MinJWTSecretLength: secret HS256 yang pendek dapat ditebak secara offline dari satu
// token yang bocor, lalu dipakai memalsukan token untuk user mana pun. 32 byte = 256 bit,
// sama dengan ukuran output HMAC-SHA256.
const MinJWTSecretLength = 32

// Batas masa berlaku token. Access token pendek membatasi dampak token bocor (token itu
// tidak dapat dicabut); refresh token panjang tetapi dapat dicabut dan sekali pakai.
const (
	DefaultAccessTokenTTL  = 15 * time.Minute
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour

	minAccessTokenTTL  = time.Minute
	maxAccessTokenTTL  = 24 * time.Hour
	minRefreshTokenTTL = time.Hour
	maxRefreshTokenTTL = 90 * 24 * time.Hour
)

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

	// AccessTokenTTL (ACCESS_TOKEN_TTL, default 15m) dan RefreshTokenTTL
	// (REFRESH_TOKEN_TTL, default 720h): format time.ParseDuration, mis. "15m", "720h".
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

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

	access, err := parseTTL("ACCESS_TOKEN_TTL", DefaultAccessTokenTTL, minAccessTokenTTL, maxAccessTokenTTL)
	if err != nil {
		return nil, err
	}
	refresh, err := parseTTL("REFRESH_TOKEN_TTL", DefaultRefreshTokenTTL, minRefreshTokenTTL, maxRefreshTokenTTL)
	if err != nil {
		return nil, err
	}
	if refresh <= access {
		return nil, fmt.Errorf("REFRESH_TOKEN_TTL (%s) harus lebih lama daripada ACCESS_TOKEN_TTL (%s)", refresh, access)
	}
	cfg.AccessTokenTTL, cfg.RefreshTokenTTL = access, refresh

	proxies, err := parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return nil, err
	}
	cfg.TrustedProxies = proxies

	return cfg, nil
}

// parseTTL membaca durasi dari environment variable name; kosong = def. Di luar [min, max]
// ditolak agar salah ketik (mis. "15" tanpa satuan, atau "9999h") tidak berjalan diam-diam.
func parseTTL(name string, def, min, max time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s tidak valid: gunakan format durasi seperti 15m atau 720h", name)
	}
	if d < min || d > max {
		return 0, fmt.Errorf("%s harus antara %s dan %s", name, min, max)
	}
	return d, nil
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
