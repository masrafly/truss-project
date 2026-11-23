package domain_test

import (
	"testing"

	"truss-project/internal/modules/workspaces/domain"
)

// Hanya Owner yang boleh mengubah peran, memindahkan kepemilikan, dan menghapus workspace.
func TestOwnerOnlyPermissions_Matrix(t *testing.T) {
	roles := []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleMember, "", "hacker", "OWNER"}
	for _, r := range roles {
		want := r == domain.RoleOwner
		if got := domain.CanChangeRole(r); got != want {
			t.Errorf("CanChangeRole(%q) = %v, harapannya %v", r, got, want)
		}
		if got := domain.CanTransferOwnership(r); got != want {
			t.Errorf("CanTransferOwnership(%q) = %v, harapannya %v", r, got, want)
		}
		if got := domain.CanDeleteWorkspace(r); got != want {
			t.Errorf("CanDeleteWorkspace(%q) = %v, harapannya %v", r, got, want)
		}
	}
}
