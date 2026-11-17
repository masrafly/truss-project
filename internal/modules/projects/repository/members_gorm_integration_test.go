package repository_test

// Integration test anggota project. Memakai helper dari project_gorm_integration_test.go.
// Di-skip otomatis jika TEST_DB_HOST tidak diisi.

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/modules/projects/repository"
	"truss-project/internal/testutil/pgtest"
)

// projectWithOwner: workspace + project milik owner; mengembalikan (workspaceID, projectID).
func projectWithOwner(t *testing.T, repo *repository.ProjectGormRepository, db *gorm.DB, owner uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	wid := insertWorkspace(t, db, owner)
	p := newProject(wid, owner, "P")
	mustCreate(t, repo, p)
	return wid, p.ID
}

// ---------- FindWorkspaceMemberByEmail ----------

func TestFindWorkspaceMemberByEmail(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	wsMember := insertUser(t, db, "anggota@example.com")
	insertUser(t, db, "asing@example.com")
	deleted := insertUser(t, db, "hapus@example.com")
	wid, pid := projectWithOwner(t, repo, db, owner)
	addWSMember(t, db, wid, wsMember, "member")
	addWSMember(t, db, wid, deleted, "member")
	pgtest.Exec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", deleted)

	got, err := repo.FindWorkspaceMemberByEmail(ctx, pid, "anggota@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != wsMember || got.Email != "anggota@example.com" || got.Name != "Tester" {
		t.Errorf("hasil = %+v", got)
	}

	for _, email := range []string{"asing@example.com", "tidakada@example.com", "hapus@example.com"} {
		if _, err := repo.FindWorkspaceMemberByEmail(ctx, pid, email); !errors.Is(err, domain.ErrWorkspaceUserNotFound) {
			t.Errorf("%s: harapannya ErrWorkspaceUserNotFound, dapat %v", email, err)
		}
	}
	if _, err := repo.FindWorkspaceMemberByEmail(ctx, uuid.New(), "anggota@example.com"); !errors.Is(err, domain.ErrWorkspaceUserNotFound) {
		t.Errorf("project tak ada: dapat %v", err)
	}
}

func TestFindWorkspaceMemberByEmail_OtherWorkspaceDoesNotCount(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	other := insertUser(t, db, "lain@example.com")
	_, pid := projectWithOwner(t, repo, db, owner)
	otherWS := insertWorkspace(t, db, owner)
	addWSMember(t, db, otherWS, other, "member") // anggota workspace LAIN

	if _, err := repo.FindWorkspaceMemberByEmail(ctx, pid, "lain@example.com"); !errors.Is(err, domain.ErrWorkspaceUserNotFound) {
		t.Fatalf("anggota workspace lain tidak boleh ditemukan, dapat %v", err)
	}
}

// ---------- AddMember / FindMemberRole ----------

func TestAddMember_PersistsAndDuplicateIsConflict(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	u := insertUser(t, db, "u@example.com")
	wid, pid := projectWithOwner(t, repo, db, owner)
	addWSMember(t, db, wid, u, "member")

	joined, err := repo.AddMember(ctx, pid, u, domain.ProjectAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if joined.IsZero() || time.Since(joined) > time.Minute {
		t.Errorf("joinedAt tidak wajar: %v", joined)
	}
	if role, err := repo.FindMemberRole(ctx, pid, u); err != nil || role != domain.ProjectAdmin {
		t.Errorf("role=%q err=%v", role, err)
	}

	if _, err := repo.AddMember(ctx, pid, u, domain.ProjectMember); !errors.Is(err, domain.ErrAlreadyMember) {
		t.Errorf("duplikat: harapannya ErrAlreadyMember, dapat %v", err)
	}
	if role, _ := repo.FindMemberRole(ctx, pid, u); role != domain.ProjectAdmin {
		t.Errorf("role tidak boleh berubah oleh penambahan ganda, dapat %q", role)
	}
}

func TestAddMember_NonWorkspaceMemberRejected(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	outsider := insertUser(t, db, "out@example.com")
	_, pid := projectWithOwner(t, repo, db, owner)

	if _, err := repo.AddMember(ctx, pid, outsider, domain.ProjectMember); !errors.Is(err, domain.ErrWorkspaceUserNotFound) {
		t.Fatalf("harapannya ErrWorkspaceUserNotFound, dapat %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE user_id = ?", outsider); n != 0 {
		t.Errorf("tidak boleh tersimpan, dapat %d", n)
	}
}

func TestFindMemberRole_NotMember(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	_, pid := projectWithOwner(t, repo, db, owner)

	if role, err := repo.FindMemberRole(ctx, pid, owner); err != nil || role != domain.ProjectOwner {
		t.Errorf("owner: role=%q err=%v", role, err)
	}
	if _, err := repo.FindMemberRole(ctx, pid, uuid.New()); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("harapannya ErrMemberNotFound, dapat %v", err)
	}
}

// AddMember mengunci keanggotaan workspace target (FOR SHARE). Bila transaksi lain
// (proses keluar workspace) memegang FOR UPDATE lalu menghapusnya, AddMember harus
// menunggu lalu ditolak, sehingga tidak ada anggota project yang bukan anggota workspace.
func TestAddMember_WaitsForConcurrentWorkspaceRemoval(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	target := insertUser(t, db, "target@example.com")
	wid, pid := projectWithOwner(t, repo, db, owner)
	addWSMember(t, db, wid, target, "member")

	var schema string
	if err := db.Raw("SELECT current_schema()").Row().Scan(&schema); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(2)
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Bila test gagal sebelum Commit, transaksi HARUS dilepas: tanpa ini conn.Close() menunggu
	// transaksi tanpa batas waktu dan test menggantung sampai timeout 10 menit.
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		"SELECT 1 FROM workspace_members WHERE workspace_id = $1 AND user_id = $2 FOR UPDATE", wid, target); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- db.Connection(func(c *gorm.DB) error {
			if err := c.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
				return err
			}
			_, err := repository.NewProjectGormRepository(c).AddMember(ctx, pid, target, domain.ProjectMember)
			return err
		})
	}()

	select {
	case err := <-done:
		t.Fatalf("AddMember tidak menunggu kunci, selesai dengan %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2", wid, target); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrWorkspaceUserNotFound) {
			t.Fatalf("harapannya ErrWorkspaceUserNotFound, dapat %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("AddMember macet")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE user_id = ?", target); n != 0 {
		t.Errorf("tidak boleh ada keanggotaan project yatim, dapat %d", n)
	}
}

