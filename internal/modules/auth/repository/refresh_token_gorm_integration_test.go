package repository_test

// Integration test refresh token: PostgreSQL asli + seluruh migration (internal/testutil/pgtest).
// Di-skip tanpa TEST_DB_HOST (gagal bila TEST_DB_REQUIRED=1).

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/repository"
	"truss-project/internal/testutil/pgtest"
)

var rtCtx = context.Background()

func rtSetup(t *testing.T) (*repository.RefreshTokenGormRepository, *gorm.DB) {
	t.Helper()
	db := pgtest.Setup(t)
	return repository.NewRefreshTokenGormRepository(db), db
}

func rtUser(t *testing.T, db *gorm.DB, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Raw(
		"INSERT INTO users (name, email, password_hash) VALUES ('T', ?, 'h') RETURNING internal_id", email,
	).Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func rtNew(user, family uuid.UUID, hash string, ttl time.Duration) *domain.RefreshToken {
	return &domain.RefreshToken{UserID: user, FamilyID: family, TokenHash: hash, ExpiresAt: time.Now().Add(ttl)}
}

func rtCount(t *testing.T, db *gorm.DB, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Raw("SELECT COUNT(*) FROM refresh_tokens WHERE "+where, args...).Row().Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// hash64 membuat string 64 karakter (sesuai CHAR(64)) yang unik per label.
func hash64(s string) string { return fmt.Sprintf("%064s", s) }

func TestRefreshToken_CreateThenRotate(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")
	fam := uuid.New()
	first := rtNew(u, fam, hash64("a"), time.Hour)
	if err := repo.Create(rtCtx, first); err != nil || first.ID == uuid.Nil || first.CreatedAt.IsZero() {
		t.Fatalf("Create: %v %+v", err, first)
	}

	next := &domain.RefreshToken{TokenHash: hash64("b"), ExpiresAt: time.Now().Add(time.Hour)}
	got, err := repo.Rotate(rtCtx, hash64("a"), time.Now(), next)
	if err != nil || got != u {
		t.Fatalf("Rotate: user=%v err=%v", got, err)
	}
	if next.UserID != u || next.FamilyID != fam || next.ID == uuid.Nil {
		t.Errorf("token baru harus mewarisi user dan family: %+v", next)
	}
	if rtCount(t, db, "token_hash = ? AND revoked_at IS NOT NULL", hash64("a")) != 1 ||
		rtCount(t, db, "token_hash = ? AND revoked_at IS NULL", hash64("b")) != 1 {
		t.Error("token lama harus dicabut dan token baru aktif")
	}
}

func TestRefreshToken_RotateRejectsUnknownAndExpired(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")

	if _, err := repo.Rotate(rtCtx, hash64("nope"), time.Now(), rtNew(u, uuid.Nil, hash64("x"), time.Hour)); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("token tak dikenal: %v", err)
	}

	if err := repo.Create(rtCtx, rtNew(u, uuid.New(), hash64("old"), time.Hour)); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Hour)
	if _, err := repo.Rotate(rtCtx, hash64("old"), future, rtNew(u, uuid.Nil, hash64("y"), time.Hour)); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("token kedaluwarsa: %v", err)
	}
	if rtCount(t, db, "token_hash = ?", hash64("y")) != 0 {
		t.Error("token kedaluwarsa tidak boleh menghasilkan token baru")
	}
	if rtCount(t, db, "token_hash = ? AND revoked_at IS NULL", hash64("old")) != 1 {
		t.Error("token kedaluwarsa dibiarkan apa adanya")
	}
}

