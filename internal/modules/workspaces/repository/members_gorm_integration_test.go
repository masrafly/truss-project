package repository_test

// Integration test anggota workspace + direktori user.
// Memakai helper setup/insertUser/count dari workspace_gorm_integration_test.go.
// Di-skip otomatis jika TEST_DB_HOST tidak diisi.

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/modules/workspaces/repository"
	"truss-project/internal/testutil/pgtest"
)

// ---------- AddMember ----------

func TestAddMember_PersistsRoleAndReturnsJoinedAt(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	newbie := insertUser(t, db, "newbie@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	joinedAt, err := repo.AddMember(ctx, w.ID, newbie, domain.RoleAdmin)
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if joinedAt.IsZero() || time.Since(joinedAt) > time.Minute {
		t.Errorf("joinedAt tidak wajar: %v", joinedAt)
	}
	role, err := repo.FindMemberRole(ctx, w.ID, newbie)
	if err != nil || role != domain.RoleAdmin {
		t.Fatalf("role tersimpan = %q, err = %v", role, err)
	}
}

func TestAddMember_DuplicateMapsToDomainError(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	newbie := insertUser(t, db, "newbie@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, w.ID, newbie, domain.RoleMember); err != nil {
		t.Fatal(err)
	}

	_, err := repo.AddMember(ctx, w.ID, newbie, domain.RoleAdmin)

	if !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("harapannya ErrAlreadyMember, dapat %v", err)
	}
	// Peran lama tidak boleh tertimpa oleh percobaan duplikat.
	if role, _ := repo.FindMemberRole(ctx, w.ID, newbie); role != domain.RoleMember {
		t.Errorf("role harus tetap member, dapat %q", role)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ? AND user_id = ?", w.ID, newbie); n != 1 {
		t.Errorf("harus tetap 1 baris, dapat %d", n)
	}
}

func TestAddMember_OwnerCannotBeAddedAgainAsMember(t *testing.T) {
	// Pembuat sudah Owner (PK gabungan): menambahkannya lagi harus 409, bukan menurunkan perannya.
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	_, err := repo.AddMember(ctx, w.ID, owner, domain.RoleMember)

	if !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("harapannya ErrAlreadyMember, dapat %v", err)
	}
	if role, _ := repo.FindMemberRole(ctx, w.ID, owner); role != domain.RoleOwner {
		t.Errorf("Owner harus tetap Owner, dapat %q", role)
	}
}

