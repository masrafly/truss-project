package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/usecase"
)

// ---------- fakes ----------

type fakeRepo struct {
	byEmail   map[string]*domain.User
	createErr error
	findErr   error // error tak terduga saat FindByEmail
	updateErr error // error saat UpdateName/UpdatePasswordHash
	created   int
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byEmail: map[string]*domain.User{}} }

func (r *fakeRepo) Create(_ context.Context, u *domain.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	if _, ok := r.byEmail[u.Email]; ok {
		return domain.ErrEmailAlreadyUsed
	}
	u.ID = uuid.New()
	cp := *u
	r.byEmail[u.Email] = &cp
	r.created++
	return nil
}

func (r *fakeRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	u, ok := r.byEmail[email]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	for _, u := range r.byEmail {
		if u.ID == id {
			cp := *u
			return &cp, nil
		}
	}
	return nil, domain.ErrUserNotFound
}

func (r *fakeRepo) byID(id uuid.UUID) *domain.User {
	for _, u := range r.byEmail {
		if u.ID == id {
			return u
		}
	}
	return nil
}

func (r *fakeRepo) UpdateName(_ context.Context, id uuid.UUID, name string) (*domain.User, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	u := r.byID(id)
	if u == nil {
		return nil, domain.ErrUserNotFound
	}
	u.Name = name
	cp := *u
	return &cp, nil
}

func (r *fakeRepo) UpdatePasswordHash(_ context.Context, id uuid.UUID, hash string) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	u := r.byID(id)
	if u == nil {
		return domain.ErrUserNotFound
	}
	u.PasswordHash = hash
	return nil
}

// fakeHasher: hash = "hashed:" + plain. Menghitung jumlah Compare.
type fakeHasher struct {
	hashErr      error
	compareCalls int
}

func (h *fakeHasher) Hash(plain string) (string, error) {
	if h.hashErr != nil {
		return "", h.hashErr
	}
	return "hashed:" + plain, nil
}

func (h *fakeHasher) Compare(hash, plain string) error {
	h.compareCalls++
	if hash == "hashed:"+plain {
		return nil
	}
	return errors.New("mismatch")
}

type fakeTokens struct{ err error }

func (t fakeTokens) Issue(userID string) (string, time.Duration, error) {
	if t.err != nil {
		return "", 0, t.err
	}
	return "token-" + userID, time.Hour, nil
}

func newUC(repo *fakeRepo, h *fakeHasher, t fakeTokens) *usecase.AuthUseCase {
	return usecase.NewAuthUseCase(repo, h, t, nil) // logger nil -> dibuang
}

// ---------- Register ----------

func TestRegister_Success(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo, &fakeHasher{}, fakeTokens{})

	u, err := uc.Register(context.Background(), "  Budi ", "  BUDI@Example.COM ", "password123")
	if err != nil {
		t.Fatalf("tidak diharapkan error: %v", err)
	}
	if u.Name != "Budi" {
		t.Errorf("nama harus di-trim, dapat %q", u.Name)
	}
	if u.Email != "budi@example.com" {
		t.Errorf("email harus lowercase dan di-trim, dapat %q", u.Email)
	}
	if u.PasswordHash != "" {
		t.Error("hash password tidak boleh dikembalikan")
	}
	if got := repo.byEmail["budi@example.com"].PasswordHash; got != "hashed:password123" {
		t.Errorf("yang disimpan harus hash, dapat %q", got)
	}
}

func TestRegister_InvalidInput(t *testing.T) {
	tests := []struct {
		name, uname, email, password string
	}{
		{"nama kosong", "", "a@b.com", "password123"},
		{"nama spasi saja", "   ", "a@b.com", "password123"},
		{"email kosong", "Budi", "", "password123"},
		{"password kosong", "Budi", "a@b.com", ""},
		{"password 7 karakter", "Budi", "a@b.com", "1234567"},
		{"password lebih dari 72 byte", "Budi", "a@b.com", strings.Repeat("a", 73)},
		{"nama 101 karakter", strings.Repeat("n", 101), "a@b.com", "password123"},
		{"email 256 karakter", "Budi", strings.Repeat("e", 250) + "@b.com", "password123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			uc := newUC(repo, &fakeHasher{}, fakeTokens{})

			_, err := uc.Register(context.Background(), tc.uname, tc.email, tc.password)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("harapannya ErrInvalidInput, dapat %v", err)
			}
			if repo.created != 0 {
				t.Error("user tidak boleh tersimpan")
			}
		})
	}
}

