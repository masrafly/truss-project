package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

// UpdateFunc menerima task terkini yang SUDAH DIKUNCI repository dan mengembalikan
// versi barunya beserta aktivitas yang harus dicatat. Error yang dikembalikan
// membatalkan transaksi apa adanya.
type UpdateFunc func(current domain.Task) (domain.Task, []domain.Activity, error)

// TaskRepository adalah port penyimpanan task. Implementasi wajib mengembalikan error domain:
//   - FindProjectAccess -> domain.ErrProjectNotFound bila project tidak ada
//   - FindTaskAccess, Update, Delete, ListActivities -> domain.ErrTaskNotFound bila task tidak ada
//   - Create, Update -> domain.ErrAssigneeNotMember bila assignee (baru) bukan anggota project
type TaskRepository interface {
	FindProjectAccess(ctx context.Context, projectID, userID uuid.UUID) (*domain.ProjectAccess, error)

	FindTaskAccess(ctx context.Context, taskID, userID uuid.UUID) (*domain.TaskView, *domain.ProjectAccess, error)

	// Create menyimpan task dan aktivitasnya dalam satu transaksi. TaskID pada
	// aktivitas diisi repository. Keanggotaan project assignee dikunci (FOR SHARE).
	Create(ctx context.Context, t *domain.Task, activities []domain.Activity) (*domain.TaskView, error)

	// Update mengunci baris task (FOR UPDATE), memanggil fn, lalu menyimpan task baru
	// dan aktivitasnya dalam transaksi yang sama.
	Update(ctx context.Context, taskID uuid.UUID, fn UpdateFunc) (*domain.TaskView, error)

	Delete(ctx context.Context, taskID uuid.UUID) error

	// List mengembalikan satu halaman task dan jumlah total yang cocok dengan filter.
	List(ctx context.Context, projectID uuid.UUID, f domain.TaskFilter) ([]domain.TaskView, int64, error)

	// Summary menghitung ringkasan task project dalam satu query agregat; task terlambat
	// dihitung per asOf.
	Summary(ctx context.Context, projectID uuid.UUID, asOf time.Time) (domain.ProjectSummary, error)

	// ListActivities: satu halaman riwayat task, terlama lebih dulu, beserta total seluruh riwayat.
	ListActivities(ctx context.Context, taskID uuid.UUID, limit, offset int) ([]domain.Activity, int64, error)
}
