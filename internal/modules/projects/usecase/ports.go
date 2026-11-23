package usecase

import (
	"context"

	"github.com/google/uuid"

	"truss-project/internal/modules/projects/domain"
)

// ProjectRepository adalah port penyimpanan project. Implementasi wajib
// mengembalikan error domain:
//   - FindWorkspaceRole -> domain.ErrNotWorkspaceMember bila user bukan anggota workspace
//   - CreateWithOwner   -> domain.ErrNotWorkspaceMember bila pembuat sudah bukan anggota
//     workspace saat transaksi berjalan; domain.ErrUserNotFound bila pembuat tidak ada
//   - FindWithAccess, Update, Delete -> domain.ErrProjectNotFound bila project tidak ada
type ProjectRepository interface {
	FindWorkspaceRole(ctx context.Context, workspaceID, userID uuid.UUID) (domain.WorkspaceRole, error)

	// CreateWithOwner menyimpan project dan keanggotaan Owner (pembuat) dalam satu
	// transaksi, sambil mengunci keanggotaan workspace pembuat.
	CreateWithOwner(ctx context.Context, p *domain.Project) error

	// ListAccessible mengembalikan project yang boleh dilihat user (lihat
	// domain.Access.CanView), terbaru lebih dulu. workspaceID nil = semua workspace.
	ListAccessible(
		ctx context.Context,
		userID uuid.UUID,
		workspaceID *uuid.UUID,
		limit, offset int,
	) ([]domain.ProjectAccess, int64, error)

	// FindWithAccess mengembalikan project beserta peran user (kosong bila bukan anggota).
	// Tidak melakukan otorisasi; itu tugas use case.
	FindWithAccess(ctx context.Context, projectID, userID uuid.UUID) (*domain.ProjectAccess, error)

	// Update menyimpan name, description, dan status; UpdatedAt diisi dari database.
	Update(ctx context.Context, p *domain.Project) error

	Delete(ctx context.Context, projectID uuid.UUID) error
}