// ---------- ListMembers ----------

func TestListMembers_OrderAndSoftDelete(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	m1 := insertUser(t, db, "m1@example.com")
	adm := insertUser(t, db, "adm@example.com")
	m2 := insertUser(t, db, "m2@example.com")
	gone := insertUser(t, db, "gone@example.com")
	wid, pid := projectWithOwner(t, repo, db, owner)
	for _, u := range []uuid.UUID{m1, adm, m2, gone} {
		addWSMember(t, db, wid, u, "member")
	}
	add := func(u uuid.UUID, r domain.ProjectRole) {
		if _, err := repo.AddMember(ctx, pid, u, r); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	add(m1, domain.ProjectMember)
	add(adm, domain.ProjectAdmin)
	add(m2, domain.ProjectMember)
	add(gone, domain.ProjectAdmin)
	pgtest.Exec(t, db, "UPDATE users SET deleted_at = NOW() WHERE internal_id = ?", gone)

	got, err := repo.ListMembers(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	want := []uuid.UUID{owner, adm, m1, m2}
	if len(got) != len(want) {
		t.Fatalf("dapat %d anggota, harapannya %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].UserID != id {
			t.Errorf("urutan[%d] = %s, harapannya %v", i, got[i].Email, id)
		}
	}
	if got[0].Role != domain.ProjectOwner || got[0].Email != "owner@example.com" || got[0].JoinedAt.IsZero() {
		t.Errorf("pemetaan field salah: %+v", got[0])
	}

	empty, err := repo.ListMembers(ctx, uuid.New())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("project tak ada: harapannya slice kosong non-nil, dapat %#v err=%v", empty, err)
	}
}

// ---------- RemoveMember ----------

func TestRemoveMember(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	u := insertUser(t, db, "u@example.com")
	wid, pid := projectWithOwner(t, repo, db, owner)
	addWSMember(t, db, wid, u, "member")
	other := newProject(wid, owner, "Lain")
	mustCreate(t, repo, other)
	for _, p := range []uuid.UUID{pid, other.ID} {
		if _, err := repo.AddMember(ctx, p, u, domain.ProjectMember); err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.RemoveMember(ctx, pid, u, u); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindMemberRole(ctx, pid, u); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("harus terhapus: %v", err)
	}
	if _, err := repo.FindMemberRole(ctx, other.ID, u); err != nil {
		t.Errorf("keanggotaan project lain harus tetap: %v", err)
	}
	if err := repo.RemoveMember(ctx, pid, u, u); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("hapus kedua kali: dapat %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ? AND user_id = ?", wid, u); n != 1 {
		t.Error("keanggotaan workspace tidak boleh tersentuh")
	}
}

func TestRemoveMember_NeverDeletesOwner(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	_, pid := projectWithOwner(t, repo, db, owner)

	if err := repo.RemoveMember(ctx, pid, owner, owner); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("harapannya ErrMemberNotFound (pertahanan terakhir), dapat %v", err)
	}
	if role, err := repo.FindMemberRole(ctx, pid, owner); err != nil || role != domain.ProjectOwner {
		t.Errorf("Owner harus tetap ada: role=%q err=%v", role, err)
	}
}

// ---------- context ----------

func TestMemberRepo_CancelledContext(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	_, pid := projectWithOwner(t, repo, db, owner)

	cctx, cancel := contextCancelled()
	defer cancel()

	if _, err := repo.FindWorkspaceMemberByEmail(cctx, pid, "owner@example.com"); err == nil || errors.Is(err, domain.ErrWorkspaceUserNotFound) {
		t.Errorf("FindWorkspaceMemberByEmail: dapat %v", err)
	}
	if _, err := repo.FindMemberRole(cctx, pid, owner); err == nil || errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("FindMemberRole: dapat %v", err)
	}
	if _, err := repo.AddMember(cctx, pid, owner, domain.ProjectMember); err == nil || errors.Is(err, domain.ErrAlreadyMember) {
		t.Errorf("AddMember: dapat %v", err)
	}
	if _, err := repo.ListMembers(cctx, pid); err == nil {
		t.Error("ListMembers harus error")
	}
	if err := repo.RemoveMember(cctx, pid, owner, owner); err == nil || errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("RemoveMember: dapat %v", err)
	}
}
