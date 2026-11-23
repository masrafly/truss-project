package usecase_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"truss-project/internal/modules/projects/domain"
)

// ---------- ChangeRole ----------

func TestMemberChangeRole_FullManagersCanPromoteAndDemote(t *testing.T) {
	for name, pick := range map[string]func(*memberFixture) uuid.UUID{
		"owner ws":      func(f *memberFixture) uuid.UUID { return f.wsOwner },
		"admin ws":      func(f *memberFixture) uuid.UUID { return f.wsAdmin },
		"owner project": func(f *memberFixture) uuid.UUID { return f.pOwner },
	} {
		t.Run(name, func(t *testing.T) {
			f := newMemberFixture()
			got, err := f.muc.ChangeRole(ctx, pick(f).String(), f.pid.String(), f.wsMember.String(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			if got.UserID != f.wsMember || got.Role != domain.ProjectAdmin || f.repo.pRoles[f.pid][f.wsMember] != domain.ProjectAdmin {
				t.Errorf("hasil = %+v, peran = %v", got, f.repo.pRoles[f.pid][f.wsMember])
			}
			if _, err := f.muc.ChangeRole(ctx, pick(f).String(), f.pid.String(), f.pAdmin.String(), "member"); err != nil {
				t.Fatalf("turunkan admin: %v", err)
			}
			if f.repo.pRoles[f.pid][f.pAdmin] != domain.ProjectMember {
				t.Error("admin harus menjadi member")
			}
		})
	}
}

func TestMemberChangeRole_ProjectAdminAndMembersAreForbidden(t *testing.T) {
	for name, pick := range map[string]func(*memberFixture) uuid.UUID{
		"admin project":  func(f *memberFixture) uuid.UUID { return f.pAdmin },
		"member project": func(f *memberFixture) uuid.UUID { return f.wsMember },
	} {
		t.Run(name, func(t *testing.T) {
			f := newMemberFixture()
			_, err := f.muc.ChangeRole(ctx, pick(f).String(), f.pid.String(), f.wsMember.String(), "admin")
			if !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("err = %v, harapannya ErrForbidden", err)
			}
			if f.mrepo.changeCalls != 0 || f.repo.pRoles[f.pid][f.wsMember] != domain.ProjectMember {
				t.Error("tidak ada perubahan yang boleh terjadi")
			}
		})
	}
}

func TestMemberChangeRole_CannotSeeProjectGets404(t *testing.T) {
	f := newMemberFixture()
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.wsPlain, "orang luar": f.outsider} {
		if _, err := f.muc.ChangeRole(ctx, uid.String(), f.pid.String(), f.wsMember.String(), "admin"); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s: err = %v, harapannya 404", name, err)
		}
	}
	if f.mrepo.changeCalls != 0 {
		t.Error("repository tidak boleh dipanggil")
	}
}

func TestMemberChangeRole_ValidationOrder(t *testing.T) {
	f := newMemberFixture()
	for _, role := range []string{"", "owner", "Admin", "root"} {
		if _, err := f.muc.ChangeRole(ctx, f.pOwner.String(), f.pid.String(), f.wsMember.String(), role); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("role %q: err = %v", role, err)
		}
	}
	// Izin (403) sebelum pencarian target (404): yang tak berhak tidak dapat menguji keberadaan anggota.
	if _, err := f.muc.ChangeRole(ctx, f.pAdmin.String(), f.pid.String(), uuid.NewString(), "member"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("err = %v, harapannya ErrForbidden", err)
	}
	if f.mrepo.changeCalls != 0 {
		t.Error("repository tidak boleh dipanggil")
	}
}

func TestMemberChangeRole_TargetErrors(t *testing.T) {
	f := newMemberFixture()
	if _, err := f.muc.ChangeRole(ctx, f.pOwner.String(), f.pid.String(), uuid.NewString(), "admin"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota: %v", err)
	}
	if _, err := f.muc.ChangeRole(ctx, f.pOwner.String(), f.pid.String(), "bukan-uuid", "admin"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("ID rusak: %v", err)
	}
	if _, err := f.muc.ChangeRole(ctx, f.wsOwner.String(), f.pid.String(), f.pOwner.String(), "member"); !errors.Is(err, domain.ErrCannotChangeOwnerRole) {
		t.Errorf("target Owner project: %v", err)
	}
	if f.repo.pRoles[f.pid][f.pOwner] != domain.ProjectOwner {
		t.Error("peran Owner tidak boleh berubah")
	}
	boom := errors.New("db mati")
	f.mrepo.changeErr = boom
	if _, err := f.muc.ChangeRole(ctx, f.pOwner.String(), f.pid.String(), f.wsMember.String(), "admin"); !errors.Is(err, boom) {
		t.Errorf("error repository: %v", err)
	}
}