func TestRegister_PasswordBoundaries(t *testing.T) {
	uc := newUC(newFakeRepo(), &fakeHasher{}, fakeTokens{})

	// tepat 8 karakter dan tepat 72 byte harus diterima
	if _, err := uc.Register(context.Background(), "A", "a@b.com", "12345678"); err != nil {
		t.Errorf("8 karakter harus valid: %v", err)
	}
	if _, err := uc.Register(context.Background(), "B", "b@b.com", strings.Repeat("a", 72)); err != nil {
		t.Errorf("72 byte harus valid: %v", err)
	}
	// 8 karakter multibyte (lebih dari 8 byte) tetap valid selama <= 72 byte
	if _, err := uc.Register(context.Background(), "C", "c@b.com", "пароль12"); err != nil {
		t.Errorf("8 karakter multibyte harus valid: %v", err)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo, &fakeHasher{}, fakeTokens{})

	if _, err := uc.Register(context.Background(), "Budi", "budi@example.com", "password123"); err != nil {
		t.Fatal(err)
	}
	_, err := uc.Register(context.Background(), "Lain", "BUDI@example.com", "password123")
	if !errors.Is(err, domain.ErrEmailAlreadyUsed) {
		t.Fatalf("harapannya ErrEmailAlreadyUsed, dapat %v", err)
	}
}

func TestRegister_RaceDuplicateFromRepository(t *testing.T) {
	// Cek awal lolos, tetapi INSERT gagal karena unique constraint (race).
	repo := newFakeRepo()
	repo.createErr = domain.ErrEmailAlreadyUsed
	uc := newUC(repo, &fakeHasher{}, fakeTokens{})

	_, err := uc.Register(context.Background(), "Budi", "budi@example.com", "password123")
	if !errors.Is(err, domain.ErrEmailAlreadyUsed) {
		t.Fatalf("harapannya ErrEmailAlreadyUsed, dapat %v", err)
	}
}

func TestRegister_UnexpectedErrors(t *testing.T) {
	boom := errors.New("db mati")

	t.Run("FindByEmail error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.findErr = boom
		uc := newUC(repo, &fakeHasher{}, fakeTokens{})
		_, err := uc.Register(context.Background(), "Budi", "a@b.com", "password123")
		if !errors.Is(err, boom) {
			t.Fatalf("error harus diteruskan, dapat %v", err)
		}
	})

	t.Run("hash error", func(t *testing.T) {
		// Hasher yang rusak: Hash dipanggil di konstruktor (dummy) dan saat register.
		uc := newUC(newFakeRepo(), &fakeHasher{hashErr: boom}, fakeTokens{})
		_, err := uc.Register(context.Background(), "Budi", "a@b.com", "password123")
		if !errors.Is(err, boom) {
			t.Fatalf("error harus diteruskan, dapat %v", err)
		}
	})

	t.Run("Create error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.createErr = boom
		uc := newUC(repo, &fakeHasher{}, fakeTokens{})
		_, err := uc.Register(context.Background(), "Budi", "a@b.com", "password123")
		if !errors.Is(err, boom) {
			t.Fatalf("error harus diteruskan, dapat %v", err)
		}
	})
}

// ---------- Login ----------

func registered(t *testing.T) (*usecase.AuthUseCase, *fakeHasher) {
	t.Helper()
	h := &fakeHasher{}
	uc := newUC(newFakeRepo(), h, fakeTokens{})
	if _, err := uc.Register(context.Background(), "Budi", "budi@example.com", "password123"); err != nil {
		t.Fatal(err)
	}
	return uc, h
}

func TestLogin_Success(t *testing.T) {
	uc, _ := registered(t)

	res, err := uc.Login(context.Background(), "  BUDI@example.com ", "password123")
	if err != nil {
		t.Fatalf("tidak diharapkan error: %v", err)
	}
	if res.AccessToken != "token-"+res.User.ID.String() {
		t.Errorf("token tidak sesuai: %q", res.AccessToken)
	}
	if res.ExpiresIn != time.Hour {
		t.Errorf("ExpiresIn salah: %v", res.ExpiresIn)
	}
	if res.User.PasswordHash != "" {
		t.Error("hash password tidak boleh dikembalikan")
	}
}

func TestLogin_WrongPasswordAndUnknownEmailAreIndistinguishable(t *testing.T) {
	uc, _ := registered(t)

	_, errWrong := uc.Login(context.Background(), "budi@example.com", "salahsalah")
	_, errUnknown := uc.Login(context.Background(), "tidakada@example.com", "password123")

	if !errors.Is(errWrong, domain.ErrInvalidCredentials) {
		t.Errorf("password salah harus ErrInvalidCredentials, dapat %v", errWrong)
	}
	if !errors.Is(errUnknown, domain.ErrInvalidCredentials) {
		t.Errorf("email tidak ada harus ErrInvalidCredentials, dapat %v", errUnknown)
	}
	if errWrong.Error() != errUnknown.Error() {
		t.Error("pesan error harus sama agar email tidak bisa ditebak")
	}
}

func TestLogin_UnknownEmailStillRunsPasswordComparison(t *testing.T) {
	// Proteksi timing: email tidak terdaftar tetap memanggil Compare (bcrypt).
	uc, h := registered(t)

	before := h.compareCalls
	_, _ = uc.Login(context.Background(), "tidakada@example.com", "password123")
	if h.compareCalls != before+1 {
		t.Errorf("Compare harus dipanggil tepat sekali untuk email tak terdaftar, dipanggil %d kali",
			h.compareCalls-before)
	}

	before = h.compareCalls
	_, _ = uc.Login(context.Background(), "budi@example.com", "salahsalah")
	if h.compareCalls != before+1 {
		t.Errorf("Compare harus dipanggil tepat sekali untuk password salah, dipanggil %d kali",
			h.compareCalls-before)
	}
}

