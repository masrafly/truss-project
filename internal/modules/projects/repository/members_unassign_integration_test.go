package repository_test

// Integration test: menghapus anggota project melepas assignee task-nya dan mencatat
// aktivitas. Di-skip otomatis tanpa TEST_DB_HOST.

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func insertTask(t *testing.T, db *gorm.DB, projectID, createdBy uuid.UUID, assignee *uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Raw(
		"INSERT INTO tasks (project_id, title, created_by, assignee_id) VALUES (?, 'T', ?, ?) RETURNING internal_id",
		projectID, createdBy, assignee,
	).Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRemoveMember_UnassignsTasksInThatProjectAndRecordsActivity(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	u := insertUser(t, db, "u@example.com")
	other := insertUser(t, db, "other@example.com")
	wid, pid := projectWithOwner(t, repo, db, owner)
	addWSMember(t, db, wid, u, "member")
	addWSMember(t, db, wid, other, "member")
	p2 := newProject(wid, owner, "P2")
	mustCreate(t, repo, p2)
	for _, p := range []uuid.UUID{pid, p2.ID} {
		addProjectMember(t, db, p, u, "member")
	}
	addProjectMember(t, db, pid, other, "member")

	mine1 := insertTask(t, db, pid, owner, &u)
	mine2 := insertTask(t, db, pid, owner, &u)
	othersTask := insertTask(t, db, pid, owner, &other)
	otherProject := insertTask(t, db, p2.ID, owner, &u)

	if err := repo.RemoveMember(ctx, pid, u, owner); err != nil {
		t.Fatal(err)
	}

	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE internal_id IN (?, ?) AND assignee_id IS NULL", mine1, mine2); n != 2 {
		t.Errorf("task di project ini harus dilepas, dapat %d", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ? AND assignee_id = ?", othersTask, other); n != 1 {
		t.Error("task milik orang lain tidak boleh tersentuh")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ? AND assignee_id = ?", otherProject, u); n != 1 {
		t.Error("task di project lain tidak boleh tersentuh")
	}
	n := count(t, db,
		`SELECT COUNT(*) FROM task_activities
		  WHERE task_id IN (?, ?) AND actor_id = ? AND activity_type = 'assignee_changed'
		    AND old_value #>> '{}' = ? AND new_value IS NULL`,
		mine1, mine2, owner, u.String())
	if n != 2 {
		t.Errorf("harus ada 2 aktivitas assignee_changed oleh aktor, dapat %d", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM task_activities WHERE task_id IN (?, ?)", othersTask, otherProject); n != 0 {
		t.Error("tidak boleh ada aktivitas untuk task lain")
	}
}

func TestRemoveMember_OwnerGuardLeavesTasksUntouched(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	_, pid := projectWithOwner(t, repo, db, owner)
	task := insertTask(t, db, pid, owner, &owner)

	if err := repo.RemoveMember(ctx, pid, owner, owner); err == nil {
		t.Fatal("Owner tidak boleh terhapus")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ? AND assignee_id = ?", task, owner); n != 1 {
		t.Error("assignee tidak boleh dilepas bila penghapusan gagal")
	}
}
