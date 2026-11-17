package repository_test

// Integration test: PostgreSQL asli + seluruh migration (lihat internal/testutil/pgtest).
// Di-skip otomatis jika TEST_DB_HOST tidak diisi.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/modules/workspaces/repository"
	"truss-project/internal/testutil/pgtest"
)

var ctx = context.Background()

// contextCancelled mengembalikan context yang sudah dibatalkan.
func contextCancelled() (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	return c, cancel
}

func setup(t *testing.T) (*repository.WorkspaceGormRepository, *gorm.DB) {
	t.Helper()
	db := pgtest.Setup(t)
	return repository.NewWorkspaceGormRepository(db), db
}

// insertUser menyisipkan user langsung lewat SQL dan mengembalikan ID-nya.
func insertUser(t *testing.T, db *gorm.DB, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Raw(
		"INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?) RETURNING internal_id",
		"Tester", email, "hash",
	).Row().Scan(&id)
	if err != nil || id == uuid.Nil {
		t.Fatalf("gagal membuat user %s: %v", email, err)
	}
	return id
}

func count(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count gagal: %v", err)
	}
	return n
}

func newWS(name string, creator uuid.UUID) *domain.Workspace {
	return &domain.Workspace{Name: name, CreatedBy: creator}
}

// ---------- CreateWithOwner ----------

func TestCreateWithOwner_PersistsWorkspaceAndOwnerMembership(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "owner@example.com")
	desc := "deskripsi"
	w := &domain.Workspace{Name: "Tim Backend", Description: &desc, CreatedBy: uid}

	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	if w.ID == uuid.Nil {
		t.Error("ID harus terisi dari database")
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() || time.Since(w.CreatedAt) > time.Minute {
		t.Errorf("timestamp tidak wajar: %v / %v", w.CreatedAt, w.UpdatedAt)
	}

	var name, d string
	var createdBy uuid.UUID
	err := db.Raw("SELECT name, description, created_by FROM workspaces WHERE internal_id = ?", w.ID).
		Row().Scan(&name, &d, &createdBy)
	if err != nil {
		t.Fatalf("baris workspace tidak ditemukan: %v", err)
	}
	if name != "Tim Backend" || d != "deskripsi" || createdBy != uid {
		t.Errorf("data tersimpan tidak sesuai: %q %q %v", name, d, createdBy)
	}

	var role string
	err = db.Raw("SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?", w.ID, uid).
		Row().Scan(&role)
	if err != nil {
		t.Fatalf("keanggotaan Owner tidak tersimpan: %v", err)
	}
	if role != "owner" {
		t.Errorf("role = %q, harapannya owner", role)
	}
}

func TestCreateWithOwner_NilDescriptionStoredAsNull(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	w := newWS("Tanpa Deskripsi", uid)

	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	if n := count(t, db, "SELECT COUNT(*) FROM workspaces WHERE internal_id = ? AND description IS NULL", w.ID); n != 1 {
		t.Errorf("description harus NULL, ditemukan %d baris", n)
	}
}