func TestLogin_DummyHashNeverMatchesRealPassword(t *testing.T) {
	// Pastikan perbandingan palsu tidak bisa membuat login lolos.
	uc := newUC(newFakeRepo(), &fakeHasher{}, fakeTokens{})
	_, err := uc.Login(context.Background(), "tidakada@example.com", "truss-dummy-password")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("harus ditolak, dapat %v", err)
	}
}

func TestLogin_InvalidInput(t *testing.T) {
	uc, h := registered(t)

	for name, c := range map[string][2]string{
		"email kosong":    {"", "password123"},
		"password kosong": {"budi@example.com", ""},
		"password >72":    {"budi@example.com", strings.Repeat("a", 73)},
	} {
		t.Run(name, func(t *testing.T) {
			before := h.compareCalls
			_, err := uc.Login(context.Background(), c[0], c[1])
			if !errors.Is(err, domain.ErrInvalidCredentials) {
				t.Fatalf("harapannya ErrInvalidCredentials, dapat %v", err)
			}
			if h.compareCalls != before {
				t.Error("input tidak valid tidak boleh memicu bcrypt")
			}
		})
	}
}

func TestLogin_ShortPasswordIsNotRejectedAsInvalid(t *testing.T) {
	// Aturan minimal 8 karakter hanya untuk register; akun lama tetap bisa login
	// dan kegagalannya muncul sebagai "kredensial salah", bukan input tidak valid.
	uc, _ := registered(t)
	_, err := uc.Login(context.Background(), "budi@example.com", "pendek")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("harapannya ErrInvalidCredentials, dapat %v", err)
	}
}

func TestLogin_UnexpectedErrors(t *testing.T) {
	boom := errors.New("db mati")

	t.Run("FindByEmail error", func(t *testing.T) {
		repo := newFakeRepo()
		repo.findErr = boom
		uc := newUC(repo, &fakeHasher{}, fakeTokens{})
		_, err := uc.Login(context.Background(), "a@b.com", "password123")
		if !errors.Is(err, boom) || errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("error infrastruktur harus diteruskan apa adanya, dapat %v", err)
		}
	})

	t.Run("token error", func(t *testing.T) {
		repo := newFakeRepo()
		h := &fakeHasher{}
		uc := newUC(repo, h, fakeTokens{err: boom})
		if _, err := uc.Register(context.Background(), "Budi", "budi@example.com", "password123"); err != nil {
			t.Fatal(err)
		}
		_, err := uc.Login(context.Background(), "budi@example.com", "password123")
		if !errors.Is(err, boom) {
			t.Fatalf("error token harus diteruskan, dapat %v", err)
		}
	})
}

func TestLogin_WorksWhenDummyHashCreationFails(t *testing.T) {
	// Jika dummy hash gagal dibuat, proteksi timing hilang tetapi login tetap benar.
	repo := newFakeRepo()
	repo.byEmail["budi@example.com"] = &domain.User{
		ID: uuid.New(), Email: "budi@example.com", PasswordHash: "hashed:password123",
	}
	uc := newUC(repo, &fakeHasher{hashErr: errors.New("gagal")}, fakeTokens{})

	if _, err := uc.Login(context.Background(), "budi@example.com", "password123"); err != nil {
		t.Fatalf("login harus tetap berhasil: %v", err)
	}
	if _, err := uc.Login(context.Background(), "tidakada@example.com", "password123"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("email tak terdaftar harus ErrInvalidCredentials, dapat %v", err)
	}
}

// ---------- GetProfile ----------

func TestGetProfile(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo, &fakeHasher{}, fakeTokens{})
	u, err := uc.Register(context.Background(), "Budi", "budi@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ditemukan", func(t *testing.T) {
		got, err := uc.GetProfile(context.Background(), u.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		if got.Email != "budi@example.com" || got.PasswordHash != "" {
			t.Errorf("hasil tidak sesuai: %+v", got)
		}
	})

	t.Run("ID bukan UUID", func(t *testing.T) {
		_, err := uc.GetProfile(context.Background(), "bukan-uuid")
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("harapannya ErrUserNotFound, dapat %v", err)
		}
	})

	t.Run("user tidak ada", func(t *testing.T) {
		_, err := uc.GetProfile(context.Background(), uuid.NewString())
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("harapannya ErrUserNotFound, dapat %v", err)
		}
	})
}

func TestRegister_LengthBoundariesAccepted(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo, &fakeHasher{}, fakeTokens{})
	name := strings.Repeat("é", 100)             // 100 karakter multibyte
	email := strings.Repeat("e", 249) + "@b.com" // tepat 255 karakter
	if _, err := uc.Register(context.Background(), name, email, "password123"); err != nil {
		t.Fatalf("batas atas harus diterima: %v", err)
	}
}
