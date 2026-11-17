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

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/repository"
	"truss-project/internal/testutil/pgtest"
)

var ctx = context.Background()

type env struct {
	repo *repository.TaskGormRepository
	db   *gorm.DB
	// owner: Owner ws & project. member: Member ws & project. plain: Member ws saja.
	owner, member, plain uuid.UUID
	wid, pid             uuid.UUID
}

func setup(t *testing.T) *env {
	t.Helper()
	db := pgtest.Setup(t)
	e := &env{repo: repository.NewTaskGormRepository(db), db: db}
	e.owner = insertUser(t, db, "owner@example.com", "Owner")
	e.member = insertUser(t, db, "member@example.com", "Member")
	e.plain = insertUser(t, db, "plain@example.com", "Plain")
	e.wid = scanID(t, db, "INSERT INTO workspaces (name, created_by) VALUES ('WS', ?) RETURNING internal_id", e.owner)
	exec(t, db, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'owner'), (?, ?, 'member'), (?, ?, 'member')",
		e.wid, e.owner, e.wid, e.member, e.wid, e.plain)
	e.pid = e.newProject(t, "P")
	exec(t, db, "INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, 'member')", e.pid, e.member)
	return e
}

func (e *env) newProject(t *testing.T, name string) uuid.UUID {
	t.Helper()
	pid := scanID(t, e.db, "INSERT INTO projects (workspace_id, name, created_by) VALUES (?, ?, ?) RETURNING internal_id", e.wid, name, e.owner)
	exec(t, e.db, "INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, 'owner')", pid, e.owner)
	return pid
}

func insertUser(t *testing.T, db *gorm.DB, email, name string) uuid.UUID {
	t.Helper()
	return scanID(t, db, "INSERT INTO users (name, email, password_hash) VALUES (?, ?, 'hash') RETURNING internal_id", name, email)
}

func scanID(t *testing.T, db *gorm.DB, q string, args ...any) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Raw(q, args...).Row().Scan(&id); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
	return id
}

func exec(t *testing.T, db *gorm.DB, q string, args ...any) {
	t.Helper()
	pgtest.Exec(t, db, q, args...)
}

func count(t *testing.T, db *gorm.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(q, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) create(t *testing.T, title string, mod func(*domain.Task)) *domain.TaskView {
	t.Helper()
	task := &domain.Task{ProjectID: e.pid, Title: title, Status: domain.StatusTodo, Priority: domain.PriorityMedium, CreatedBy: e.owner}
	if mod != nil {
		mod(task)
	}
	v, err := e.repo.Create(ctx, task, []domain.Activity{domain.CreatedActivity(*task, e.owner)})
	if err != nil {
		t.Fatalf("Create %q: %v", title, err)
	}
	return v
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }

// ---------- FindProjectAccess ----------

func TestFindProjectAccess(t *testing.T) {
	e := setup(t)
	outsider := insertUser(t, e.db, "out@example.com", "Out")

	cases := []struct {
		uid    uuid.UUID
		ws, pr string
	}{
		{e.owner, "owner", "owner"},
		{e.member, "member", "member"},
		{e.plain, "member", ""},
		{outsider, "", ""},
	}
	for _, c := range cases {
		a, err := e.repo.FindProjectAccess(ctx, e.pid, c.uid)
		if err != nil {
			t.Fatal(err)
		}
		if string(a.WorkspaceRole) != c.ws || string(a.ProjectRole) != c.pr || a.ProjectID != e.pid || a.WorkspaceID != e.wid || a.Archived {
			t.Errorf("user %v: akses = %+v", c.uid, a)
		}
	}

	exec(t, e.db, "UPDATE projects SET status = 'archived' WHERE internal_id = ?", e.pid)
	if a, _ := e.repo.FindProjectAccess(ctx, e.pid, e.owner); !a.Archived {
		t.Error("Archived harus true")
	}
	exec(t, e.db, "UPDATE projects SET status = 'completed' WHERE internal_id = ?", e.pid)
	if a, _ := e.repo.FindProjectAccess(ctx, e.pid, e.owner); a.Archived {
		t.Error("completed bukan archived")
	}
	if _, err := e.repo.FindProjectAccess(ctx, uuid.New(), e.owner); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
}

// ---------- Create / FindTaskAccess ----------

func TestCreate_PersistsTaskAndActivity(t *testing.T) {
	e := setup(t)
	due := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	desc := "isi"

	v := e.create(t, "Tulis test", func(task *domain.Task) {
		task.Description, task.AssigneeID, task.DueDate = &desc, ptrUUID(e.member), &due
		task.Status, task.Priority = domain.StatusInProgress, domain.PriorityUrgent
	})

	if v.ID == uuid.Nil || v.CreatedAt.IsZero() || v.Title != "Tulis test" || *v.Description != "isi" ||
		v.Status != domain.StatusInProgress || v.Priority != domain.PriorityUrgent || !v.DueDate.Equal(due) {
		t.Errorf("view salah: %+v", v.Task)
	}
	if v.Assignee == nil || v.Assignee.ID != e.member || v.Assignee.Name != "Member" {
		t.Errorf("assignee view = %+v", v.Assignee)
	}

	acts, err := e.repo.ListActivities(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0].Type != domain.ActivityCreated || acts[0].OldValue != nil ||
		acts[0].NewValue == nil || *acts[0].NewValue != "Tulis test" || acts[0].Actor.Name != "Owner" {
		t.Errorf("aktivitas = %+v", acts)
	}

	got, access, err := e.repo.FindTaskAccess(ctx, v.ID, e.member)
	if err != nil || got.ID != v.ID || access.ProjectRole != domain.ProjectMember {
		t.Errorf("FindTaskAccess: %+v %+v %v", got, access, err)
	}
	if _, _, err := e.repo.FindTaskAccess(ctx, uuid.New(), e.owner); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("dapat %v", err)
	}
}

