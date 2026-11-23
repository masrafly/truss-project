package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/modules/workspaces/usecase"
)

// ---------- fakes ----------

type fakeMemberRepo struct {
	roles map[uuid.UUID]map[uuid.UUID]domain.Role // workspaceID -> userID -> role
	names map[uuid.UUID]domain.UserRef

	roleErr, addErr, listErr, removeErr error
	changeErr, transferErr              error

	changeCalls, transferCalls int
	lastChange                 struct {
		wid, actor, target uuid.UUID
		role               domain.Role
	}
	lastTransfer struct{ wid, actor, target uuid.UUID }

	addCalls, removeCalls, roleCalls, listCalls int
	lastLimit, lastOffset                       int
	lastAdded                                   struct {
		wid, uid uuid.UUID
		role     domain.Role
	}
	lastRemoved struct{ wid, uid, actor uuid.UUID }
}

func newFakeMemberRepo() *fakeMemberRepo {
	return &fakeMemberRepo{
		roles: map[uuid.UUID]map[uuid.UUID]domain.Role{},
		names: map[uuid.UUID]domain.UserRef{},
	}
}

// seed membuat workspace baru dengan satu anggota per peran, dan mengembalikan ID-nya.
func (r *fakeMemberRepo) seed(owner, admin, member uuid.UUID) uuid.UUID {
	wid := uuid.New()
	r.roles[wid] = map[uuid.UUID]domain.Role{owner: domain.RoleOwner, admin: domain.RoleAdmin, member: domain.RoleMember}
	return wid
}

func (r *fakeMemberRepo) FindMemberRole(_ context.Context, wid, uid uuid.UUID) (domain.Role, error) {
	r.roleCalls++
	if r.roleErr != nil {
		return "", r.roleErr
	}
	if role, ok := r.roles[wid][uid]; ok {
		return role, nil
	}
	return "", domain.ErrNotMember
}

func (r *fakeMemberRepo) AddMember(_ context.Context, wid, uid uuid.UUID, role domain.Role) (time.Time, error) {
	r.addCalls++
	r.lastAdded.wid, r.lastAdded.uid, r.lastAdded.role = wid, uid, role
	if r.addErr != nil {
		return time.Time{}, r.addErr
	}
	if _, ok := r.roles[wid][uid]; ok {
		return time.Time{}, domain.ErrAlreadyMember
	}
	if r.roles[wid] == nil {
		r.roles[wid] = map[uuid.UUID]domain.Role{}
	}
	r.roles[wid][uid] = role
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), nil
}

func (r *fakeMemberRepo) ListMembers(
	_ context.Context,
	wid uuid.UUID,
	limit, offset int,
) ([]domain.Member, int64, error) {
	r.listCalls++
	r.lastLimit, r.lastOffset = limit, offset
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	var out []domain.Member
	for uid, role := range r.roles[wid] {
		ref := r.names[uid]
		out = append(out, domain.Member{UserID: uid, Name: ref.Name, Email: ref.Email, Role: role})
	}
	return out, int64(len(out)), nil
}

func (r *fakeMemberRepo) RemoveMember(_ context.Context, wid, uid, actor uuid.UUID) error {
	r.removeCalls++
	r.lastRemoved.wid, r.lastRemoved.uid, r.lastRemoved.actor = wid, uid, actor
	if r.removeErr != nil {
		return r.removeErr
	}
	if _, ok := r.roles[wid][uid]; !ok {
		return domain.ErrMemberNotFound
	}
	delete(r.roles[wid], uid)
	return nil
}

type fakeDirectory struct {
	byEmail map[string]domain.UserRef
	err     error
	calls   int
	lastQ   string
}

func (d *fakeDirectory) FindByEmail(_ context.Context, email string) (*domain.UserRef, error) {
	d.calls++
	d.lastQ = email
	if d.err != nil {
		return nil, d.err
	}
	ref, ok := d.byEmail[email]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return &ref, nil
}

// fixture: workspace dengan owner, admin, member; plus 'newbie' yang terdaftar tapi belum anggota.
type fixture struct {
	repo                                *fakeMemberRepo
	dir                                 *fakeDirectory
	uc                                  *usecase.MemberUseCase
	wid                                 uuid.UUID
	owner, admin, member, newbie, other uuid.UUID
}

