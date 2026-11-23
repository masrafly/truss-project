package usecase

import (
	"context"

	"github.com/google/uuid"

	"truss-project/internal/modules/workspaces/domain"
)

// WorkspaceRepository adalah port untuk penyimpanan workspace.
// Implementasi wajib mengembalikan error domain (bukan error GORM):
//   - FindByID           -> domain.ErrWorkspaceNotFound bila tidak ada
//   - FindMemberRole     -> domain.ErrNotMember bila user bukan anggota
//   - CreateWithOwner    -> domain.ErrUserNotFound bila pembuat tidak ada
type WorkspaceRepository interface {
	// CreateWithOwner menyimpan workspace DAN keanggotaan Owner untuk w.CreatedBy
	// secara atomik (semua berhasil atau tidak sama sekali). Mengisi ID dan timestamp w.
	CreateWithOwner(ctx context.Context, w *domain.Workspace) error

	// ListByMember mengembalikan satu halaman workspace milik user beserta perannya, terbaru
	// lebih dulu, dan total seluruh workspace user.
	ListByMember(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.WorkspaceAccess, int64, error)

	FindByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error)

	FindMemberRole(ctx context.Context, workspaceID, userID uuid.UUID) (domain.Role, error)

	// Delete menghapus workspace beserta seluruh isinya (project, anggota, task, komentar,
	// riwayat; cascade di database). Actor diperiksa ulang sebagai Owner pada baris
	// keanggotaan yang terkunci: domain.ErrWorkspaceNotFound bila bukan anggota,
	// domain.ErrForbidden bila bukan Owner.
	Delete(ctx context.Context, workspaceID, actorID uuid.UUID) error
}
