package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

var ctx = context.Background()

// ---------- fake ----------

type fakeRepo struct {
	access   map[uuid.UUID]domain.ProjectAccess // userID -> akses ke project tunggal
	project  uuid.UUID
	archived bool
	members  map[uuid.UUID]bool // anggota project (calon assignee yang sah)
	tasks    map[uuid.UUID]domain.Task
	acts     []domain.Activity

	accessErr, createErr, updateErr, deleteErr, listErr, actsErr error
	actsLimit, actsOffset                                        int
	// mutateBeforeLock, bila diisi, mengubah task "di database" tepat sebelum fn dipanggil
	// (mensimulasikan perubahan oleh pengguna lain di antara baca dan kunci).
	mutateBeforeLock func(*domain.Task)

	createCalls, updateCalls, deleteCalls, listCalls, fnCalls int
	lastFilter                                                domain.TaskFilter
	summaryErr                                                error
	lastAsOf                                                  time.Time
	summaryCalls                                              int
}

func (r *fakeRepo) accessFor(uid uuid.UUID) *domain.ProjectAccess {
	a := r.access[uid]
	a.ProjectID = r.project
	a.Archived = r.archived
	return &a
}

func (r *fakeRepo) FindProjectAccess(_ context.Context, pid, uid uuid.UUID) (*domain.ProjectAccess, error) {
	if r.accessErr != nil {
		return nil, r.accessErr
	}
	if pid != r.project {
		return nil, domain.ErrProjectNotFound
	}
	return r.accessFor(uid), nil
}

func (r *fakeRepo) FindTaskAccess(_ context.Context, tid, uid uuid.UUID) (*domain.TaskView, *domain.ProjectAccess, error) {
	if r.accessErr != nil {
		return nil, nil, r.accessErr
	}
	t, ok := r.tasks[tid]
	if !ok {
		return nil, nil, domain.ErrTaskNotFound
	}
	return &domain.TaskView{Task: t}, r.accessFor(uid), nil
}

func (r *fakeRepo) checkAssignee(a *uuid.UUID) error {
	if a != nil && !r.members[*a] {
		return domain.ErrAssigneeNotMember
	}
	return nil
}

func (r *fakeRepo) Create(_ context.Context, t *domain.Task, acts []domain.Activity) (*domain.TaskView, error) {
	r.createCalls++
	if r.createErr != nil {
		return nil, r.createErr
	}
	if err := r.checkAssignee(t.AssigneeID); err != nil {
		return nil, err
	}
	t.ID = uuid.New()
	t.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	t.UpdatedAt = t.CreatedAt
	r.tasks[t.ID] = *t
	for _, a := range acts {
		a.TaskID = t.ID
		r.acts = append(r.acts, a)
	}
	return &domain.TaskView{Task: *t}, nil
}

func (r *fakeRepo) Update(_ context.Context, tid uuid.UUID, fn usecase.UpdateFunc) (*domain.TaskView, error) {
	r.updateCalls++
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	cur, ok := r.tasks[tid]
	if !ok {
		return nil, domain.ErrTaskNotFound
	}
	if r.mutateBeforeLock != nil {
		r.mutateBeforeLock(&cur)
		r.tasks[tid] = cur
	}
	r.fnCalls++
	next, acts, err := fn(cur)
	if err != nil {
		return nil, err
	}
	if !sameUUID(cur.AssigneeID, next.AssigneeID) {
		if err := r.checkAssignee(next.AssigneeID); err != nil {
			return nil, err
		}
	}
	next.UpdatedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	r.tasks[tid] = next
	r.acts = append(r.acts, acts...)
	return &domain.TaskView{Task: next}, nil
}

func (r *fakeRepo) Delete(_ context.Context, tid uuid.UUID) error {
	r.deleteCalls++
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.tasks, tid)
	return nil
}

func (r *fakeRepo) List(_ context.Context, pid uuid.UUID, f domain.TaskFilter) ([]domain.TaskView, int64, error) {
	r.listCalls++
	r.lastFilter = f
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return nil, 42, nil
}

