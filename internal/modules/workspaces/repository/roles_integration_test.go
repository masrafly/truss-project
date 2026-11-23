package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/modules/workspaces/repository"
	"truss-project/internal/testutil/pgtest"
)

// rolesEnv: workspace dengan Owner, Admin, dan Member.
type rolesEnv struct {
	repo                 *repository.WorkspaceGormRepository
	db                   *gorm.DB
	wid                  uuid.UUID
	owner, admin, member uuid.UUID
}

func newRolesEnv(t *testing.T) *rolesEnv {
	t.Helper()
	repo, db := setup(t)
	e := &rolesEnv{
		repo: repo, db: db,
		owner:  insertUser(t, db, "owner@example.com"),
		admin:  insertUser(t, db, "admin@example.com"),
		member: insertUser(t, db, "member@example.com"),
	}
	w := newWS("WS", e.owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}
	e.wid = w.ID
	if _, err := repo.AddMember(ctx, e.wid, e.admin, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, e.wid, e.member, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *rolesEnv) role(t *testing.T, uid uuid.UUID) domain.Role {
	t.Helper()
	r, err := e.repo.FindMemberRole(ctx, e.wid, uid)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *rolesEnv) owners(t *testing.T) int64 {
	t.Helper()
	return count(t, e.db, "SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ? AND role = 'owner'", e.wid)
}

// ---------- ChangeMemberRole ----------

func TestChangeMemberRole_PersistsAndReturnsProfile(t *testing.T) {
	e := newRolesEnv(t)

	m, err := e.repo.ChangeMemberRole(ctx, e.wid, e.owner, e.member, domain.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if m.UserID != e.member || m.Role != domain.RoleAdmin || m.Email != "member@example.com" || m.Name != "Tester" || m.JoinedAt.IsZero() {
		t.Errorf("hasil = %+v", m)
	}
	if e.role(t, e.member) != domain.RoleAdmin {
		t.Error("peran harus tersimpan")
	}
	if _, err := e.repo.ChangeMemberRole(ctx, e.wid, e.owner, e.admin, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if e.role(t, e.admin) != domain.RoleMember || e.role(t, e.owner) != domain.RoleOwner {
		t.Error("hanya target yang berubah")
	}
}

func TestChangeMemberRole_RejectsNonOwnerActorAndLeavesDataUntouched(t *testing.T) {
	e := newRolesEnv(t)
	for name, actor := range map[string]uuid.UUID{"admin": e.admin, "member": e.member} {
		_, err := e.repo.ChangeMemberRole(ctx, e.wid, actor, e.member, domain.RoleAdmin)
		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	outsider := insertUser(t, e.db, "out@example.com")
	if _, err := e.repo.ChangeMemberRole(ctx, e.wid, outsider, e.member, domain.RoleAdmin); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("non-anggota: %v", err)
	}
	if e.role(t, e.member) != domain.RoleMember {
		t.Error("peran tidak boleh berubah")
	}
}

func TestChangeMemberRole_TargetErrors(t *testing.T) {
	e := newRolesEnv(t)
	if _, err := e.repo.ChangeMemberRole(ctx, e.wid, e.owner, uuid.New(), domain.RoleAdmin); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota: %v", err)
	}
	if _, err := e.repo.ChangeMemberRole(ctx, e.wid, e.owner, e.owner, domain.RoleMember); !errors.Is(err, domain.ErrCannotChangeOwnerRole) {
		t.Errorf("target Owner: %v", err)
	}
	if e.owners(t) != 1 {
		t.Error("harus tetap ada tepat satu Owner")
	}
	other := newWS("Lain", e.owner)
	if err := e.repo.CreateWithOwner(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.ChangeMemberRole(ctx, other.ID, e.owner, e.member, domain.RoleAdmin); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("anggota workspace lain tidak boleh terpengaruh: %v", err)
	}
}

func TestChangeMemberRole_SoftDeletedTargetNotFound(t *testing.T) {
	e := newRolesEnv(t)
	pgtest.Exec(t, e.db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", e.member)
	if _, err := e.repo.ChangeMemberRole(ctx, e.wid, e.owner, e.member, domain.RoleAdmin); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("err = %v", err)
	}
	if e.role(t, e.member) != domain.RoleMember {
		t.Error("perubahan harus dibatalkan (rollback)")
	}
}

// ---------- TransferOwnership ----------

func TestTransferOwnership_SwapsRolesAndKeepsExactlyOneOwner(t *testing.T) {
	e := newRolesEnv(t)

	if err := e.repo.TransferOwnership(ctx, e.wid, e.owner, e.member); err != nil {
		t.Fatal(err)
	}
	if e.role(t, e.member) != domain.RoleOwner || e.role(t, e.owner) != domain.RoleAdmin || e.role(t, e.admin) != domain.RoleAdmin {
		t.Errorf("peran setelah transfer: owner=%s member=%s admin=%s", e.role(t, e.owner), e.role(t, e.member), e.role(t, e.admin))
	}
	if e.owners(t) != 1 {
		t.Errorf("harus tepat satu Owner, dapat %d", e.owners(t))
	}
	// Owner lama (kini Admin) tidak lagi dapat mengubah peran atau memindahkan kepemilikan.
	if err := e.repo.TransferOwnership(ctx, e.wid, e.owner, e.admin); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Owner lama harus ditolak: %v", err)
	}
}

func TestTransferOwnership_Rejections(t *testing.T) {
	e := newRolesEnv(t)
	if err := e.repo.TransferOwnership(ctx, e.wid, e.admin, e.member); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Admin: %v", err)
	}
	if err := e.repo.TransferOwnership(ctx, e.wid, e.owner, uuid.New()); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("target bukan anggota: %v", err)
	}
	if err := e.repo.TransferOwnership(ctx, e.wid, e.owner, e.owner); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("ke diri sendiri: %v", err)
	}
	if e.role(t, e.owner) != domain.RoleOwner || e.owners(t) != 1 {
		t.Error("tidak ada perubahan yang boleh terjadi")
	}
}

