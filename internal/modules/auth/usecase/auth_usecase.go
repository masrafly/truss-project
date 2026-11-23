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

	// Refresh token asli berukuran 43 karakter; batas longgar menolak sampah besar sebelum
	// menyentuh database.
	maxRefreshTokenLength = 512
)

type AuthUseCase struct {
	users  UserRepository
	hasher PasswordHasher
	tokens TokenIssuer
	log    *slog.Logger

	// throttle (opsional) membatasi login gagal per akun. nil = nonaktif.
	throttle LoginThrottle

	// Refresh token (opsional). nil = fitur nonaktif: login tidak menerbitkan refresh token.
	refresh    RefreshTokenRepository
	refreshGen RefreshTokenCodec
	refreshTTL time.Duration
	now        func() time.Time

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
		now:    time.Now,
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

// WithRefreshTokens mengaktifkan refresh token: login menerbitkannya, Refresh merotasinya
// (sekali pakai), dan Logout/LogoutAll mencabutnya.
func (u *AuthUseCase) WithRefreshTokens(repo RefreshTokenRepository, codec RefreshTokenCodec, ttl time.Duration) *AuthUseCase {
	u.refresh, u.refreshGen, u.refreshTTL = repo, codec, ttl
	return u
}

// WithClock mengganti sumber waktu (untuk test).
func (u *AuthUseCase) WithClock(now func() time.Time) *AuthUseCase {
	u.now = now
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

	// RefreshToken kosong bila refresh token tidak diaktifkan.
	RefreshToken     string
	RefreshExpiresIn time.Duration
}

// TokenPair adalah hasil Refresh: access token baru dan refresh token pengganti.
type TokenPair struct {
	AccessToken      string
	ExpiresIn        time.Duration
	RefreshToken     string
	RefreshExpiresIn time.Duration
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

	res := &LoginResult{AccessToken: token, ExpiresIn: expiresIn}
	if u.refresh != nil {
		// Login baru memulai rantai (family) baru.
		plain, err := u.storeRefreshToken(ctx, user.ID, uuid.New())
		if err != nil {
			return nil, err
		}
		res.RefreshToken, res.RefreshExpiresIn = plain, u.refreshTTL
	}

	u.log.InfoContext(ctx, "login berhasil", "user_id", user.ID)

	user.PasswordHash = ""
	res.User = user
	return res, nil
}

// storeRefreshToken membuat dan menyimpan refresh token baru; mengembalikan nilai aslinya.
func (u *AuthUseCase) storeRefreshToken(ctx context.Context, userID, family uuid.UUID) (string, error) {
	plain, hash, err := u.refreshGen.New()
	if err != nil {
		return "", err
	}
	t := &domain.RefreshToken{
		UserID: userID, FamilyID: family, TokenHash: hash,
		ExpiresAt: u.now().Add(u.refreshTTL),
	}
	if err := u.refresh.Create(ctx, t); err != nil {
		return "", err
	}
	return plain, nil
}

// Refresh menukar refresh token yang sah dengan access token baru DAN refresh token
// pengganti; token lama langsung tidak berlaku. Memakai ulang token lama mencabut seluruh
// rantainya (lihat RefreshTokenRepository). Semua kegagalan token bermuara ke
// ErrInvalidRefreshToken agar tidak membocorkan status token.
func (u *AuthUseCase) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if u.refresh == nil || refreshToken == "" || len(refreshToken) > maxRefreshTokenLength {
		return nil, domain.ErrInvalidRefreshToken
	}

	plain, hash, err := u.refreshGen.New()
	if err != nil {
		return nil, err
	}
	now := u.now()
	next := &domain.RefreshToken{TokenHash: hash, ExpiresAt: now.Add(u.refreshTTL)}

	userID, err := u.refresh.Rotate(ctx, u.refreshGen.Hash(refreshToken), now, next)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidRefreshToken) {
			u.log.WarnContext(ctx, "refresh token ditolak")
		}
		return nil, err
	}

	// Akun yang sudah dihapus tidak boleh memperpanjang sesi.
	if _, err := u.users.FindByID(ctx, userID); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			if rerr := u.refresh.RevokeAllForUser(ctx, userID, now); rerr != nil {
				return nil, rerr
			}
			return nil, domain.ErrInvalidRefreshToken
		}
		return nil, err
	}

	access, expiresIn, err := u.tokens.Issue(userID.String())
	if err != nil {
		return nil, err
	}
	u.log.InfoContext(ctx, "token diperbarui", "user_id", userID)
	return &TokenPair{
		AccessToken: access, ExpiresIn: expiresIn,
		RefreshToken: plain, RefreshExpiresIn: u.refreshTTL,
	}, nil
}

// Logout mencabut seluruh rantai refresh token milik token ini. Idempoten: token yang tidak
// dikenal atau sudah dicabut dianggap berhasil (client tetap dapat membuang sesinya).
func (u *AuthUseCase) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" || len(refreshToken) > maxRefreshTokenLength {
		return domain.ErrInvalidInput
	}
	if u.refresh == nil {
		return nil
	}
	return u.refresh.RevokeFamily(ctx, u.refreshGen.Hash(refreshToken), u.now())
}

// LogoutAll mencabut semua refresh token user (semua perangkat).
func (u *AuthUseCase) LogoutAll(ctx context.Context, userID string) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return domain.ErrUserNotFound
	}
	if u.refresh == nil {
		return nil
	}
	if err := u.refresh.RevokeAllForUser(ctx, id, u.now()); err != nil {
		return err
	}
	u.log.InfoContext(ctx, "logout dari semua perangkat", "user_id", id)
	return nil
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
