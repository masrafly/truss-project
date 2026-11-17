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

var ctx = context.Background()

// ---------- fake ----------

type fakeRepo struct {
	wsRoles  map[uuid.UUID]map[uuid.UUID]domain.WorkspaceRole // workspace -> user -> role
	projects map[uuid.UUID]domain.Project
	pRoles   map[uuid.UUID]map[uuid.UUID]domain.ProjectRole // project -> user -> role

	wsRoleErr, createErr, listErr, findErr, updateErr, deleteErr error
	// listRaw, bila diisi, dikembalikan apa adanya oleh ListAccessible (menguji saringan use case).
	listRaw []domain.ProjectAccess

	createCalls, listCalls, updateCalls, deleteCalls, wsRoleCalls int
	lastListWS                                                    *uuid.UUID
	lastUpdated                                                   domain.Project
	lastDeleted                                                   uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		wsRoles:  map[uuid.UUID]map[uuid.UUID]domain.WorkspaceRole{},
		projects: map[uuid.UUID]domain.Project{},
		pRoles:   map[uuid.UUID]map[uuid.UUID]domain.ProjectRole{},
	}
}

func (r *fakeRepo) access(pid, uid uuid.UUID) domain.Access {
	p := r.projects[pid]
	return domain.Access{WorkspaceRole: r.wsRoles[p.WorkspaceID][uid], ProjectRole: r.pRoles[pid][uid]}
}

func (r *fakeRepo) FindWorkspaceRole(_ context.Context, wid, uid uuid.UUID) (domain.WorkspaceRole, error) {
	r.wsRoleCalls++
	if r.wsRoleErr != nil {
		return "", r.wsRoleErr
	}
	role, ok := r.wsRoles[wid][uid]
	if !ok {
		return "", domain.ErrNotWorkspaceMember
	}
	return role, nil
}

func (r *fakeRepo) CreateWithOwner(_ context.Context, p *domain.Project) error {
	r.createCalls++
	if r.createErr != nil {
		return r.createErr
	}
	p.ID = uuid.New()
	p.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	p.UpdatedAt = p.CreatedAt
	r.projects[p.ID] = *p
	r.pRoles[p.ID] = map[uuid.UUID]domain.ProjectRole{p.CreatedBy: domain.ProjectOwner}
	return nil
}

func (r *fakeRepo) ListAccessible(_ context.Context, uid uuid.UUID, wid *uuid.UUID) ([]domain.ProjectAccess, error) {
	r.listCalls++
	r.lastListWS = wid
	if r.listErr != nil {
		return nil, r.listErr
	}
	if r.listRaw != nil {
		return r.listRaw, nil
	}
	var out []domain.ProjectAccess
	for id, p := range r.projects {
		if wid != nil && p.WorkspaceID != *wid {
			continue
		}
		a := r.access(id, uid)
		if a.WorkspaceRole == "" || (a.ProjectRole == "" && a.WorkspaceRole == domain.WorkspaceMember) {
			continue
		}
		out = append(out, domain.ProjectAccess{Project: p, Access: a})
	}
	return out, nil
}

func (r *fakeRepo) FindWithAccess(_ context.Context, pid, uid uuid.UUID) (*domain.ProjectAccess, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	p, ok := r.projects[pid]
	if !ok {
		return nil, domain.ErrProjectNotFound
	}
	return &domain.ProjectAccess{Project: p, Access: r.access(pid, uid)}, nil
}

func (r *fakeRepo) Update(_ context.Context, p *domain.Project) error {
	r.updateCalls++
	r.lastUpdated = *p
	if r.updateErr != nil {
		return r.updateErr
	}
	p.UpdatedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	r.projects[p.ID] = *p
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, pid uuid.UUID) error {
	r.deleteCalls++
	r.lastDeleted = pid
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.projects, pid)
	return nil
}

// ---------- fixture ----------

