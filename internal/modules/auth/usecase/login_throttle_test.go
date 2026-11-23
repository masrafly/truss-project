package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/usecase"
	"truss-project/internal/platform/ratelimit"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// throttled: user Budi terdaftar, throttle 3 gagal per 15 menit dengan jam palsu.
func throttled(t *testing.T) (*usecase.AuthUseCase, *fakeHasher, *clock) {
	t.Helper()
	uc, h := registered(t)
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uc.WithLoginThrottle(ratelimit.New(3, 15*time.Minute).WithClock(c.now))
	return uc, h, c
}

var bg = context.Background()

func TestThrottle_BlocksAfterFailuresEvenWithCorrectPassword(t *testing.T) {
	uc, h, c := throttled(t)
	for i := 0; i < 3; i++ {
		if _, err := uc.Login(bg, "budi@example.com", "salahsalah"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("gagal ke-%d: dapat %v", i+1, err)
		}
	}
	compares := h.compareCalls
	c.t = c.t.Add(5 * time.Minute)

	_, err := uc.Login(bg, "budi@example.com", "password123")

	var tm *domain.TooManyAttemptsError
	if !errors.As(err, &tm) || !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("harapannya TooManyAttemptsError, dapat %v", err)
	}
	if tm.RetryAfter != 10*time.Minute {
		t.Errorf("RetryAfter = %v, harapannya 10m", tm.RetryAfter)
	}
	if h.compareCalls != compares {
		t.Error("akun terblokir tidak boleh menjalankan bcrypt (hemat CPU)")
	}
}

func TestThrottle_ExpiresAfterWindow(t *testing.T) {
	uc, _, c := throttled(t)
	for i := 0; i < 3; i++ {
		uc.Login(bg, "budi@example.com", "salahsalah")
	}
	c.t = c.t.Add(15 * time.Minute)
	if _, err := uc.Login(bg, "budi@example.com", "password123"); err != nil {
		t.Fatalf("setelah jendela berakhir login harus bisa: %v", err)
	}
}

func TestThrottle_SuccessResetsCounter(t *testing.T) {
	uc, _, _ := throttled(t)
	uc.Login(bg, "budi@example.com", "salahsalah")
	uc.Login(bg, "budi@example.com", "salahsalah")
	if _, err := uc.Login(bg, "budi@example.com", "password123"); err != nil {
		t.Fatal(err)
	}
	// Setelah berhasil, hitungan kembali nol: 2 gagal lagi belum memblokir.
	uc.Login(bg, "budi@example.com", "salahsalah")
	uc.Login(bg, "budi@example.com", "salahsalah")
	if _, err := uc.Login(bg, "budi@example.com", "password123"); err != nil {
		t.Fatalf("login berhasil harus mereset hitungan: %v", err)
	}
}

// Email tidak terdaftar dihitung dan diblokir dengan cara yang sama, sehingga blokir
// tidak membocorkan keberadaan akun.
func TestThrottle_UnknownEmailBehavesTheSame(t *testing.T) {
	uc, _, _ := throttled(t)
	for i := 0; i < 3; i++ {
		uc.Login(bg, "hantu@example.com", "password123")
	}
	if _, err := uc.Login(bg, "hantu@example.com", "password123"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("email tak terdaftar juga harus terblokir, dapat %v", err)
	}
}

func TestThrottle_KeyIsNormalizedEmailAndPerAccount(t *testing.T) {
	uc, _, _ := throttled(t)
	for _, e := range []string{"BUDI@example.com", " budi@EXAMPLE.com ", "budi@example.com"} {
		uc.Login(bg, e, "salahsalah")
	}
	if _, err := uc.Login(bg, "budi@example.com", "password123"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("variasi huruf/spasi harus dihitung sebagai akun yang sama, dapat %v", err)
	}
	if _, err := uc.Login(bg, "lain@example.com", "password123"); errors.Is(err, domain.ErrTooManyAttempts) {
		t.Error("akun lain tidak boleh ikut terblokir")
	}
}

func TestThrottle_InfrastructureErrorNotCountedAsFailure(t *testing.T) {
	// Kegagalan teknis (bukan kredensial salah) tidak boleh mengunci akun.
	broken := usecase.NewAuthUseCase(errRepo{}, &fakeHasher{}, fakeTokens{}, nil)
	th := ratelimit.New(1, time.Minute)
	broken.WithLoginThrottle(th)
	broken.Login(bg, "budi@example.com", "password123")
	if blocked, _ := th.Blocked("budi@example.com"); blocked {
		t.Error("error database tidak boleh dihitung sebagai login gagal")
	}
}

func TestThrottle_DisabledByDefault(t *testing.T) {
	uc, _ := registered(t)
	for i := 0; i < 50; i++ {
		uc.Login(bg, "budi@example.com", "salahsalah")
	}
	if _, err := uc.Login(bg, "budi@example.com", "password123"); err != nil {
		t.Fatalf("tanpa throttle tidak ada blokir: %v", err)
	}
}

type errRepo struct{}

func (errRepo) Create(context.Context, *domain.User) error { return errors.New("db mati") }
func (errRepo) FindByID(context.Context, uuid.UUID) (*domain.User, error) {
	return nil, errors.New("db mati")
}
func (errRepo) FindByEmail(context.Context, string) (*domain.User, error) {
	return nil, errors.New("db mati")
}

func (errRepo) UpdateName(context.Context, uuid.UUID, string) (*domain.User, error) {
	return nil, errors.New("db mati")
}
func (errRepo) UpdatePasswordHash(context.Context, uuid.UUID, string) error {
	return errors.New("db mati")
}