func newFixture() *fixture {
	f := &fixture{
		repo:  newFakeMemberRepo(),
		owner: uuid.New(), admin: uuid.New(), member: uuid.New(),
		newbie: uuid.New(), other: uuid.New(),
	}
	f.wid = f.repo.seed(f.owner, f.admin, f.member)
	f.dir = &fakeDirectory{byEmail: map[string]domain.UserRef{
		"newbie@example.com": {ID: f.newbie, Name: "Newbie", Email: "newbie@example.com"},
		"member@example.com": {ID: f.member, Name: "Member", Email: "member@example.com"},
	}}
	f.uc = usecase.NewMemberUseCase(f.repo, f.dir, nil)
	return f
}

// ---------- Add ----------

func TestAdd_OwnerAddsMemberByDefault(t *testing.T) {
	f := newFixture()

	got, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if got.UserID != f.newbie || got.Role != domain.RoleMember {
		t.Errorf("hasil = %+v", got)
	}
	if got.Name != "Newbie" || got.Email != "newbie@example.com" {
		t.Errorf("profil tidak sesuai: %+v", got)
	}
	if got.JoinedAt.IsZero() {
		t.Error("JoinedAt harus terisi")
	}
	if f.repo.roles[f.wid][f.newbie] != domain.RoleMember {
		t.Error("keanggotaan harus tersimpan")
	}
}

func TestAdd_OwnerCanAddAdmin(t *testing.T) {
	f := newFixture()
	got, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", "admin")
	if err != nil || got.Role != domain.RoleAdmin {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
}

func TestAdd_AdminCanAddMemberButNotAdmin(t *testing.T) {
	f := newFixture()

	if _, err := f.uc.Add(ctx, f.admin.String(), f.wid.String(), "newbie@example.com", "admin"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Admin menambah Admin harus 403, dapat %v", err)
	}
	if f.repo.addCalls != 0 {
		t.Error("tidak boleh ada penyimpanan saat ditolak")
	}

	if _, err := f.uc.Add(ctx, f.admin.String(), f.wid.String(), "newbie@example.com", "member"); err != nil {
		t.Fatalf("Admin menambah Member harus berhasil: %v", err)
	}
}

func TestAdd_PlainMemberIsForbidden(t *testing.T) {
	f := newFixture()

	for _, role := range []string{"", "member", "admin"} {
		_, err := f.uc.Add(ctx, f.member.String(), f.wid.String(), "newbie@example.com", role)
		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("role %q: Member biasa harus 403, dapat %v", role, err)
		}
	}
	if f.repo.addCalls != 0 || f.dir.calls != 0 {
		t.Error("penolakan terjadi sebelum direktori user dan penyimpanan disentuh")
	}
}

func TestAdd_NonMemberGets404NotForbidden(t *testing.T) {
	f := newFixture()

	_, err := f.uc.Add(ctx, f.other.String(), f.wid.String(), "newbie@example.com", "member")

	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("non-anggota harus 404 (bukan 403), dapat %v", err)
	}
	if f.dir.calls != 0 || f.repo.addCalls != 0 {
		t.Error("tidak boleh ada pencarian user atau penyimpanan")
	}
}

func TestAdd_OwnerRoleCannotBeAssigned(t *testing.T) {
	f := newFixture()
	_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", "owner")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("role owner harus ditolak sebagai input tidak valid, dapat %v", err)
	}
}

func TestAdd_InvalidInput(t *testing.T) {
	f := newFixture()

	tests := []struct{ name, email, role string }{
		{"email kosong", "", "member"},
		{"email spasi saja", "   ", "member"},
		{"email lebih dari 255", strings.Repeat("a", 250) + "@x.com", "member"},
		{"role tidak dikenal", "newbie@example.com", "superuser"},
		{"role beda huruf besar", "newbie@example.com", "Admin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), tc.email, tc.role)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("harapannya ErrInvalidInput, dapat %v", err)
			}
			if f.repo.addCalls != 0 {
				t.Error("tidak boleh ada penyimpanan")
			}
		})
	}
}

func TestAdd_EmailIsNormalizedBeforeLookup(t *testing.T) {
	f := newFixture()

	if _, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "  NEWBIE@Example.COM ", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if f.dir.lastQ != "newbie@example.com" {
		t.Errorf("email dicari sebagai %q, harapannya huruf kecil dan di-trim", f.dir.lastQ)
	}
}

func TestAdd_UnknownEmailIsUserNotFound(t *testing.T) {
	f := newFixture()

	_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "tidakada@example.com", "member")

	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("harapannya ErrUserNotFound, dapat %v", err)
	}
	if f.repo.addCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

func TestAdd_AlreadyMemberIsConflict(t *testing.T) {
	f := newFixture()

	_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "member@example.com", "member")

	if !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("harapannya ErrAlreadyMember, dapat %v", err)
	}
}