func TestTransferOwnership_ConcurrentTransfersLeaveExactlyOneOwner(t *testing.T) {
	e := newRolesEnv(t)
	crepo := repository.NewWorkspaceGormRepository(pgtest.Concurrent(t, e.db, 8)) // koneksi paralel sungguhan

	// Dua transfer serentak dari Owner yang sama ke dua target berbeda: yang kedua melihat
	// dirinya sudah bukan Owner pada baris terkunci dan harus ditolak.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, target := range []uuid.UUID{e.admin, e.member} {
		wg.Add(1)
		go func(i int, target uuid.UUID) {
			defer wg.Done()
			errs[i] = crepo.TransferOwnership(ctx, e.wid, e.owner, target)
		}(i, target)
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, domain.ErrForbidden):
			t.Fatalf("error tak terduga: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("tepat satu transfer boleh berhasil, berhasil = %d", ok)
	}
	if e.owners(t) != 1 {
		t.Errorf("harus tepat satu Owner, dapat %d", e.owners(t))
	}
}

func TestOwnerOperations_ConcurrentTransferAndChangeRoleNeverDeadlockOrCorrupt(t *testing.T) {
	e := newRolesEnv(t)
	crepo := repository.NewWorkspaceGormRepository(pgtest.Concurrent(t, e.db, 8)) // koneksi paralel sungguhan
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = crepo.TransferOwnership(ctx, e.wid, e.owner, e.admin)
	}()
	go func() {
		defer wg.Done()
		_, _ = crepo.ChangeMemberRole(ctx, e.wid, e.owner, e.member, domain.RoleAdmin)
	}()
	wg.Wait()
	if e.owners(t) != 1 {
		t.Errorf("harus tepat satu Owner, dapat %d", e.owners(t))
	}
}

// ---------- Delete ----------

func TestDeleteWorkspace_OwnerRemovesEverythingInsideButNothingElse(t *testing.T) {
	e := newRolesEnv(t)
	pid := insertProject(t, e.db, e.wid, e.owner, "Proyek")
	addProjectMember(t, e.db, pid, e.member, "member")
	var tid uuid.UUID
	if err := e.db.Raw(
		"INSERT INTO tasks (project_id, title, created_by) VALUES (?, 'T', ?) RETURNING internal_id", pid, e.owner,
	).Row().Scan(&tid); err != nil {
		t.Fatal(err)
	}
	pgtest.Exec(t, e.db, "INSERT INTO task_comments (task_id, user_id, content) VALUES (?, ?, 'c')", tid, e.owner)
	pgtest.Exec(t, e.db, "INSERT INTO task_activities (task_id, actor_id, activity_type) VALUES (?, ?, 'created')", tid, e.owner)

	other := newWS("Lain", e.owner)
	if err := e.repo.CreateWithOwner(ctx, other); err != nil {
		t.Fatal(err)
	}
	otherProject := insertProject(t, e.db, other.ID, e.owner, "Aman")

	if err := e.repo.Delete(ctx, e.wid, e.owner); err != nil {
		t.Fatal(err)
	}

	for table, where := range map[string]string{
		"workspaces":        "internal_id = ?",
		"workspace_members": "workspace_id = ?",
		"projects":          "workspace_id = ?",
	} {
		if n := count(t, e.db, "SELECT COUNT(*) FROM "+table+" WHERE "+where, e.wid); n != 0 {
			t.Errorf("%s: %d baris tersisa", table, n)
		}
	}
	for table, col := range map[string]string{"project_members": "project_id", "tasks": "project_id"} {
		if n := count(t, e.db, "SELECT COUNT(*) FROM "+table+" WHERE "+col+" = ?", pid); n != 0 {
			t.Errorf("%s: %d baris tersisa", table, n)
		}
	}
	for _, table := range []string{"task_comments", "task_activities"} {
		if n := count(t, e.db, "SELECT COUNT(*) FROM "+table+" WHERE task_id = ?", tid); n != 0 {
			t.Errorf("%s: %d baris tersisa", table, n)
		}
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM users WHERE internal_id IN (?, ?, ?)", e.owner, e.admin, e.member); n != 3 {
		t.Errorf("user tidak boleh ikut terhapus, tersisa %d", n)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM projects WHERE internal_id = ?", otherProject); n != 1 {
		t.Error("workspace lain tidak boleh terpengaruh")
	}
}

