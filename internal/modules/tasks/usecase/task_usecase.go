// Package usecase berisi logika bisnis tasks: validasi, otorisasi (lihat
// domain/access.go), dan pencatatan aktivitas.
package usecase

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

const (
	maxTitleLength       = 200   // karakter, sesuai VARCHAR(200)
	maxDescriptionLength = 10000 // karakter
	maxQueryLength       = 100
	defaultLimit         = 20
	maxLimit             = 100
)

// Optional membedakan "tidak dikirim" (Set=false) dari "dikirim null" (Set=true, Value=nil).
type Optional[T any] struct {
	Set   bool
	Value *T
}

type CreateInput struct {
	Title       string
	Description *string
	Status      string  // kosong = todo
	Priority    string  // kosong = medium
	AssigneeID  *string // UUID user
	DueDate     *string // RFC3339
}

// UpdateInput: field yang tidak dikirim tidak diubah. Description, AssigneeID, dan
// DueDate dapat dikosongkan dengan null.
type UpdateInput struct {
	Title       *string
	Description Optional[string]
	Status      *string
	Priority    *string
	AssigneeID  Optional[string]
	DueDate     Optional[string]
}

// ListInput: Statuses/Priorities kosong = semua. Assignee: "" (semua), "me",
// "none" (tanpa assignee), atau UUID user. Page/Limit 0 = default.
type ListInput struct {
	Statuses   []string
	Priorities []string
	Assignee   string
	Query      string
	Overdue    bool // hanya task terlambat (definisi sama dengan ringkasan project)
	Page       int
	Limit      int
}

type ListResult struct {
	Items []domain.TaskView
	Total int64
	Page  int
	Limit int
}

type TaskUseCase struct {
	repo TaskRepository
	log  *slog.Logger
	now  func() time.Time
}

// WithClock mengganti sumber waktu (untuk test). Waktu dipakai sebagai acuan "terlambat".
func (u *TaskUseCase) WithClock(now func() time.Time) *TaskUseCase {
	u.now = now
	return u
}

// log boleh nil (mis. pada unit test); dalam kasus itu log dibuang.
func NewTaskUseCase(repo TaskRepository, log *slog.Logger) *TaskUseCase {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &TaskUseCase{repo: repo, log: log.With("module", "tasks"), now: time.Now}
}

// Create: dapat melihat project (404) -> validasi (400) -> izin (403) -> project
// tidak diarsipkan (409) -> assignee anggota project (400, dicek repository dalam transaksi).
func (u *TaskUseCase) Create(ctx context.Context, userID, projectID string, in CreateInput) (*domain.TaskView, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, domain.ErrProjectNotFound
	}
	access, err := u.repo.FindProjectAccess(ctx, pid, uid)
	if err != nil {
		return nil, err
	}
	if !access.CanView() {
		u.log.WarnContext(ctx, "akses project ditolak", "project_id", pid, "user_id", uid)
		return nil, domain.ErrProjectNotFound
	}

	t := domain.Task{ProjectID: pid, CreatedBy: uid, Status: domain.StatusTodo, Priority: domain.PriorityMedium}
	if t.Title, err = validateTitle(in.Title); err != nil {
		return nil, err
	}
	if t.Description, err = validateDescription(in.Description); err != nil {
		return nil, err
	}
	if in.Status != "" {
		s, ok := domain.ParseStatus(in.Status)
		if !ok {
			return nil, domain.ErrInvalidInput
		}
		t.Status = s
	}
	if in.Priority != "" {
		p, ok := domain.ParsePriority(in.Priority)
		if !ok {
			return nil, domain.ErrInvalidInput
		}
		t.Priority = p
	}
	if t.AssigneeID, err = parseOptionalUUID(in.AssigneeID); err != nil {
		return nil, err
	}
	if t.DueDate, err = parseDueDate(in.DueDate); err != nil {
		return nil, err
	}

	if !access.CanCreateTask() {
		return nil, domain.ErrForbidden
	}
	if err := access.CheckWritable(); err != nil {
		return nil, err
	}

	view, err := u.repo.Create(ctx, &t, []domain.Activity{domain.CreatedActivity(t, uid)})
	if err != nil {
		return nil, err
	}
	u.log.InfoContext(ctx, "task dibuat", "task_id", view.ID, "project_id", pid, "user_id", uid)
	return view, nil
}

// List: daftar task dengan filter dan pagination. Yang tidak dapat melihat project -> 404.
func (u *TaskUseCase) List(ctx context.Context, userID, projectID string, in ListInput) (*ListResult, error) {
	uid, pid, err := u.visibleProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	f, page, limit, err := buildFilter(in, uid)
	if err != nil {
		return nil, err
	}
	if in.Overdue {
		asOf := u.now().UTC()
		f.OverdueAsOf = &asOf
	}

	items, total, err := u.repo.List(ctx, pid, f)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.TaskView{}
	}
	return &ListResult{Items: items, Total: total, Page: page, Limit: limit}, nil
}

