package domain_test

import (
	"fmt"
	"testing"

	"truss-project/internal/modules/projects/domain"
)

var (
	wsRoles = []domain.WorkspaceRole{"", domain.WorkspaceOwner, domain.WorkspaceAdmin, domain.WorkspaceMember, "superuser"}
	pRoles  = []domain.ProjectRole{"", domain.ProjectOwner, domain.ProjectAdmin, domain.ProjectMember}
)

func TestCanCreateProject(t *testing.T) {
	want := map[domain.WorkspaceRole]bool{
		domain.WorkspaceOwner:  true,
		domain.WorkspaceAdmin:  true,
		domain.WorkspaceMember: false,
		"":                     false,
		"superuser":            false,
	}
	for _, ws := range wsRoles {
		if got := domain.CanCreateProject(ws); got != want[ws] {
			t.Errorf("CanCreateProject(%q) = %v, harapannya %v", ws, got, want[ws])
		}
	}
}

// Matriks lengkap: baris = peran workspace, kolom = peran project.
// Nilai = "VUD" (View, Update, Delete); "-" berarti tidak.
func TestAccessMatrix(t *testing.T) {
	expect := map[domain.WorkspaceRole]map[domain.ProjectRole]string{
		domain.WorkspaceOwner: {
			"":                   "VUD",
			domain.ProjectOwner:  "VUD",
			domain.ProjectAdmin:  "VUD",
			domain.ProjectMember: "VUD",
		},
		domain.WorkspaceAdmin: {
			"":                   "VU-",
			domain.ProjectOwner:  "VUD",
			domain.ProjectAdmin:  "VU-",
			domain.ProjectMember: "VU-",
		},
		domain.WorkspaceMember: {
			"":                   "---",
			domain.ProjectOwner:  "VUD",
			domain.ProjectAdmin:  "VU-",
			domain.ProjectMember: "V--",
		},
		// Bukan anggota workspace: tidak ada hak sama sekali, apa pun peran project-nya.
		"": {
			"":                   "---",
			domain.ProjectOwner:  "---",
			domain.ProjectAdmin:  "---",
			domain.ProjectMember: "---",
		},
		// Peran workspace tak dikenal diperlakukan seperti bukan anggota.
		"superuser": {
			"":                   "---",
			domain.ProjectOwner:  "---",
			domain.ProjectAdmin:  "---",
			domain.ProjectMember: "---",
		},
	}

	for _, ws := range wsRoles {
		for _, p := range pRoles {
			a := domain.Access{WorkspaceRole: ws, ProjectRole: p}
			got := flag(a.CanView(), "V") + flag(a.CanUpdate(), "U") + flag(a.CanDelete(), "D")
			if want := expect[ws][p]; got != want {
				t.Errorf("ws=%q project=%q: dapat %s, harapannya %s", ws, p, got, want)
			}
		}
	}
}

// Properti: hak yang lebih kuat menyiratkan hak yang lebih lemah.
func TestAccessIsMonotonic(t *testing.T) {
	for _, ws := range wsRoles {
		for _, p := range pRoles {
			a := domain.Access{WorkspaceRole: ws, ProjectRole: p}
			name := fmt.Sprintf("ws=%q project=%q", ws, p)
			if a.CanDelete() && !a.CanUpdate() {
				t.Errorf("%s: boleh hapus tetapi tidak boleh ubah", name)
			}
			if a.CanUpdate() && !a.CanView() {
				t.Errorf("%s: boleh ubah tetapi tidak boleh lihat", name)
			}
		}
	}
}

func TestParseStatus(t *testing.T) {
	for _, ok := range []string{"active", "archived", "completed"} {
		if s, valid := domain.ParseStatus(ok); !valid || string(s) != ok {
			t.Errorf("ParseStatus(%q) harus valid", ok)
		}
	}
	for _, bad := range []string{"", "Active", "deleted", " active"} {
		if _, valid := domain.ParseStatus(bad); valid {
			t.Errorf("ParseStatus(%q) harus ditolak", bad)
		}
	}
}

func flag(b bool, s string) string {
	if b {
		return s
	}
	return "-"
}
