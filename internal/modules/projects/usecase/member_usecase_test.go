package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/modules/projects/usecase"
)

// fakeMemberRepo memperluas fakeRepo (project_usecase_test.go) dengan operasi anggota.
type fakeMemberRepo struct {
	*fakeRepo
	users map[string]domain.UserRef // email -> user (seluruh aplikasi)

	lookupErr, roleErr, addErr, listMembersErr, removeErr error
	lookupCalls, addCalls, removeCalls, listMembersCalls  int
	lastLookup                                            string
	lastRemoved                                           struct{ pid, uid, actor uuid.UUID }
}

func (r *fakeMemberRepo) FindWorkspaceMemberByEmail(_ context.Context, pid uuid.UUID, email string) (*domain.UserRef, error) {
	r.lookupCalls++
	r.lastLookup = email
	if r.lookupErr != nil {
		return nil, r.lookupErr
	}
	ref, ok := r.users[email]
	if !ok {
		return nil, domain.ErrWorkspaceUserNotFound
	}
	if _, member := r.wsRoles[r.projects[pid].WorkspaceID][ref.ID]; !member {
		return nil, domain.ErrWorkspaceUserNotFound
	}
	return &ref, nil
}

func (r *fakeMemberRepo) FindMemberRole(_ context.Context, pid, uid uuid.UUID) (domain.ProjectRole, error) {
	if r.roleErr != nil {
		return "", r.roleErr
	}
	role, ok := r.pRoles[pid][uid]
	if !ok {
		return "", domain.ErrMemberNotFound
	}
	return role, nil
}

func (r *fakeMemberRepo) AddMember(_ context.Context, pid, uid uuid.UUID, role domain.ProjectRole) (time.Time, error) {
	r.addCalls++
	if r.addErr != nil {
		return time.Time{}, r.addErr
	}
	if _, ok := r.pRoles[pid][uid]; ok {
		return time.Time{}, domain.ErrAlreadyMember
	}
	r.pRoles[pid][uid] = role
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), nil
}

func (r *fakeMemberRepo) ListMembers(_ context.Context, pid uuid.UUID) ([]domain.Member, error) {
	r.listMembersCalls++
	if r.listMembersErr != nil {
		return nil, r.listMembersErr
	}
	var out []domain.Member
	for uid, role := range r.pRoles[pid] {
		out = append(out, domain.Member{UserID: uid, Role: role})
	}
	return out, nil
}

func (r *fakeMemberRepo) RemoveMember(_ context.Context, pid, uid, actor uuid.UUID) error {
	r.removeCalls++
	r.lastRemoved.pid, r.lastRemoved.uid, r.lastRemoved.actor = pid, uid, actor
	if r.removeErr != nil {
		return r.removeErr
	}
	if _, ok := r.pRoles[pid][uid]; !ok {
		return domain.ErrMemberNotFound
	}
	delete(r.pRoles[pid], uid)
	return nil
}

type memberFixture struct {
	*fixture
	mrepo *fakeMemberRepo
	muc   *usecase.MemberUseCase
	// candidate: anggota workspace, belum anggota project. stranger: terdaftar, bukan anggota workspace.
	candidate, stranger uuid.UUID
}

func newMemberFixture() *memberFixture {
	f := &memberFixture{fixture: newFixture(), candidate: uuid.New(), stranger: uuid.New()}
	f.repo.wsRoles[f.wid][f.candidate] = domain.WorkspaceMember
	f.mrepo = &fakeMemberRepo{fakeRepo: f.repo, users: map[string]domain.UserRef{
		"calon@example.com":  {ID: f.candidate, Name: "Calon", Email: "calon@example.com"},
		"asing@example.com":  {ID: f.stranger, Name: "Asing", Email: "asing@example.com"},
		"member@example.com": {ID: f.wsMember, Name: "Member", Email: "member@example.com"},
	}}
	f.muc = usecase.NewMemberUseCase(f.mrepo, nil)
	return f
}

// ---------- Add ----------

