package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"truss-project/internal/modules/projects/domain"
)

type actor struct {
	name string
	a    domain.Access
}

// Semua aktor yang relevan. "luar" = tidak bisa melihat project.
var actors = []actor{
	{"owner ws", domain.Access{WorkspaceRole: domain.WorkspaceOwner}},
	{"admin ws", domain.Access{WorkspaceRole: domain.WorkspaceAdmin}},
	{"owner project", domain.Access{WorkspaceRole: domain.WorkspaceMember, ProjectRole: domain.ProjectOwner}},
	{"admin project", domain.Access{WorkspaceRole: domain.WorkspaceMember, ProjectRole: domain.ProjectAdmin}},
	{"member project", domain.Access{WorkspaceRole: domain.WorkspaceMember, ProjectRole: domain.ProjectMember}},
	{"member ws saja", domain.Access{WorkspaceRole: domain.WorkspaceMember}},
	{"anggota project basi (bukan anggota ws)", domain.Access{ProjectRole: domain.ProjectOwner}},
	{"luar", domain.Access{}},
}

func TestCanAddMember_Matrix(t *testing.T) {
	// nilai: [boleh tambah admin, boleh tambah member]
	want := map[string][2]bool{
		"owner ws":       {true, true},
		"admin ws":       {true, true},
		"owner project":  {true, true},
		"admin project":  {false, true},
		"member project": {false, false},
		"member ws saja": {false, false},
		"anggota project basi (bukan anggota ws)": {false, false},
		"luar": {false, false},
	}
	for _, ac := range actors {
		got := [2]bool{ac.a.CanAddMember(domain.ProjectAdmin), ac.a.CanAddMember(domain.ProjectMember)}
		if got != want[ac.name] {
			t.Errorf("%s: dapat %v, harapannya %v", ac.name, got, want[ac.name])
		}
		// Role owner tidak pernah bisa diberikan, oleh siapa pun.
		if ac.a.CanAddMember(domain.ProjectOwner) || ac.a.CanAddMember("") || ac.a.CanAddMember("superuser") {
			t.Errorf("%s: role owner/kosong/tak dikenal tidak boleh diberikan", ac.name)
		}
	}
}

func TestCheckRemoveMember_OtherTargets(t *testing.T) {
	// Menghapus ORANG LAIN. nilai per target: admin, member.
	// "ok" = nil, "403" = ErrForbidden.
	want := map[string][2]string{
		"owner ws":       {"ok", "ok"},
		"admin ws":       {"ok", "ok"},
		"owner project":  {"ok", "ok"},
		"admin project":  {"403", "ok"},
		"member project": {"403", "403"},
		"member ws saja": {"403", "403"},
		"anggota project basi (bukan anggota ws)": {"403", "403"},
		"luar": {"403", "403"},
	}
	me, other := uuid.New(), uuid.New()
	for _, ac := range actors {
		got := [2]string{
			label(ac.a.CheckRemoveMember(me, domain.ProjectAdmin, other)),
			label(ac.a.CheckRemoveMember(me, domain.ProjectMember, other)),
		}
		if got != want[ac.name] {
			t.Errorf("%s: dapat %v, harapannya %v", ac.name, got, want[ac.name])
		}
	}
}

func TestCheckRemoveMember_OwnerTarget(t *testing.T) {
	// Owner project tidak pernah bisa dihapus: 409 untuk yang berwenang/diri sendiri, 403 lainnya.
	want := map[string]string{
		"owner ws":       "409",
		"admin ws":       "409",
		"owner project":  "409", // menghapus Owner lain (secara teori) -> tetap 409
		"admin project":  "403",
		"member project": "403",
		"member ws saja": "403",
		"anggota project basi (bukan anggota ws)": "403",
		"luar": "403",
	}
	me, other := uuid.New(), uuid.New()
	for _, ac := range actors {
		if got := label(ac.a.CheckRemoveMember(me, domain.ProjectOwner, other)); got != want[ac.name] {
			t.Errorf("%s: dapat %s, harapannya %s", ac.name, got, want[ac.name])
		}
	}

	owner := domain.Access{WorkspaceRole: domain.WorkspaceMember, ProjectRole: domain.ProjectOwner}
	if got := label(owner.CheckRemoveMember(me, domain.ProjectOwner, me)); got != "409" {
		t.Errorf("Owner project keluar sendiri: dapat %s, harapannya 409", got)
	}
}

func TestCheckRemoveMember_Leave(t *testing.T) {
	me := uuid.New()
	for _, role := range []domain.ProjectRole{domain.ProjectAdmin, domain.ProjectMember} {
		a := domain.Access{WorkspaceRole: domain.WorkspaceMember, ProjectRole: role}
		if err := a.CheckRemoveMember(me, role, me); err != nil {
			t.Errorf("%s project harus bisa keluar sendiri, dapat %v", role, err)
		}
	}
	// Tanpa keanggotaan workspace, bahkan keluar pun tidak lewat jalur ini.
	stale := domain.Access{ProjectRole: domain.ProjectMember}
	if err := stale.CheckRemoveMember(me, domain.ProjectMember, me); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("anggota basi: harapannya 403, dapat %v", err)
	}
}

// Properti: Owner project tidak pernah bisa dihapus, apa pun aksesnya dan siapa pun aktornya.
func TestOwnerNeverRemovable(t *testing.T) {
	id := uuid.New()
	for _, ws := range wsRoles {
		for _, p := range pRoles {
			a := domain.Access{WorkspaceRole: ws, ProjectRole: p}
			for _, actorID := range []uuid.UUID{id, uuid.New()} {
				if err := a.CheckRemoveMember(actorID, domain.ProjectOwner, id); err == nil {
					t.Errorf("ws=%q p=%q: Owner terhapus", ws, p)
				}
			}
		}
	}
}

// Properti: siapa pun yang boleh mengelola anggota pasti boleh melihat project.
func TestMemberManagementImpliesView(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	for _, ws := range wsRoles {
		for _, p := range pRoles {
			a := domain.Access{WorkspaceRole: ws, ProjectRole: p}
			name := fmt.Sprintf("ws=%q p=%q", ws, p)
			manages := a.CanAddMember(domain.ProjectMember) ||
				a.CheckRemoveMember(me, domain.ProjectMember, other) == nil
			if manages && !a.CanView() {
				t.Errorf("%s: mengelola anggota tanpa bisa melihat project", name)
			}
		}
	}
}

func TestParseAssignableRole(t *testing.T) {
	for _, ok := range []string{"admin", "member"} {
		if r, valid := domain.ParseAssignableRole(ok); !valid || string(r) != ok {
			t.Errorf("%q harus valid", ok)
		}
	}
	for _, bad := range []string{"owner", "", "Admin", "superuser"} {
		if _, valid := domain.ParseAssignableRole(bad); valid {
			t.Errorf("%q harus ditolak", bad)
		}
	}
}

func label(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, domain.ErrForbidden):
		return "403"
	case errors.Is(err, domain.ErrCannotRemoveOwner):
		return "409"
	}
	return "?" + err.Error()
}
