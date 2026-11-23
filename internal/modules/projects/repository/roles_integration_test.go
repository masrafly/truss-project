package repository_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/modules/projects/repository"
	wsdomain "truss-project/internal/modules/workspaces/domain"
	wsrepo "truss-project/internal/modules/workspaces/repository"
	"truss-project/internal/testutil/pgtest"
)

// roleEnv: workspace dengan owner ws; project dimiliki `owner` (Owner project) dengan
// anggota Admin dan Member project.
type roleEnv struct {
	repo                          *repository.ProjectGormRepository
	db                            *gorm.DB
	wid, pid                      uuid.UUID
	wsOwner, owner, admin, member uuid.UUID
}

func newRoleEnv(t *testing.T) *roleEnv {
	t.Helper()
	repo, db := setup(t)
	e := &roleEnv{
		repo: repo, db: db,
		wsOwner: insertUser(t, db, "wsowner@example.com"),
		owner:   insertUser(t, db, "owner@example.com"),
		admin:   insertUser(t, db, "admin@example.com"),
		member:  insertUser(t, db, "member@example.com"),
	}
	e.wid = insertWorkspace(t, db, e.wsOwner)
	for _, u := range []uuid.UUID{e.owner, e.admin, e.member} {
		addWSMember(t, db, e.wid, u, "member")
	}
	p := newProject(e.wid, e.owner, "P")
	mustCreate(t, repo, p)
	e.pid = p.ID
	addProjectMember(t, db, e.pid, e.admin, "admin")
	addProjectMember(t, db, e.pid, e.member, "member")
	return e
}

func (e *roleEnv) role(t *testing.T, uid uuid.UUID) domain.ProjectRole {
	t.Helper()
	r, err := e.repo.FindMemberRole(ctx, e.pid, uid)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *roleEnv) owners(t *testing.T) int64 {
	t.Helper()
	return count(t, e.db, "SELECT COUNT(*) FROM project_members WHERE project_id = ? AND role = 'owner'", e.pid)
}

// ---------- ChangeMemberRole ----------

func TestProjectChangeMemberRole_PersistsAndReturnsProfile(t *testing.T) {
	e := newRoleEnv(t)
	m, err := e.repo.ChangeMemberRole(ctx, e.pid, e.member, domain.ProjectAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if m.UserID != e.member || m.Role != domain.ProjectAdmin || m.Email != "member@example.com" || m.JoinedAt.IsZero() {
		t.Errorf("hasil = %+v", m)
	}
	if e.role(t, e.member) != domain.ProjectAdmin || e.role(t, e.admin) != domain.ProjectAdmin || e.role(t, e.owner) != domain.ProjectOwner {
		t.Error("hanya target yang berubah")
	}
	if _, err := e.repo.ChangeMemberRole(ctx, e.pid, e.admin, domain.ProjectMember); err != nil {
		t.Fatal(err)
	}
	if e.role(t, e.admin) != domain.ProjectMember {
		t.Error("admin harus menjadi member")
	}
}

func TestProjectChangeMemberRole_Errors(t *testing.T) {
	e := newRoleEnv(t)
	if _, err := e.repo.ChangeMemberRole(ctx, e.pid, uuid.New(), domain.ProjectAdmin); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota: %v", err)
	}
	if _, err := e.repo.ChangeMemberRole(ctx, uuid.New(), e.member, domain.ProjectAdmin); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("project lain: %v", err)
	}
	if _, err := e.repo.ChangeMemberRole(ctx, e.pid, e.owner, domain.ProjectMember); !errors.Is(err, domain.ErrCannotChangeOwnerRole) {
		t.Errorf("target Owner: %v", err)
	}
	if e.owners(t) != 1 || e.role(t, e.owner) != domain.ProjectOwner {
		t.Error("Owner tidak boleh berubah")
	}
	pgtest.Exec(t, e.db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", e.member)
	if _, err := e.repo.ChangeMemberRole(ctx, e.pid, e.member, domain.ProjectAdmin); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("akun terhapus: %v", err)
	}
	if e.role(t, e.member) != domain.ProjectMember {
		t.Error("perubahan harus dibatalkan (rollback)")
	}
}

// ---------- TransferOwnership ----------

