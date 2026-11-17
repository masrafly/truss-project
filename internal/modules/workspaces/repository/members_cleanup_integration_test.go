package repository_test

// Integration test: keluar/dihapus dari workspace ikut membersihkan keanggotaan project,
// dan ditolak bila user masih Owner project. Di-skip otomatis tanpa TEST_DB_HOST.

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/testutil/pgtest"
)

// insertProject menyisipkan project langsung lewat SQL (modul projects tidak diimpor).
func insertProject(t *testing.T, db *gorm.DB, workspaceID, createdBy uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Raw(
		"INSERT INTO projects (workspace_id, name, created_by) VALUES (?, ?, ?) RETURNING internal_id",
		workspaceID, name, createdBy,
	).Row().Scan(&id)
	if err != nil {
		t.Fatalf("gagal membuat project: %v", err)
	}
	return id
}

func addProjectMember(t *testing.T, db *gorm.DB, projectID, userID uuid.UUID, role string) {
	t.Helper()
	pgtest.Exec(t, db, "INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)", projectID, userID, role)
}

func TestRemoveMember_AlsoRemovesProjectMembershipsInThatWorkspaceOnly(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	member := insertUser(t, db, "member@example.com")

	w1, w2 := newWS("WS1", owner), newWS("WS2", owner)
	for _, w := range []*domain.Workspace{w1, w2} {
		if err := repo.CreateWithOwner(ctx, w); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.AddMember(ctx, w.ID, member, domain.RoleMember); err != nil {
			t.Fatal(err)
		}
	}
	p1a := insertProject(t, db, w1.ID, owner, "P1a")
	p1b := insertProject(t, db, w1.ID, owner, "P1b")
	p2 := insertProject(t, db, w2.ID, owner, "P2")
	addProjectMember(t, db, p1a, owner, "owner")
	addProjectMember(t, db, p1a, member, "member")
	addProjectMember(t, db, p1b, member, "admin")
	addProjectMember(t, db, p2, member, "member")

	if err := repo.RemoveMember(ctx, w1.ID, member, member); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE user_id = ? AND project_id IN (?, ?)", member, p1a, p1b); n != 0 {
		t.Errorf("keanggotaan project di WS1 harus terhapus, tersisa %d", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE user_id = ? AND project_id = ?", member, p2); n != 1 {
		t.Errorf("keanggotaan project di WS2 harus tetap, dapat %d", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE user_id = ? AND project_id = ?", owner, p1a); n != 1 {
		t.Errorf("keanggotaan user lain tidak boleh tersentuh, dapat %d", n)
	}
	if _, err := repo.FindMemberRole(ctx, w2.ID, member); err != nil {
		t.Errorf("keanggotaan WS2 harus tetap: %v", err)
	}
}

func TestRemoveMember_ProjectOwnerIsRejectedAndNothingChanges(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	admin := insertUser(t, db, "admin@example.com")

	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, w.ID, admin, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	owned := insertProject(t, db, w.ID, admin, "Milik Admin")
	other := insertProject(t, db, w.ID, owner, "Lain")
	addProjectMember(t, db, owned, admin, "owner")
	addProjectMember(t, db, other, admin, "member")

	err := repo.RemoveMember(ctx, w.ID, admin, admin)

	if !errors.Is(err, domain.ErrMemberOwnsProjects) {
		t.Fatalf("harapannya ErrMemberOwnsProjects, dapat %v", err)
	}
	if role, err := repo.FindMemberRole(ctx, w.ID, admin); err != nil || role != domain.RoleAdmin {
		t.Errorf("keanggotaan workspace harus tetap: role=%q err=%v", role, err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE user_id = ?", admin); n != 2 {
		t.Errorf("tidak ada keanggotaan project yang boleh terhapus (rollback), tersisa %d", n)
	}
}

func TestRemoveMember_UnassignsTasksInWorkspaceAndRecordsActivity(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	member := insertUser(t, db, "member@example.com")

	w1, w2 := newWS("WS1", owner), newWS("WS2", owner)
	for _, w := range []*domain.Workspace{w1, w2} {
		if err := repo.CreateWithOwner(ctx, w); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.AddMember(ctx, w.ID, member, domain.RoleMember); err != nil {
			t.Fatal(err)
		}
	}
	p1 := insertProject(t, db, w1.ID, owner, "P1")
	p2 := insertProject(t, db, w2.ID, owner, "P2")
	addProjectMember(t, db, p1, member, "member")
	addProjectMember(t, db, p2, member, "member")
	taskOf := func(pid uuid.UUID) uuid.UUID {
		var id uuid.UUID
		if err := db.Raw("INSERT INTO tasks (project_id, title, created_by, assignee_id) VALUES (?, 'T', ?, ?) RETURNING internal_id",
			pid, owner, member).Row().Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	t1, t2 := taskOf(p1), taskOf(p2)

	if err := repo.RemoveMember(ctx, w1.ID, member, owner); err != nil {
		t.Fatal(err)
	}

	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ? AND assignee_id IS NULL", t1); n != 1 {
		t.Error("task di WS1 harus dilepas")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ? AND assignee_id = ?", t2, member); n != 1 {
		t.Error("task di WS2 tidak boleh tersentuh")
	}
	n := count(t, db,
		`SELECT COUNT(*) FROM task_activities
		  WHERE task_id = ? AND actor_id = ? AND activity_type = 'assignee_changed'
		    AND old_value #>> '{}' = ? AND new_value IS NULL`, t1, owner, member.String())
	if n != 1 {
		t.Errorf("aktivitas pelepasan harus tercatat atas nama aktor, dapat %d", n)
	}
}

func TestRemoveMember_RejectedRemovalKeepsAssignments(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	admin := insertUser(t, db, "admin@example.com")
	w := newWS("WS", owner)
	if err := repo.CreateWithOwner(ctx, w); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, w.ID, admin, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	owned := insertProject(t, db, w.ID, admin, "Milik Admin")
	addProjectMember(t, db, owned, admin, "owner")
	pgtest.Exec(t, db, "INSERT INTO tasks (project_id, title, created_by, assignee_id) VALUES (?, 'T', ?, ?)", owned, admin, admin)

	if err := repo.RemoveMember(ctx, w.ID, admin, owner); !errors.Is(err, domain.ErrMemberOwnsProjects) {
		t.Fatalf("dapat %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE assignee_id = ?", admin); n != 1 {
		t.Error("assignee tidak boleh dilepas bila penghapusan ditolak")
	}
}

func TestRemoveMember_OwnershipInAnotherWorkspaceDoesNotBlock(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	member := insertUser(t, db, "member@example.com")

	w1, w2 := newWS("WS1", owner), newWS("WS2", owner)
	for _, w := range []*domain.Workspace{w1, w2} {
		if err := repo.CreateWithOwner(ctx, w); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.AddMember(ctx, w.ID, member, domain.RoleAdmin); err != nil {
			t.Fatal(err)
		}
	}
	p2 := insertProject(t, db, w2.ID, member, "Milik di WS2")
	addProjectMember(t, db, p2, member, "owner")

	if err := repo.RemoveMember(ctx, w1.ID, member, member); err != nil {
		t.Fatalf("Owner project di WS2 tetap boleh keluar dari WS1: %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE user_id = ? AND project_id = ?", member, p2); n != 1 {
		t.Errorf("project di WS2 tidak boleh tersentuh, dapat %d", n)
	}
}