func TestAdd_RepeatedAddIsConflictSecondTime(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", ""); err != nil {
		t.Fatal(err)
	}
	_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", "")
	if !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("penambahan kedua harus ErrAlreadyMember, dapat %v", err)
	}
}

func TestAdd_InvalidIDs(t *testing.T) {
	f := newFixture()

	if _, err := f.uc.Add(ctx, "bukan-uuid", f.wid.String(), "newbie@example.com", ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("user ID rusak: harapannya ErrUnauthenticated, dapat %v", err)
	}
	if _, err := f.uc.Add(ctx, f.owner.String(), "bukan-uuid", "newbie@example.com", ""); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("workspace ID rusak: harapannya ErrWorkspaceNotFound, dapat %v", err)
	}
	if f.repo.roleCalls != 0 && f.repo.addCalls != 0 {
		t.Error("repository tidak boleh dipanggil dengan ID rusak")
	}
}

func TestAdd_InfrastructureErrorsAreNotDisguised(t *testing.T) {
	boom := errors.New("db mati")

	t.Run("saat cek peran", func(t *testing.T) {
		f := newFixture()
		f.repo.roleErr = boom
		_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", "")
		if !errors.Is(err, boom) || errors.Is(err, domain.ErrWorkspaceNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("saat cari user", func(t *testing.T) {
		f := newFixture()
		f.dir.err = boom
		_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", "")
		if !errors.Is(err, boom) || errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("saat simpan", func(t *testing.T) {
		f := newFixture()
		f.repo.addErr = boom
		_, err := f.uc.Add(ctx, f.owner.String(), f.wid.String(), "newbie@example.com", "")
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAdd_CannotManageAnotherWorkspace(t *testing.T) {
	// Owner workspace A tidak punya hak apa pun di workspace B.
	f := newFixture()
	otherWID := f.repo.seed(uuid.New(), uuid.New(), uuid.New())

	_, err := f.uc.Add(ctx, f.owner.String(), otherWID.String(), "newbie@example.com", "member")

	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("harapannya ErrWorkspaceNotFound, dapat %v", err)
	}
}

// ---------- List ----------

func TestList_AnyMemberCanSee(t *testing.T) {
	f := newFixture()
	f.repo.names[f.owner] = domain.UserRef{Name: "Owner", Email: "owner@example.com"}

	for name, uid := range map[string]uuid.UUID{"owner": f.owner, "admin": f.admin, "member": f.member} {
		res, err := f.uc.List(ctx, uid.String(), f.wid.String(), 0, 0)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(res.Items) != 3 || res.Total != 3 {
			t.Errorf("%s harus melihat 3 anggota, dapat %d (total %d)", name, len(res.Items), res.Total)
		}
	}
}

func TestList_NonMemberGets404(t *testing.T) {
	f := newFixture()

	_, err := f.uc.List(ctx, f.other.String(), f.wid.String(), 0, 0)

	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("harapannya ErrWorkspaceNotFound, dapat %v", err)
	}
	if f.repo.listCalls != 0 {
		t.Error("daftar anggota tidak boleh dibaca untuk non-anggota")
	}
}

func TestMemberList_EmptyIsNonNilSlice(t *testing.T) {
	f := newFixture()
	f.repo.roles[f.wid] = map[uuid.UUID]domain.Role{f.owner: domain.RoleOwner}
	f.repo.listErr = nil
	// Paksa ListMembers mengembalikan nil dengan mengosongkan data setelah cek peran lolos.
	repo := &emptyListRepo{fakeMemberRepo: f.repo}
	uc := usecase.NewMemberUseCase(repo, f.dir, nil)

	res, err := uc.List(ctx, f.owner.String(), f.wid.String(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Items == nil || len(res.Items) != 0 {
		t.Errorf("harapannya slice kosong non-nil, dapat %#v", res.Items)
	}
}

type emptyListRepo struct{ *fakeMemberRepo }

func (emptyListRepo) ListMembers(context.Context, uuid.UUID, int, int) ([]domain.Member, int64, error) {
	return nil, 0, nil
}

func TestList_ErrorsPassedThrough(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.listErr = boom

	if _, err := f.uc.List(ctx, f.owner.String(), f.wid.String(), 0, 0); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

// ---------- Remove ----------

func TestRemove_OwnerRemovesAdminAndMember(t *testing.T) {
	f := newFixture()

	if err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.admin.String()); err != nil {
		t.Errorf("Owner menghapus Admin: %v", err)
	}
	if err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.member.String()); err != nil {
		t.Errorf("Owner menghapus Member: %v", err)
	}
	if _, ok := f.repo.roles[f.wid][f.admin]; ok {
		t.Error("Admin harus terhapus")
	}
	if _, ok := f.repo.roles[f.wid][f.member]; ok {
		t.Error("Member harus terhapus")
	}
}

func TestRemove_OwnerCannotBeRemovedByAnyone(t *testing.T) {
	f := newFixture()

	// Owner sendiri -> 409
	if err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.owner.String()); !errors.Is(err, domain.ErrCannotRemoveOwner) {
		t.Errorf("Owner menghapus dirinya: harapannya ErrCannotRemoveOwner, dapat %v", err)
	}
	// Admin dan Member -> 403
	for name, actor := range map[string]uuid.UUID{"admin": f.admin, "member": f.member} {
		if err := f.uc.Remove(ctx, actor.String(), f.wid.String(), f.owner.String()); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s menghapus Owner: harapannya ErrForbidden, dapat %v", name, err)
		}
	}
	if f.repo.removeCalls != 0 {
		t.Error("tidak boleh ada penghapusan")
	}
	if f.repo.roles[f.wid][f.owner] != domain.RoleOwner {
		t.Error("Owner harus tetap ada")
	}
}

func TestRemove_AdminRemovesMemberButNotOtherAdmin(t *testing.T) {
	f := newFixture()
	otherAdmin := uuid.New()
	f.repo.roles[f.wid][otherAdmin] = domain.RoleAdmin

	if err := f.uc.Remove(ctx, f.admin.String(), f.wid.String(), otherAdmin.String()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Admin menghapus Admin lain: harapannya ErrForbidden, dapat %v", err)
	}
	if err := f.uc.Remove(ctx, f.admin.String(), f.wid.String(), f.member.String()); err != nil {
		t.Errorf("Admin menghapus Member: %v", err)
	}
}

func TestRemove_MemberCanOnlyLeaveThemselves(t *testing.T) {
	f := newFixture()
	peer := uuid.New()
	f.repo.roles[f.wid][peer] = domain.RoleMember

	if err := f.uc.Remove(ctx, f.member.String(), f.wid.String(), peer.String()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Member menghapus rekan: harapannya ErrForbidden, dapat %v", err)
	}
	if err := f.uc.Remove(ctx, f.member.String(), f.wid.String(), f.member.String()); err != nil {
		t.Errorf("Member keluar sendiri harus berhasil: %v", err)
	}
	if _, ok := f.repo.roles[f.wid][f.member]; ok {
		t.Error("Member harus sudah keluar")
	}
}

func TestRemove_AdminCanLeave(t *testing.T) {
	f := newFixture()
	if err := f.uc.Remove(ctx, f.admin.String(), f.wid.String(), f.admin.String()); err != nil {
		t.Fatalf("Admin keluar sendiri: %v", err)
	}
}

func TestRemove_NonMemberGets404(t *testing.T) {
	f := newFixture()

	err := f.uc.Remove(ctx, f.other.String(), f.wid.String(), f.member.String())

	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("non-anggota harus 404, dapat %v", err)
	}
	if f.repo.removeCalls != 0 {
		t.Error("tidak boleh ada penghapusan")
	}
}

func TestRemove_TargetNotAMemberIsNotFound(t *testing.T) {
	f := newFixture()

	err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.newbie.String())

	if !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("harapannya ErrMemberNotFound, dapat %v", err)
	}
	if f.repo.removeCalls != 0 {
		t.Error("tidak boleh ada penghapusan")
	}
}

func TestRemove_InvalidTargetID(t *testing.T) {
	f := newFixture()

	err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), "bukan-uuid")

	if !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("harapannya ErrMemberNotFound, dapat %v", err)
	}
}

