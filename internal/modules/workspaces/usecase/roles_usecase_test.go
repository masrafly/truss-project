package usecase_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"truss-project/internal/modules/workspaces/domain"
)

// ---------- ChangeRole ----------

func TestChangeRole_OwnerPromotesAndDemotes(t *testing.T) {
	f := newFixture()
	f.repo.names[f.member] = domain.UserRef{Name: "Member", Email: "member@example.com"}

	got, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), f.member.String(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != f.member || got.Role != domain.RoleAdmin || got.Email != "member@example.com" {
		t.Errorf("hasil = %+v", got)
	}
	if f.repo.roles[f.wid][f.member] != domain.RoleAdmin {
		t.Error("peran harus tersimpan")
	}
	if _, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), f.admin.String(), "member"); err != nil {
		t.Fatalf("turunkan admin: %v", err)
	}
	if f.repo.roles[f.wid][f.admin] != domain.RoleMember {
		t.Error("admin harus menjadi member")
	}
}

func TestChangeRole_OnlyOwnerMayChangeRoles(t *testing.T) {
	f := newFixture()
	for name, actor := range map[string]uuid.UUID{"admin": f.admin, "member": f.member} {
		_, err := f.uc.ChangeRole(ctx, actor.String(), f.wid.String(), f.member.String(), "admin")
		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: err = %v, harapannya ErrForbidden", name, err)
		}
	}
	if f.repo.changeCalls != 0 {
		t.Error("repository tidak boleh dipanggil untuk yang tidak berhak")
	}
	if f.repo.roles[f.wid][f.member] != domain.RoleMember {
		t.Error("peran tidak boleh berubah")
	}
}

func TestChangeRole_NonMemberGets404BeforeAnythingElse(t *testing.T) {
	f := newFixture()
	_, err := f.uc.ChangeRole(ctx, f.other.String(), f.wid.String(), f.member.String(), "admin")
	if !errors.Is(err, domain.ErrWorkspaceNotFound) || f.repo.changeCalls != 0 {
		t.Fatalf("err = %v calls = %d", err, f.repo.changeCalls)
	}
	if _, err := f.uc.ChangeRole(ctx, f.owner.String(), "bukan-uuid", f.member.String(), "admin"); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("workspace ID rusak: %v", err)
	}
	if _, err := f.uc.ChangeRole(ctx, "bukan-uuid", f.wid.String(), f.member.String(), "admin"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("user ID rusak: %v", err)
	}
}

func TestChangeRole_RejectsInvalidRoles(t *testing.T) {
	f := newFixture()
	for _, role := range []string{"", "owner", "Admin", "superadmin", " member"} {
		_, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), f.member.String(), role)
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("role %q: err = %v, harapannya ErrInvalidInput", role, err)
		}
	}
	if f.repo.changeCalls != 0 {
		t.Error("repository tidak boleh dipanggil untuk role tidak valid")
	}
}

func TestChangeRole_InvalidRoleIs400EvenForNonOwner_ButPermissionBeatsTarget(t *testing.T) {
	f := newFixture()
	// Urutan: validasi role (400) sebelum izin (403).
	if _, err := f.uc.ChangeRole(ctx, f.member.String(), f.wid.String(), f.member.String(), "owner"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("err = %v", err)
	}
	// Izin (403) sebelum pencarian target (404): non-Owner tidak dapat menguji keberadaan anggota.
	if _, err := f.uc.ChangeRole(ctx, f.admin.String(), f.wid.String(), uuid.NewString(), "member"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("err = %v", err)
	}
}

func TestChangeRole_TargetErrors(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), uuid.NewString(), "admin"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota: %v", err)
	}
	if _, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), "bukan-uuid", "admin"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("ID rusak: %v", err)
	}
	if _, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), f.owner.String(), "member"); !errors.Is(err, domain.ErrCannotChangeOwnerRole) {
		t.Errorf("Owner tidak boleh menurunkan dirinya lewat ubah peran: %v", err)
	}
	if f.repo.roles[f.wid][f.owner] != domain.RoleOwner {
		t.Error("peran Owner tidak boleh berubah")
	}
}