// Workspace dengan wsOwner, wsAdmin, wsMember (anggota project sebagai member),
// wsPlain (anggota workspace, bukan anggota project), pOwner (member workspace,
// Owner project), outsider (bukan anggota workspace). Satu project milik pOwner.
type fixture struct {
	repo                                                          *fakeRepo
	uc                                                            *usecase.ProjectUseCase
	wid, pid                                                      uuid.UUID
	wsOwner, wsAdmin, wsMember, wsPlain, pOwner, pAdmin, outsider uuid.UUID
}

func newFixture() *fixture {
	f := &fixture{
		repo: newFakeRepo(),
		wid:  uuid.New(), pid: uuid.New(),
		wsOwner: uuid.New(), wsAdmin: uuid.New(), wsMember: uuid.New(), wsPlain: uuid.New(),
		pOwner: uuid.New(), pAdmin: uuid.New(), outsider: uuid.New(),
	}
	f.repo.wsRoles[f.wid] = map[uuid.UUID]domain.WorkspaceRole{
		f.wsOwner: domain.WorkspaceOwner, f.wsAdmin: domain.WorkspaceAdmin,
		f.wsMember: domain.WorkspaceMember, f.wsPlain: domain.WorkspaceMember,
		f.pOwner: domain.WorkspaceMember, f.pAdmin: domain.WorkspaceMember,
	}
	desc := "awal"
	f.repo.projects[f.pid] = domain.Project{
		ID: f.pid, WorkspaceID: f.wid, Name: "Proyek", Description: &desc,
		Status: domain.StatusActive, CreatedBy: f.pOwner,
	}
	f.repo.pRoles[f.pid] = map[uuid.UUID]domain.ProjectRole{
		f.pOwner: domain.ProjectOwner, f.pAdmin: domain.ProjectAdmin, f.wsMember: domain.ProjectMember,
	}
	f.uc = usecase.NewProjectUseCase(f.repo, nil)
	return f
}

func strp(s string) *string { return &s }

// ---------- Create ----------

func TestCreate_WorkspaceOwnerAndAdminCanCreate(t *testing.T) {
	for name, pick := range map[string]func(*fixture) uuid.UUID{
		"owner": func(f *fixture) uuid.UUID { return f.wsOwner },
		"admin": func(f *fixture) uuid.UUID { return f.wsAdmin },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			uid := pick(f)

			got, err := f.uc.Create(ctx, uid.String(), f.wid.String(), "  Proyek Baru  ", strp("  isi  "))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if got.Name != "Proyek Baru" || got.Description == nil || *got.Description != "isi" {
				t.Errorf("input harus di-trim: %+v", got.Project)
			}
			if got.Status != domain.StatusActive {
				t.Errorf("status awal = %q, harapannya active", got.Status)
			}
			if got.CreatedBy != uid || got.WorkspaceID != f.wid {
				t.Errorf("pembuat/workspace salah: %+v", got.Project)
			}
			if got.ProjectRole != domain.ProjectOwner {
				t.Errorf("pembuat harus Owner project, dapat %q", got.ProjectRole)
			}
			if got.ID == uuid.Nil {
				t.Error("ID harus terisi")
			}
		})
	}
}

func TestCreate_WorkspaceMemberIsForbidden(t *testing.T) {
	f := newFixture()

	_, err := f.uc.Create(ctx, f.wsPlain.String(), f.wid.String(), "X", nil)

	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Member workspace harus 403, dapat %v", err)
	}
	if f.repo.createCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

func TestCreate_NonMemberGets404(t *testing.T) {
	f := newFixture()

	_, err := f.uc.Create(ctx, f.outsider.String(), f.wid.String(), "X", nil)

	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("non-anggota harus 404, dapat %v", err)
	}
	if f.repo.createCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

func TestCreate_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		pn   string
		desc *string
	}{
		{"nama kosong", "", nil},
		{"nama spasi", "   ", nil},
		{"nama 101 karakter", strings.Repeat("x", 101), nil},
		{"deskripsi 2001 karakter", "OK", strp(strings.Repeat("d", 2001))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			_, err := f.uc.Create(ctx, f.wsOwner.String(), f.wid.String(), tc.pn, tc.desc)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("harapannya ErrInvalidInput, dapat %v", err)
			}
			if f.repo.createCalls != 0 {
				t.Error("tidak boleh ada penyimpanan")
			}
		})
	}
}

