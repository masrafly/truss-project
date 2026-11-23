// Package domain berisi entity dan aturan paling inti dari modul auth.
// Package ini TIDAK boleh mengimpor gin, gorm, jwt, bcrypt, atau package
// framework/infrastruktur lain.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// User adalah entity murni: tanpa tag gorm/json.
type User struct {
	ID           uuid.UUID
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RefreshToken adalah catatan refresh token. Nilai aslinya tidak pernah disimpan, hanya
// hash-nya. FamilyID sama untuk seluruh token hasil rotasi dari satu login.
type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	FamilyID  uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// Error domain. Lapisan luar (handler) menerjemahkannya ke status HTTP,
// lapisan dalam tidak perlu tahu apa itu HTTP atau GORM.
var (
	ErrInvalidInput       = errors.New("input tidak valid")
	ErrEmailAlreadyUsed   = errors.New("email sudah digunakan")
	ErrInvalidCredentials = errors.New("email atau password salah")
	// ErrWrongPassword: password saat ini salah pada ganti password (403, bukan 401, agar client
	// tidak mengira token-nya kedaluwarsa).
	ErrWrongPassword = errors.New("password saat ini salah")
	ErrUserNotFound  = errors.New("user tidak ditemukan")

	// ErrInvalidRefreshToken: refresh token tidak dikenal, kedaluwarsa, sudah dicabut, atau
	// sudah dipakai (rotasi). Semua kasus sengaja menghasilkan pesan yang sama (401).
	ErrInvalidRefreshToken = errors.New("refresh token tidak valid")
)

// ErrTooManyAttempts: terlalu banyak percobaan login gagal untuk akun ini (429).
var ErrTooManyAttempts = errors.New("terlalu banyak percobaan login, coba lagi nanti")

// TooManyAttemptsError membawa sisa waktu blokir; errors.Is(err, ErrTooManyAttempts) bernilai true.
type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string        { return ErrTooManyAttempts.Error() }
func (e *TooManyAttemptsError) Is(target error) bool { return target == ErrTooManyAttempts }