func TestCreateWithOwner_UnknownCreatorMapsToDomainErrorAndLeavesNoRows(t *testing.T) {
	repo, db := setup(t)

	err := repo.CreateWithOwner(ctx, newWS("Yatim", uuid.New()))

	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("harapannya ErrUserNotFound, dapat %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM workspaces"); n != 0 {
		t.Errorf("tidak boleh ada workspace tersisa, ditemukan %d", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM workspace_members"); n != 0 {
		t.Errorf("tidak boleh ada keanggotaan tersisa, ditemukan %d", n)
	}
}

func TestCreateWithOwner_IsAtomic_MembershipFailureRollsBackWorkspace(t *testing.T) {
	// Memaksa insert keanggotaan gagal (CHECK role) lewat trigger-free cara:
	// mengganti constraint sementara sehingga 'owner' tidak diizinkan.
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	pgtest.Exec(t, db, "ALTER TABLE workspace_members DROP CONSTRAINT chk_workspace_member_role")
	pgtest.Exec(t, db, "ALTER TABLE workspace_members ADD CONSTRAINT chk_tmp CHECK (role = 'member')")

	err := repo.CreateWithOwner(ctx, newWS("Harus Rollback", uid))

	if err == nil {
		t.Fatal("insert keanggotaan harus gagal")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM workspaces"); n != 0 {
		t.Errorf("workspace harus ikut di-rollback, ditemukan %d baris", n)
	}
}

func TestCreateWithOwner_ValueTooLongIsNotMappedToUserNotFound(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")

	err := repo.CreateWithOwner(ctx, newWS(strings.Repeat("x", 101), uid)) // VARCHAR(100)

	if err == nil {
		t.Fatal("nama 101 karakter harus ditolak database")
	}
	if errors.Is(err, domain.ErrUserNotFound) {
		t.Error("error panjang kolom tidak boleh dipetakan ke ErrUserNotFound")
	}
}

func TestCreateWithOwner_GeneratesDistinctIDs(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	a, b := newWS("A", uid), newWS("B", uid)

	if err := repo.CreateWithOwner(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWithOwner(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Error("ID harus berbeda")
	}
}

// ---------- ListByMember ----------

func TestListByMember_OnlyOwnWorkspacesNewestFirstWithRoles(t *testing.T) {
	repo, db := setup(t)
	alice := insertUser(t, db, "alice@example.com")
	bob := insertUser(t, db, "bob@example.com")

	older, newer, bobs := newWS("Lama", alice), newWS("Baru", alice), newWS("Milik Bob", bob)
	for _, w := range []*domain.Workspace{older, bobs, newer} {
		if err := repo.CreateWithOwner(ctx, w); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond) // pastikan created_at berbeda
	}
	// Alice juga anggota biasa di workspace Bob.
	pgtest.Exec(t, db, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')", bobs.ID, alice)

	got, err := repo.ListByMember(ctx, alice)
	if err != nil {
		t.Fatalf("ListByMember: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("Alice harus melihat 3 workspace, dapat %d", len(got))
	}
	// Terbaru lebih dulu: Milik Bob (dibuat setelah Lama), Baru dibuat terakhir.
	wantOrder := []uuid.UUID{newer.ID, bobs.ID, older.ID}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Errorf("urutan[%d] = %v (%s), harapannya %v", i, got[i].ID, got[i].Name, id)
		}
	}
	roles := map[uuid.UUID]domain.Role{}
	for _, g := range got {
		roles[g.ID] = g.Role
	}
	if roles[older.ID] != domain.RoleOwner || roles[newer.ID] != domain.RoleOwner {
		t.Errorf("Alice harus Owner di workspace miliknya: %v", roles)
	}
	if roles[bobs.ID] != domain.RoleMember {
		t.Errorf("Alice harus member di workspace Bob, dapat %q", roles[bobs.ID])
	}
}

func TestListByMember_DoesNotLeakOtherUsersWorkspaces(t *testing.T) {
	repo, db := setup(t)
	alice := insertUser(t, db, "alice@example.com")
	bob := insertUser(t, db, "bob@example.com")
	if err := repo.CreateWithOwner(ctx, newWS("Rahasia Bob", bob)); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListByMember(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("harapannya slice kosong non-nil, dapat %#v", got)
	}
}

func TestListByMember_MapsAllFields(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	desc := "isi"
	w := &domain.Workspace{Name: "Lengkap", Description: &desc, CreatedBy: uid}
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.ListByMember(ctx, uid)
	if len(got) != 1 {
		t.Fatalf("dapat %d", len(got))
	}
	g := got[0]
	if g.ID != w.ID || g.Name != "Lengkap" || g.CreatedBy != uid || g.Role != domain.RoleOwner {
		t.Errorf("field tidak sesuai: %+v", g)
	}
	if g.Description == nil || *g.Description != "isi" {
		t.Errorf("description = %v", g.Description)
	}
	if g.CreatedAt.IsZero() || g.UpdatedAt.IsZero() {
		t.Error("timestamp harus terisi")
	}
}

// ---------- FindByID ----------

func TestFindByID(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	w := newWS("Cari Aku", uid)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	t.Run("ditemukan", func(t *testing.T) {
		got, err := repo.FindByID(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != w.ID || got.Name != "Cari Aku" || got.CreatedBy != uid {
			t.Errorf("data tidak sesuai: %+v", got)
		}
	})

	t.Run("tidak ada", func(t *testing.T) {
		got, err := repo.FindByID(ctx, uuid.New())
		if !errors.Is(err, domain.ErrWorkspaceNotFound) {
			t.Fatalf("harapannya ErrWorkspaceNotFound, dapat %v", err)
		}
		if got != nil {
			t.Errorf("hasil harus nil, dapat %+v", got)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			t.Error("error GORM tidak boleh bocor keluar repository")
		}
	})
}

// ---------- FindMemberRole ----------

func TestFindMemberRole(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	member := insertUser(t, db, "member@example.com")
	outsider := insertUser(t, db, "outsider@example.com")

	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}
	pgtest.Exec(t, db, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')", w.ID, member)

	t.Run("owner", func(t *testing.T) {
		role, err := repo.FindMemberRole(ctx, w.ID, owner)
		if err != nil || role != domain.RoleOwner {
			t.Fatalf("role = %q, err = %v", role, err)
		}
	})

	t.Run("member", func(t *testing.T) {
		role, err := repo.FindMemberRole(ctx, w.ID, member)
		if err != nil || role != domain.RoleMember {
			t.Fatalf("role = %q, err = %v", role, err)
		}
	})

	t.Run("bukan anggota", func(t *testing.T) {
		role, err := repo.FindMemberRole(ctx, w.ID, outsider)
		if !errors.Is(err, domain.ErrNotMember) {
			t.Fatalf("harapannya ErrNotMember, dapat %v", err)
		}
		if role != "" {
			t.Errorf("role harus kosong, dapat %q", role)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			t.Error("error GORM tidak boleh bocor")
		}
	})

	t.Run("workspace tidak ada", func(t *testing.T) {
		_, err := repo.FindMemberRole(ctx, uuid.New(), owner)
		if !errors.Is(err, domain.ErrNotMember) {
			t.Fatalf("harapannya ErrNotMember, dapat %v", err)
		}
	})
}

// ---------- context ----------

func TestRepository_CancelledContextReturnsRealError(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")

	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := repo.CreateWithOwner(cctx, newWS("X", uid)); err == nil {
		t.Error("CreateWithOwner dengan context dibatalkan harus error")
	}
	if _, err := repo.ListByMember(cctx, uid); err == nil {
		t.Error("ListByMember dengan context dibatalkan harus error")
	}
	_, err := repo.FindMemberRole(cctx, uuid.New(), uid)
	if err == nil || errors.Is(err, domain.ErrNotMember) {
		t.Errorf("context dibatalkan tidak boleh dipetakan menjadi ErrNotMember, dapat %v", err)
	}
	_, err = repo.FindByID(cctx, uuid.New())
	if err == nil || errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("context dibatalkan tidak boleh dipetakan menjadi ErrWorkspaceNotFound, dapat %v", err)
	}
}
