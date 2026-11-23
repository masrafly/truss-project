package domain_test

import (
	"testing"

	"truss-project/internal/modules/projects/domain"
)

// CanManageRoles: hanya tingkat penuh (Owner project, atau Owner/Admin workspace) yang boleh
// mengubah peran dan memindahkan kepemilikan; selalu dengan syarat dapat melihat project.
func TestCanManageRoles_FullMatrix(t *testing.T) {
	wsRoles := []domain.WorkspaceRole{domain.WorkspaceOwner, domain.WorkspaceAdmin, domain.WorkspaceMember, "", "hacker"}
	pRoles := []domain.ProjectRole{domain.ProjectOwner, domain.ProjectAdmin, domain.ProjectMember, "", "hacker"}

	for _, ws := range wsRoles {
		for _, pr := range pRoles {
			a := domain.Access{WorkspaceRole: ws, ProjectRole: pr}
			wsValid := ws == domain.WorkspaceOwner || ws == domain.WorkspaceAdmin || ws == domain.WorkspaceMember
			manager := ws == domain.WorkspaceOwner || ws == domain.WorkspaceAdmin
			want := wsValid && (pr == domain.ProjectOwner || manager)
			if got := a.CanManageRoles(); got != want {
				t.Errorf("CanManageRoles(ws=%q, project=%q) = %v, harapannya %v", ws, pr, got, want)
			}
		}
	}
}

func TestCanManageRoles_ProjectAdminAndMemberAreNotEnough(t *testing.T) {
	for _, pr := range []domain.ProjectRole{domain.ProjectAdmin, domain.ProjectMember} {
		if (domain.Access{WorkspaceRole: domain.WorkspaceMember, ProjectRole: pr}).CanManageRoles() {
			t.Errorf("%s project tidak boleh mengubah peran", pr)
		}
	}
	if (domain.Access{WorkspaceRole: "", ProjectRole: domain.ProjectOwner}).CanManageRoles() {
		t.Error("Owner project yang sudah bukan anggota workspace tidak boleh bertindak (data basi)")
	}
}