func TestCreate_BoundaryLengthsAccepted(t *testing.T) {
	f := newFixture()
	// 100 karakter multibyte tetap valid (dihitung per rune, bukan byte).
	name := strings.Repeat("é", 100)
	if _, err := f.uc.Create(ctx, f.wsOwner.String(), f.wid.String(), name, strp(strings.Repeat("d", 2000))); err != nil {
		t.Fatalf("batas atas harus diterima: %v", err)
	}
}

func TestCreate_EmptyDescriptionStoredAsNil(t *testing.T) {
	f := newFixture()
	got, err := f.uc.Create(ctx, f.wsOwner.String(), f.wid.String(), "X", strp("   "))
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != nil {
		t.Errorf("deskripsi kosong harus nil, dapat %q", *got.Description)
	}
}

func TestCreate_InvalidIDs(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Create(ctx, "x", f.wid.String(), "X", nil); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("user ID rusak: dapat %v", err)
	}
	if _, err := f.uc.Create(ctx, f.wsOwner.String(), "x", "X", nil); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("workspace ID rusak: dapat %v", err)
	}
	if f.repo.wsRoleCalls != 0 {
		t.Error("repository tidak boleh dipanggil dengan ID rusak")
	}
}

func TestCreate_RepositoryErrorMapping(t *testing.T) {
	boom := errors.New("db mati")
	tests := []struct {
		name string
		repo error
		want error
	}{
		{"dikeluarkan dari workspace di tengah proses", domain.ErrNotWorkspaceMember, domain.ErrWorkspaceNotFound},
		{"user sudah tidak ada", domain.ErrUserNotFound, domain.ErrUnauthenticated},
		{"error infrastruktur", boom, boom},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			f.repo.createErr = tc.repo
			_, err := f.uc.Create(ctx, f.wsOwner.String(), f.wid.String(), "X", nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("dapat %v, harapannya %v", err, tc.want)
			}
		})
	}
}

func TestCreate_WorkspaceRoleInfraErrorNotDisguised(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.wsRoleErr = boom
	_, err := f.uc.Create(ctx, f.wsOwner.String(), f.wid.String(), "X", nil)
	if !errors.Is(err, boom) || errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// ---------- List ----------

func TestListInWorkspace_VisibilityPerRole(t *testing.T) {
	tests := []struct {
		name    string
		pick    func(*fixture) uuid.UUID
		visible bool
	}{
		{"owner workspace (bukan anggota project)", func(f *fixture) uuid.UUID { return f.wsOwner }, true},
		{"admin workspace (bukan anggota project)", func(f *fixture) uuid.UUID { return f.wsAdmin }, true},
		{"member workspace + anggota project", func(f *fixture) uuid.UUID { return f.wsMember }, true},
		{"member workspace saja", func(f *fixture) uuid.UUID { return f.wsPlain }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			got, err := f.uc.ListInWorkspace(ctx, tc.pick(f).String(), f.wid.String())
			if err != nil {
				t.Fatal(err)
			}
			if (len(got) == 1) != tc.visible {
				t.Errorf("visible = %v, dapat %d project", tc.visible, len(got))
			}
			if f.repo.lastListWS == nil || *f.repo.lastListWS != f.wid {
				t.Error("daftar harus dibatasi pada workspace yang diminta")
			}
		})
	}
}

func TestListInWorkspace_NonMemberGets404(t *testing.T) {
	f := newFixture()
	_, err := f.uc.ListInWorkspace(ctx, f.outsider.String(), f.wid.String())
	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("dapat %v", err)
	}
	if f.repo.listCalls != 0 {
		t.Error("daftar tidak boleh dibaca untuk non-anggota")
	}
}

func TestListAll_NoWorkspaceFilterAndNonNil(t *testing.T) {
	f := newFixture()
	got, err := f.uc.ListAll(ctx, f.outsider.String())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("harapannya slice kosong non-nil, dapat %#v", got)
	}
	if f.repo.lastListWS != nil {
		t.Error("ListAll tidak boleh memfilter workspace")
	}
	if _, err := f.uc.ListAll(ctx, "x"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("user ID rusak: dapat %v", err)
	}
}

