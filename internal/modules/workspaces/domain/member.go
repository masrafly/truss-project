package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Member adalah anggota workspace beserta data profil dasarnya.
type Member struct {
	UserID   uuid.UUID
	Name     string
	Email    string
	Role     Role
	JoinedAt time.Time
}

// UserRef adalah rujukan minimal ke user terdaftar (hasil pencarian berdasarkan email).
type UserRef struct {
	ID    uuid.UUID
	Name  string
	Email string
}

var (
	// ErrForbidden: user adalah anggota, tetapi perannya tidak mengizinkan aksi ini (403).
	// Berbeda dengan non-anggota yang mendapat ErrWorkspaceNotFound (404).
	ErrForbidden = errors.New("tidak memiliki izin untuk aksi ini")

	ErrAlreadyMember = errors.New("user sudah menjadi anggota workspace")

	ErrMemberNotFound = errors.New("anggota tidak ditemukan")

	// ErrCannotRemoveOwner: keanggotaan Owner tidak dapat dihapus, agar workspace
	// selalu memiliki setidaknya satu Owner (aturan bisnis PRD no. 3).
	ErrCannotRemoveOwner = errors.New("owner workspace tidak dapat dihapus")

	// ErrMemberOwnsProjects: anggota masih menjadi Owner project di workspace ini.
	// Ia tidak boleh keluar/dihapus agar project tidak kehilangan pengelola (PRD FR-04 #4).
	ErrMemberOwnsProjects = errors.New("anggota masih menjadi owner project di workspace ini")
)

// ParseAssignableRole memvalidasi role yang boleh diberikan saat menambah anggota.
// Role "owner" tidak dapat diberikan lewat penambahan anggota.
func ParseAssignableRole(s string) (Role, bool) {
	switch Role(s) {
	case RoleAdmin:
		return RoleAdmin, true
	case RoleMember:
		return RoleMember, true
	default:
		return "", false
	}
}

// CanAddMember: siapa boleh menambahkan anggota dengan role tertentu.
//
//	Owner  -> admin atau member
//	Admin  -> hanya member
//	Member -> tidak boleh
func CanAddMember(actor, newRole Role) bool {
	switch actor {
	case RoleOwner:
		return newRole == RoleAdmin || newRole == RoleMember
	case RoleAdmin:
		return newRole == RoleMember
	default:
		return false
	}
}

// CheckRemoveMember menentukan apakah actor boleh menghapus target.
// Mengembalikan nil jika boleh, atau ErrForbidden / ErrCannotRemoveOwner.
//
//	Owner  -> boleh menghapus admin dan member; Owner sendiri tidak dapat dihapus
//	Admin  -> boleh menghapus member, atau dirinya sendiri (keluar)
//	Member -> hanya dirinya sendiri (keluar)
func CheckRemoveMember(actor Role, actorID uuid.UUID, target Role, targetID uuid.UUID) error {
	// Invarian Owner dijaga di sini, apa pun kombinasi argumennya:
	// keanggotaan Owner tidak pernah dapat dihapus oleh siapa pun.
	if target == RoleOwner {
		if actor == RoleOwner {
			return ErrCannotRemoveOwner
		}
		return ErrForbidden
	}

	self := actorID == targetID

	switch actor {
	case RoleOwner:
		return nil
	case RoleAdmin:
		if self || target == RoleMember {
			return nil
		}
		return ErrForbidden
	case RoleMember:
		if self {
			return nil
		}
		return ErrForbidden
	default:
		return ErrForbidden
	}
}
