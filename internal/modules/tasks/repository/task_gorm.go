// Package repository mengimplementasikan usecase.TaskRepository dengan GORM.
// Semua detail GORM/SQL berhenti di sini.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

// taskRecord: model persistence tabel tasks.
type taskRecord struct {
	ID          uuid.UUID  `gorm:"column:internal_id;type:uuid;primaryKey;default:gen_random_uuid()"`
	ProjectID   uuid.UUID  `gorm:"type:uuid;not null"`
	Title       string     `gorm:"type:varchar(200);not null"`
	Description *string    `gorm:"type:text"`
	Status      string     `gorm:"type:varchar(20);not null"`
	Priority    string     `gorm:"type:varchar(20);not null"`
	AssigneeID  *uuid.UUID `gorm:"type:uuid"`
	CreatedBy   uuid.UUID  `gorm:"type:uuid;not null"`
	DueDate     *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (taskRecord) TableName() string { return "tasks" }

func (r taskRecord) toDomain() domain.Task {
	return domain.Task{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		Title:       r.Title,
		Description: r.Description,
		Status:      domain.Status(r.Status),
		Priority:    domain.Priority(r.Priority),
		AssigneeID:  r.AssigneeID,
		CreatedBy:   r.CreatedBy,
		DueDate:     r.DueDate,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// taskRow: task + nama assignee (LEFT JOIN users). Sengaja struct DATAR, bukan menyematkan
// taskRecord: GORM mengabaikan field yang tidak diekspor, dan tipe sematan taskRecord
// tidak diekspor, sehingga semua kolom task terbaca kosong tanpa error (lihat schema_test.go).
type taskRow struct {
	ID           uuid.UUID  `gorm:"column:internal_id"`
	ProjectID    uuid.UUID  `gorm:"column:project_id"`
	Title        string     `gorm:"column:title"`
	Description  *string    `gorm:"column:description"`
	Status       string     `gorm:"column:status"`
	Priority     string     `gorm:"column:priority"`
	AssigneeID   *uuid.UUID `gorm:"column:assignee_id"`
	CreatedBy    uuid.UUID  `gorm:"column:created_by"`
	DueDate      *time.Time `gorm:"column:due_date"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
	AssigneeName *string    `gorm:"column:assignee_name"`
}

func (r taskRow) toView() domain.TaskView {
	v := domain.TaskView{Task: domain.Task{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		Title:       r.Title,
		Description: r.Description,
		Status:      domain.Status(r.Status),
		Priority:    domain.Priority(r.Priority),
		AssigneeID:  r.AssigneeID,
		CreatedBy:   r.CreatedBy,
		DueDate:     r.DueDate,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}}
	if r.AssigneeID != nil {
		ref := domain.UserRef{ID: *r.AssigneeID}
		if r.AssigneeName != nil {
			ref.Name = *r.AssigneeName
		}
		v.Assignee = &ref
	}
	return v
}

const viewSelect = `t.internal_id, t.project_id, t.title, t.description, t.status, t.priority,
	t.assignee_id, t.created_by, t.due_date, t.created_at, t.updated_at, u.name AS assignee_name`

type TaskGormRepository struct {
	db *gorm.DB
}

func NewTaskGormRepository(db *gorm.DB) *TaskGormRepository {
	return &TaskGormRepository{db: db}
}

// ---------- akses ----------

type accessRow struct {
	ProjectID     uuid.UUID `gorm:"column:internal_id"`
	WorkspaceID   uuid.UUID `gorm:"column:workspace_id"`
	Status        string    `gorm:"column:status"`
	WorkspaceRole string    `gorm:"column:workspace_role"`
	ProjectRole   string    `gorm:"column:project_role"`
}

// FindProjectAccess membaca projects, workspace_members, dan project_members (baca-saja).
func (r *TaskGormRepository) FindProjectAccess(
	ctx context.Context,
	projectID, userID uuid.UUID,
) (*domain.ProjectAccess, error) {
	var rows []accessRow
	err := r.db.WithContext(ctx).
		Table("projects AS p").
		Select(`p.internal_id, p.workspace_id, p.status,
			COALESCE(wm.role, '') AS workspace_role, COALESCE(pm.role, '') AS project_role`).
		Joins("LEFT JOIN workspace_members AS wm ON wm.workspace_id = p.workspace_id AND wm.user_id = ?", userID).
		Joins("LEFT JOIN project_members AS pm ON pm.project_id = p.internal_id AND pm.user_id = ?", userID).
		Where("p.internal_id = ?", projectID).
		Limit(1).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrProjectNotFound
	}
	row := rows[0]
	return &domain.ProjectAccess{
		ProjectID:     row.ProjectID,
		WorkspaceID:   row.WorkspaceID,
		Archived:      row.Status == "archived",
		WorkspaceRole: domain.WorkspaceRole(row.WorkspaceRole),
		ProjectRole:   domain.ProjectRole(row.ProjectRole),
	}, nil
}

func (r *TaskGormRepository) FindTaskAccess(
	ctx context.Context,
	taskID, userID uuid.UUID,
) (*domain.TaskView, *domain.ProjectAccess, error) {
	view, err := findView(r.db.WithContext(ctx), taskID)
	if err != nil {
		return nil, nil, err
	}
	access, err := r.FindProjectAccess(ctx, view.ProjectID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotFound) {
			return nil, nil, domain.ErrTaskNotFound // project terhapus di antara dua query
		}
		return nil, nil, err
	}
	return view, access, nil
}

func findView(db *gorm.DB, taskID uuid.UUID) (*domain.TaskView, error) {
	var rows []taskRow
	err := db.Table("tasks AS t").
		Select(viewSelect).
		Joins("LEFT JOIN users AS u ON u.internal_id = t.assignee_id").
		Where("t.internal_id = ?", taskID).
		Limit(1).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrTaskNotFound
	}
	v := rows[0].toView()
	return &v, nil
}

// lockAssignee memastikan assignee anggota project dan mengunci keanggotaannya
// (FOR SHARE), sehingga tidak bertabrakan dengan penghapusan anggota yang sedang berjalan.
func lockAssignee(tx *gorm.DB, projectID uuid.UUID, assignee *uuid.UUID) error {
	if assignee == nil {
		return nil
	}
	var one int
	err := tx.Raw(
		"SELECT 1 FROM project_members WHERE project_id = ? AND user_id = ? FOR SHARE",
		projectID, *assignee,
	).Row().Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrAssigneeNotMember
	}
	return err
}

// ---------- tulis ----------

func (r *TaskGormRepository) Create(
	ctx context.Context,
	t *domain.Task,
	activities []domain.Activity,
) (*domain.TaskView, error) {
	rec := taskRecord{
		ProjectID:   t.ProjectID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Priority:    string(t.Priority),
		AssigneeID:  t.AssigneeID,
		CreatedBy:   t.CreatedBy,
		DueDate:     t.DueDate,
	}

	var view *domain.TaskView
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAssignee(tx, t.ProjectID, t.AssigneeID); err != nil {
			return err
		}
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		if err := insertActivities(tx, rec.ID, activities); err != nil {
			return err
		}
		var err error
		view, err = findView(tx, rec.ID)
		return err
	})
	if err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			// Project (atau pembuat) terhapus di tengah proses.
			return nil, domain.ErrProjectNotFound
		}
		return nil, err
	}
	*t = view.Task
	return view, nil
}

func (r *TaskGormRepository) Update(
	ctx context.Context,
	taskID uuid.UUID,
	fn usecase.UpdateFunc,
) (*domain.TaskView, error) {
	var view *domain.TaskView
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec taskRecord
		err := tx.Raw("SELECT * FROM tasks WHERE internal_id = ? FOR UPDATE", taskID).Scan(&rec).Error
		if err != nil {
			return err
		}
		if rec.ID == uuid.Nil {
			return domain.ErrTaskNotFound
		}
		cur := rec.toDomain()

		next, activities, err := fn(cur)
		if err != nil {
			return err
		}
		// Field yang tidak boleh berubah lewat Update, apa pun yang dikembalikan fn.
		next.ID, next.ProjectID, next.CreatedBy, next.CreatedAt = cur.ID, cur.ProjectID, cur.CreatedBy, cur.CreatedAt

		if !sameUUID(cur.AssigneeID, next.AssigneeID) {
			if err := lockAssignee(tx, cur.ProjectID, next.AssigneeID); err != nil {
				return err
			}
		}

		err = tx.Exec(
			`UPDATE tasks
			    SET title = ?, description = ?, status = ?, priority = ?,
			        assignee_id = ?, due_date = ?, updated_at = NOW()
			  WHERE internal_id = ?`,
			next.Title, next.Description, string(next.Status), string(next.Priority),
			next.AssigneeID, next.DueDate, cur.ID,
		).Error
		if err != nil {
			return err
		}
		if err := insertActivities(tx, cur.ID, activities); err != nil {
			return err
		}
		view, err = findView(tx, cur.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (r *TaskGormRepository) Delete(ctx context.Context, taskID uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&taskRecord{}, "internal_id = ?", taskID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrTaskNotFound
	}
	return nil
}

// ---------- baca ----------

func (r *TaskGormRepository) List(
	ctx context.Context,
	projectID uuid.UUID,
	f domain.TaskFilter,
) ([]domain.TaskView, int64, error) {
	q := r.db.WithContext(ctx).Table("tasks AS t").Where("t.project_id = ?", projectID)
	if len(f.Statuses) > 0 {
		s := make([]string, len(f.Statuses))
		for i, v := range f.Statuses {
			s[i] = string(v)
		}
		q = q.Where("t.status IN ?", s)
	}
	if len(f.Priorities) > 0 {
		p := make([]string, len(f.Priorities))
		for i, v := range f.Priorities {
			p[i] = string(v)
		}
		q = q.Where("t.priority IN ?", p)
	}
	if f.Unassigned {
		q = q.Where("t.assignee_id IS NULL")
	} else if f.AssigneeID != nil {
		q = q.Where("t.assignee_id = ?", *f.AssigneeID)
	}
	if f.Query != "" {
		q = q.Where(`t.title ILIKE ? ESCAPE '\'`, "%"+escapeLike(f.Query)+"%")
	}
	if f.OverdueAsOf != nil {
		q = q.Where(overdueCondition, *f.OverdueAsOf)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []taskRow
	err := q.Session(&gorm.Session{}).
		Select(viewSelect).
		Joins("LEFT JOIN users AS u ON u.internal_id = t.assignee_id").
		Order("t.created_at DESC, t.internal_id").
		Limit(f.Limit).
		Offset(f.Offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	out := make([]domain.TaskView, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toView())
	}
	return out, total, nil
}

// escapeLike meloloskan karakter khusus LIKE agar pencarian "50%" mencari teks "50%".
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ---------- aktivitas ----------

type activityRow struct {
	ID        uuid.UUID `gorm:"column:internal_id"`
	TaskID    uuid.UUID `gorm:"column:task_id"`
	ActorID   uuid.UUID `gorm:"column:actor_id"`
	ActorName *string   `gorm:"column:actor_name"`
	Type      string    `gorm:"column:activity_type"`
	OldValue  *string   `gorm:"column:old_value"`
	NewValue  *string   `gorm:"column:new_value"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (r *TaskGormRepository) ListActivities(ctx context.Context, taskID uuid.UUID) ([]domain.Activity, error) {
	var rows []activityRow
	err := r.db.WithContext(ctx).
		Table("task_activities AS a").
		// #>> '{}' mengambil nilai skalar JSON sebagai teks (null JSON/SQL -> NULL).
		Select(`a.internal_id, a.task_id, a.actor_id, u.name AS actor_name, a.activity_type,
			a.old_value #>> '{}' AS old_value, a.new_value #>> '{}' AS new_value, a.created_at`).
		Joins("LEFT JOIN users AS u ON u.internal_id = a.actor_id").
		Where("a.task_id = ?", taskID).
		Order("a.created_at, a.internal_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]domain.Activity, 0, len(rows))
	for _, row := range rows {
		a := domain.Activity{
			ID: row.ID, TaskID: row.TaskID, ActorID: row.ActorID,
			Type: domain.ActivityType(row.Type), OldValue: row.OldValue, NewValue: row.NewValue,
			CreatedAt: row.CreatedAt,
		}
		ref := domain.UserRef{ID: row.ActorID}
		if row.ActorName != nil {
			ref.Name = *row.ActorName
		}
		a.Actor = &ref
		out = append(out, a)
	}
	return out, nil
}

// insertActivities menyimpan aktivitas. created_at memakai clock_timestamp() (bukan
// NOW() yang konstan sepanjang transaksi) agar urutan beberapa aktivitas terjaga.
func insertActivities(tx *gorm.DB, taskID uuid.UUID, activities []domain.Activity) error {
	for _, a := range activities {
		oldV, err := jsonScalar(a.OldValue)
		if err != nil {
			return err
		}
		newV, err := jsonScalar(a.NewValue)
		if err != nil {
			return err
		}
		err = tx.Exec(
			`INSERT INTO task_activities (task_id, actor_id, activity_type, old_value, new_value, created_at)
			 VALUES (?, ?, ?, CAST(? AS jsonb), CAST(? AS jsonb), clock_timestamp())`,
			taskID, a.ActorID, string(a.Type), oldV, newV,
		).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// jsonScalar: nil -> NULL SQL; string -> string JSON (mis. "todo" -> "\"todo\"").
func jsonScalar(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	b, err := json.Marshal(*s)
	if err != nil {
		return nil, err
	}
	v := string(b)
	return &v, nil
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ---------- ringkasan ----------

// overdueCondition adalah padanan SQL dari domain.IsOverdue. Dipakai oleh filter daftar
// task DAN ringkasan, sehingga keduanya selalu konsisten (PRD FR-09 #3).
const overdueCondition = "t.status <> 'done' AND t.due_date IS NOT NULL AND t.due_date < ?"

type summaryRow struct {
	Total      int64 `gorm:"column:total"`
	Todo       int64 `gorm:"column:todo"`
	InProgress int64 `gorm:"column:in_progress"`
	Done       int64 `gorm:"column:done"`
	Low        int64 `gorm:"column:low"`
	Medium     int64 `gorm:"column:medium"`
	High       int64 `gorm:"column:high"`
	Urgent     int64 `gorm:"column:urgent"`
	Unassigned int64 `gorm:"column:unassigned"`
	Overdue    int64 `gorm:"column:overdue"`
}

// Summary menghitung semua angka dalam SATU query agregat (satu kali baca, satu snapshot),
// hanya untuk task pada project yang diminta (PRD FR-09 #1).
func (r *TaskGormRepository) Summary(ctx context.Context, projectID uuid.UUID, asOf time.Time) (domain.ProjectSummary, error) {
	var row summaryRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT COUNT(*)                                          AS total,
		        COUNT(*) FILTER (WHERE t.status = 'todo')         AS todo,
		        COUNT(*) FILTER (WHERE t.status = 'in_progress')  AS in_progress,
		        COUNT(*) FILTER (WHERE t.status = 'done')         AS done,
		        COUNT(*) FILTER (WHERE t.priority = 'low')        AS low,
		        COUNT(*) FILTER (WHERE t.priority = 'medium')     AS medium,
		        COUNT(*) FILTER (WHERE t.priority = 'high')       AS high,
		        COUNT(*) FILTER (WHERE t.priority = 'urgent')     AS urgent,
		        COUNT(*) FILTER (WHERE t.assignee_id IS NULL)     AS unassigned,
		        COUNT(*) FILTER (WHERE `+overdueCondition+`)     AS overdue
		   FROM tasks AS t
		  WHERE t.project_id = ?`,
		asOf, projectID,
	).Scan(&row).Error
	if err != nil {
		return domain.ProjectSummary{}, err
	}
	return domain.ProjectSummary{
		Total: row.Total, Todo: row.Todo, InProgress: row.InProgress, Done: row.Done,
		Low: row.Low, Medium: row.Medium, High: row.High, Urgent: row.Urgent,
		Unassigned: row.Unassigned, Overdue: row.Overdue,
	}, nil
}
