package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/auth/domain"
)

// Ports: kontrak yang dibutuhkan use case dari dunia luar.
// Interface didefinisikan di sisi pemakai (use case), implementasinya
// ada di lapisan infrastruktur (repository, platform/security).

// UserRepository wajib mengembalikan domain.ErrUserNotFound jika data tidak
// ada, dan domain.ErrEmailAlreadyUsed jika email duplikat, bukan error GORM.
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	FindByEmail(ctx context.Context, email string) (*domain.User, error)

	// UpdateName mengubah nama dan mengembalikan user terbaru; domain.ErrUserNotFound bila tidak ada.
	UpdateName(ctx context.Context, id uuid.UUID, name string) (*domain.User, error)

	// UpdatePasswordHash mengganti hash password; domain.ErrUserNotFound bila user tidak ada.
	UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error
}

type PasswordHasher interface {
	Hash(plain string) (string, error)
	Compare(hash, plain string) error
}

type TokenIssuer interface {
	Issue(userID string) (token string, expiresIn time.Duration, err error)
}

// LoginThrottle membatasi percobaan login GAGAL per akun (kunci: email ternormalisasi).
// Dipenuhi oleh platform/ratelimit.Limiter.
type LoginThrottle interface {
	Blocked(key string) (bool, time.Duration)
	Add(key string)
	Reset(key string)
}

// RefreshTokenRepository menyimpan refresh token (hanya hash).
//
// Kontrak: Rotate mengembalikan domain.ErrInvalidRefreshToken untuk token yang tidak ada,
// kedaluwarsa, atau SUDAH DICABUT. Pada kasus terakhir (pemakaian ulang token lama, tanda
// token dicuri) seluruh rantai token (family) ikut dicabut sebelum error dikembalikan.
type RefreshTokenRepository interface {
	// Create menyimpan token baru. Pemanggil mengisi UserID, FamilyID (rantai baru = UUID baru),
	// TokenHash, dan ExpiresAt.
	Create(ctx context.Context, t *domain.RefreshToken) error

	// Rotate, secara atomik: mengunci token lama, mencabutnya, dan menyimpan next dengan
	// UserID dan FamilyID yang sama. Mengembalikan ID pemilik token.
	Rotate(ctx context.Context, oldHash string, now time.Time, next *domain.RefreshToken) (uuid.UUID, error)

	// RevokeFamily mencabut seluruh rantai milik token ini. Token tidak dikenal bukan error
	// (idempoten).
	RevokeFamily(ctx context.Context, hash string, now time.Time) error

	// RevokeAllForUser mencabut semua refresh token user (logout dari semua perangkat).
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, now time.Time) error
}

// RefreshTokenCodec membuat token opaque beserta hash-nya (implementasi: platform/security).
type RefreshTokenCodec interface {
	New() (plain, hash string, err error)
	Hash(plain string) string
}