func (r *fakeRepo) ListActivities(
	_ context.Context,
	tid uuid.UUID,
	limit, offset int,
) ([]domain.Activity, int64, error) {
	r.actsLimit, r.actsOffset = limit, offset
	if r.actsErr != nil {
		return nil, 0, r.actsErr
	}
	var out []domain.Activity
	for _, a := range r.acts {
		if a.TaskID == tid {
			out = append(out, a)
		}
	}
	return out, int64(len(out)), nil
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ---------- fixture ----------

// Project dengan: wsOwner (pengelola ws, bukan anggota project), pAdmin (pengelola project),
// creator & assignee & bystander (Member project), plain (member ws saja), outsider.
// Satu task: dibuat creator, ditugaskan ke assignee.
type fixture struct {
	repo                                                           *fakeRepo
	uc                                                             *usecase.TaskUseCase
	pid, tid                                                       uuid.UUID
	wsOwner, pAdmin, creator, assignee, bystander, plain, outsider uuid.UUID
}

func newFixture() *fixture {
	f := &fixture{
		pid: uuid.New(), tid: uuid.New(),
		wsOwner: uuid.New(), pAdmin: uuid.New(), creator: uuid.New(), assignee: uuid.New(),
		bystander: uuid.New(), plain: uuid.New(), outsider: uuid.New(),
	}
	m := func(p domain.ProjectRole) domain.ProjectAccess {
		return domain.ProjectAccess{WorkspaceRole: domain.WorkspaceMember, ProjectRole: p}
	}
	f.repo = &fakeRepo{
		project: f.pid,
		access: map[uuid.UUID]domain.ProjectAccess{
			f.wsOwner:   {WorkspaceRole: domain.WorkspaceOwner},
			f.pAdmin:    m(domain.ProjectAdmin),
			f.creator:   m(domain.ProjectMember),
			f.assignee:  m(domain.ProjectMember),
			f.bystander: m(domain.ProjectMember),
			f.plain:     {WorkspaceRole: domain.WorkspaceMember},
		},
		members: map[uuid.UUID]bool{f.pAdmin: true, f.creator: true, f.assignee: true, f.bystander: true},
		tasks:   map[uuid.UUID]domain.Task{},
	}
	a := f.assignee
	f.repo.tasks[f.tid] = domain.Task{
		ID: f.tid, ProjectID: f.pid, Title: "Awal", Status: domain.StatusTodo,
		Priority: domain.PriorityMedium, AssigneeID: &a, CreatedBy: f.creator,
	}
	f.uc = usecase.NewTaskUseCase(f.repo, nil)
	return f
}

func strp(s string) *string { return &s }

func set(s string) usecase.Optional[string] { return usecase.Optional[string]{Set: true, Value: &s} }
func setNull() usecase.Optional[string]     { return usecase.Optional[string]{Set: true} }
func activityTypes(acts []domain.Activity) []domain.ActivityType {
	out := []domain.ActivityType{}
	for _, a := range acts {
		out = append(out, a.Type)
	}
	return out
}

// ---------- Create ----------

func TestCreate_DefaultsAndCreatedActivity(t *testing.T) {
	f := newFixture()

	got, err := f.uc.Create(ctx, f.bystander.String(), f.pid.String(), usecase.CreateInput{Title: "  Tulis test  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Title != "Tulis test" || got.Status != domain.StatusTodo || got.Priority != domain.PriorityMedium {
		t.Errorf("default salah: %+v", got.Task)
	}
	if got.CreatedBy != f.bystander || got.ProjectID != f.pid {
		t.Errorf("pembuat/project salah: %+v", got.Task)
	}
	if len(f.repo.acts) != 1 || f.repo.acts[0].Type != domain.ActivityCreated || f.repo.acts[0].ActorID != f.bystander ||
		f.repo.acts[0].TaskID != got.ID {
		t.Errorf("aktivitas created harus tercatat: %+v", f.repo.acts)
	}
}

func TestCreate_AllFields(t *testing.T) {
	f := newFixture()
	got, err := f.uc.Create(ctx, f.pAdmin.String(), f.pid.String(), usecase.CreateInput{
		Title: "T", Description: strp(" d "), Status: "in_progress", Priority: "urgent",
		AssigneeID: strp(f.assignee.String()), DueDate: strp("2026-03-01T17:00:00+07:00"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if *got.Description != "d" || got.Status != domain.StatusInProgress || got.Priority != domain.PriorityUrgent {
		t.Errorf("field salah: %+v", got.Task)
	}
	if got.AssigneeID == nil || *got.AssigneeID != f.assignee {
		t.Errorf("assignee = %v", got.AssigneeID)
	}
	if got.DueDate == nil || !got.DueDate.Equal(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)) || got.DueDate.Location() != time.UTC {
		t.Errorf("due date harus disimpan dalam UTC: %v", got.DueDate)
	}
}

func TestCreate_Access(t *testing.T) {
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{"owner ws": f.wsOwner, "member project": f.bystander} {
		if _, err := f.uc.Create(ctx, uid.String(), f.pid.String(), usecase.CreateInput{Title: "X"}); err != nil {
			t.Errorf("%s harus boleh membuat: %v", name, err)
		}
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.plain, "orang luar": f.outsider} {
		if _, err := f.uc.Create(ctx, uid.String(), f.pid.String(), usecase.CreateInput{Title: "X"}); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if _, err := f.uc.Create(ctx, f.pAdmin.String(), uuid.NewString(), usecase.CreateInput{Title: "X"}); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("project tak ada: dapat %v", err)
	}
	if f.repo.createCalls != 2 {
		t.Errorf("hanya yang berhak yang menyimpan, createCalls=%d", f.repo.createCalls)
	}
}

func TestCreate_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		in   usecase.CreateInput
	}{
		{"judul kosong", usecase.CreateInput{Title: "  "}},
		{"judul 201", usecase.CreateInput{Title: strings.Repeat("x", 201)}},
		{"deskripsi terlalu panjang", usecase.CreateInput{Title: "T", Description: strp(strings.Repeat("d", 10001))}},
		{"status", usecase.CreateInput{Title: "T", Status: "doing"}},
		{"prioritas", usecase.CreateInput{Title: "T", Priority: "critical"}},
		{"assignee bukan uuid", usecase.CreateInput{Title: "T", AssigneeID: strp("budi")}},
		{"due date bukan RFC3339", usecase.CreateInput{Title: "T", DueDate: strp("2026-03-01")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			if _, err := f.uc.Create(ctx, f.pAdmin.String(), f.pid.String(), tc.in); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("harapannya ErrInvalidInput, dapat %v", err)
			}
			if f.repo.createCalls != 0 {
				t.Error("tidak boleh ada penyimpanan")
			}
		})
	}
}

func TestCreate_BoundaryTitle(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Create(ctx, f.pAdmin.String(), f.pid.String(), usecase.CreateInput{Title: strings.Repeat("é", 200)}); err != nil {
		t.Fatalf("200 karakter (multibyte) harus diterima: %v", err)
	}
}

func TestCreate_AssigneeMustBeProjectMember(t *testing.T) {
	f := newFixture()
	_, err := f.uc.Create(ctx, f.pAdmin.String(), f.pid.String(), usecase.CreateInput{Title: "T", AssigneeID: strp(f.plain.String())})
	if !errors.Is(err, domain.ErrAssigneeNotMember) {
		t.Fatalf("harapannya ErrAssigneeNotMember, dapat %v", err)
	}
}

func TestCreate_ArchivedProjectIsReadOnly(t *testing.T) {
	f := newFixture()
	f.repo.archived = true
	if _, err := f.uc.Create(ctx, f.pAdmin.String(), f.pid.String(), usecase.CreateInput{Title: "T"}); !errors.Is(err, domain.ErrProjectArchived) {
		t.Fatalf("harapannya ErrProjectArchived, dapat %v", err)
	}
	if f.repo.createCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

func TestCreate_InvalidIDsAndErrors(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Create(ctx, "x", f.pid.String(), usecase.CreateInput{Title: "T"}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.uc.Create(ctx, f.pAdmin.String(), "x", usecase.CreateInput{Title: "T"}); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
	boom := errors.New("db mati")
	f.repo.createErr = boom
	if _, err := f.uc.Create(ctx, f.pAdmin.String(), f.pid.String(), usecase.CreateInput{Title: "T"}); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
	f.repo.accessErr = boom
	if _, err := f.uc.Create(ctx, f.pAdmin.String(), f.pid.String(), usecase.CreateInput{Title: "T"}); !errors.Is(err, boom) || errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
}

// ---------- List ----------

func TestList_FilterAndPagination(t *testing.T) {
	f := newFixture()
	res, err := f.uc.List(ctx, f.bystander.String(), f.pid.String(), usecase.ListInput{
		Statuses: []string{"todo", "done"}, Priorities: []string{"high"}, Assignee: "me",
		Query: "  login ", Page: 3, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	lf := f.repo.lastFilter
	if len(lf.Statuses) != 2 || lf.Statuses[1] != domain.StatusDone || len(lf.Priorities) != 1 {
		t.Errorf("filter status/prioritas salah: %+v", lf)
	}
	if lf.AssigneeID == nil || *lf.AssigneeID != f.bystander || lf.Unassigned {
		t.Errorf("assignee=me harus menjadi ID pemanggil: %+v", lf)
	}
	if lf.Query != "login" || lf.Limit != 10 || lf.Offset != 20 {
		t.Errorf("query/limit/offset salah: %+v", lf)
	}
	if res.Total != 42 || res.Page != 3 || res.Limit != 10 || res.Items == nil {
		t.Errorf("hasil salah: %+v", res)
	}
}

func TestList_DefaultsAndAssigneeVariants(t *testing.T) {
	f := newFixture()
	res, err := f.uc.List(ctx, f.wsOwner.String(), f.pid.String(), usecase.ListInput{Assignee: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Page != 1 || res.Limit != 20 || f.repo.lastFilter.Offset != 0 || !f.repo.lastFilter.Unassigned {
		t.Errorf("default/none salah: %+v %+v", res, f.repo.lastFilter)
	}
	if _, err := f.uc.List(ctx, f.wsOwner.String(), f.pid.String(), usecase.ListInput{Assignee: f.assignee.String()}); err != nil {
		t.Fatal(err)
	}
	if a := f.repo.lastFilter.AssigneeID; a == nil || *a != f.assignee {
		t.Errorf("assignee UUID salah: %v", a)
	}
}

func TestList_InvalidInput(t *testing.T) {
	tests := map[string]usecase.ListInput{
		"status":        {Statuses: []string{"todo", "doing"}},
		"prioritas":     {Priorities: []string{"critical"}},
		"assignee":      {Assignee: "budi"},
		"query panjang": {Query: strings.Repeat("q", 101)},
		"page negatif":  {Page: -1},
		"limit negatif": {Limit: -5},
		"limit di atas": {Limit: 101},
	}
	for name, in := range tests {
		f := newFixture()
		if _, err := f.uc.List(ctx, f.wsOwner.String(), f.pid.String(), in); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: harapannya ErrInvalidInput, dapat %v", name, err)
		}
		if f.repo.listCalls != 0 {
			t.Errorf("%s: repository tidak boleh dipanggil", name)
		}
	}
}

func TestList_AccessAndErrors(t *testing.T) {
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.plain, "orang luar": f.outsider} {
		if _, err := f.uc.List(ctx, uid.String(), f.pid.String(), usecase.ListInput{}); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if f.repo.listCalls != 0 {
		t.Error("daftar tidak boleh dibaca untuk yang tak berhak")
	}
	boom := errors.New("db mati")
	f.repo.listErr = boom
	if _, err := f.uc.List(ctx, f.wsOwner.String(), f.pid.String(), usecase.ListInput{}); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
}

// ---------- Get ----------

func TestGet_Access(t *testing.T) {
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{"owner ws": f.wsOwner, "bystander": f.bystander} {
		if _, err := f.uc.Get(ctx, uid.String(), f.tid.String()); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.plain, "orang luar": f.outsider} {
		if _, err := f.uc.Get(ctx, uid.String(), f.tid.String()); !errors.Is(err, domain.ErrTaskNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if _, err := f.uc.Get(ctx, f.wsOwner.String(), "x"); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("ID rusak: dapat %v", err)
	}
	if _, err := f.uc.Get(ctx, f.wsOwner.String(), uuid.NewString()); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("tak ada: dapat %v", err)
	}
}

// ---------- Update ----------

func TestUpdate_PermissionMatrix(t *testing.T) {
	allowed := map[string]func(*fixture) uuid.UUID{
		"owner ws":      func(f *fixture) uuid.UUID { return f.wsOwner },
		"admin project": func(f *fixture) uuid.UUID { return f.pAdmin },
		"pembuat":       func(f *fixture) uuid.UUID { return f.creator },
		"assignee":      func(f *fixture) uuid.UUID { return f.assignee },
	}
	for name, pick := range allowed {
		f := newFixture()
		if _, err := f.uc.Update(ctx, pick(f).String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")}); err != nil {
			t.Errorf("%s harus boleh mengubah: %v", name, err)
		}
	}

	f := newFixture()
	if _, err := f.uc.Update(ctx, f.bystander.String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("anggota lain harus 403, dapat %v", err)
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.plain, "orang luar": f.outsider} {
		if _, err := f.uc.Update(ctx, uid.String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")}); !errors.Is(err, domain.ErrTaskNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if f.repo.updateCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

func TestUpdate_PartialAndActivities(t *testing.T) {
	f := newFixture()
	got, err := f.uc.Update(ctx, f.pAdmin.String(), f.tid.String(), usecase.UpdateInput{
		Status: strp("in_progress"), Priority: strp("high"), AssigneeID: set(f.bystander.String()),
		DueDate: set("2026-04-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Awal" {
		t.Errorf("judul tidak dikirim, harus tetap: %q", got.Title)
	}
	want := []domain.ActivityType{domain.ActivityStatusChanged, domain.ActivityAssigneeChanged, domain.ActivityPriorityChanged, domain.ActivityDueDateChanged}
	gotTypes := activityTypes(f.repo.acts)
	if len(gotTypes) != len(want) {
		t.Fatalf("aktivitas = %v", gotTypes)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Errorf("aktivitas[%d] = %s, harapannya %s", i, gotTypes[i], want[i])
		}
		if f.repo.acts[i].ActorID != f.pAdmin {
			t.Errorf("aktor aktivitas harus pemanggil")
		}
	}
}

func TestUpdate_TitleOnlyRecordsNoActivity(t *testing.T) {
	f := newFixture()
	got, err := f.uc.Update(ctx, f.creator.String(), f.tid.String(), usecase.UpdateInput{Title: strp(" Baru ")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Baru" {
		t.Errorf("judul = %q", got.Title)
	}
	if len(f.repo.acts) != 0 {
		t.Errorf("perubahan judul tidak dicatat sebagai aktivitas: %v", activityTypes(f.repo.acts))
	}
}

func TestUpdate_NullClearsOptionalFields(t *testing.T) {
	f := newFixture()
	task := f.repo.tasks[f.tid]
	d, due := "isi", time.Now().UTC()
	task.Description, task.DueDate = &d, &due
	f.repo.tasks[f.tid] = task

	got, err := f.uc.Update(ctx, f.creator.String(), f.tid.String(), usecase.UpdateInput{
		Description: setNull(), AssigneeID: setNull(), DueDate: setNull(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != nil || got.AssigneeID != nil || got.DueDate != nil {
		t.Errorf("semua harus kosong: %+v", got.Task)
	}
	if len(f.repo.acts) != 2 {
		t.Errorf("assignee dan due date dikosongkan -> 2 aktivitas, dapat %v", activityTypes(f.repo.acts))
	}
}

func TestUpdate_InvalidInput(t *testing.T) {
	tests := map[string]usecase.UpdateInput{
		"kosong":          {},
		"judul kosong":    {Title: strp(" ")},
		"status":          {Status: strp("doing")},
		"prioritas":       {Priority: strp("x")},
		"assignee rusak":  {AssigneeID: set("budi")},
		"due date rusak":  {DueDate: set("besok")},
		"deskripsi besar": {Description: set(strings.Repeat("d", 10001))},
	}
	for name, in := range tests {
		f := newFixture()
		if _, err := f.uc.Update(ctx, f.pAdmin.String(), f.tid.String(), in); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: harapannya ErrInvalidInput, dapat %v", name, err)
		}
		if f.repo.updateCalls != 0 {
			t.Errorf("%s: tidak boleh ada penyimpanan", name)
		}
	}
}

func TestUpdate_AssigneeMustBeProjectMember(t *testing.T) {
	f := newFixture()
	_, err := f.uc.Update(ctx, f.pAdmin.String(), f.tid.String(), usecase.UpdateInput{AssigneeID: set(f.plain.String())})
	if !errors.Is(err, domain.ErrAssigneeNotMember) {
		t.Fatalf("harapannya ErrAssigneeNotMember, dapat %v", err)
	}
	if a := f.repo.tasks[f.tid].AssigneeID; a == nil || *a != f.assignee {
		t.Error("assignee tidak boleh berubah")
	}
}

func TestUpdate_ArchivedIsReadOnly(t *testing.T) {
	f := newFixture()
	f.repo.archived = true
	if _, err := f.uc.Update(ctx, f.pAdmin.String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")}); !errors.Is(err, domain.ErrProjectArchived) {
		t.Fatalf("dapat %v", err)
	}
	if f.repo.updateCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

// Assignee dialihkan orang lain di antara pembacaan dan penguncian: hak ubah assignee
// lama harus diperiksa ulang pada data terkunci.
func TestUpdate_RechecksPermissionOnLockedRow(t *testing.T) {
	f := newFixture()
	f.repo.mutateBeforeLock = func(t *domain.Task) {
		b := f.bystander
		t.AssigneeID = &b
	}

	_, err := f.uc.Update(ctx, f.assignee.String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")})

	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("harapannya ErrForbidden, dapat %v", err)
	}
	if f.repo.tasks[f.tid].Status != domain.StatusTodo {
		t.Error("status tidak boleh berubah")
	}
}

// Diff aktivitas dihitung dari data terkunci, bukan dari data yang dibaca lebih awal.
func TestUpdate_ActivitiesDiffAgainstLockedRow(t *testing.T) {
	f := newFixture()
	f.repo.mutateBeforeLock = func(t *domain.Task) { t.Status = domain.StatusDone }

	if _, err := f.uc.Update(ctx, f.pAdmin.String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")}); err != nil {
		t.Fatal(err)
	}
	if len(f.repo.acts) != 0 {
		t.Errorf("status sudah done saat dikunci -> tidak ada aktivitas, dapat %v", activityTypes(f.repo.acts))
	}
}

func TestUpdate_RepositoryErrorPassedThrough(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.updateErr = boom
	if _, err := f.uc.Update(ctx, f.pAdmin.String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")}); !errors.Is(err, boom) {
		t.Fatalf("dapat %v", err)
	}
}

// ---------- Delete ----------

func TestDelete_PermissionMatrix(t *testing.T) {
	allowed := map[string]func(*fixture) uuid.UUID{
		"owner ws":      func(f *fixture) uuid.UUID { return f.wsOwner },
		"admin project": func(f *fixture) uuid.UUID { return f.pAdmin },
		"pembuat":       func(f *fixture) uuid.UUID { return f.creator },
	}
	for name, pick := range allowed {
		f := newFixture()
		if err := f.uc.Delete(ctx, pick(f).String(), f.tid.String()); err != nil {
			t.Errorf("%s harus boleh menghapus: %v", name, err)
		}
	}
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{"assignee": f.assignee, "anggota lain": f.bystander} {
		if err := f.uc.Delete(ctx, uid.String(), f.tid.String()); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s harus 403, dapat %v", name, err)
		}
	}
	if err := f.uc.Delete(ctx, f.outsider.String(), f.tid.String()); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("orang luar harus 404, dapat %v", err)
	}
	f.repo.archived = true
	if err := f.uc.Delete(ctx, f.pAdmin.String(), f.tid.String()); !errors.Is(err, domain.ErrProjectArchived) {
		t.Errorf("archived harus 409, dapat %v", err)
	}
	if f.repo.deleteCalls != 0 {
		t.Error("tidak boleh ada penghapusan")
	}
}

// ---------- Activities ----------

func TestActivities(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Update(ctx, f.pAdmin.String(), f.tid.String(), usecase.UpdateInput{Status: strp("done")}); err != nil {
		t.Fatal(err)
	}
	res, err := f.uc.Activities(ctx, f.bystander.String(), f.tid.String(), 0, 0)
	if err != nil || len(res.Items) != 1 || res.Total != 1 || res.Items[0].Type != domain.ActivityStatusChanged {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := f.uc.Activities(ctx, f.outsider.String(), f.tid.String(), 0, 0); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("orang luar harus 404, dapat %v", err)
	}

	empty := newFixture()
	list, err := empty.uc.Activities(ctx, empty.pAdmin.String(), empty.tid.String(), 0, 0)
	if err != nil || list.Items == nil || len(list.Items) != 0 {
		t.Errorf("harapannya slice kosong non-nil, dapat %#v err=%v", list, err)
	}
}

func TestActivities_PaginationDefaultsAndValidation(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Activities(ctx, f.pAdmin.String(), f.tid.String(), 0, 0); err != nil ||
		f.repo.actsLimit != 50 || f.repo.actsOffset != 0 {
		t.Fatalf("default: err=%v limit=%d offset=%d", err, f.repo.actsLimit, f.repo.actsOffset)
	}
	if _, err := f.uc.Activities(ctx, f.pAdmin.String(), f.tid.String(), 3, 10); err != nil ||
		f.repo.actsLimit != 10 || f.repo.actsOffset != 20 {
		t.Fatalf("halaman 3: err=%v limit=%d offset=%d", err, f.repo.actsLimit, f.repo.actsOffset)
	}
	f.repo.actsLimit = -1
	for _, c := range [][2]int{{-1, 10}, {1, -1}, {1, 101}} {
		if _, err := f.uc.Activities(ctx, f.pAdmin.String(), f.tid.String(), c[0], c[1]); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("page=%d limit=%d: dapat %v", c[0], c[1], err)
		}
	}
	if f.repo.actsLimit != -1 {
		t.Error("repository tidak boleh dipanggil untuk pagination tidak valid")
	}
}

// Summary menghitung dari data fake memakai definisi domain.IsOverdue.
func (r *fakeRepo) Summary(_ context.Context, pid uuid.UUID, asOf time.Time) (domain.ProjectSummary, error) {
	r.summaryCalls++
	r.lastAsOf = asOf
	if r.summaryErr != nil {
		return domain.ProjectSummary{}, r.summaryErr
	}
	var s domain.ProjectSummary
	for _, t := range r.tasks {
		if t.ProjectID != pid {
			continue
		}
		s.Total++
		if t.Status == domain.StatusDone {
			s.Done++
		}
		if domain.IsOverdue(t, asOf) {
			s.Overdue++
		}
	}
	return s, nil
}