// ---------- TransferOwnership ----------

func TestMemberTransferOwnership_FullManagersMoveOwnerToExistingMember(t *testing.T) {
	for name, pick := range map[string]func(*memberFixture) uuid.UUID{
		"owner ws":      func(f *memberFixture) uuid.UUID { return f.wsOwner },
		"admin ws":      func(f *memberFixture) uuid.UUID { return f.wsAdmin },
		"owner project": func(f *memberFixture) uuid.UUID { return f.pOwner },
	} {
		t.Run(name, func(t *testing.T) {
			f := newMemberFixture()
			if err := f.muc.TransferOwnership(ctx, pick(f).String(), f.pid.String(), f.wsMember.String()); err != nil {
				t.Fatal(err)
			}
			roles := f.repo.pRoles[f.pid]
			if roles[f.wsMember] != domain.ProjectOwner || roles[f.pOwner] != domain.ProjectAdmin {
				t.Errorf("peran = %v", roles)
			}
			owners := 0
			for _, r := range roles {
				if r == domain.ProjectOwner {
					owners++
				}
			}
			if owners != 1 {
				t.Errorf("harus tepat satu Owner, dapat %d", owners)
			}
		})
	}
}

func TestMemberTransferOwnership_OldOwnerThenCanLeaveButNewOwnerCannot(t *testing.T) {
	f := newMemberFixture()
	if err := f.muc.TransferOwnership(ctx, f.pOwner.String(), f.pid.String(), f.pAdmin.String()); err != nil {
		t.Fatal(err)
	}
	if err := f.muc.Remove(ctx, f.pOwner.String(), f.pid.String(), f.pOwner.String()); err != nil {
		t.Errorf("Owner lama (kini Admin) harus bisa keluar: %v", err)
	}
	if err := f.muc.Remove(ctx, f.pAdmin.String(), f.pid.String(), f.pAdmin.String()); !errors.Is(err, domain.ErrCannotRemoveOwner) {
		t.Errorf("Owner baru tidak boleh keluar: %v", err)
	}
}

func TestMemberTransferOwnership_ForbiddenAndNotFound(t *testing.T) {
	f := newMemberFixture()
	for name, uid := range map[string]uuid.UUID{"admin project": f.pAdmin, "member project": f.wsMember} {
		if err := f.muc.TransferOwnership(ctx, uid.String(), f.pid.String(), f.wsMember.String()); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.wsPlain, "orang luar": f.outsider} {
		if err := f.muc.TransferOwnership(ctx, uid.String(), f.pid.String(), f.wsMember.String()); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s: err = %v, harapannya 404", name, err)
		}
	}
	if f.mrepo.transferCalls != 0 {
		t.Error("repository tidak boleh dipanggil")
	}
}

func TestMemberTransferOwnership_TargetValidation(t *testing.T) {
	f := newMemberFixture()
	if err := f.muc.TransferOwnership(ctx, f.wsOwner.String(), f.pid.String(), f.pOwner.String()); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("target sudah Owner: %v", err)
	}
	if err := f.muc.TransferOwnership(ctx, f.wsOwner.String(), f.pid.String(), f.candidate.String()); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota project: %v", err)
	}
	if err := f.muc.TransferOwnership(ctx, f.wsOwner.String(), f.pid.String(), "bukan-uuid"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("ID rusak: %v", err)
	}
	boom := errors.New("db mati")
	f.mrepo.transferErr = boom
	if err := f.muc.TransferOwnership(ctx, f.wsOwner.String(), f.pid.String(), f.wsMember.String()); !errors.Is(err, boom) {
		t.Errorf("error repository: %v", err)
	}
}