func TestList_DefenseInDepthFiltersNonViewable(t *testing.T) {
	// Jika repository (keliru) mengembalikan project yang tidak boleh dilihat,
	// use case tetap menyaringnya.
	f := newFixture()
	p := f.repo.projects[f.pid]
	f.repo.listRaw = []domain.ProjectAccess{
		{Project: p, Access: domain.Access{WorkspaceRole: domain.WorkspaceMember}}, // tidak boleh
		{Project: p, Access: domain.Access{ProjectRole: domain.ProjectOwner}},      // bukan anggota workspace
		{Project: p, Access: domain.Access{WorkspaceRole: domain.WorkspaceAdmin}},  // boleh
	}
	got, err := f.uc.ListAll(ctx, f.wsPlain.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].WorkspaceRole != domain.WorkspaceAdmin {
		t.Errorf("hanya yang boleh dilihat yang lolos, dapat %+v", got)
	}
}

func TestList_ErrorsPassedThrough(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.listErr = boom
	if _, err := f.uc.ListAll(ctx, f.wsOwner.String()); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.uc.ListInWorkspace(ctx, f.wsOwner.String(), f.wid.String()); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
}

// ---------- Get ----------

func TestGet_Visibility(t *testing.T) {
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{
		"owner ws": f.wsOwner, "admin ws": f.wsAdmin, "anggota project": f.wsMember, "owner project": f.pOwner,
	} {
		got, err := f.uc.Get(ctx, uid.String(), f.pid.String())
		if err != nil {
			t.Errorf("%s harus bisa melihat: %v", name, err)
			continue
		}
		if got.ID != f.pid {
			t.Errorf("%s: project salah", name)
		}
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.wsPlain, "orang luar": f.outsider} {
		if _, err := f.uc.Get(ctx, uid.String(), f.pid.String()); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
}

func TestGet_StaleProjectMembershipWithoutWorkspaceGets404(t *testing.T) {
	f := newFixture()
	delete(f.repo.wsRoles[f.wid], f.wsMember) // masih anggota project, sudah bukan anggota workspace

	if _, err := f.uc.Get(ctx, f.wsMember.String(), f.pid.String()); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Fatalf("harapannya 404, dapat %v", err)
	}
}

