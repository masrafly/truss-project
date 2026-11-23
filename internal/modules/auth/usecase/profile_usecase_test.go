package usecase_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/platform/ratelimit"
)

// ---------- UpdateProfile ----------

func TestUpdateProfile_TrimsAndPersistsWithoutHash(t *testing.T) {
	f := newRefreshFixture(t)
	got, err := f.uc.UpdateProfile(ctx, f.user.ID.String(), "  Budi Baru  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Budi Baru" || got.PasswordHash != "" {
		t.Errorf("hasil = %+v", got)
	}
	if f.users.byEmail["budi@example.com"].Name != "Budi Baru" {
		t.Error("nama harus tersimpan")
	}
}

func TestUpdateProfile_Validation(t *testing.T) {
	f := newRefreshFixture(t)
	id := f.user.ID.String()
	for name, in := range map[string]string{
		"kosong": "", "spasi saja": "   ", "101 karakter": strings.Repeat("a", 101),
	} {
		if _, err := f.uc.UpdateProfile(ctx, id, in); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := f.uc.UpdateProfile(ctx, id, strings.Repeat("é", 100)); err != nil {
		t.Errorf("100 karakter (multi-byte) harus diterima: %v", err)
	}
	if _, err := f.uc.UpdateProfile(ctx, "bukan-uuid", "X"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("ID rusak: %v", err)
	}
	if _, err := f.uc.UpdateProfile(ctx, uuid.NewString(), "X"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("user tidak ada: %v", err)
	}
	boom := errors.New("db mati")
	f.users.updateErr = boom
	if _, err := f.uc.UpdateProfile(ctx, id, "X"); !errors.Is(err, boom) {
		t.Errorf("error repository: %v", err)
	}
}

// ---------- ChangePassword ----------

func TestChangePassword_Success_NewPasswordWorksOldDoesNot_AllSessionsRevoked(t *testing.T) {
	f := newRefreshFixture(t)
	s1, s2 := f.login(t), f.login(t)

	if err := f.uc.ChangePassword(ctx, f.user.ID.String(), "password123", "passwordBaru1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.uc.Login(ctx, "budi@example.com", "passwordBaru1"); err != nil {
		t.Errorf("password baru harus bisa login: %v", err)
	}
	if _, err := f.uc.Login(ctx, "budi@example.com", "password123"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("password lama harus ditolak: %v", err)
	}
	for _, tok := range []string{s1.RefreshToken, s2.RefreshToken} {
		if _, err := f.uc.Refresh(ctx, tok); !errors.Is(err, domain.ErrInvalidRefreshToken) {
			t.Errorf("semua sesi lama harus dicabut, dapat %v", err)
		}
	}
	if h := f.users.byEmail["budi@example.com"].PasswordHash; h == "passwordBaru1" || h != "hashed:passwordBaru1" {
		t.Errorf("yang tersimpan harus hash dari password baru, dapat %q", h)
	}
}

func TestChangePassword_RejectsBadInputWithoutTouchingAnything(t *testing.T) {
	f := newRefreshFixture(t)
	id := f.user.ID.String()
	before := f.users.byEmail["budi@example.com"].PasswordHash
	for name, c := range map[string][2]string{
		"password saat ini kosong":       {"", "passwordBaru1"},
		"password baru pendek":           {"password123", "short"},
		"password baru > 72 byte":        {"password123", strings.Repeat("a", 73)},
		"password baru sama dengan lama": {"password123", "password123"},
		"password saat ini > 72 byte":    {strings.Repeat("a", 73), "passwordBaru1"},
	} {
		if err := f.uc.ChangePassword(ctx, id, c[0], c[1]); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if f.users.byEmail["budi@example.com"].PasswordHash != before {
		t.Error("hash tidak boleh berubah")
	}
	if f.rt.revokeAllCalls != 0 {
		t.Error("sesi tidak boleh dicabut untuk input tidak valid")
	}
}

func TestChangePassword_WrongCurrentPasswordIsForbiddenAndChangesNothing(t *testing.T) {
	f := newRefreshFixture(t)
	f.login(t)
	before := f.users.byEmail["budi@example.com"].PasswordHash

	err := f.uc.ChangePassword(ctx, f.user.ID.String(), "salahsalah", "passwordBaru1")

	if !errors.Is(err, domain.ErrWrongPassword) {
		t.Fatalf("err = %v", err)
	}
	if f.users.byEmail["budi@example.com"].PasswordHash != before || f.rt.revokeAllCalls != 0 {
		t.Error("password dan sesi tidak boleh berubah saat password saat ini salah")
	}
}

func TestChangePassword_UnknownUser(t *testing.T) {
	f := newRefreshFixture(t)
	if err := f.uc.ChangePassword(ctx, "bukan-uuid", "a", "passwordBaru1"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("ID rusak: %v", err)
	}
	if err := f.uc.ChangePassword(ctx, uuid.NewString(), "password123", "passwordBaru1"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("user tidak ada: %v", err)
	}
}

func TestChangePassword_RevokesSessionsBeforeUpdatingPassword(t *testing.T) {
	// Bila penyimpanan password gagal, sesi sudah dicabut dan password lama masih berlaku:
	// user cukup login ulang. Urutan terbalik dapat meninggalkan sesi hidup setelah ganti password.
	f := newRefreshFixture(t)
	s := f.login(t)
	boom := errors.New("db mati")
	f.users.updateErr = boom

	err := f.uc.ChangePassword(ctx, f.user.ID.String(), "password123", "passwordBaru1")

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.uc.Refresh(ctx, s.RefreshToken); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("sesi harus sudah dicabut, dapat %v", err)
	}
	f.users.updateErr = nil
	if _, err := f.uc.Login(ctx, "budi@example.com", "password123"); err != nil {
		t.Errorf("password lama harus masih berlaku: %v", err)
	}
}

func TestChangePassword_RevokeFailureAbortsBeforePasswordChanges(t *testing.T) {
	f := newRefreshFixture(t)
	before := f.users.byEmail["budi@example.com"].PasswordHash
	boom := errors.New("db mati")
	f.rt.revokeAllErr = boom

	if err := f.uc.ChangePassword(ctx, f.user.ID.String(), "password123", "passwordBaru1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if f.users.byEmail["budi@example.com"].PasswordHash != before {
		t.Error("password tidak boleh berubah bila pencabutan sesi gagal")
	}
}

func TestChangePassword_HasherFailurePropagates(t *testing.T) {
	f := newRefreshFixture(t)
	h := &fakeHasher{}
	uc := newUC(f.users, h, fakeTokens{}).WithRefreshTokens(f.rt, f.codec, testRefreshTTL)
	h.hashErr = errors.New("bcrypt mati")
	if err := uc.ChangePassword(ctx, f.user.ID.String(), "password123", "passwordBaru1"); err == nil ||
		errors.Is(err, domain.ErrWrongPassword) {
		t.Errorf("err = %v", err)
	}
}

func TestChangePassword_WorksWithoutRefreshTokensConfigured(t *testing.T) {
	uc2 := newUC(newFakeRepo(), &fakeHasher{}, fakeTokens{})
	u, err := uc2.Register(ctx, "A", "a@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := uc2.ChangePassword(ctx, u.ID.String(), "password123", "passwordBaru1"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc2.Login(ctx, "a@example.com", "passwordBaru1"); err != nil {
		t.Errorf("login dengan password baru: %v", err)
	}
}

func TestChangePassword_WrongAttemptsAreThrottledPerUserAndResetOnSuccess(t *testing.T) {
	f := newRefreshFixture(t)
	other, err := f.uc.Register(ctx, "Ani", "ani@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	f.uc.WithLoginThrottle(ratelimit.New(3, 15*time.Minute).WithClock(func() time.Time { return f.now }))
	id := f.user.ID.String()

	for i := 0; i < 3; i++ {
		if err := f.uc.ChangePassword(ctx, id, "salahsalah", "passwordBaru1"); !errors.Is(err, domain.ErrWrongPassword) {
			t.Fatalf("percobaan %d: %v", i+1, err)
		}
	}
	// Setelah batas, bahkan password yang BENAR ditolak sementara (tanpa menyentuh bcrypt).
	var tm *domain.TooManyAttemptsError
	err = f.uc.ChangePassword(ctx, id, "password123", "passwordBaru1")
	if !errors.As(err, &tm) || tm.RetryAfter <= 0 {
		t.Fatalf("harus 429 dengan RetryAfter, dapat %v", err)
	}
	// User lain tidak terpengaruh; login user ini juga tidak (kunci berbeda).
	if err := f.uc.ChangePassword(ctx, other.ID.String(), "password123", "passwordBaru1"); err != nil {
		t.Errorf("user lain tidak boleh ikut terblokir: %v", err)
	}
	if _, err := f.uc.Login(ctx, "budi@example.com", "password123"); err != nil {
		t.Errorf("percobaan ganti password tidak boleh mengunci login: %v", err)
	}
	// Jendela berakhir lalu berhasil: penghitung di-reset.
	f.now = f.now.Add(16 * time.Minute)
	if err := f.uc.ChangePassword(ctx, id, "password123", "passwordBaru1"); err != nil {
		t.Errorf("setelah jendela berakhir harus berhasil: %v", err)
	}
}

func TestChangePassword_SuccessResetsFailureCounter(t *testing.T) {
	f := newRefreshFixture(t)
	f.uc.WithLoginThrottle(ratelimit.New(3, 15*time.Minute).WithClock(func() time.Time { return f.now }))
	id := f.user.ID.String()

	for i := 0; i < 2; i++ {
		_ = f.uc.ChangePassword(ctx, id, "salahsalah", "passwordBaru1")
	}
	if err := f.uc.ChangePassword(ctx, id, "password123", "passwordBaru1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_ = f.uc.ChangePassword(ctx, id, "salahsalah", "passwordLain1")
	}
	// Total gagal sejak awal = 4 (>= batas 3), tetapi penghitung sempat di-reset oleh keberhasilan:
	// yang berlaku hanya 2 kegagalan terakhir, jadi percobaan ini tidak boleh diblokir.
	if err := f.uc.ChangePassword(ctx, id, "passwordBaru1", "passwordLain1"); err != nil {
		t.Fatalf("penghitung harus di-reset setelah berhasil, dapat %v", err)
	}
}
