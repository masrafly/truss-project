package domain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"truss-project/internal/modules/workspaces/domain"
)

func TestParseAssignableRole(t *testing.T) {
	tests := []struct {
		in   string
		want domain.Role
		ok   bool
	}{
		{"member", domain.RoleMember, true},
		{"admin", domain.RoleAdmin, true},
		{"owner", "", false}, // owner tidak bisa diberikan lewat penambahan anggota
		{"", "", false},
		{"Admin", "", false}, // peka huruf besar-kecil
		{"superadmin", "", false},
		{" member", "", false},
	}

	for _, tc := range tests {
		got, ok := domain.ParseAssignableRole(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseAssignableRole(%q) = (%q, %v), harapannya (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// Matriks lengkap: siapa boleh menambahkan role apa.
func TestCanAddMember_Matrix(t *testing.T) {
	tests := []struct {
		actor, newRole domain.Role
		want           bool
	}{
		{domain.RoleOwner, domain.RoleAdmin, true},
		{domain.RoleOwner, domain.RoleMember, true},
		{domain.RoleOwner, domain.RoleOwner, false}, // tidak ada cara menambah Owner

		{domain.RoleAdmin, domain.RoleMember, true},
		{domain.RoleAdmin, domain.RoleAdmin, false}, // Admin tidak boleh mengangkat Admin
		{domain.RoleAdmin, domain.RoleOwner, false},

		{domain.RoleMember, domain.RoleMember, false},
		{domain.RoleMember, domain.RoleAdmin, false},
		{domain.RoleMember, domain.RoleOwner, false},

		{"", domain.RoleMember, false},       // peran kosong
		{"hacker", domain.RoleMember, false}, // peran tidak dikenal
		{domain.RoleOwner, "hacker", false},  // role target tidak dikenal
		{domain.RoleOwner, "", false},
	}

	for _, tc := range tests {
		if got := domain.CanAddMember(tc.actor, tc.newRole); got != tc.want {
			t.Errorf("CanAddMember(%q, %q) = %v, harapannya %v", tc.actor, tc.newRole, got, tc.want)
		}
	}
}

// Matriks lengkap penghapusan anggota: actor x target, untuk target orang lain dan diri sendiri.
func TestCheckRemoveMember_Matrix(t *testing.T) {
	me, other := uuid.New(), uuid.New()

	tests := []struct {
		name   string
		actor  domain.Role
		target domain.Role
		self   bool
		want   error
	}{
		// Owner
		{"owner hapus admin", domain.RoleOwner, domain.RoleAdmin, false, nil},
		{"owner hapus member", domain.RoleOwner, domain.RoleMember, false, nil},
		{"owner hapus owner lain", domain.RoleOwner, domain.RoleOwner, false, domain.ErrCannotRemoveOwner},
		{"owner keluar sendiri", domain.RoleOwner, domain.RoleOwner, true, domain.ErrCannotRemoveOwner},

		// Admin
		{"admin hapus member", domain.RoleAdmin, domain.RoleMember, false, nil},
		{"admin hapus admin lain", domain.RoleAdmin, domain.RoleAdmin, false, domain.ErrForbidden},
		{"admin hapus owner", domain.RoleAdmin, domain.RoleOwner, false, domain.ErrForbidden},
		{"admin keluar sendiri", domain.RoleAdmin, domain.RoleAdmin, true, nil},

		// Member
		{"member hapus member lain", domain.RoleMember, domain.RoleMember, false, domain.ErrForbidden},
		{"member hapus admin", domain.RoleMember, domain.RoleAdmin, false, domain.ErrForbidden},
		{"member hapus owner", domain.RoleMember, domain.RoleOwner, false, domain.ErrForbidden},
		{"member keluar sendiri", domain.RoleMember, domain.RoleMember, true, nil},

		// Tidak dikenal
		{"peran kosong", "", domain.RoleMember, false, domain.ErrForbidden},
		{"peran tidak dikenal", "hacker", domain.RoleMember, true, domain.ErrForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targetID := other
			if tc.self {
				targetID = me
			}

			got := domain.CheckRemoveMember(tc.actor, me, tc.target, targetID)

			if !errors.Is(got, tc.want) && !(got == nil && tc.want == nil) {
				t.Errorf("hasil = %v, harapannya %v", got, tc.want)
			}
		})
	}
}

func TestCheckRemoveMember_OwnerIsNeverRemovable(t *testing.T) {
	// Properti: siapa pun perannya, Owner tidak pernah bisa dihapus (nil tidak pernah dikembalikan).
	me, other := uuid.New(), uuid.New()

	for _, actor := range []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleMember, "", "hacker"} {
		for _, self := range []bool{true, false} {
			targetID := other
			if self {
				targetID = me
			}
			if err := domain.CheckRemoveMember(actor, me, domain.RoleOwner, targetID); err == nil {
				t.Errorf("actor %q (self=%v) tidak boleh bisa menghapus Owner", actor, self)
			}
		}
	}
}
