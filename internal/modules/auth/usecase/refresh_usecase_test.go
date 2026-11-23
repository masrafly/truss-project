package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/usecase"
)

// ---------- fakes ----------

// fakeRefreshRepo meniru semantik RefreshTokenRepository (rotasi sekali pakai, pemakaian ulang
// mencabut family). Perilaku SQL aslinya diuji di integration test repository.
type fakeRefreshRepo struct {
	tokens map[string]*domain.RefreshToken // kunci: hash

	rotateErr, createErr, revokeFamilyErr, revokeAllErr error
	rotateCalls, revokeAllCalls                         int
	lastRevokeAllUser                                   uuid.UUID
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{tokens: map[string]*domain.RefreshToken{}}
}

func (r *fakeRefreshRepo) Create(_ context.Context, t *domain.RefreshToken) error {
	if r.createErr != nil {
		return r.createErr
	}
	cp := *t
	r.tokens[t.TokenHash] = &cp
	return nil
}

func (r *fakeRefreshRepo) revokeFamily(fam uuid.UUID, now time.Time) {
	for _, t := range r.tokens {
		if t.FamilyID == fam && t.RevokedAt == nil {
			n := now
			t.RevokedAt = &n
		}
	}
}

func (r *fakeRefreshRepo) Rotate(_ context.Context, oldHash string, now time.Time, next *domain.RefreshToken) (uuid.UUID, error) {
	r.rotateCalls++
	if r.rotateErr != nil {
		return uuid.Nil, r.rotateErr
	}
	old, ok := r.tokens[oldHash]
	if !ok {
		return uuid.Nil, domain.ErrInvalidRefreshToken
	}
	if old.RevokedAt != nil {
		r.revokeFamily(old.FamilyID, now)
		return uuid.Nil, domain.ErrInvalidRefreshToken
	}
	if !old.ExpiresAt.After(now) {
		return uuid.Nil, domain.ErrInvalidRefreshToken
	}
	n := now
	old.RevokedAt = &n
	next.UserID, next.FamilyID = old.UserID, old.FamilyID
	cp := *next
	r.tokens[next.TokenHash] = &cp
	return old.UserID, nil
}

func (r *fakeRefreshRepo) RevokeFamily(_ context.Context, hash string, now time.Time) error {
	if r.revokeFamilyErr != nil {
		return r.revokeFamilyErr
	}
	if t, ok := r.tokens[hash]; ok {
		r.revokeFamily(t.FamilyID, now)
	}
	return nil
}

func (r *fakeRefreshRepo) RevokeAllForUser(_ context.Context, userID uuid.UUID, now time.Time) error {
	r.revokeAllCalls++
	r.lastRevokeAllUser = userID
	if r.revokeAllErr != nil {
		return r.revokeAllErr
	}
	for _, t := range r.tokens {
		if t.UserID == userID && t.RevokedAt == nil {
			n := now
			t.RevokedAt = &n
		}
	}
	return nil
}

func (r *fakeRefreshRepo) active(userID uuid.UUID) int {
	n := 0
	for _, t := range r.tokens {
		if t.UserID == userID && t.RevokedAt == nil {
			n++
		}
	}
	return n
}

// fakeCodec: token "rt-N", hash "h(rt-N)". Hash yang dapat diprediksi memudahkan pemeriksaan
// bahwa nilai asli tidak disimpan.
type fakeCodec struct {
	n   int
	err error
}

func (c *fakeCodec) New() (string, string, error) {
	if c.err != nil {
		return "", "", c.err
	}
	c.n++
	p := fmt.Sprintf("rt-%d", c.n)
	return p, c.Hash(p), nil
}

func (*fakeCodec) Hash(p string) string { return "h(" + p + ")" }

const testRefreshTTL = 720 * time.Hour

type refreshFixture struct {
	uc    *usecase.AuthUseCase
	users *fakeRepo
	rt    *fakeRefreshRepo
	codec *fakeCodec
	now   time.Time
	user  *domain.User
}