func TestGet_InvalidIDsAndMissing(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Get(ctx, "x", f.pid.String()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.uc.Get(ctx, f.wsOwner.String(), "x"); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.uc.Get(ctx, f.wsOwner.String(), uuid.NewString()); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
	boom := errors.New("db mati")
	f.repo.findErr = boom
	if _, err := f.uc.Get(ctx, f.wsOwner.String(), f.pid.String()); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
}

// ---------- Update ----------

func TestUpdate_AllowedRoles(t *testing.T) {
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{
		"owner ws": f.wsOwner, "admin ws": f.wsAdmin, "owner project": f.pOwner, "admin project": f.pAdmin,
	} {
		if _, err := f.uc.Update(ctx, uid.String(), f.pid.String(), usecase.UpdateInput{Name: strp(name)}); err != nil {
			t.Errorf("%s harus boleh mengubah: %v", name, err)
		}
	}
}

func TestUpdate_ProjectMemberForbiddenAndOutsiders404(t *testing.T) {
	f := newFixture()
	in := usecase.UpdateInput{Name: strp("Diretas")}

	if _, err := f.uc.Update(ctx, f.wsMember.String(), f.pid.String(), in); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("anggota project biasa harus 403, dapat %v", err)
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.wsPlain, "orang luar": f.outsider} {
		if _, err := f.uc.Update(ctx, uid.String(), f.pid.String(), in); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if f.repo.updateCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
	if f.repo.projects[f.pid].Name != "Proyek" {
		t.Error("nama tidak boleh berubah")
	}
}

func TestUpdate_PartialFieldsAndNormalization(t *testing.T) {
	f := newFixture()

	got, err := f.uc.Update(ctx, f.pOwner.String(), f.pid.String(), usecase.UpdateInput{Status: strp("archived")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusArchived {
		t.Errorf("status = %q", got.Status)
	}
	if got.Name != "Proyek" || got.Description == nil || *got.Description != "awal" {
		t.Errorf("field yang tidak dikirim tidak boleh berubah: %+v", got.Project)
	}
	if got.ProjectRole != domain.ProjectOwner || got.WorkspaceRole != domain.WorkspaceMember {
		t.Errorf("akses harus dipertahankan pada respons: %+v", got.Access)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt harus diisi dari repository")
	}

	got, err = f.uc.Update(ctx, f.pOwner.String(), f.pid.String(), usecase.UpdateInput{
		Name: strp("  Nama Baru "), Description: strp("  "),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Nama Baru" || got.Description != nil {
		t.Errorf("nama di-trim dan deskripsi kosong menjadi nil: %+v", got.Project)
	}
	if f.repo.lastUpdated.ID != f.pid || f.repo.lastUpdated.WorkspaceID != f.wid || f.repo.lastUpdated.CreatedBy != f.pOwner {
		t.Errorf("field tetap (ID, workspace, pembuat) tidak boleh berubah: %+v", f.repo.lastUpdated)
	}
}

func TestUpdate_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		in   usecase.UpdateInput
	}{
		{"tidak ada field", usecase.UpdateInput{}},
		{"nama kosong", usecase.UpdateInput{Name: strp("  ")}},
		{"nama terlalu panjang", usecase.UpdateInput{Name: strp(strings.Repeat("x", 101))}},
		{"deskripsi terlalu panjang", usecase.UpdateInput{Description: strp(strings.Repeat("x", 2001))}},
		{"status tidak dikenal", usecase.UpdateInput{Status: strp("deleted")}},
		{"status huruf besar", usecase.UpdateInput{Status: strp("Active")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			if _, err := f.uc.Update(ctx, f.pOwner.String(), f.pid.String(), tc.in); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("harapannya ErrInvalidInput, dapat %v", err)
			}
			if f.repo.updateCalls != 0 {
				t.Error("tidak boleh ada penyimpanan")
			}
		})
	}
}

func TestUpdate_RepositoryErrorPassedThrough(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.updateErr = boom
	if _, err := f.uc.Update(ctx, f.pOwner.String(), f.pid.String(), usecase.UpdateInput{Name: strp("X")}); !errors.Is(err, boom) {
		t.Fatalf("dapat %v", err)
	}
}

// ---------- Delete ----------

func TestDelete_AllowedRoles(t *testing.T) {
	for name, pick := range map[string]func(*fixture) uuid.UUID{
		"owner ws":      func(f *fixture) uuid.UUID { return f.wsOwner },
		"owner project": func(f *fixture) uuid.UUID { return f.pOwner },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			if err := f.uc.Delete(ctx, pick(f).String(), f.pid.String()); err != nil {
				t.Fatal(err)
			}
			if f.repo.lastDeleted != f.pid {
				t.Error("project yang benar harus dihapus")
			}
		})
	}
}

func TestDelete_ForbiddenAndNotFound(t *testing.T) {
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{
		"admin ws": f.wsAdmin, "admin project": f.pAdmin, "member project": f.wsMember,
	} {
		if err := f.uc.Delete(ctx, uid.String(), f.pid.String()); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s harus 403, dapat %v", name, err)
		}
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.wsPlain, "orang luar": f.outsider} {
		if err := f.uc.Delete(ctx, uid.String(), f.pid.String()); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	if f.repo.deleteCalls != 0 {
		t.Error("tidak boleh ada penghapusan")
	}
}

func TestDelete_RepositoryErrorPassedThrough(t *testing.T) {
	f := newFixture()
	boom := errors.New("db mati")
	f.repo.deleteErr = boom
	if err := f.uc.Delete(ctx, f.pOwner.String(), f.pid.String()); !errors.Is(err, boom) {
		t.Fatalf("dapat %v", err)
	}
}