func TestRemove_InvalidActorOrWorkspaceID(t *testing.T) {
	f := newFixture()

	if err := f.uc.Remove(ctx, "x", f.wid.String(), f.member.String()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("actor rusak: dapat %v", err)
	}
	if err := f.uc.Remove(ctx, f.owner.String(), "x", f.member.String()); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("workspace rusak: dapat %v", err)
	}
}

func TestRemove_CannotRemoveFromAnotherWorkspace(t *testing.T) {
	// Owner A tidak bisa menghapus anggota workspace B, walau tahu ID-nya.
	f := newFixture()
	bOwner, bAdmin, bMember := uuid.New(), uuid.New(), uuid.New()
	bWID := f.repo.seed(bOwner, bAdmin, bMember)

	err := f.uc.Remove(ctx, f.owner.String(), bWID.String(), bMember.String())

	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("harapannya ErrWorkspaceNotFound, dapat %v", err)
	}
	if _, ok := f.repo.roles[bWID][bMember]; !ok {
		t.Error("anggota workspace B tidak boleh terhapus")
	}
}

func TestRemove_RemovesFromCorrectWorkspaceOnly(t *testing.T) {
	// User yang sama anggota di dua workspace: hanya satu yang berkurang.
	f := newFixture()
	otherWID := uuid.New()
	f.repo.roles[otherWID] = map[uuid.UUID]domain.Role{f.owner: domain.RoleOwner, f.member: domain.RoleMember}

	if err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.member.String()); err != nil {
		t.Fatal(err)
	}
	if f.repo.lastRemoved.wid != f.wid {
		t.Errorf("penghapusan di workspace %v, harapannya %v", f.repo.lastRemoved.wid, f.wid)
	}
	if f.repo.lastRemoved.actor != f.owner {
		t.Error("aktor (untuk riwayat pelepasan assignee) harus pemanggil")
	}
	if _, ok := f.repo.roles[otherWID][f.member]; !ok {
		t.Error("keanggotaan di workspace lain harus tetap ada")
	}
}

