package repository_test

// Integration test ringkasan project (PRD FR-09). Memakai env/setup dari
// task_gorm_integration_test.go. Di-skip otomatis jika TEST_DB_HOST tidak diisi.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

func TestSummary_CountsOnlyRequestedProject(t *testing.T) {
	e := setup(t)
	asOf := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	past, future := asOf.Add(-time.Hour), asOf.Add(time.Hour)

	mk := func(s domain.Status, p domain.Priority, due *time.Time, a *uuid.UUID) {
		e.create(t, "T", func(task *domain.Task) { task.Status, task.Priority, task.DueDate, task.AssigneeID = s, p, due, a })
	}
	mk(domain.StatusTodo, domain.PriorityHigh, &past, ptrUUID(e.member)) // terlambat
	mk(domain.StatusInProgress, domain.PriorityUrgent, &past, nil)       // terlambat
	mk(domain.StatusDone, domain.PriorityLow, &past, ptrUUID(e.owner))   // selesai: TIDAK terlambat
	mk(domain.StatusTodo, domain.PriorityMedium, &future, nil)           // belum jatuh tempo
	mk(domain.StatusTodo, domain.PriorityMedium, nil, nil)               // tanpa deadline
	mk(domain.StatusTodo, domain.PriorityHigh, &asOf, nil)               // tepat pada deadline: belum terlambat

	// Project lain di workspace yang sama: tidak boleh ikut terhitung (PRD FR-09 #1).
	other := e.newProject(t, "Lain")
	exec(t, e.db, "INSERT INTO tasks (project_id, title, created_by, due_date) VALUES (?, 'X', ?, ?)", other, e.owner, past)

	s, err := e.repo.Summary(ctx, e.pid, asOf)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.ProjectSummary{
		Total: 6, Todo: 4, InProgress: 1, Done: 1,
		Low: 1, Medium: 2, High: 2, Urgent: 1,
		Unassigned: 4, Overdue: 2,
	}
	if s != want {
		t.Errorf("ringkasan =\n %+v\nharapannya\n %+v", s, want)
	}
}

func TestSummary_EmptyProject(t *testing.T) {
	e := setup(t)
	s, err := e.repo.Summary(ctx, e.pid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s != (domain.ProjectSummary{}) {
		t.Errorf("project kosong harus semua nol, dapat %+v", s)
	}
	if s, _ := e.repo.Summary(ctx, uuid.New(), time.Now()); s.Total != 0 {
		t.Errorf("project tak ada: %+v", s)
	}
}

// PRD FR-09 #3: angka ringkasan harus konsisten dengan data task. Filter overdue
// pada daftar task dan hitungan overdue ringkasan memakai kondisi SQL yang sama.
func TestSummary_ConsistentWithListFilters(t *testing.T) {
	e := setup(t)
	asOf := time.Now().UTC()
	statuses := []domain.Status{domain.StatusTodo, domain.StatusInProgress, domain.StatusDone}
	for i := 0; i < 12; i++ {
		due := asOf.Add(time.Duration(i-6) * time.Hour) // separuh lewat, separuh belum
		var d *time.Time
		if i%4 != 0 {
			d = &due
		}
		s := statuses[i%3]
		e.create(t, "T", func(task *domain.Task) { task.Status, task.DueDate = s, d })
	}

	sum, err := e.repo.Summary(ctx, e.pid, asOf)
	if err != nil {
		t.Fatal(err)
	}
	totalOf := func(f domain.TaskFilter) int64 {
		f.Limit = 1
		_, n, err := e.repo.List(ctx, e.pid, f)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	checks := map[string][2]int64{
		"total":       {sum.Total, totalOf(domain.TaskFilter{})},
		"todo":        {sum.Todo, totalOf(domain.TaskFilter{Statuses: []domain.Status{domain.StatusTodo}})},
		"in_progress": {sum.InProgress, totalOf(domain.TaskFilter{Statuses: []domain.Status{domain.StatusInProgress}})},
		"done":        {sum.Done, totalOf(domain.TaskFilter{Statuses: []domain.Status{domain.StatusDone}})},
		"unassigned":  {sum.Unassigned, totalOf(domain.TaskFilter{Unassigned: true})},
		"overdue":     {sum.Overdue, totalOf(domain.TaskFilter{OverdueAsOf: &asOf})},
	}
	for name, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s: ringkasan %d, daftar %d", name, v[0], v[1])
		}
	}
	if sum.Overdue == 0 || sum.Overdue == sum.Total {
		t.Errorf("data uji harus mencampur terlambat dan tidak: overdue=%d total=%d", sum.Overdue, sum.Total)
	}

	// Setiap task pada daftar overdue memang terlambat menurut definisi domain.
	list, _, err := e.repo.List(ctx, e.pid, domain.TaskFilter{OverdueAsOf: &asOf, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list {
		if !domain.IsOverdue(v.Task, asOf) {
			t.Errorf("task %v ada di daftar overdue tetapi domain.IsOverdue = false", v.ID)
		}
	}
}

func TestSummary_CancelledContext(t *testing.T) {
	e := setup(t)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.repo.Summary(cctx, e.pid, time.Now()); err == nil {
		t.Error("harus error")
	}
}