func TestDeleteWorkspace_NonOwnerAndOutsiderCannotDelete(t *testing.T) {
	e := newRolesEnv(t)
	for name, actor := range map[string]uuid.UUID{"admin": e.admin, "member": e.member} {
		if err := e.repo.Delete(ctx, e.wid, actor); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	outsider := insertUser(t, e.db, "out@example.com")
	if err := e.repo.Delete(ctx, e.wid, outsider); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("non-anggota: %v", err)
	}
	if err := e.repo.Delete(ctx, uuid.New(), e.owner); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("workspace tak ada: %v", err)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM workspaces WHERE internal_id = ?", e.wid); n != 1 {
		t.Error("workspace tidak boleh terhapus")
	}
}

func TestDeleteWorkspace_FormerOwnerAfterTransferCannotDelete(t *testing.T) {
	e := newRolesEnv(t)
	if err := e.repo.TransferOwnership(ctx, e.wid, e.owner, e.admin); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Delete(ctx, e.wid, e.owner); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Owner lama harus ditolak: %v", err)
	}
	if err := e.repo.Delete(ctx, e.wid, e.admin); err != nil {
		t.Fatalf("Owner baru boleh menghapus: %v", err)
	}
}

func TestRolesOperations_CanceledContextReturnsInfrastructureErrors(t *testing.T) {
	e := newRolesEnv(t)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.repo.ChangeMemberRole(cctx, e.wid, e.owner, e.member, domain.RoleAdmin); err == nil || errors.Is(err, domain.ErrForbidden) {
		t.Errorf("ChangeMemberRole: %v", err)
	}
	if err := e.repo.TransferOwnership(cctx, e.wid, e.owner, e.member); err == nil || errors.Is(err, domain.ErrForbidden) {
		t.Errorf("TransferOwnership: %v", err)
	}
	if err := e.repo.Delete(cctx, e.wid, e.owner); err == nil || errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Delete: %v", err)
	}
	if e.role(t, e.owner) != domain.RoleOwner {
		t.Error("tidak ada perubahan yang boleh terjadi")
	}
}

// Owner yang sedang diturunkan (transaksi lain belum commit) tidak boleh sempat menjalankan
// operasi khusus Owner: operasi harus MENUNGGU kunci baris, lalu memeriksa ulang peran pada
// baris yang sudah ter-commit. Tanpa kunci, pemeriksaan membaca peran lama (Owner) dan lolos.
func TestOwnerOperations_WaitForRowLockThenRecheckActorRole(t *testing.T) {
	ops := map[string]func(*repository.WorkspaceGormRepository, *rolesEnv) error{
		"ubah peran": func(r *repository.WorkspaceGormRepository, e *rolesEnv) error {
			_, err := r.ChangeMemberRole(ctx, e.wid, e.owner, e.member, domain.RoleAdmin)
			return err
		},
		"transfer": func(r *repository.WorkspaceGormRepository, e *rolesEnv) error {
			return r.TransferOwnership(ctx, e.wid, e.owner, e.member)
		},
		"hapus workspace": func(r *repository.WorkspaceGormRepository, e *rolesEnv) error {
			return r.Delete(ctx, e.wid, e.owner)
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			e := newRolesEnv(t)
			cdb := pgtest.Concurrent(t, e.db, 4) // koneksi paralel sungguhan
			crepo := repository.NewWorkspaceGormRepository(cdb)

			tx := cdb.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			committed := false
			defer func() {
				if !committed {
					_ = tx.Rollback()
				}
			}()
			// Menurunkan Owner (belum commit) = menahan kunci baris Owner.
			if err := tx.Exec(
				"UPDATE workspace_members SET role = 'admin' WHERE workspace_id = ? AND user_id = ?", e.wid, e.owner,
			).Error; err != nil {
				t.Fatal(err)
			}

			done := make(chan error, 1)
			go func() { done <- op(crepo, e) }()

			select {
			case err := <-done:
				t.Fatalf("operasi harus menunggu kunci baris Owner, tetapi selesai dengan %v", err)
			case <-time.After(400 * time.Millisecond):
			}

			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			committed = true

			select {
			case err := <-done:
				if !errors.Is(err, domain.ErrForbidden) {
					t.Fatalf("setelah Owner turun, operasi harus ErrForbidden, dapat %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("operasi macet")
			}
			if e.role(t, e.member) != domain.RoleMember {
				t.Error("peran member tidak boleh berubah")
			}
			if n := count(t, e.db, "SELECT COUNT(*) FROM workspaces WHERE internal_id = ?", e.wid); n != 1 {
				t.Error("workspace tidak boleh terhapus")
			}
		})
	}
}