func TestProjectTransferOwnership_SwapsRolesKeepsExactlyOneOwner(t *testing.T) {
	e := newRoleEnv(t)
	if err := e.repo.TransferOwnership(ctx, e.pid, e.member); err != nil {
		t.Fatal(err)
	}
	if e.role(t, e.member) != domain.ProjectOwner || e.role(t, e.owner) != domain.ProjectAdmin || e.role(t, e.admin) != domain.ProjectAdmin {
		t.Errorf("peran: owner=%s member=%s admin=%s", e.role(t, e.owner), e.role(t, e.member), e.role(t, e.admin))
	}
	if e.owners(t) != 1 {
		t.Errorf("harus tepat satu Owner, dapat %d", e.owners(t))
	}
	// Dapat dipindahkan lagi ke Owner semula.
	if err := e.repo.TransferOwnership(ctx, e.pid, e.owner); err != nil {
		t.Fatal(err)
	}
	if e.role(t, e.owner) != domain.ProjectOwner || e.owners(t) != 1 {
		t.Error("Owner awal harus kembali menjadi satu-satunya Owner")
	}
}

func TestProjectTransferOwnership_Rejections(t *testing.T) {
	e := newRoleEnv(t)
	if err := e.repo.TransferOwnership(ctx, e.pid, e.owner); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("target sudah Owner: %v", err)
	}
	if err := e.repo.TransferOwnership(ctx, e.pid, uuid.New()); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota: %v", err)
	}
	// Anggota workspace yang belum menjadi anggota project tidak dapat menjadi Owner.
	candidate := insertUser(t, e.db, "calon@example.com")
	addWSMember(t, e.db, e.wid, candidate, "member")
	if err := e.repo.TransferOwnership(ctx, e.pid, candidate); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota project: %v", err)
	}
	// Keanggotaan project basi tanpa keanggotaan workspace tidak dapat menjadi Owner.
	pgtest.Exec(t, e.db, "DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?", e.wid, e.member)
	if err := e.repo.TransferOwnership(ctx, e.pid, e.member); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota workspace: %v", err)
	}
	if e.role(t, e.owner) != domain.ProjectOwner || e.owners(t) != 1 {
		t.Error("tidak ada perubahan yang boleh terjadi")
	}
}

func TestProjectTransferOwnership_OnlyAffectsThatProject(t *testing.T) {
	e := newRoleEnv(t)
	other := newProject(e.wid, e.owner, "Lain")
	mustCreate(t, e.repo, other)
	addProjectMember(t, e.db, other.ID, e.member, "member")

	if err := e.repo.TransferOwnership(ctx, e.pid, e.member); err != nil {
		t.Fatal(err)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM project_members WHERE project_id = ? AND user_id = ? AND role = 'owner'", other.ID, e.owner); n != 1 {
		t.Error("Owner project lain tidak boleh berubah")
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM project_members WHERE project_id = ? AND user_id = ? AND role = 'member'", other.ID, e.member); n != 1 {
		t.Error("anggota project lain tidak boleh berubah")
	}
}

func TestProjectTransferOwnership_ConcurrentTransfersLeaveExactlyOneOwner(t *testing.T) {
	e := newRoleEnv(t)
	crepo := repository.NewProjectGormRepository(pgtest.Concurrent(t, e.db, 8)) // koneksi paralel sungguhan
	var wg sync.WaitGroup
	for _, target := range []uuid.UUID{e.admin, e.member, e.admin, e.member} {
		wg.Add(1)
		go func(target uuid.UUID) {
			defer wg.Done()
			_ = crepo.TransferOwnership(ctx, e.pid, target)
		}(target)
	}
	wg.Wait()
	if e.owners(t) != 1 {
		t.Fatalf("harus tepat satu Owner, dapat %d", e.owners(t))
	}
}

// Target dipindahi kepemilikan SEKALIGUS keluar dari workspace: hasilnya harus konsisten dan
// project tidak boleh pernah kehilangan Owner.
func TestProjectTransferOwnership_RacingWithTargetLeavingWorkspaceNeverOrphansProject(t *testing.T) {
	for i := 0; i < 8; i++ {
		e := newRoleEnv(t)
		cdb := pgtest.Concurrent(t, e.db, 8) // koneksi paralel sungguhan
		crepo := repository.NewProjectGormRepository(cdb)
		wsRepo := wsrepo.NewWorkspaceGormRepository(cdb)

		var wg sync.WaitGroup
		var transferErr, leaveErr error
		wg.Add(2)
		go func() { defer wg.Done(); transferErr = crepo.TransferOwnership(ctx, e.pid, e.member) }()
		go func() { defer wg.Done(); leaveErr = wsRepo.RemoveMember(ctx, e.wid, e.member, e.member) }()
		wg.Wait()

		if e.owners(t) != 1 {
			t.Fatalf("iterasi %d: project harus punya tepat satu Owner, dapat %d (transfer=%v keluar=%v)", i, e.owners(t), transferErr, leaveErr)
		}
		switch {
		case transferErr == nil && errors.Is(leaveErr, wsdomain.ErrMemberOwnsProjects):
			// Transfer menang: target kini Owner, sehingga keluar workspace ditolak.
			if e.role(t, e.member) != domain.ProjectOwner {
				t.Fatalf("iterasi %d: target harus Owner", i)
			}
		case errors.Is(transferErr, domain.ErrMemberNotFound) && leaveErr == nil:
			// Keluar menang: target sudah bukan anggota; Owner lama tetap.
			if e.role(t, e.owner) != domain.ProjectOwner {
				t.Fatalf("iterasi %d: Owner lama harus tetap Owner", i)
			}
		default:
			t.Fatalf("iterasi %d: kombinasi hasil tidak konsisten: transfer=%v keluar=%v", i, transferErr, leaveErr)
		}
	}
}

