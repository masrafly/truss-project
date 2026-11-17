package domain_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

var (
	wsRoles = []domain.WorkspaceRole{"", domain.WorkspaceOwner, domain.WorkspaceAdmin, domain.WorkspaceMember, "superuser"}
	pRoles  = []domain.ProjectRole{"", domain.ProjectOwner, domain.ProjectAdmin, domain.ProjectMember}
)

// Hubungan aktor dengan task.
type relation string

const (
	relNone     relation = "lain"
	relCreator  relation = "pembuat"
	relAssignee relation = "assignee"
)

func taskFor(actor uuid.UUID, rel relation) domain.Task {
	t := domain.Task{ID: uuid.New(), CreatedBy: uuid.New()}
	switch rel {
	case relCreator:
		t.CreatedBy = actor
	case relAssignee:
		a := actor
		t.AssigneeID = &a
	}
	return t
}

// Matriks: (peran ws, peran project) x relasi -> "VCED" (View, Create, Edit, Delete).
func TestTaskAccessMatrix(t *testing.T) {
	type key struct {
		ws  domain.WorkspaceRole
		p   domain.ProjectRole
		rel relation
	}
	expect := func(ws domain.WorkspaceRole, p domain.ProjectRole, rel relation) string {
		member := ws == domain.WorkspaceOwner || ws == domain.WorkspaceAdmin || ws == domain.WorkspaceMember
		manager := ws == domain.WorkspaceOwner || ws == domain.WorkspaceAdmin ||
			p == domain.ProjectOwner || p == domain.ProjectAdmin
		view := member && (p != "" || ws == domain.WorkspaceOwner || ws == domain.WorkspaceAdmin)
		if !view {
			return "----"
		}
		edit := manager || rel == relCreator || rel == relAssignee
		del := manager || rel == relCreator
		return "VC" + flag(edit, "E") + flag(del, "D")
	}
	// Beberapa titik yang ditulis eksplisit agar matriks di atas tidak sekadar tautologi.
	explicit := map[key]string{
		{domain.WorkspaceMember, domain.ProjectMember, relNone}:     "VC--",
		{domain.WorkspaceMember, domain.ProjectMember, relAssignee}: "VCE-",
		{domain.WorkspaceMember, domain.ProjectMember, relCreator}:  "VCED",
		{domain.WorkspaceMember, domain.ProjectAdmin, relNone}:      "VCED",
		{domain.WorkspaceAdmin, "", relNone}:                        "VCED",
		{domain.WorkspaceMember, "", relCreator}:                    "----",
		{"", domain.ProjectOwner, relCreator}:                       "----",
		{"superuser", domain.ProjectOwner, relNone}:                 "----",
	}

	actor := uuid.New()
	for _, ws := range wsRoles {
		for _, p := range pRoles {
			for _, rel := range []relation{relNone, relCreator, relAssignee} {
				a := domain.ProjectAccess{WorkspaceRole: ws, ProjectRole: p}
				task := taskFor(actor, rel)
				got := flag(a.CanView(), "V") + flag(a.CanCreateTask(), "C") +
					flag(a.CanEditTask(task, actor), "E") + flag(a.CanDeleteTask(task, actor), "D")
				want := expect(ws, p, rel)
				if e, ok := explicit[key{ws, p, rel}]; ok && e != want {
					t.Fatalf("tabel eksplisit dan aturan tidak sepakat untuk ws=%q p=%q %s", ws, p, rel)
				}
				if got != want {
					t.Errorf("ws=%q p=%q %s: dapat %s, harapannya %s", ws, p, rel, got, want)
				}
			}
		}
	}
}

func TestTaskAccess_Monotonic(t *testing.T) {
	actor := uuid.New()
	for _, ws := range wsRoles {
		for _, p := range pRoles {
			for _, rel := range []relation{relNone, relCreator, relAssignee} {
				a := domain.ProjectAccess{WorkspaceRole: ws, ProjectRole: p}
				task := taskFor(actor, rel)
				name := fmt.Sprintf("ws=%q p=%q %s", ws, p, rel)
				if a.CanDeleteTask(task, actor) && !a.CanEditTask(task, actor) {
					t.Errorf("%s: boleh hapus tapi tidak boleh ubah", name)
				}
				if a.CanEditTask(task, actor) && !a.CanView() {
					t.Errorf("%s: boleh ubah tapi tidak boleh lihat", name)
				}
			}
		}
	}
}