func newRefreshFixture(t *testing.T) *refreshFixture {
	t.Helper()
	f := &refreshFixture{
		users: newFakeRepo(), rt: newFakeRefreshRepo(), codec: &fakeCodec{},
		now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	f.uc = newUC(f.users, &fakeHasher{}, fakeTokens{}).
		WithRefreshTokens(f.rt, f.codec, testRefreshTTL).
		WithClock(func() time.Time { return f.now })
	u, err := f.uc.Register(ctx, "Budi", "budi@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	f.user = u
	return f
}

var ctx = context.Background()

func (f *refreshFixture) login(t *testing.T) *usecase.LoginResult {
	t.Helper()
	res, err := f.uc.Login(ctx, "budi@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// ---------- Login ----------

func TestLogin_IssuesRefreshTokenStoredOnlyAsHash(t *testing.T) {
	f := newRefreshFixture(t)
	res := f.login(t)

	if res.RefreshToken == "" || res.RefreshExpiresIn != testRefreshTTL {
		t.Fatalf("refresh token = %q, ttl = %v", res.RefreshToken, res.RefreshExpiresIn)
	}
	if _, stored := f.rt.tokens[res.RefreshToken]; stored {
		t.Error("nilai asli refresh token tidak boleh menjadi kunci/isi penyimpanan")
	}
	got, ok := f.rt.tokens[f.codec.Hash(res.RefreshToken)]
	if !ok || got.UserID != f.user.ID || !got.ExpiresAt.Equal(f.now.Add(testRefreshTTL)) || got.RevokedAt != nil {
		t.Fatalf("catatan token = %+v", got)
	}
}

func TestLogin_EachLoginStartsNewFamily(t *testing.T) {
	f := newFakeFixtureTwoLogins(t)
	if f[0] == f[1] {
		t.Error("setiap login harus memulai family baru (logout satu perangkat tidak boleh mematikan yang lain)")
	}
}

func newFakeFixtureTwoLogins(t *testing.T) [2]uuid.UUID {
	f := newRefreshFixture(t)
	a, b := f.login(t), f.login(t)
	return [2]uuid.UUID{
		f.rt.tokens[f.codec.Hash(a.RefreshToken)].FamilyID,
		f.rt.tokens[f.codec.Hash(b.RefreshToken)].FamilyID,
	}
}

func TestLogin_WithoutRefreshTokensConfiguredReturnsNone(t *testing.T) {
	uc, _ := registered(t)
	res, err := uc.Login(ctx, "budi@example.com", "password123")
	if err != nil || res.RefreshToken != "" || res.RefreshExpiresIn != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestLogin_RefreshStorageFailureFailsLogin(t *testing.T) {
	f := newRefreshFixture(t)
	boom := errors.New("db mati")
	f.rt.createErr = boom
	if _, err := f.uc.Login(ctx, "budi@example.com", "password123"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	f.rt.createErr = nil
	f.codec.err = boom
	if _, err := f.uc.Login(ctx, "budi@example.com", "password123"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

// ---------- Refresh ----------

func TestRefresh_RotatesAndReturnsNewPair(t *testing.T) {
	f := newRefreshFixture(t)
	login := f.login(t)
	f.now = f.now.Add(10 * time.Minute)

	pair, err := f.uc.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if pair.AccessToken != "token-"+f.user.ID.String() || pair.ExpiresIn != time.Hour {
		t.Errorf("access token = %q (%v)", pair.AccessToken, pair.ExpiresIn)
	}
	if pair.RefreshToken == "" || pair.RefreshToken == login.RefreshToken || pair.RefreshExpiresIn != testRefreshTTL {
		t.Errorf("refresh token baru = %q", pair.RefreshToken)
	}
	if got := f.rt.active(f.user.ID); got != 1 {
		t.Errorf("tepat satu token aktif setelah rotasi, dapat %d", got)
	}
	if !f.rt.tokens[f.codec.Hash(pair.RefreshToken)].ExpiresAt.Equal(f.now.Add(testRefreshTTL)) {
		t.Error("masa berlaku token baru dihitung dari waktu refresh")
	}
}

func TestRefresh_OldTokenCannotBeUsedTwice_AndReuseKillsTheChain(t *testing.T) {
	f := newRefreshFixture(t)
	login := f.login(t)
	pair, err := f.uc.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.uc.Refresh(ctx, login.RefreshToken); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("pemakaian ulang harus ditolak, dapat %v", err)
	}
	if _, err := f.uc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("token terbaru dari rantai yang dicuri harus ikut mati, dapat %v", err)
	}
	if f.rt.active(f.user.ID) != 0 {
		t.Error("tidak boleh ada token aktif tersisa di rantai itu")
	}
}

func TestRefresh_RejectsBadTokensWithoutLeakingWhy(t *testing.T) {
	f := newRefreshFixture(t)
	login := f.login(t)

	for name, tok := range map[string]string{
		"kosong":          "",
		"tak dikenal":     "rt-unknown",
		"terlalu panjang": strings.Repeat("x", 513),
	} {
		_, err := f.uc.Refresh(ctx, tok)
		if !errors.Is(err, domain.ErrInvalidRefreshToken) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if f.rt.rotateCalls != 1 { // hanya "tak dikenal" yang sampai ke repository
		t.Errorf("rotateCalls = %d: input kosong/terlalu panjang harus ditolak sebelum menyentuh database", f.rt.rotateCalls)
	}

	f.now = f.now.Add(testRefreshTTL + time.Second)
	if _, err := f.uc.Refresh(ctx, login.RefreshToken); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("kedaluwarsa: err = %v", err)
	}
}

func TestRefresh_DisabledWhenNotConfigured(t *testing.T) {
	uc, _ := registered(t)
	if _, err := uc.Refresh(ctx, "rt-1"); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefresh_DeletedUserCannotExtendSession(t *testing.T) {
	f := newRefreshFixture(t)
	login := f.login(t)
	second := f.login(t)
	delete(f.users.byEmail, "budi@example.com")

	if _, err := f.uc.Refresh(ctx, login.RefreshToken); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("err = %v", err)
	}
	if f.rt.revokeAllCalls != 1 || f.rt.lastRevokeAllUser != f.user.ID {
		t.Error("semua token user yang sudah dihapus harus dicabut")
	}
	if f.rt.active(f.user.ID) != 0 {
		t.Error("tidak boleh ada token aktif untuk user yang sudah dihapus")
	}
	if _, err := f.uc.Refresh(ctx, second.RefreshToken); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("sesi lain user itu juga harus mati, dapat %v", err)
	}
}

func TestRefresh_InfrastructureErrorsAreNotMaskedAsInvalidToken(t *testing.T) {
	f := newRefreshFixture(t)
	login := f.login(t)
	boom := errors.New("db mati")

	f.rt.rotateErr = boom
	if _, err := f.uc.Refresh(ctx, login.RefreshToken); !errors.Is(err, boom) || errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("rotate: err = %v", err)
	}
	f.rt.rotateErr = nil

	f.codec.err = boom
	if _, err := f.uc.Refresh(ctx, login.RefreshToken); !errors.Is(err, boom) {
		t.Errorf("codec: err = %v", err)
	}
	f.codec.err = nil

	f.users.findErr = boom // FindByEmail saja; FindByID tidak memakainya, jadi cek jalur lain
	f.rt.revokeAllErr = boom
	delete(f.users.byEmail, "budi@example.com")
	if _, err := f.uc.Refresh(ctx, login.RefreshToken); !errors.Is(err, boom) {
		t.Errorf("revoke-all gagal: err = %v", err)
	}
}

func TestRefresh_AccessTokenIssueFailurePropagates(t *testing.T) {
	f := newRefreshFixture(t)
	login := f.login(t)
	boom := errors.New("sign gagal")
	uc := newUC(f.users, &fakeHasher{}, fakeTokens{err: boom}).
		WithRefreshTokens(f.rt, f.codec, testRefreshTTL).
		WithClock(func() time.Time { return f.now })
	if _, err := uc.Refresh(ctx, login.RefreshToken); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

// ---------- Logout ----------

func TestLogout_RevokesTheWholeSessionChain(t *testing.T) {
	f := newRefreshFixture(t)
	login := f.login(t)
	other := f.login(t) // perangkat lain
	pair, err := f.uc.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.uc.Logout(ctx, pair.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := f.uc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("setelah logout token harus mati, dapat %v", err)
	}
	if _, err := f.uc.Refresh(ctx, other.RefreshToken); err != nil {
		t.Errorf("sesi di perangkat lain tidak boleh terpengaruh: %v", err)
	}
}

func TestLogout_IsIdempotentAndValidatesInput(t *testing.T) {
	f := newRefreshFixture(t)
	if err := f.uc.Logout(ctx, "rt-unknown"); err != nil {
		t.Errorf("token tak dikenal harus dianggap berhasil: %v", err)
	}
	login := f.login(t)
	for i := 0; i < 2; i++ {
		if err := f.uc.Logout(ctx, login.RefreshToken); err != nil {
			t.Errorf("logout ke-%d: %v", i+1, err)
		}
	}
	for _, tok := range []string{"", strings.Repeat("x", 513)} {
		if err := f.uc.Logout(ctx, tok); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("token %d karakter: err = %v", len(tok), err)
		}
	}
	boom := errors.New("db mati")
	f.rt.revokeFamilyErr = boom
	if err := f.uc.Logout(ctx, login.RefreshToken); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	uc, _ := registered(t)
	if err := uc.Logout(ctx, "rt-1"); err != nil {
		t.Errorf("tanpa konfigurasi refresh, logout no-op: %v", err)
	}
}

func TestLogoutAll_RevokesEveryDeviceOfThatUserOnly(t *testing.T) {
	f := newRefreshFixture(t)
	a, b := f.login(t), f.login(t)
	if _, err := f.uc.Register(ctx, "Ani", "ani@example.com", "password123"); err != nil {
		t.Fatal(err)
	}
	aniLogin, err := f.uc.Login(ctx, "ani@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}

	if err := f.uc.LogoutAll(ctx, f.user.ID.String()); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{a.RefreshToken, b.RefreshToken} {
		if _, err := f.uc.Refresh(ctx, tok); !errors.Is(err, domain.ErrInvalidRefreshToken) {
			t.Errorf("semua sesi user harus mati, dapat %v", err)
		}
	}
	if _, err := f.uc.Refresh(ctx, aniLogin.RefreshToken); err != nil {
		t.Errorf("sesi user lain tidak boleh terpengaruh: %v", err)
	}
}

func TestLogoutAll_InvalidUserAndErrors(t *testing.T) {
	f := newRefreshFixture(t)
	if err := f.uc.LogoutAll(ctx, "bukan-uuid"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("err = %v", err)
	}
	boom := errors.New("db mati")
	f.rt.revokeAllErr = boom
	if err := f.uc.LogoutAll(ctx, f.user.ID.String()); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	uc, _ := registered(t)
	if err := uc.LogoutAll(ctx, uuid.NewString()); err != nil {
		t.Errorf("tanpa konfigurasi refresh, logout-all no-op: %v", err)
	}
}