func TestChangeRole_RepositoryErrorsPassedThrough(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.changeErr = boom
	if _, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), f.member.String(), "admin"); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	f.repo.changeErr = domain.ErrForbidden // Owner kehilangan hak di tengah balapan
	if _, err := f.uc.ChangeRole(ctx, f.owner.String(), f.wid.String(), f.member.String(), "admin"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("err = %v", err)
	}
}

// ---------- TransferOwnership ----------

func TestTransferOwnership_SwapsRolesAtomically(t *testing.T) {
	f := newFixture()

	if err := f.uc.TransferOwnership(ctx, f.owner.String(), f.wid.String(), f.member.String()); err != nil {
		t.Fatal(err)
	}
	if f.repo.roles[f.wid][f.member] != domain.RoleOwner || f.repo.roles[f.wid][f.owner] != domain.RoleAdmin {
		t.Errorf("peran = %v", f.repo.roles[f.wid])
	}
	owners := 0
	for _, r := range f.repo.roles[f.wid] {
		if r == domain.RoleOwner {
			owners++
		}
	}
	if owners != 1 {
		t.Errorf("harus tepat satu Owner, dapat %d", owners)
	}
	if f.repo.lastTransfer.actor != f.owner || f.repo.lastTransfer.target != f.member || f.repo.lastTransfer.wid != f.wid {
		t.Errorf("argumen = %+v", f.repo.lastTransfer)
	}
}

func TestTransferOwnership_OldOwnerCanThenLeaveNewOwnerCannotBeRemoved(t *testing.T) {
	f := newFixture()
	if err := f.uc.TransferOwnership(ctx, f.owner.String(), f.wid.String(), f.admin.String()); err != nil {
		t.Fatal(err)
	}
	// Owner lama kini Admin: boleh keluar. Owner baru tidak dapat dihapus.
	if err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.owner.String()); err != nil {
		t.Errorf("Owner lama (kini Admin) harus bisa keluar: %v", err)
	}
	if err := f.uc.Remove(ctx, f.member.String(), f.wid.String(), f.admin.String()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Member tidak boleh menghapus Owner baru: %v", err)
	}
}

func TestTransferOwnership_OnlyOwner(t *testing.T) {
	f := newFixture()
	for name, actor := range map[string]uuid.UUID{"admin": f.admin, "member": f.member} {
		err := f.uc.TransferOwnership(ctx, actor.String(), f.wid.String(), f.member.String())
		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if f.repo.transferCalls != 0 {
		t.Error("repository tidak boleh dipanggil untuk yang tidak berhak")
	}
	if err := f.uc.TransferOwnership(ctx, f.other.String(), f.wid.String(), f.member.String()); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("non-anggota harus 404: %v", err)
	}
}

func TestTransferOwnership_TargetValidation(t *testing.T) {
	f := newFixture()
	if err := f.uc.TransferOwnership(ctx, f.owner.String(), f.wid.String(), f.owner.String()); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("ke diri sendiri: %v", err)
	}
	if err := f.uc.TransferOwnership(ctx, f.owner.String(), f.wid.String(), uuid.NewString()); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("bukan anggota: %v", err)
	}
	if err := f.uc.TransferOwnership(ctx, f.owner.String(), f.wid.String(), "bukan-uuid"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("ID rusak: %v", err)
	}
	if f.repo.roles[f.wid][f.owner] != domain.RoleOwner {
		t.Error("tidak ada perubahan peran yang boleh terjadi")
	}
}