func TestRefreshToken_ReuseRevokesWholeFamilyButNotOthers(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")
	famA, famB := uuid.New(), uuid.New()
	if err := repo.Create(rtCtx, rtNew(u, famA, hash64("a1"), time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(rtCtx, rtNew(u, famB, hash64("b1"), time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Rotate(rtCtx, hash64("a1"), time.Now(), rtNew(u, uuid.Nil, hash64("a2"), time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Penyerang/klien memakai ulang a1 yang sudah dirotasi.
	if _, err := repo.Rotate(rtCtx, hash64("a1"), time.Now(), rtNew(u, uuid.Nil, hash64("a3"), time.Hour)); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("pemakaian ulang harus ditolak, dapat %v", err)
	}
	if n := rtCount(t, db, "family_id = ? AND revoked_at IS NULL", famA); n != 0 {
		t.Errorf("seluruh family A harus dicabut, %d token masih aktif", n)
	}
	if rtCount(t, db, "token_hash = ?", hash64("a3")) != 0 {
		t.Error("pemakaian ulang tidak boleh menerbitkan token baru")
	}
	if _, err := repo.Rotate(rtCtx, hash64("a2"), time.Now(), rtNew(u, uuid.Nil, hash64("a4"), time.Hour)); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("token terbaru family A harus mati: %v", err)
	}
	if n := rtCount(t, db, "family_id = ? AND revoked_at IS NULL", famB); n != 1 {
		t.Errorf("family B tidak boleh terpengaruh, aktif = %d", n)
	}
}

func TestRefreshToken_ConcurrentRotateOfSameTokenAllowsExactlyOne(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")
	if err := repo.Create(rtCtx, rtNew(u, uuid.New(), hash64("start"), time.Hour)); err != nil {
		t.Fatal(err)
	}

	const workers = 6
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.Rotate(rtCtx, hash64("start"), time.Now(),
				rtNew(u, uuid.Nil, hash64(fmt.Sprintf("n%d", i)), time.Hour))
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, domain.ErrInvalidRefreshToken):
			t.Fatalf("error tak terduga: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("tepat satu rotasi boleh berhasil, berhasil = %d", ok)
	}
	if n := rtCount(t, db, "token_hash <> ?", hash64("start")); n != 1 {
		t.Errorf("hanya satu token turunan yang boleh tercipta, dapat %d", n)
	}
}

func TestRefreshToken_RevokeFamilyIsIdempotentAndScoped(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")
	famA, famB := uuid.New(), uuid.New()
	_ = repo.Create(rtCtx, rtNew(u, famA, hash64("a"), time.Hour))
	_ = repo.Create(rtCtx, rtNew(u, famB, hash64("b"), time.Hour))

	if err := repo.RevokeFamily(rtCtx, hash64("unknown"), time.Now()); err != nil {
		t.Fatalf("token tak dikenal bukan error: %v", err)
	}
	if rtCount(t, db, "revoked_at IS NOT NULL") != 0 {
		t.Fatal("token tak dikenal tidak boleh mencabut apa pun")
	}
	for i := 0; i < 2; i++ { // idempoten
		if err := repo.RevokeFamily(rtCtx, hash64("a"), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if rtCount(t, db, "family_id = ? AND revoked_at IS NOT NULL", famA) != 1 ||
		rtCount(t, db, "family_id = ? AND revoked_at IS NULL", famB) != 1 {
		t.Error("hanya family A yang boleh dicabut")
	}
}

func TestRefreshToken_RevokeAllForUserOnlyThatUser(t *testing.T) {
	repo, db := rtSetup(t)
	u1, u2 := rtUser(t, db, "a@example.com"), rtUser(t, db, "b@example.com")
	_ = repo.Create(rtCtx, rtNew(u1, uuid.New(), hash64("a1"), time.Hour))
	_ = repo.Create(rtCtx, rtNew(u1, uuid.New(), hash64("a2"), time.Hour))
	_ = repo.Create(rtCtx, rtNew(u2, uuid.New(), hash64("b1"), time.Hour))

	if err := repo.RevokeAllForUser(rtCtx, u1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if rtCount(t, db, "user_id = ? AND revoked_at IS NULL", u1) != 0 ||
		rtCount(t, db, "user_id = ? AND revoked_at IS NULL", u2) != 1 {
		t.Error("hanya token user 1 yang boleh dicabut")
	}
}

func TestRefreshToken_CreatePurgesOnlyOwnExpiredTokens(t *testing.T) {
	repo, db := rtSetup(t)
	u1, u2 := rtUser(t, db, "a@example.com"), rtUser(t, db, "b@example.com")
	_ = repo.Create(rtCtx, rtNew(u1, uuid.New(), hash64("expired"), time.Hour))
	_ = repo.Create(rtCtx, rtNew(u1, uuid.New(), hash64("revoked-live"), time.Hour))
	_ = repo.Create(rtCtx, rtNew(u2, uuid.New(), hash64("other-expired"), time.Hour))
	pgtest.Exec(t, db, "UPDATE refresh_tokens SET expires_at = NOW() - interval '1 hour' WHERE token_hash IN (?, ?)",
		hash64("expired"), hash64("other-expired"))
	pgtest.Exec(t, db, "UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_hash = ?", hash64("revoked-live"))

	if err := repo.Create(rtCtx, rtNew(u1, uuid.New(), hash64("fresh"), time.Hour)); err != nil {
		t.Fatal(err)
	}
	if rtCount(t, db, "token_hash = ?", hash64("expired")) != 0 {
		t.Error("token kedaluwarsa milik user ini harus dibuang")
	}
	if rtCount(t, db, "token_hash = ?", hash64("revoked-live")) != 1 {
		t.Error("token dicabut tetapi belum kedaluwarsa harus dipertahankan (deteksi pemakaian ulang)")
	}
	if rtCount(t, db, "token_hash = ?", hash64("other-expired")) != 1 {
		t.Error("token user lain tidak boleh disentuh")
	}
}

func TestRefreshToken_DeletedUserCascades(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")
	_ = repo.Create(rtCtx, rtNew(u, uuid.New(), hash64("a"), time.Hour))
	pgtest.Exec(t, db, "DELETE FROM users WHERE internal_id = ?", u)
	if rtCount(t, db, "user_id = ?", u) != 0 {
		t.Error("refresh token harus ikut terhapus bersama user")
	}
}

func TestRefreshToken_DuplicateHashRejected(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")
	if err := repo.Create(rtCtx, rtNew(u, uuid.New(), hash64("dup"), time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(rtCtx, rtNew(u, uuid.New(), hash64("dup"), time.Hour)); err == nil {
		t.Error("hash duplikat harus ditolak")
	}
}

func TestRefreshToken_CanceledContextReturnsErrors(t *testing.T) {
	repo, db := rtSetup(t)
	u := rtUser(t, db, "a@example.com")
	cctx, cancel := context.WithCancel(rtCtx)
	cancel()
	if err := repo.Create(cctx, rtNew(u, uuid.New(), hash64("c"), time.Hour)); err == nil {
		t.Error("Create harus error")
	}
	if _, err := repo.Rotate(cctx, hash64("c"), time.Now(), rtNew(u, uuid.Nil, hash64("d"), time.Hour)); err == nil || errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("Rotate harus mengembalikan error infrastruktur, dapat %v", err)
	}
	if err := repo.RevokeFamily(cctx, hash64("c"), time.Now()); err == nil {
		t.Error("RevokeFamily harus error")
	}
	if err := repo.RevokeAllForUser(cctx, u, time.Now()); err == nil {
		t.Error("RevokeAllForUser harus error")
	}
}
