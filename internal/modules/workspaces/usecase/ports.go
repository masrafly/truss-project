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

	// ListByMember mengembalikan workspace milik user beserta perannya, terbaru lebih dulu.
	ListByMember(ctx context.Context, userID uuid.UUID) ([]domain.WorkspaceAccess, error)

	FindByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error)

	FindMemberRole(ctx context.Context, workspaceID, userID uuid.UUID) (domain.Role, error)
}