func TestTransferOwnership_RepositoryErrorsPassedThrough(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.transferErr = boom
	if err := f.uc.TransferOwnership(ctx, f.owner.String(), f.wid.String(), f.member.String()); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

// ---------- Delete workspace ----------

func wsWithOwner(t *testing.T) (*fakeRepo, uuid.UUID, uuid.UUID) {
	t.Helper()
	repo := newFakeRepo()
	owner := uuid.New()
	ws, err := newUC(repo).Create(ctx, owner.String(), "Tim Backend", nil)
	if err != nil {
		t.Fatal(err)
	}
	return repo, owner, ws.ID
}

func TestDeleteWorkspace_OwnerWithMatchingNameDeletes(t *testing.T) {
	repo, owner, wid := wsWithOwner(t)

	if err := newUC(repo).Delete(ctx, owner.String(), wid.String(), "  Tim Backend  "); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.workspaces[wid]; ok || repo.deleteCalls != 1 || repo.lastDeleted.actor != owner {
		t.Errorf("workspace harus terhapus oleh owner: calls=%d last=%+v", repo.deleteCalls, repo.lastDeleted)
	}
}

func TestDeleteWorkspace_ConfirmationMustMatchExactly(t *testing.T) {
	repo, owner, wid := wsWithOwner(t)
	for _, bad := range []string{"", "   ", "tim backend", "Tim Backen", "Tim Backend 2", "Tim  Backend"} {
		if err := newUC(repo).Delete(ctx, owner.String(), wid.String(), bad); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("konfirmasi %q: err = %v, harapannya ErrInvalidInput", bad, err)
		}
	}
	if repo.deleteCalls != 0 || repo.workspaces[wid] == nil {
		t.Error("workspace tidak boleh terhapus tanpa konfirmasi yang benar")
	}
}

func TestDeleteWorkspace_OnlyOwner_NonMemberGets404(t *testing.T) {
	repo, owner, wid := wsWithOwner(t)
	admin, member, outsider := uuid.New(), uuid.New(), uuid.New()
	repo.members[wid][admin] = domain.RoleAdmin
	repo.members[wid][member] = domain.RoleMember
	_ = owner

	for name, uid := range map[string]uuid.UUID{"admin": admin, "member": member} {
		if err := newUC(repo).Delete(ctx, uid.String(), wid.String(), "Tim Backend"); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := newUC(repo).Delete(ctx, outsider.String(), wid.String(), "Tim Backend"); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("non-anggota harus 404: %v", err)
	}
	if err := newUC(repo).Delete(ctx, outsider.String(), "bukan-uuid", "x"); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("ID workspace rusak: %v", err)
	}
	if err := newUC(repo).Delete(ctx, "bukan-uuid", wid.String(), "x"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("ID user rusak: %v", err)
	}
	if repo.deleteCalls != 0 || repo.workspaces[wid] == nil {
		t.Error("workspace tidak boleh terhapus oleh yang tidak berhak")
	}
}

func TestDeleteWorkspace_Order403Before400(t *testing.T) {
	// Non-Owner tidak boleh mengetahui nama workspace lewat perbedaan respons konfirmasi.
	repo, _, wid := wsWithOwner(t)
	member := uuid.New()
	repo.members[wid][member] = domain.RoleMember
	if err := newUC(repo).Delete(ctx, member.String(), wid.String(), "salah"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("err = %v, harapannya ErrForbidden (bukan 400)", err)
	}
}

func TestDeleteWorkspace_InfrastructureErrorsPassedThrough(t *testing.T) {
	repo, owner, wid := wsWithOwner(t)
	boom := errors.New("db mati")

	repo.roleErr = boom
	if err := newUC(repo).Delete(ctx, owner.String(), wid.String(), "Tim Backend"); !errors.Is(err, boom) {
		t.Errorf("role: %v", err)
	}
	repo.roleErr = nil
	repo.findErr = boom
	if err := newUC(repo).Delete(ctx, owner.String(), wid.String(), "Tim Backend"); !errors.Is(err, boom) {
		t.Errorf("find: %v", err)
	}
	repo.findErr = nil
	repo.deleteErr = boom
	if err := newUC(repo).Delete(ctx, owner.String(), wid.String(), "Tim Backend"); !errors.Is(err, boom) {
		t.Errorf("delete: %v", err)
	}
}