func TestCreate_AssigneeMustBeProjectMember(t *testing.T) {
	e := setup(t)
	task := &domain.Task{ProjectID: e.pid, Title: "T", Status: domain.StatusTodo, Priority: domain.PriorityLow,
		CreatedBy: e.owner, AssigneeID: ptrUUID(e.plain)}

	_, err := e.repo.Create(ctx, task, []domain.Activity{domain.CreatedActivity(*task, e.owner)})

	if !errors.Is(err, domain.ErrAssigneeNotMember) {
		t.Fatalf("harapannya ErrAssigneeNotMember, dapat %v", err)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM tasks"); n != 0 {
		t.Errorf("tidak boleh ada task, dapat %d", n)
	}
}

func TestCreate_SchemaRejectsBadValues(t *testing.T) {
	e := setup(t)
	for name, mod := range map[string]func(*domain.Task){
		"status":      func(t *domain.Task) { t.Status = "doing" },
		"prioritas":   func(t *domain.Task) { t.Priority = "critical" },
		"judul > 200": func(t *domain.Task) { t.Title = strings.Repeat("x", 201) },
	} {
		task := &domain.Task{ProjectID: e.pid, Title: "T", Status: domain.StatusTodo, Priority: domain.PriorityLow, CreatedBy: e.owner}
		mod(task)
		if _, err := e.repo.Create(ctx, task, nil); err == nil {
			t.Errorf("%s harus ditolak database", name)
		}
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM tasks"); n != 0 {
		t.Errorf("tidak boleh ada task, dapat %d", n)
	}
}

func TestCreate_UnknownProjectMapsToNotFound(t *testing.T) {
	e := setup(t)
	task := &domain.Task{ProjectID: uuid.New(), Title: "T", Status: domain.StatusTodo, Priority: domain.PriorityLow, CreatedBy: e.owner}
	if _, err := e.repo.Create(ctx, task, nil); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Fatalf("dapat %v", err)
	}
}

// ---------- Update ----------

