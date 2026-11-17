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