// Summary: ringkasan project (PRD FR-09). Hanya untuk yang dapat melihat project (FR-09 #4);
// project diarsipkan tetap dapat diringkas.
func (u *TaskUseCase) Summary(ctx context.Context, userID, projectID string) (*domain.ProjectSummary, error) {
	_, pid, err := u.visibleProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	asOf := u.now().UTC()
	s, err := u.repo.Summary(ctx, pid, asOf)
	if err != nil {
		return nil, err
	}
	s.AsOf = asOf
	return &s, nil
}

// visibleProject memastikan pemanggil boleh melihat project; jika tidak -> 404.
func (u *TaskUseCase) visibleProject(ctx context.Context, userID, projectID string) (uuid.UUID, uuid.UUID, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, domain.ErrUnauthenticated
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return uuid.Nil, uuid.Nil, domain.ErrProjectNotFound
	}
	access, err := u.repo.FindProjectAccess(ctx, pid, uid)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if !access.CanView() {
		u.log.WarnContext(ctx, "akses project ditolak", "project_id", pid, "user_id", uid)
		return uuid.Nil, uuid.Nil, domain.ErrProjectNotFound
	}
	return uid, pid, nil
}

// Get: detail task. Yang tidak dapat melihat project -> 404.
func (u *TaskUseCase) Get(ctx context.Context, userID, taskID string) (*domain.TaskView, error) {
	_, view, _, err := u.loadTask(ctx, userID, taskID)
	return view, err
}

// Update: dapat melihat (404) -> validasi (400) -> izin (403) -> tidak diarsipkan (409)
// -> assignee anggota project (400). Izin diperiksa ulang terhadap baris yang dikunci.
func (u *TaskUseCase) Update(ctx context.Context, userID, taskID string, in UpdateInput) (*domain.TaskView, error) {
	uid, view, access, err := u.loadTask(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}

	apply, err := u.buildPatch(in)
	if err != nil {
		return nil, err
	}

	if !access.CanEditTask(view.Task, uid) {
		u.logDenied(ctx, "perubahan task ditolak", view.Task, uid, access)
		return nil, domain.ErrForbidden
	}
	if err := access.CheckWritable(); err != nil {
		return nil, err
	}

	updated, err := u.repo.Update(ctx, view.ID, func(cur domain.Task) (domain.Task, []domain.Activity, error) {
		// Assignee/pembuat bisa berubah sejak dibaca: periksa ulang pada data terkunci.
		if !access.CanEditTask(cur, uid) {
			return domain.Task{}, nil, domain.ErrForbidden
		}
		next := apply(cur)
		return next, domain.DiffActivities(cur, next, uid), nil
	})
	if err != nil {
		return nil, err
	}
	u.log.InfoContext(ctx, "task diperbarui", "task_id", updated.ID, "user_id", uid)
	return updated, nil
}

// Delete: dapat melihat (404) -> izin (403) -> tidak diarsipkan (409).
func (u *TaskUseCase) Delete(ctx context.Context, userID, taskID string) error {
	uid, view, access, err := u.loadTask(ctx, userID, taskID)
	if err != nil {
		return err
	}
	if !access.CanDeleteTask(view.Task, uid) {
		u.logDenied(ctx, "penghapusan task ditolak", view.Task, uid, access)
		return domain.ErrForbidden
	}
	if err := access.CheckWritable(); err != nil {
		return err
	}
	if err := u.repo.Delete(ctx, view.ID); err != nil {
		return err
	}
	u.log.InfoContext(ctx, "task dihapus", "task_id", view.ID, "project_id", view.ProjectID, "user_id", uid)
	return nil
}

// Activities: riwayat task untuk siapa pun yang dapat melihat task.
func (u *TaskUseCase) Activities(ctx context.Context, userID, taskID string) ([]domain.Activity, error) {
	_, view, _, err := u.loadTask(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	list, err := u.repo.ListActivities(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.Activity{}
	}
	return list, nil
}

// ---------- helpers ----------

func (u *TaskUseCase) loadTask(
	ctx context.Context,
	userID, taskID string,
) (uuid.UUID, *domain.TaskView, *domain.ProjectAccess, error) {
	return loadVisibleTask(ctx, u.repo, u.log, userID, taskID)
}

// loadVisibleTask memuat task dan memastikan pemanggil boleh melihatnya; jika tidak -> 404.
// Dipakai bersama oleh TaskUseCase dan CommentUseCase.
func loadVisibleTask(
	ctx context.Context,
	finder TaskAccessFinder,
	log *slog.Logger,
	userID, taskID string,
) (uuid.UUID, *domain.TaskView, *domain.ProjectAccess, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, nil, nil, domain.ErrUnauthenticated
	}
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return uuid.Nil, nil, nil, domain.ErrTaskNotFound
	}
	view, access, err := finder.FindTaskAccess(ctx, tid, uid)
	if err != nil {
		return uuid.Nil, nil, nil, err
	}
	if !access.CanView() {
		log.WarnContext(ctx, "akses task ditolak", "task_id", tid, "user_id", uid)
		return uuid.Nil, nil, nil, domain.ErrTaskNotFound
	}
	return uid, view, access, nil
}