func TestMemberAdd_FullManagersCanAddAdminAndMember(t *testing.T) {
	for name, pick := range map[string]func(*memberFixture) uuid.UUID{
		"owner ws":      func(f *memberFixture) uuid.UUID { return f.wsOwner },
		"admin ws":      func(f *memberFixture) uuid.UUID { return f.wsAdmin },
		"owner project": func(f *memberFixture) uuid.UUID { return f.pOwner },
	} {
		for _, role := range []string{"admin", "member"} {
			t.Run(name+"/"+role, func(t *testing.T) {
				f := newMemberFixture()
				got, err := f.muc.Add(ctx, pick(f).String(), f.pid.String(), "calon@example.com", role)
				if err != nil {
					t.Fatalf("Add: %v", err)
				}
				if got.UserID != f.candidate || string(got.Role) != role || got.Name != "Calon" || got.JoinedAt.IsZero() {
					t.Errorf("hasil = %+v", got)
				}
				if string(f.repo.pRoles[f.pid][f.candidate]) != role {
					t.Error("keanggotaan harus tersimpan")
				}
			})
		}
	}
}

func TestMemberAdd_ProjectAdminOnlyMembers(t *testing.T) {
	f := newMemberFixture()
	if _, err := f.muc.Add(ctx, f.pAdmin.String(), f.pid.String(), "calon@example.com", "admin"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Admin project menambah Admin harus 403, dapat %v", err)
	}
	if f.mrepo.lookupCalls != 0 || f.mrepo.addCalls != 0 {
		t.Error("penolakan harus terjadi sebelum pencarian user dan penyimpanan")
	}
	got, err := f.muc.Add(ctx, f.pAdmin.String(), f.pid.String(), "calon@example.com", "")
	if err != nil || got.Role != domain.ProjectMember {
		t.Fatalf("Admin project menambah Member (default): got=%+v err=%v", got, err)
	}
}

func TestMemberAdd_ForbiddenAndNotFoundActors(t *testing.T) {
	f := newMemberFixture()
	if _, err := f.muc.Add(ctx, f.wsMember.String(), f.pid.String(), "calon@example.com", "member"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("anggota project biasa harus 403, dapat %v", err)
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.wsPlain, "orang luar": f.outsider} {
		if _, err := f.muc.Add(ctx, uid.String(), f.pid.String(), "calon@example.com", "member"); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if f.mrepo.lookupCalls != 0 || f.mrepo.addCalls != 0 {
		t.Error("tidak boleh ada pencarian user atau penyimpanan")
	}
}

func TestMemberAdd_TargetMustBeWorkspaceMember(t *testing.T) {
	f := newMemberFixture()
	for _, email := range []string{"asing@example.com", "tidakada@example.com"} {
		_, err := f.muc.Add(ctx, f.pOwner.String(), f.pid.String(), email, "member")
		if !errors.Is(err, domain.ErrWorkspaceUserNotFound) {
			t.Errorf("%s: harapannya ErrWorkspaceUserNotFound, dapat %v", email, err)
		}
	}
	if f.mrepo.addCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

func TestMemberAdd_InvalidInput(t *testing.T) {
	tests := []struct{ name, email, role string }{
		{"email kosong", "", "member"},
		{"email spasi", "  ", "member"},
		{"email terlalu panjang", strings.Repeat("a", 250) + "@x.com", "member"},
		{"role owner", "calon@example.com", "owner"},
		{"role tak dikenal", "calon@example.com", "superuser"},
		{"role huruf besar", "calon@example.com", "Member"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMemberFixture()
			if _, err := f.muc.Add(ctx, f.pOwner.String(), f.pid.String(), tc.email, tc.role); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("harapannya ErrInvalidInput, dapat %v", err)
			}
			if f.mrepo.addCalls != 0 {
				t.Error("tidak boleh ada penyimpanan")
			}
		})
	}
}

func TestMemberAdd_EmailNormalized(t *testing.T) {
	f := newMemberFixture()
	if _, err := f.muc.Add(ctx, f.pOwner.String(), f.pid.String(), "  CALON@Example.COM ", ""); err != nil {
		t.Fatal(err)
	}
	if f.mrepo.lastLookup != "calon@example.com" {
		t.Errorf("email dicari sebagai %q", f.mrepo.lastLookup)
	}
}

