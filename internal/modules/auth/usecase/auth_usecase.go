// Package usecase berisi logika bisnis aplikasi. Hanya bergantung pada
// domain dan interface (ports), tidak pada gin/gorm/bcrypt/jwt.
package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"truss-project/internal/modules/auth/domain"
)

const (
	minPasswordLength = 8  // dalam karakter
	maxPasswordLength = 72 // dalam byte, batas bcrypt
	maxNameLength     = 100
	maxEmailLength    = 255
)

type AuthUseCase struct {
	users  UserRepository
	hasher PasswordHasher
	tokens TokenIssuer
	log    *slog.Logger

	// throttle (opsional) membatasi login gagal per akun. nil = nonaktif.
	throttle LoginThrottle

	// dummyHash dipakai agar login dengan email tidak terdaftar tetap
	// menjalankan perbandingan bcrypt (waktu respons sama dengan password salah),
	// sehingga email terdaftar tidak bisa ditebak lewat timing.
	dummyHash string
}

// log boleh nil (mis. pada unit test); dalam kasus itu log dibuang.
func NewAuthUseCase(
	users UserRepository,
	hasher PasswordHasher,
	tokens TokenIssuer,
	log *slog.Logger,
) *AuthUseCase {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	u := &AuthUseCase{
		users:  users,
		hasher: hasher,
		tokens: tokens,
		log:    log.With("module", "auth"),
	}

	// Jika gagal, dummyHash kosong: login tetap benar, hanya proteksi timing hilang.
	if h, err := hasher.Hash("truss-dummy-password"); err == nil {
		u.dummyHash = h
	} else {
		u.log.Warn("gagal membuat dummy hash; proteksi timing login nonaktif", "error", err)
	}

	return u
}

// WithLoginThrottle mengaktifkan batas percobaan login gagal per akun.
//
// Trade-off yang disadari: penyerang yang tahu email korban bisa sengaja gagal login
// untuk mengunci akun itu sementara (sampai jendela berakhir). Karena itu batasnya
// longgar dan sementara; perlindungan per IP ada di middleware.
func (u *AuthUseCase) WithLoginThrottle(t LoginThrottle) *AuthUseCase {
	u.throttle = t
	return u
}

// validPassword: minimal 8 karakter, maksimal 72 byte (batas bcrypt).
func validPassword(p string) bool {
	return utf8.RuneCountInString(p) >= minPasswordLength && len(p) <= maxPasswordLength
}

type LoginResult struct {
	User        *domain.User
	AccessToken string
	ExpiresIn   time.Duration
}

func (u *AuthUseCase) Register(
	ctx context.Context,
	name, email, password string,
) (*domain.User, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))

	// Batas panjang mengikuti kolom (VARCHAR(100) / VARCHAR(255)); tanpa ini input
	// panjang menjadi error database (500) alih-alih 400.
	if name == "" || utf8.RuneCountInString(name) > maxNameLength ||
		email == "" || utf8.RuneCountInString(email) > maxEmailLength ||
		!validPassword(password) {
		return nil, domain.ErrInvalidInput
	}

	_, err := u.users.FindByEmail(ctx, email)
	if err == nil {
		return nil, domain.ErrEmailAlreadyUsed
	}
	if !errors.Is(err, domain.ErrUserNotFound) {
		return nil, err
	}

	hash, err := u.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	user := &domain.User{Name: name, Email: email, PasswordHash: hash}
	if err := u.users.Create(ctx, user); err != nil {
		return nil, err
	}

	u.log.InfoContext(ctx, "user terdaftar", "user_id", user.ID)

	user.PasswordHash = ""
	return user, nil
}

func (u *AuthUseCase) Login(
	ctx context.Context,
	email, password string,
) (*LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if email == "" || password == "" || len(password) > maxPasswordLength {
		return nil, domain.ErrInvalidCredentials
	}

	// Dicek SEBELUM bcrypt agar akun yang sedang diserang tidak ikut membebani CPU.
	// Email terdaftar dan tidak terdaftar dihitung dengan cara sama, sehingga blokir
	// tidak membocorkan keberadaan akun.
	if u.throttle != nil {
		if blocked, retry := u.throttle.Blocked(email); blocked {
			u.log.WarnContext(ctx, "login ditolak: terlalu banyak percobaan gagal")
			return nil, &domain.TooManyAttemptsError{RetryAfter: retry}
		}
	}

	user, err := u.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			// Jalankan perbandingan palsu agar waktu respons setara dengan
			// kasus password salah. Hasilnya sengaja diabaikan.
			_ = u.hasher.Compare(u.dummyHash, password)

			// Email tidak dicatat (data pribadi); cukup peristiwanya.
			u.log.WarnContext(ctx, "login gagal", "reason", "email tidak terdaftar")
			u.recordFailure(email)
			return nil, domain.ErrInvalidCredentials
		}
		return nil, err
	}

	if err := u.hasher.Compare(user.PasswordHash, password); err != nil {
		u.log.WarnContext(ctx, "login gagal", "reason", "password salah", "user_id", user.ID)
		u.recordFailure(email)
		return nil, domain.ErrInvalidCredentials
	}

	if u.throttle != nil {
		u.throttle.Reset(email)
	}

	token, expiresIn, err := u.tokens.Issue(user.ID.String())
	if err != nil {
		return nil, err
	}

	u.log.InfoContext(ctx, "login berhasil", "user_id", user.ID)

	user.PasswordHash = ""
	return &LoginResult{User: user, AccessToken: token, ExpiresIn: expiresIn}, nil
}

func (u *AuthUseCase) recordFailure(email string) {
	if u.throttle != nil {
		u.throttle.Add(email)
	}
}

func (u *AuthUseCase) GetProfile(ctx context.Context, userID string) (*domain.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}

	user, err := u.users.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	user.PasswordHash = ""
	return user, nil
}
