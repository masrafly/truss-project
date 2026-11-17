package usecase

import (
	"context"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

// TaskAccessFinder memuat task beserta akses user terhadap project-nya.
type TaskAccessFinder interface {
	FindTaskAccess(ctx context.Context, taskID, userID uuid.UUID) (*domain.TaskView, *domain.ProjectAccess, error)
}

// CommentRepository adalah port penyimpanan komentar. Implementasi wajib mengembalikan:
//   - CreateComment -> domain.ErrTaskNotFound bila task terhapus di tengah proses
//   - FindComment, UpdateComment, DeleteComment -> domain.ErrCommentNotFound bila komentar
//     tidak ada ATAU bukan milik task tersebut
type CommentRepository interface {
	TaskAccessFinder

	// CreateComment menyimpan komentar dan mengisi ID, CreatedAt, UpdatedAt, dan Author.
	CreateComment(ctx context.Context, c *domain.Comment) error

	// ListComments: terlama lebih dulu, beserta jumlah total.
	ListComments(ctx context.Context, taskID uuid.UUID, limit, offset int) ([]domain.Comment, int64, error)

	FindComment(ctx context.Context, taskID, commentID uuid.UUID) (*domain.Comment, error)

	// UpdateComment menyimpan Content dan mengisi UpdatedAt dari database.
	UpdateComment(ctx context.Context, c *domain.Comment) error

	DeleteComment(ctx context.Context, taskID, commentID uuid.UUID) error
}