func TestMemberAdd_AlreadyMemberConflict(t *testing.T) {
	f := newMemberFixture()
	if _, err := f.muc.Add(ctx, f.pOwner.String(), f.pid.String(), "member@example.com", "member"); !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("harapannya ErrAlreadyMember, dapat %v", err)
	}
}

func TestMemberAdd_InvalidIDs(t *testing.T) {
	f := newMemberFixture()
	if _, err := f.muc.Add(ctx, "x", f.pid.String(), "calon@example.com", ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.muc.Add(ctx, f.pOwner.String(), "x", "calon@example.com", ""); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
}

func TestMemberAdd_InfrastructureErrorsNotDisguised(t *testing.T) {
	boom := errors.New("db mati")
	t.Run("cari user", func(t *testing.T) {
		f := newMemberFixture()
		f.mrepo.lookupErr = boom
		if _, err := f.muc.Add(ctx, f.pOwner.String(), f.pid.String(), "calon@example.com", ""); !errors.Is(err, boom) {
			t.Fatalf("dapat %v", err)
		}
	})
	t.Run("simpan", func(t *testing.T) {
		f := newMemberFixture()
		f.mrepo.addErr = boom
		if _, err := f.muc.Add(ctx, f.pOwner.String(), f.pid.String(), "calon@example.com", ""); !errors.Is(err, boom) {
			t.Fatalf("dapat %v", err)
		}
	})
	t.Run("muat project", func(t *testing.T) {
		f := newMemberFixture()
		f.repo.findErr = boom
		if _, err := f.muc.Add(ctx, f.pOwner.String(), f.pid.String(), "calon@example.com", ""); !errors.Is(err, boom) {
			t.Fatalf("dapat %v", err)
		}
	})
}

// ---------- List ----------

func TestMemberList_ViewersOnly(t *testing.T) {
	f := newMemberFixture()
	for name, uid := range map[string]uuid.UUID{
		"owner ws": f.wsOwner, "admin ws": f.wsAdmin, "owner project": f.pOwner, "member project": f.wsMember,
	} {
		got, err := f.muc.List(ctx, uid.String(), f.pid.String())
		if err != nil || len(got) != 3 {
			t.Errorf("%s: len=%d err=%v", name, len(got), err)
		}
	}
	calls := f.mrepo.listMembersCalls
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.wsPlain, "orang luar": f.outsider} {
		if _, err := f.muc.List(ctx, uid.String(), f.pid.String()); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if f.mrepo.listMembersCalls != calls {
		t.Error("daftar anggota tidak boleh dibaca untuk yang tak berhak")
	}
}

func TestMemberList_EmptyNonNilAndErrors(t *testing.T) {
	f := newMemberFixture()
	f.repo.pRoles[f.pid] = map[uuid.UUID]domain.ProjectRole{}
	got, err := f.muc.List(ctx, f.wsOwner.String(), f.pid.String())
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("harapannya slice kosong non-nil, dapat %#v err=%v", got, err)
	}
	boom := errors.New("db mati")
	f.mrepo.listMembersErr = boom
	if _, err := f.muc.List(ctx, f.wsOwner.String(), f.pid.String()); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
}

// ---------- Remove ----------

func TestMemberRemove_Allowed(t *testing.T) {
	tests := []struct {
		name          string
		actor, target func(*memberFixture) uuid.UUID
	}{
		{"owner ws hapus admin project", func(f *memberFixture) uuid.UUID { return f.wsOwner }, func(f *memberFixture) uuid.UUID { return f.pAdmin }},
		{"admin ws hapus admin project", func(f *memberFixture) uuid.UUID { return f.wsAdmin }, func(f *memberFixture) uuid.UUID { return f.pAdmin }},
		{"owner project hapus admin", func(f *memberFixture) uuid.UUID { return f.pOwner }, func(f *memberFixture) uuid.UUID { return f.pAdmin }},
		{"admin project hapus member", func(f *memberFixture) uuid.UUID { return f.pAdmin }, func(f *memberFixture) uuid.UUID { return f.wsMember }},
		{"member keluar sendiri", func(f *memberFixture) uuid.UUID { return f.wsMember }, func(f *memberFixture) uuid.UUID { return f.wsMember }},
		{"admin keluar sendiri", func(f *memberFixture) uuid.UUID { return f.pAdmin }, func(f *memberFixture) uuid.UUID { return f.pAdmin }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMemberFixture()
			target := tc.target(f)
			actor := tc.actor(f)
			if err := f.muc.Remove(ctx, actor.String(), f.pid.String(), target.String()); err != nil {
				t.Fatal(err)
			}
			if _, ok := f.repo.pRoles[f.pid][target]; ok {
				t.Error("target harus terhapus")
			}
			if f.mrepo.lastRemoved.pid != f.pid {
				t.Error("penghapusan di project yang salah")
			}
			if f.mrepo.lastRemoved.actor != actor {
				t.Error("aktor (untuk riwayat pelepasan assignee) harus pemanggil")
			}
		})
	}
}

