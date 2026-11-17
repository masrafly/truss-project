package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/projects/domain"
)

// AccessFinder memuat project beserta akses user (dipakai bersama oleh semua use case).
type AccessFinder interface {
	FindWithAccess(ctx context.Context, projectID, userID uuid.UUID) (*domain.ProjectAccess, error)
}

// MemberRepository adalah port penyimpanan keanggotaan project. Implementasi wajib
// mengembalikan error domain:
//   - FindWithAccess              -> domain.ErrProjectNotFound bila project tidak ada
//   - FindWorkspaceMemberByEmail  -> domain.ErrWorkspaceUserNotFound bila email tidak
//     terdaftar atau pemiliknya bukan anggota workspace project
//   - FindMemberRole, RemoveMember -> domain.ErrMemberNotFound bila bukan anggota
//   - AddMember -> domain.ErrAlreadyMember bila sudah anggota; domain.ErrWorkspaceUserNotFound
//     bila target sudah bukan anggota workspace saat transaksi berjalan
type MemberRepository interface {
	AccessFinder

	FindWorkspaceMemberByEmail(ctx context.Context, projectID uuid.UUID, email string) (*domain.UserRef, error)

	FindMemberRole(ctx context.Context, projectID, userID uuid.UUID) (domain.ProjectRole, error)

	// AddMember menyimpan keanggotaan dan mengembalikan waktu bergabung. Keanggotaan
	// workspace target dikunci (FOR SHARE) agar tidak bertabrakan dengan proses keluar workspace.
	AddMember(ctx context.Context, projectID, userID uuid.UUID, role domain.ProjectRole) (time.Time, error)

	// ListMembers: Owner, Admin, lalu Member; di dalam peran urut waktu bergabung.
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]domain.Member, error)

	// RemoveMember tidak pernah menghapus Owner (pertahanan terakhir di SQL). Task yang
	// ditugaskan kepada user dilepas (assignee = null) dan dicatat atas nama actorID,
	// dalam transaksi yang sama.
	RemoveMember(ctx context.Context, projectID, userID, actorID uuid.UUID) error
}
