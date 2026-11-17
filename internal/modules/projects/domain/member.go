package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Member adalah anggota project beserta profil singkatnya.
type Member struct {
	UserID   uuid.UUID
	Name     string
	Email    string
	Role     ProjectRole
	JoinedAt time.Time
}

// UserRef adalah referensi user (hasil pencarian anggota workspace berdasarkan email).
type UserRef struct {
	ID    uuid.UUID
	Name  string
	Email string
}

var (
	ErrAlreadyMember = errors.New("user sudah menjadi anggota project")

	ErrMemberNotFound = errors.New("anggota project tidak ditemukan")

	// ErrWorkspaceUserNotFound: email tidak terdaftar ATAU pemiliknya bukan anggota
	// workspace project ini. Keduanya sengaja identik agar pengelola project tidak
	// dapat memakai endpoint ini untuk menebak email yang terdaftar di aplikasi.
	ErrWorkspaceUserNotFound = errors.New("user tidak ditemukan di workspace ini")

	// ErrCannotRemoveOwner: Owner project tidak dapat dihapus/keluar agar project
	// selalu memiliki pengelola (PRD FR-04 #4).
	ErrCannotRemoveOwner = errors.New("owner project tidak dapat dihapus")
)

// ParseAssignableRole: hanya admin/member yang boleh diberikan lewat penambahan anggota.
func ParseAssignableRole(s string) (ProjectRole, bool) {
	switch ProjectRole(s) {
	case ProjectAdmin, ProjectMember:
		return ProjectRole(s), true
	}
	return "", false
}

// Tingkat pengelolaan anggota project:
//
//	2 = penuh   : Owner project, atau Owner/Admin workspace (pengelola)
//	1 = terbatas: Admin project (hanya mengelola Member)
//	0 = tidak ada
func (a Access) memberManageLevel() int {
	if !a.CanView() {
		return 0
	}
	if a.ProjectRole == ProjectOwner || a.WorkspaceRole.isManager() {
		return 2
	}
	if a.ProjectRole == ProjectAdmin {
		return 1
	}
	return 0
}

// CanAddMember: tingkat penuh boleh menambah admin/member; Admin project hanya member.
func (a Access) CanAddMember(newRole ProjectRole) bool {
	switch a.memberManageLevel() {
	case 2:
		return newRole == ProjectAdmin || newRole == ProjectMember
	case 1:
		return newRole == ProjectMember
	}
	return false
}

// CheckRemoveMember memutuskan apakah aktor boleh menghapus target dari project.
// Mengembalikan nil, ErrCannotRemoveOwner (409), atau ErrForbidden (403).
func (a Access) CheckRemoveMember(actorID uuid.UUID, target ProjectRole, targetID uuid.UUID) error {
	level := a.memberManageLevel()
	self := actorID == targetID

	// Invarian lebih dulu: Owner project tidak pernah bisa dihapus.
	if target == ProjectOwner {
		if level == 2 || self {
			return ErrCannotRemoveOwner
		}
		return ErrForbidden
	}

	if !a.CanView() {
		return ErrForbidden
	}
	if self {
		return nil // keluar dari project
	}
	switch level {
	case 2:
		return nil
	case 1:
		if target == ProjectMember {
			return nil
		}
	}
	return ErrForbidden
}
