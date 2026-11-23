package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
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

	// CORSAllowedOrigins (CORS_ALLOWED_ORIGINS): origin frontend yang boleh memanggil API dari
	// browser, mis. https://app.example.com. Kosong = tidak ada header CORS (hanya same-origin).
	CORSAllowedOrigins []string

	// AllowInsecureDB (ALLOW_INSECURE_DB=true) mengizinkan DB_SSLMODE tanpa TLS saat
	// APP_ENV=production. Hanya untuk database di jaringan tertutup yang sama (mis. Compose).
	AllowInsecureDB bool
}

// secureSSLModes: mode yang benar-benar mengenkripsi koneksi ke database. "prefer" dan "allow"
// turun ke koneksi tanpa enkripsi bila server tidak mendukung TLS (atau penyerang memaksanya).
var secureSSLModes = map[string]bool{"require": true, "verify-ca": true, "verify-full": true}

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

	origins, err := parseCORSOrigins(os.Getenv("CORS_ALLOWED_ORIGINS"), cfg.AppEnv)
	if err != nil {
		return nil, err
	}
	cfg.CORSAllowedOrigins = origins

	if raw := strings.TrimSpace(os.Getenv("ALLOW_INSECURE_DB")); raw != "" {
		allow, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, errors.New("ALLOW_INSECURE_DB tidak valid: gunakan true atau false")
		}
		cfg.AllowInsecureDB = allow
	}
	if cfg.AppEnv == "production" && !secureSSLModes[cfg.DB.SSLMode] && !cfg.AllowInsecureDB {
		return nil, fmt.Errorf(
			"DB_SSLMODE=%q tidak aman untuk production: gunakan require, verify-ca, atau verify-full "+
				"(atau ALLOW_INSECURE_DB=true bila database berada di jaringan tertutup yang sama)",
			cfg.DB.SSLMode)
	}

	return cfg, nil
}

// InsecureDBInProduction melaporkan apakah aplikasi berjalan di production dengan koneksi
// database tanpa TLS (diizinkan lewat ALLOW_INSECURE_DB); dipakai untuk peringatan saat start.
func (c *Config) InsecureDBInProduction() bool {
	return c.AppEnv == "production" && !secureSSLModes[c.DB.SSLMode]
}

// parseCORSOrigins menerima daftar origin dipisah koma. Setiap origin harus persis
// skema://host[:port] (tanpa path, query, atau garis miring di akhir); "*" ditolak karena
// mengizinkan situs mana pun memanggil API dari browser pengguna. Di production, origin
// http:// hanya diizinkan untuk localhost.
func parseCORSOrigins(s, appEnv string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(s, ",") {
		o := strings.TrimSpace(raw)
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || o != u.Scheme+"://"+u.Host {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS memuat origin tidak valid %q: gunakan bentuk https://app.example.com (tanpa path atau garis miring akhir; * tidak diizinkan)", o)
		}
		host := u.Hostname()
		if appEnv == "production" && u.Scheme == "http" && host != "localhost" && host != "127.0.0.1" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS: origin http %q tidak diizinkan di production (gunakan https)", o)
		}
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	return out, nil
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