func TestAddMember_SameUserCanJoinDifferentWorkspaces(t *testing.T) {
	repo, db := setup(t)
	o1 := insertUser(t, db, "o1@example.com")
	o2 := insertUser(t, db, "o2@example.com")
	shared := insertUser(t, db, "shared@example.com")
	w1, w2 := newWS("W1", o1), newWS("W2", o2)
	for _, w := range []*domain.Workspace{w1, w2} {
		if err := repo.CreateWithOwner(ctx, w); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := repo.AddMember(ctx, w1.ID, shared, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, w2.ID, shared, domain.RoleAdmin); err != nil {
		t.Fatalf("user yang sama harus bisa masuk workspace lain: %v", err)
	}
}

func TestAddMember_DatabaseErrorsAreNotMappedToDuplicate(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	t.Run("user tidak ada (FK)", func(t *testing.T) {
		_, err := repo.AddMember(ctx, w.ID, uuid.New(), domain.RoleMember)
		if err == nil || errors.Is(err, domain.ErrAlreadyMember) {
			t.Fatalf("harus error non-duplikat, dapat %v", err)
		}
	})
	t.Run("workspace tidak ada (FK)", func(t *testing.T) {
		u := insertUser(t, db, "x@example.com")
		_, err := repo.AddMember(ctx, uuid.New(), u, domain.RoleMember)
		if err == nil || errors.Is(err, domain.ErrAlreadyMember) {
			t.Fatalf("harus error non-duplikat, dapat %v", err)
		}
	})
	t.Run("role melanggar CHECK", func(t *testing.T) {
		u := insertUser(t, db, "y@example.com")
		_, err := repo.AddMember(ctx, w.ID, u, domain.Role("hacker"))
		if err == nil || errors.Is(err, domain.ErrAlreadyMember) {
			t.Fatalf("harus error non-duplikat, dapat %v", err)
		}
	})
}

// ---------- ListMembers ----------

func TestListMembers_OrderedByRoleThenJoinTimeWithProfile(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	adminU := insertUser(t, db, "admin@example.com")
	m1 := insertUser(t, db, "m1@example.com")
	m2 := insertUser(t, db, "m2@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	// Urutan masuk sengaja diacak: member dulu, baru admin, lalu member lain.
	for _, add := range []struct {
		uid  uuid.UUID
		role domain.Role
	}{{m1, domain.RoleMember}, {adminU, domain.RoleAdmin}, {m2, domain.RoleMember}} {
		if _, err := repo.AddMember(ctx, w.ID, add.uid, add.role); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond)
	}

	got, err := repo.ListMembers(ctx, w.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}

	want := []struct {
		uid  uuid.UUID
		role domain.Role
	}{{owner, domain.RoleOwner}, {adminU, domain.RoleAdmin}, {m1, domain.RoleMember}, {m2, domain.RoleMember}}
	if len(got) != len(want) {
		t.Fatalf("harapannya %d anggota, dapat %d", len(want), len(got))
	}
	for i, wnt := range want {
		if got[i].UserID != wnt.uid || got[i].Role != wnt.role {
			t.Errorf("urutan[%d] = (%v, %q), harapannya (%v, %q)", i, got[i].UserID, got[i].Role, wnt.uid, wnt.role)
		}
	}

	if got[0].Email != "owner@example.com" || got[0].Name != "Tester" {
		t.Errorf("profil tidak ikut terbaca: %+v", got[0])
	}
	if got[0].JoinedAt.IsZero() {
		t.Error("JoinedAt harus terisi")
	}
}

func TestListMembers_OnlyThisWorkspace(t *testing.T) {
	repo, db := setup(t)
	o1 := insertUser(t, db, "o1@example.com")
	o2 := insertUser(t, db, "o2@example.com")
	w1, w2 := newWS("W1", o1), newWS("W2", o2)
	for _, w := range []*domain.Workspace{w1, w2} {
		if err := repo.CreateWithOwner(ctx, w); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.ListMembers(ctx, w1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UserID != o1 {
		t.Fatalf("hanya Owner W1 yang boleh muncul, dapat %+v", got)
	}
}

func TestListMembers_HidesSoftDeletedUsers(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	gone := insertUser(t, db, "gone@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, w.ID, gone, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	pgtest.Exec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", gone)

	got, err := repo.ListMembers(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UserID != owner {
		t.Fatalf("user yang di-soft-delete tidak boleh tampil, dapat %+v", got)
	}
}

func TestListMembers_UnknownWorkspaceReturnsEmptyNonNil(t *testing.T) {
	repo, _ := setup(t)

	got, err := repo.ListMembers(ctx, uuid.New())

	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("harapannya slice kosong non-nil, dapat %#v", got)
	}
}

// ---------- RemoveMember ----------

func TestRemoveMember_DeletesOnlyThatMembership(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	member := insertUser(t, db, "member@example.com")
	w1, w2 := newWS("W1", owner), newWS("W2", owner)
	for _, w := range []*domain.Workspace{w1, w2} {
		if err := repo.CreateWithOwner(ctx, w); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.AddMember(ctx, w.ID, member, domain.RoleMember); err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.RemoveMember(ctx, w1.ID, member, member); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	if _, err := repo.FindMemberRole(ctx, w1.ID, member); !errors.Is(err, domain.ErrNotMember) {
		t.Errorf("keanggotaan di W1 harus hilang, dapat %v", err)
	}
	if _, err := repo.FindMemberRole(ctx, w2.ID, member); err != nil {
		t.Errorf("keanggotaan di W2 harus tetap ada: %v", err)
	}
	if _, err := repo.FindMemberRole(ctx, w1.ID, owner); err != nil {
		t.Errorf("Owner W1 harus tetap ada: %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM users WHERE internal_id = ?", member); n != 1 {
		t.Error("akun user tidak boleh ikut terhapus")
	}
}

func TestRemoveMember_NotAMemberMapsToDomainError(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	stranger := insertUser(t, db, "stranger@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	err := repo.RemoveMember(ctx, w.ID, stranger, stranger)

	if !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("harapannya ErrMemberNotFound, dapat %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ?", w.ID); n != 1 {
		t.Errorf("anggota lain tidak boleh terpengaruh, tersisa %d", n)
	}
}

func TestRemoveMember_UnknownWorkspaceIsMemberNotFound(t *testing.T) {
	repo, db := setup(t)
	u := insertUser(t, db, "a@example.com")

	if err := repo.RemoveMember(ctx, uuid.New(), u, u); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("harapannya ErrMemberNotFound, dapat %v", err)
	}
}

func TestRemoveMember_CancelledContextReturnsRealError(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}

	cctx, cancel := contextCancelled()
	defer cancel()

	err := repo.RemoveMember(cctx, w.ID, owner, owner)
	if err == nil || errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("context dibatalkan harus error asli, dapat %v", err)
	}
	if _, err := repo.AddMember(cctx, w.ID, uuid.New(), domain.RoleMember); err == nil {
		t.Error("AddMember dengan context dibatalkan harus error")
	}
	if _, err := repo.ListMembers(cctx, w.ID); err == nil {
		t.Error("ListMembers dengan context dibatalkan harus error")
	}
}

// ---------- integritas skema ----------

func TestSchema_HardDeletingAMemberUserCascadesTheirMembership(t *testing.T) {
	// ON DELETE CASCADE pada workspace_members.user_id: keanggotaan ikut hilang,
	// sementara workspace dan Owner tidak terpengaruh.
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	member := insertUser(t, db, "member@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, w.ID, member, domain.RoleMember); err != nil {
		t.Fatal(err)
	}

	pgtest.Exec(t, db, "DELETE FROM users WHERE internal_id = ?", member)

	if _, err := repo.FindMemberRole(ctx, w.ID, member); !errors.Is(err, domain.ErrNotMember) {
		t.Errorf("keanggotaan harus ikut terhapus, dapat %v", err)
	}
	if _, err := repo.FindMemberRole(ctx, w.ID, owner); err != nil {
		t.Errorf("Owner harus tetap ada: %v", err)
	}
}

func TestSchema_CreatorCannotBeHardDeletedWhileWorkspaceExists(t *testing.T) {
	// ON DELETE RESTRICT pada workspaces.created_by melindungi workspace dari pembuat yang hilang.
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	if err := repo.CreateWithOwner(ctx, newWS("WS", owner)); err != nil {
		t.Fatal(err)
	}

	if err := db.Exec("DELETE FROM users WHERE internal_id = ?", owner).Error; err == nil {
		t.Fatal("penghapusan pembuat workspace harus ditolak (RESTRICT)")
	}
}

// ---------- UserDirectoryGorm ----------

func TestUserDirectory_FindByEmail(t *testing.T) {
	_, db := setup(t)
	dir := repository.NewUserDirectoryGorm(db)
	id := insertUser(t, db, "budi@example.com")

	t.Run("ditemukan", func(t *testing.T) {
		got, err := dir.FindByEmail(ctx, "budi@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != id || got.Email != "budi@example.com" || got.Name != "Tester" {
			t.Errorf("hasil tidak sesuai: %+v", got)
		}
	})

	t.Run("tidak ada", func(t *testing.T) {
		got, err := dir.FindByEmail(ctx, "tidakada@example.com")
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("harapannya ErrUserNotFound, dapat %v", err)
		}
		if got != nil {
			t.Errorf("hasil harus nil, dapat %+v", got)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			t.Error("error GORM tidak boleh bocor")
		}
	})

	t.Run("pencocokan persis (normalisasi tugas use case)", func(t *testing.T) {
		if _, err := dir.FindByEmail(ctx, "BUDI@example.com"); !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("dapat %v", err)
		}
	})

	t.Run("injeksi SQL tidak berdampak", func(t *testing.T) {
		if _, err := dir.FindByEmail(ctx, "' OR '1'='1"); !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("payload tidak boleh mengembalikan user, dapat %v", err)
		}
		_, _ = dir.FindByEmail(ctx, "x'; DROP TABLE users; --")
		if n := count(t, db, "SELECT COUNT(*) FROM users"); n != 1 {
			t.Errorf("tabel users harus utuh, dapat %d baris", n)
		}
	})
}

func TestUserDirectory_SoftDeletedUserIsNotFound(t *testing.T) {
	_, db := setup(t)
	dir := repository.NewUserDirectoryGorm(db)
	id := insertUser(t, db, "gone@example.com")
	pgtest.Exec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", id)

	if _, err := dir.FindByEmail(ctx, "gone@example.com"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("user yang di-soft-delete tidak boleh bisa ditambahkan, dapat %v", err)
	}
}

func TestUserDirectory_CancelledContextReturnsRealError(t *testing.T) {
	_, db := setup(t)
	dir := repository.NewUserDirectoryGorm(db)
	insertUser(t, db, "a@example.com")

	cctx, cancel := contextCancelled()
	defer cancel()

	_, err := dir.FindByEmail(cctx, "a@example.com")
	if err == nil || errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("context dibatalkan harus error asli, dapat %v", err)
	}
}