func TestCheckWritable(t *testing.T) {
	if err := (domain.ProjectAccess{Archived: true}).CheckWritable(); err != domain.ErrProjectArchived {
		t.Errorf("archived: dapat %v", err)
	}
	if err := (domain.ProjectAccess{}).CheckWritable(); err != nil {
		t.Errorf("aktif: dapat %v", err)
	}
}

func TestParseStatusAndPriority(t *testing.T) {
	for _, s := range []string{"todo", "in_progress", "done"} {
		if _, ok := domain.ParseStatus(s); !ok {
			t.Errorf("status %q harus valid", s)
		}
	}
	for _, s := range []string{"", "Todo", "doing", "archived"} {
		if _, ok := domain.ParseStatus(s); ok {
			t.Errorf("status %q harus ditolak", s)
		}
	}
	for _, s := range []string{"low", "medium", "high", "urgent"} {
		if _, ok := domain.ParsePriority(s); !ok {
			t.Errorf("prioritas %q harus valid", s)
		}
	}
	for _, s := range []string{"", "High", "critical"} {
		if _, ok := domain.ParsePriority(s); ok {
			t.Errorf("prioritas %q harus ditolak", s)
		}
	}
}

// ---------- aktivitas ----------

func TestDiffActivities(t *testing.T) {
	actor := uuid.New()
	a1, a2 := uuid.New(), uuid.New()
	d1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	d1SameInstant := d1.UTC()
	d2 := d1.Add(24 * time.Hour)

	base := domain.Task{ID: uuid.New(), Title: "T", Status: domain.StatusTodo, Priority: domain.PriorityMedium, AssigneeID: &a1, DueDate: &d1}

	t.Run("tanpa perubahan penting", func(t *testing.T) {
		after := base
		after.Title = "Judul baru"
		after.DueDate = &d1SameInstant // instan sama, zona berbeda
		a1copy := a1
		after.AssigneeID = &a1copy // pointer berbeda, nilai sama
		if got := domain.DiffActivities(base, after, actor); len(got) != 0 {
			t.Errorf("tidak boleh ada aktivitas, dapat %+v", got)
		}
	})

	t.Run("semua berubah", func(t *testing.T) {
		after := base
		after.Status = domain.StatusDone
		after.Priority = domain.PriorityUrgent
		after.AssigneeID = &a2
		after.DueDate = &d2
		got := domain.DiffActivities(base, after, actor)
		want := []struct {
			typ      domain.ActivityType
			old, new string
		}{
			{domain.ActivityStatusChanged, "todo", "done"},
			{domain.ActivityAssigneeChanged, a1.String(), a2.String()},
			{domain.ActivityPriorityChanged, "medium", "urgent"},
			{domain.ActivityDueDateChanged, "2026-03-01T03:00:00Z", "2026-03-02T03:00:00Z"},
		}
		if len(got) != len(want) {
			t.Fatalf("dapat %d aktivitas: %+v", len(got), got)
		}
		for i, w := range want {
			g := got[i]
			if g.Type != w.typ || deref(g.OldValue) != w.old || deref(g.NewValue) != w.new {
				t.Errorf("[%d] = %s %s->%s, harapannya %s %s->%s", i, g.Type, deref(g.OldValue), deref(g.NewValue), w.typ, w.old, w.new)
			}
			if g.ActorID != actor || g.TaskID != base.ID {
				t.Errorf("[%d] aktor/task salah", i)
			}
		}
	})

	t.Run("mengosongkan dan mengisi", func(t *testing.T) {
		after := base
		after.AssigneeID = nil
		after.DueDate = nil
		got := domain.DiffActivities(base, after, actor)
		if len(got) != 2 || got[0].NewValue != nil || got[1].NewValue != nil {
			t.Fatalf("pengosongan harus tercatat dengan nilai baru nil: %+v", got)
		}
		back := domain.DiffActivities(after, base, actor)
		if len(back) != 2 || back[0].OldValue != nil || deref(back[0].NewValue) != a1.String() {
			t.Fatalf("pengisian harus tercatat dengan nilai lama nil: %+v", back)
		}
	})
}

func TestCreatedActivity(t *testing.T) {
	actor := uuid.New()
	task := domain.Task{ID: uuid.New(), Title: "Siapkan rilis"}
	a := domain.CreatedActivity(task, actor)
	if a.Type != domain.ActivityCreated || a.OldValue != nil || deref(a.NewValue) != "Siapkan rilis" || a.ActorID != actor || a.TaskID != task.ID {
		t.Errorf("hasil = %+v", a)
	}
}

func flag(b bool, s string) string {
	if b {
		return s
	}
	return "-"
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