func TestUpdate_PersistsAndRecordsActivities(t *testing.T) {
	e := setup(t)
	v := e.create(t, "T", nil)
	time.Sleep(15 * time.Millisecond)

	due := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	got, err := e.repo.Update(ctx, v.ID, func(cur domain.Task) (domain.Task, []domain.Activity, error) {
		next := cur
		next.Status, next.AssigneeID, next.DueDate = domain.StatusDone, ptrUUID(e.member), &due
		// Mencoba mengubah field yang tidak boleh berubah: harus diabaikan repository.
		next.ProjectID, next.CreatedBy = uuid.New(), e.member
		return next, domain.DiffActivities(cur, next, e.member), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusDone || got.Assignee == nil || got.Assignee.ID != e.member || !got.DueDate.Equal(due) {
		t.Errorf("perubahan tidak tersimpan: %+v", got.Task)
	}
	if got.ProjectID != e.pid || got.CreatedBy != e.owner {
		t.Errorf("project/pembuat tidak boleh berubah: %+v", got.Task)
	}
	if !got.UpdatedAt.After(v.UpdatedAt) {
		t.Errorf("updated_at harus maju (PRD FR-05 #5): %v -> %v", v.UpdatedAt, got.UpdatedAt)
	}

	acts, err := e.repo.ListActivities(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		typ      domain.ActivityType
		old, new string
	}
	want := []row{
		{domain.ActivityCreated, "<nil>", "T"},
		{domain.ActivityStatusChanged, "todo", "done"},
		{domain.ActivityAssigneeChanged, "<nil>", e.member.String()},
		{domain.ActivityDueDateChanged, "<nil>", "2026-04-01T00:00:00Z"},
	}
	if len(acts) != len(want) {
		t.Fatalf("aktivitas = %+v", acts)
	}
	for i, w := range want {
		g := row{acts[i].Type, deref(acts[i].OldValue), deref(acts[i].NewValue)}
		if g != w {
			t.Errorf("[%d] = %+v, harapannya %+v (urutan harus terjaga)", i, g, w)
		}
	}
	if acts[1].Actor == nil || acts[1].Actor.ID != e.member || acts[1].Actor.Name != "Member" {
		t.Errorf("aktor = %+v", acts[1].Actor)
	}
}

func TestUpdate_FnErrorRollsBack(t *testing.T) {
	e := setup(t)
	v := e.create(t, "T", nil)
	boom := errors.New("ditolak")

	_, err := e.repo.Update(ctx, v.ID, func(cur domain.Task) (domain.Task, []domain.Activity, error) {
		return domain.Task{}, nil, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("dapat %v", err)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ? AND title = 'T' AND status = 'todo'", v.ID); n != 1 {
		t.Error("task tidak boleh berubah")
	}
}

func TestUpdate_AssigneeNotMemberRollsBack(t *testing.T) {
	e := setup(t)
	v := e.create(t, "T", nil)

	_, err := e.repo.Update(ctx, v.ID, func(cur domain.Task) (domain.Task, []domain.Activity, error) {
		next := cur
		next.Status, next.AssigneeID = domain.StatusDone, ptrUUID(e.plain)
		return next, domain.DiffActivities(cur, next, e.owner), nil
	})
	if !errors.Is(err, domain.ErrAssigneeNotMember) {
		t.Fatalf("dapat %v", err)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ? AND status = 'todo' AND assignee_id IS NULL", v.ID); n != 1 {
		t.Error("task tidak boleh berubah")
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM task_activities WHERE task_id = ?", v.ID); n != 1 {
		t.Errorf("hanya aktivitas created yang boleh ada, dapat %d", n)
	}
}

// Assignee yang TIDAK berubah tidak dicek ulang: task lama yang assignee-nya (secara
// teori) bukan anggota tetap bisa diubah statusnya.
func TestUpdate_UnchangedAssigneeNotRechecked(t *testing.T) {
	e := setup(t)
	v := e.create(t, "T", func(task *domain.Task) { task.AssigneeID = ptrUUID(e.member) })
	exec(t, e.db, "DELETE FROM project_members WHERE project_id = ? AND user_id = ?", e.pid, e.member)

	_, err := e.repo.Update(ctx, v.ID, func(cur domain.Task) (domain.Task, []domain.Activity, error) {
		next := cur
		next.Status = domain.StatusDone
		return next, nil, nil
	})
	if err != nil {
		t.Fatalf("dapat %v", err)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	e := setup(t)
	called := false
	_, err := e.repo.Update(ctx, uuid.New(), func(cur domain.Task) (domain.Task, []domain.Activity, error) {
		called = true
		return cur, nil, nil
	})
	if !errors.Is(err, domain.ErrTaskNotFound) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

// Penugasan mengunci keanggotaan project assignee (FOR SHARE). Bila penghapusan anggota
// (memegang kunci baris tersebut) sedang berjalan, Update menunggu lalu ditolak.
func TestUpdate_AssignWaitsForConcurrentMemberRemoval(t *testing.T) {
	e := setup(t)
	v := e.create(t, "T", nil)

	var schema string
	if err := e.db.Raw("SELECT current_schema()").Row().Scan(&schema); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := e.db.DB()
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
	if _, err := tx.ExecContext(ctx, "DELETE FROM project_members WHERE project_id = $1 AND user_id = $2", e.pid, e.member); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- e.db.Connection(func(c *gorm.DB) error {
			if err := c.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
				return err
			}
			_, err := repository.NewTaskGormRepository(c).Update(ctx, v.ID, func(cur domain.Task) (domain.Task, []domain.Activity, error) {
				next := cur
				next.AssigneeID = ptrUUID(e.member)
				return next, nil, nil
			})
			return err
		})
	}()

	select {
	case err := <-done:
		t.Fatalf("Update tidak menunggu kunci, selesai dengan %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrAssigneeNotMember) {
			t.Fatalf("harapannya ErrAssigneeNotMember, dapat %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Update macet")
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM tasks WHERE assignee_id IS NOT NULL"); n != 0 {
		t.Error("tidak boleh ada assignee yang bukan anggota")
	}
}

// ---------- List ----------

func TestList_FiltersPaginationAndOrder(t *testing.T) {
	e := setup(t)
	mk := func(title string, s domain.Status, p domain.Priority, a *uuid.UUID) uuid.UUID {
		v := e.create(t, title, func(task *domain.Task) { task.Status, task.Priority, task.AssigneeID = s, p, a })
		time.Sleep(10 * time.Millisecond)
		return v.ID
	}
	t1 := mk("Login page", domain.StatusTodo, domain.PriorityHigh, ptrUUID(e.member))
	t2 := mk("Diskon 50% promo", domain.StatusInProgress, domain.PriorityLow, nil)
	t3 := mk("login_api", domain.StatusDone, domain.PriorityHigh, ptrUUID(e.owner))
	t4 := mk("Deploy", domain.StatusTodo, domain.PriorityUrgent, nil)
	other := e.newProject(t, "Lain")
	exec(t, e.db, "INSERT INTO tasks (project_id, title, created_by) VALUES (?, 'Login lain', ?)", other, e.owner)

	ids := func(f domain.TaskFilter) ([]uuid.UUID, int64) {
		if f.Limit == 0 {
			f.Limit = 20
		}
		list, total, err := e.repo.List(ctx, e.pid, f)
		if err != nil {
			t.Fatal(err)
		}
		out := []uuid.UUID{}
		for _, v := range list {
			out = append(out, v.ID)
		}
		return out, total
	}
	eq := func(name string, got []uuid.UUID, total int64, want []uuid.UUID, wantTotal int64) {
		t.Helper()
		if total != wantTotal || len(got) != len(want) {
			t.Errorf("%s: total=%d len=%d, harapannya total=%d len=%d", name, total, len(got), wantTotal, len(want))
			return
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: urutan[%d] salah", name, i)
			}
		}
	}

	g, n := ids(domain.TaskFilter{})
	eq("semua (terbaru dulu, project lain tidak ikut)", g, n, []uuid.UUID{t4, t3, t2, t1}, 4)
	g, n = ids(domain.TaskFilter{Statuses: []domain.Status{domain.StatusTodo, domain.StatusDone}})
	eq("status todo+done", g, n, []uuid.UUID{t4, t3, t1}, 3)
	g, n = ids(domain.TaskFilter{Statuses: []domain.Status{domain.StatusTodo}, Priorities: []domain.Priority{domain.PriorityHigh}})
	eq("filter digabung", g, n, []uuid.UUID{t1}, 1)
	g, n = ids(domain.TaskFilter{AssigneeID: ptrUUID(e.member)})
	eq("assignee", g, n, []uuid.UUID{t1}, 1)
	g, n = ids(domain.TaskFilter{Unassigned: true})
	eq("tanpa assignee", g, n, []uuid.UUID{t4, t2}, 2)
	g, n = ids(domain.TaskFilter{Query: "LOGIN"})
	eq("cari tanpa peka huruf", g, n, []uuid.UUID{t3, t1}, 2)
	g, n = ids(domain.TaskFilter{Query: "50%"})
	eq("cari '%' literal", g, n, []uuid.UUID{t2}, 1)
	// Tanpa escape, '_' cocok dengan karakter apa pun: "n_p" akan cocok dengan "Login page".
	g, n = ids(domain.TaskFilter{Query: "n_p"})
	eq("cari '_' literal", g, n, []uuid.UUID{}, 0)
	g, n = ids(domain.TaskFilter{Query: "login_"})
	eq("cari 'login_' literal", g, n, []uuid.UUID{t3}, 1)
	g, n = ids(domain.TaskFilter{Limit: 2, Offset: 2})
	eq("halaman 2", g, n, []uuid.UUID{t2, t1}, 4)
	g, n = ids(domain.TaskFilter{Limit: 2, Offset: 10})
	eq("halaman kosong tetap memberi total", g, n, []uuid.UUID{}, 4)

	list, _, _ := e.repo.List(ctx, e.pid, domain.TaskFilter{AssigneeID: ptrUUID(e.member), Limit: 20})
	if len(list) != 1 || list[0].Assignee == nil || list[0].Assignee.Name != "Member" {
		t.Errorf("nama assignee harus ikut dimuat: %+v", list)
	}
}

// ---------- Delete ----------

func TestDelete_CascadesActivities(t *testing.T) {
	e := setup(t)
	gone, kept := e.create(t, "Hapus", nil), e.create(t, "Simpan", nil)

	if err := e.repo.Delete(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM task_activities WHERE task_id = ?", gone.ID); n != 0 {
		t.Error("aktivitas harus ikut terhapus")
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM tasks WHERE internal_id = ?", kept.ID); n != 1 {
		t.Error("task lain harus tetap")
	}
	if err := e.repo.Delete(ctx, gone.ID); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("hapus kedua kali: dapat %v", err)
	}
}

// ---------- context ----------

func TestCancelledContext(t *testing.T) {
	e := setup(t)
	v := e.create(t, "T", nil)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.repo.FindProjectAccess(cctx, e.pid, e.owner); err == nil || errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("FindProjectAccess: %v", err)
	}
	if _, _, err := e.repo.FindTaskAccess(cctx, v.ID, e.owner); err == nil || errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("FindTaskAccess: %v", err)
	}
	task := &domain.Task{ProjectID: e.pid, Title: "X", Status: domain.StatusTodo, Priority: domain.PriorityLow, CreatedBy: e.owner}
	if _, err := e.repo.Create(cctx, task, nil); err == nil {
		t.Error("Create harus error")
	}
	if _, err := e.repo.Update(cctx, v.ID, func(c domain.Task) (domain.Task, []domain.Activity, error) { return c, nil, nil }); err == nil || errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("Update: %v", err)
	}
	if err := e.repo.Delete(cctx, v.ID); err == nil || errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("Delete: %v", err)
	}
	if _, _, err := e.repo.List(cctx, e.pid, domain.TaskFilter{Limit: 10}); err == nil {
		t.Error("List harus error")
	}
	if _, err := e.repo.ListActivities(cctx, v.ID); err == nil {
		t.Error("ListActivities harus error")
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
