package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/workspaces/domain"
)

// MemberRepository adalah port penyimpanan keanggotaan workspace.
// Implementasi wajib mengembalikan error domain:
//   - FindMemberRole -> domain.ErrNotMember bila user bukan anggota
//   - AddMember      -> domain.ErrAlreadyMember bila sudah menjadi anggota
//   - RemoveMember   -> domain.ErrMemberNotFound bila tidak ada baris yang dihapus
type MemberRepository interface {
	FindMemberRole(ctx context.Context, workspaceID, userID uuid.UUID) (domain.Role, error)

	// AddMember menyimpan keanggotaan dan mengembalikan waktu bergabung.
	AddMember(ctx context.Context, workspaceID, userID uuid.UUID, role domain.Role) (time.Time, error)

	// ListMembers: Owner lebih dulu, lalu Admin, lalu Member; di dalam peran, urut waktu bergabung.
	// Mengembalikan satu halaman beserta total seluruh anggota.
	ListMembers(ctx context.Context, workspaceID uuid.UUID, limit, offset int) ([]domain.Member, int64, error)

	// RemoveMember menghapus keanggotaan workspace sekaligus keanggotaan project user
	// di workspace tersebut, dan melepasnya dari task (dicatat atas nama actorID), secara
	// atomik. Mengembalikan domain.ErrMemberOwnsProjects (tanpa menghapus apa pun) bila
	// user masih Owner suatu project di workspace ini.
	RemoveMember(ctx context.Context, workspaceID, userID, actorID uuid.UUID) error

	// ChangeMemberRole mengubah peran target menjadi admin/member dan mengembalikan anggota
	// terbaru. Dalam satu transaksi, baris actor dan target dikunci (urut user_id, agar tidak
	// deadlock dengan TransferOwnership) dan keduanya diperiksa ulang pada baris yang terkunci:
	// actor harus masih Owner (domain.ErrForbidden), target harus anggota
	// (domain.ErrMemberNotFound) dan bukan Owner (domain.ErrCannotChangeOwnerRole).
	ChangeMemberRole(ctx context.Context, workspaceID, actorID, targetID uuid.UUID, role domain.Role) (*domain.Member, error)

	// TransferOwnership, atomik: target menjadi Owner dan actor menjadi Admin. Actor diperiksa
	// ulang pada baris terkunci (domain.ErrForbidden bila sudah bukan Owner); target harus
	// anggota (domain.ErrMemberNotFound).
	TransferOwnership(ctx context.Context, workspaceID, actorID, targetID uuid.UUID) error
}

// UserDirectory mencari user terdaftar. Ini adalah port ke modul lain (users/auth);
// modul workspaces tidak mengetahui bagaimana user disimpan.
// FindByEmail wajib mengembalikan domain.ErrUserNotFound bila tidak ada.
type UserDirectory interface {
	FindByEmail(ctx context.Context, email string) (*domain.UserRef, error)
}