func (u *TaskUseCase) logDenied(ctx context.Context, msg string, t domain.Task, uid uuid.UUID, a *domain.ProjectAccess) {
	u.log.WarnContext(ctx, msg, "task_id", t.ID, "user_id", uid,
		"workspace_role", a.WorkspaceRole, "project_role", a.ProjectRole)
}

// buildPatch memvalidasi input dan mengembalikan fungsi yang menerapkannya ke task.
func (u *TaskUseCase) buildPatch(in UpdateInput) (func(domain.Task) domain.Task, error) {
	if in.Title == nil && !in.Description.Set && in.Status == nil && in.Priority == nil &&
		!in.AssigneeID.Set && !in.DueDate.Set {
		return nil, domain.ErrInvalidInput
	}

	var (
		title    string
		desc     *string
		status   domain.Status
		priority domain.Priority
		assignee *uuid.UUID
		due      *time.Time
		err      error
	)
	if in.Title != nil {
		if title, err = validateTitle(*in.Title); err != nil {
			return nil, err
		}
	}
	if in.Description.Set {
		if desc, err = validateDescription(in.Description.Value); err != nil {
			return nil, err
		}
	}
	if in.Status != nil {
		var ok bool
		if status, ok = domain.ParseStatus(*in.Status); !ok {
			return nil, domain.ErrInvalidInput
		}
	}
	if in.Priority != nil {
		var ok bool
		if priority, ok = domain.ParsePriority(*in.Priority); !ok {
			return nil, domain.ErrInvalidInput
		}
	}
	if in.AssigneeID.Set {
		if assignee, err = parseOptionalUUID(in.AssigneeID.Value); err != nil {
			return nil, err
		}
	}
	if in.DueDate.Set {
		if due, err = parseDueDate(in.DueDate.Value); err != nil {
			return nil, err
		}
	}

	return func(t domain.Task) domain.Task {
		if in.Title != nil {
			t.Title = title
		}
		if in.Description.Set {
			t.Description = desc
		}
		if in.Status != nil {
			t.Status = status
		}
		if in.Priority != nil {
			t.Priority = priority
		}
		if in.AssigneeID.Set {
			t.AssigneeID = assignee
		}
		if in.DueDate.Set {
			t.DueDate = due
		}
		return t
	}, nil
}

func buildFilter(in ListInput, uid uuid.UUID) (domain.TaskFilter, int, int, error) {
	var f domain.TaskFilter
	for _, s := range in.Statuses {
		st, ok := domain.ParseStatus(s)
		if !ok {
			return f, 0, 0, domain.ErrInvalidInput
		}
		f.Statuses = append(f.Statuses, st)
	}
	for _, p := range in.Priorities {
		pr, ok := domain.ParsePriority(p)
		if !ok {
			return f, 0, 0, domain.ErrInvalidInput
		}
		f.Priorities = append(f.Priorities, pr)
	}
	switch in.Assignee {
	case "":
	case "me":
		f.AssigneeID = &uid
	case "none":
		f.Unassigned = true
	default:
		id, err := uuid.Parse(in.Assignee)
		if err != nil {
			return f, 0, 0, domain.ErrInvalidInput
		}
		f.AssigneeID = &id
	}
	f.Query = strings.TrimSpace(in.Query)
	if utf8.RuneCountInString(f.Query) > maxQueryLength {
		return f, 0, 0, domain.ErrInvalidInput
	}

	page, limit := in.Page, in.Limit
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = defaultLimit
	}
	if page < 1 || limit < 1 || limit > maxLimit {
		return f, 0, 0, domain.ErrInvalidInput
	}
	f.Limit = limit
	f.Offset = (page - 1) * limit
	return f, page, limit, nil
}

func validateTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleLength {
		return "", domain.ErrInvalidInput
	}
	return title, nil
}

// validateDescription: nil tetap nil; string kosong (setelah trim) menjadi nil.
func validateDescription(description *string) (*string, error) {
	if description == nil {
		return nil, nil
	}
	d := strings.TrimSpace(*description)
	if utf8.RuneCountInString(d) > maxDescriptionLength {
		return nil, domain.ErrInvalidInput
	}
	if d == "" {
		return nil, nil
	}
	return &d, nil
}

func parseOptionalUUID(s *string) (*uuid.UUID, error) {
	if s == nil {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*s))
	if err != nil {
		return nil, domain.ErrInvalidInput
	}
	return &id, nil
}

// parseDueDate menerima RFC3339 (mis. 2026-03-01T17:00:00+07:00) dan menyimpannya dalam UTC.
func parseDueDate(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*s))
	if err != nil {
		return nil, domain.ErrInvalidInput
	}
	t = t.UTC()
	return &t, nil
}