func TestRemove_ProjectOwnerCannotLeavePassedThrough(t *testing.T) {
	f := newFixture()
	f.repo.removeErr = domain.ErrMemberOwnsProjects

	err := f.uc.Remove(ctx, f.member.String(), f.wid.String(), f.member.String())

	if !errors.Is(err, domain.ErrMemberOwnsProjects) {
		t.Fatalf("harapannya ErrMemberOwnsProjects, dapat %v", err)
	}
}

func TestRemove_InfrastructureErrorsAreNotDisguised(t *testing.T) {
	boom := errors.New("db mati")

	t.Run("saat cek peran", func(t *testing.T) {
		f := newFixture()
		f.repo.roleErr = boom
		err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.member.String())
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("saat hapus", func(t *testing.T) {
		f := newFixture()
		f.repo.removeErr = boom
		err := f.uc.Remove(ctx, f.owner.String(), f.wid.String(), f.member.String())
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestMemberList_PaginationDefaultsAndValidation(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.List(ctx, f.owner.String(), f.wid.String(), 2, 5); err != nil ||
		f.repo.lastLimit != 5 || f.repo.lastOffset != 5 {
		t.Fatalf("err=%v limit=%d offset=%d", err, f.repo.lastLimit, f.repo.lastOffset)
	}
	before := f.repo.listCalls
	if _, err := f.uc.List(ctx, f.owner.String(), f.wid.String(), 1, 101); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("limit 101 harus ErrInvalidInput, dapat %v", err)
	}
	if f.repo.listCalls != before {
		t.Error("repository tidak boleh dipanggil untuk pagination tidak valid")
	}
}

// ---------- fake: ubah peran & transfer kepemilikan ----------

func (r *fakeMemberRepo) ChangeMemberRole(
	_ context.Context,
	wid, actor, target uuid.UUID,
	role domain.Role,
) (*domain.Member, error) {
	r.changeCalls++
	r.lastChange.wid, r.lastChange.actor, r.lastChange.target, r.lastChange.role = wid, actor, target, role
	if r.changeErr != nil {
		return nil, r.changeErr
	}
	if r.roles[wid][actor] != domain.RoleOwner {
		return nil, domain.ErrForbidden
	}
	cur, ok := r.roles[wid][target]
	if !ok {
		return nil, domain.ErrMemberNotFound
	}
	if cur == domain.RoleOwner {
		return nil, domain.ErrCannotChangeOwnerRole
	}
	r.roles[wid][target] = role
	ref := r.names[target]
	return &domain.Member{UserID: target, Name: ref.Name, Email: ref.Email, Role: role}, nil
}

func (r *fakeMemberRepo) TransferOwnership(_ context.Context, wid, actor, target uuid.UUID) error {
	r.transferCalls++
	r.lastTransfer.wid, r.lastTransfer.actor, r.lastTransfer.target = wid, actor, target
	if r.transferErr != nil {
		return r.transferErr
	}
	if r.roles[wid][actor] != domain.RoleOwner {
		return domain.ErrForbidden
	}
	if _, ok := r.roles[wid][target]; !ok {
		return domain.ErrMemberNotFound
	}
	r.roles[wid][target] = domain.RoleOwner
	r.roles[wid][actor] = domain.RoleAdmin
	return nil
}