// Urutan kejadian dibuat deterministik: transaksi test meniru proses keluar workspace yang
// SUDAH memeriksa "bukan Owner project" dan menahan kunci baris keanggotaan workspace.
// Transfer yang masuk sesudahnya harus MENUNGGU kunci itu; bila tidak, ia berhasil lebih dulu
// lalu keluar workspace menghapus keanggotaan project Owner yang baru (project tanpa Owner).
func TestProjectTransferOwnership_WaitsForWorkspaceLeaveInProgress(t *testing.T) {
	e := newRoleEnv(t)
	cdb := pgtest.Concurrent(t, e.db, 4) // koneksi paralel sungguhan
	crepo := repository.NewProjectGormRepository(cdb)

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
	var locked int
	if err := tx.Raw(
		"SELECT 1 FROM workspace_members WHERE workspace_id = ? AND user_id = ? FOR UPDATE", e.wid, e.member,
	).Row().Scan(&locked); err != nil {
		t.Fatal(err)
	}
	var owned int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM project_members pm JOIN projects p ON p.internal_id = pm.project_id WHERE p.workspace_id = ? AND pm.user_id = ? AND pm.role = 'owner'",
		e.wid, e.member,
	).Row().Scan(&owned); err != nil || owned != 0 {
		t.Fatalf("prasyarat: target bukan Owner project (owned=%d err=%v)", owned, err)
	}

	done := make(chan error, 1)
	go func() { done <- crepo.TransferOwnership(ctx, e.pid, e.member) }()

	select {
	case err := <-done:
		t.Fatalf("transfer harus menunggu proses keluar workspace yang sedang berjalan, tetapi selesai dengan %v", err)
	case <-time.After(400 * time.Millisecond):
	}

	if err := tx.Exec("DELETE FROM project_members WHERE user_id = ? AND project_id = ?", e.member, e.pid).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?", e.wid, e.member).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	committed = true

	if err := <-done; !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("setelah target keluar, transfer harus ErrMemberNotFound, dapat %v", err)
	}
	if e.owners(t) != 1 || e.role(t, e.owner) != domain.ProjectOwner {
		t.Errorf("Owner lama harus tetap Owner (owners=%d)", e.owners(t))
	}
}

// Target sedang dijadikan Owner oleh transaksi lain (belum commit). Ubah peran harus memeriksa
// ulang peran pada baris yang terkunci: tanpa kunci ia membaca peran lama (member) lalu
// menimpa Owner baru menjadi admin, sehingga project kehilangan Owner.
func TestProjectChangeMemberRole_RechecksTargetOnLockedRow(t *testing.T) {
	e := newRoleEnv(t)
	cdb := pgtest.Concurrent(t, e.db, 4) // koneksi paralel sungguhan
	crepo := repository.NewProjectGormRepository(cdb)

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
	// Meniru transfer yang sedang berjalan: target menjadi Owner, Owner lama menjadi Admin.
	if err := tx.Exec(
		`UPDATE project_members SET role = CASE WHEN user_id = ? THEN 'owner' ELSE 'admin' END
		  WHERE project_id = ? AND user_id IN (?, ?)`,
		e.member, e.pid, e.member, e.owner,
	).Error; err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := crepo.ChangeMemberRole(ctx, e.pid, e.member, domain.ProjectAdmin)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)

	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	committed = true

	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrCannotChangeOwnerRole) {
			t.Fatalf("target kini Owner: harapannya ErrCannotChangeOwnerRole, dapat %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ubah peran macet")
	}
	if e.owners(t) != 1 || e.role(t, e.member) != domain.ProjectOwner {
		t.Errorf("Owner baru tidak boleh tertimpa (owners=%d)", e.owners(t))
	}
}