func TestMemberRemove_Rejected(t *testing.T) {
	tests := []struct {
		name          string
		actor, target func(*memberFixture) uuid.UUID
		want          error
	}{
		{"admin project hapus admin lain", func(f *memberFixture) uuid.UUID { return f.pAdmin }, nil, domain.ErrForbidden},
		{"member hapus rekan", func(f *memberFixture) uuid.UUID { return f.wsMember }, func(f *memberFixture) uuid.UUID { return f.pAdmin }, domain.ErrForbidden},
		{"owner project keluar", func(f *memberFixture) uuid.UUID { return f.pOwner }, func(f *memberFixture) uuid.UUID { return f.pOwner }, domain.ErrCannotRemoveOwner},
		{"owner ws hapus owner project", func(f *memberFixture) uuid.UUID { return f.wsOwner }, func(f *memberFixture) uuid.UUID { return f.pOwner }, domain.ErrCannotRemoveOwner},
		{"admin project hapus owner", func(f *memberFixture) uuid.UUID { return f.pAdmin }, func(f *memberFixture) uuid.UUID { return f.pOwner }, domain.ErrForbidden},
		{"member ws saja", func(f *memberFixture) uuid.UUID { return f.wsPlain }, func(f *memberFixture) uuid.UUID { return f.wsMember }, domain.ErrProjectNotFound},
		{"orang luar", func(f *memberFixture) uuid.UUID { return f.outsider }, func(f *memberFixture) uuid.UUID { return f.wsMember }, domain.ErrProjectNotFound},
		{"target bukan anggota", func(f *memberFixture) uuid.UUID { return f.pOwner }, func(f *memberFixture) uuid.UUID { return f.candidate }, domain.ErrMemberNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMemberFixture()
			target := f.pAdmin
			if tc.target != nil {
				target = tc.target(f)
			} else {
				// admin project kedua sebagai target
				target = uuid.New()
				f.repo.wsRoles[f.wid][target] = domain.WorkspaceMember
				f.repo.pRoles[f.pid][target] = domain.ProjectAdmin
			}
			err := f.muc.Remove(ctx, tc.actor(f).String(), f.pid.String(), target.String())
			if !errors.Is(err, tc.want) {
				t.Fatalf("harapannya %v, dapat %v", tc.want, err)
			}
			if f.mrepo.removeCalls != 0 {
				t.Error("tidak boleh ada penghapusan")
			}
		})
	}
}

func TestMemberRemove_InvalidIDsAndErrors(t *testing.T) {
	f := newMemberFixture()
	if err := f.muc.Remove(ctx, f.pOwner.String(), f.pid.String(), "x"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("target rusak: dapat %v", err)
	}
	if err := f.muc.Remove(ctx, "x", f.pid.String(), f.wsMember.String()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("actor rusak: dapat %v", err)
	}
	if err := f.muc.Remove(ctx, f.pOwner.String(), "x", f.wsMember.String()); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("project rusak: dapat %v", err)
	}
	boom := errors.New("db mati")
	f.mrepo.roleErr = boom
	if err := f.muc.Remove(ctx, f.pOwner.String(), f.pid.String(), f.wsMember.String()); !errors.Is(err, boom) {
		t.Errorf("cek peran target: dapat %v", err)
	}
	f.mrepo.roleErr = nil
	f.mrepo.removeErr = boom
	if err := f.muc.Remove(ctx, f.pOwner.String(), f.pid.String(), f.wsMember.String()); !errors.Is(err, boom) {
		t.Errorf("hapus: dapat %v", err)
	}
}
