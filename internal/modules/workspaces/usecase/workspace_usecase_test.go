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

// ---------- fake repository ----------

type fakeRepo struct {
	workspaces map[uuid.UUID]*domain.Workspace
	members    map[uuid.UUID]map[uuid.UUID]domain.Role // workspaceID -> userID -> role

	createErr, listErr, findErr, roleErr, deleteErr error
	deleteCalls                                     int
	lastDeleted                                     struct{ wid, actor uuid.UUID }

	createCalls, listCalls, findCalls, roleCalls int
	lastCreated                                  *domain.Workspace
	lastLimit, lastOffset                        int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		workspaces: map[uuid.UUID]*domain.Workspace{},
		members:    map[uuid.UUID]map[uuid.UUID]domain.Role{},
	}
}

func (r *fakeRepo) CreateWithOwner(_ context.Context, w *domain.Workspace) error {
	r.createCalls++
	if r.createErr != nil {
		return r.createErr
	}
	w.ID = uuid.New()
	w.CreatedAt = time.Now()
	w.UpdatedAt = w.CreatedAt
	cp := *w
	r.workspaces[w.ID] = &cp
	r.members[w.ID] = map[uuid.UUID]domain.Role{w.CreatedBy: domain.RoleOwner}
	r.lastCreated = &cp
	return nil
}

func (r *fakeRepo) ListByMember(
	_ context.Context,
	userID uuid.UUID,
	limit, offset int,
) ([]domain.WorkspaceAccess, int64, error) {
	r.listCalls++
	r.lastLimit, r.lastOffset = limit, offset
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	var out []domain.WorkspaceAccess
	for wid, m := range r.members {
		if role, ok := m[userID]; ok {
			out = append(out, domain.WorkspaceAccess{Workspace: *r.workspaces[wid], Role: role})
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Workspace, error) {
	r.findCalls++
	if r.findErr != nil {
		return nil, r.findErr
	}
	w, ok := r.workspaces[id]
	if !ok {
		return nil, domain.ErrWorkspaceNotFound
	}
	cp := *w
	return &cp, nil
}

func (r *fakeRepo) FindMemberRole(_ context.Context, wid, uid uuid.UUID) (domain.Role, error) {
	r.roleCalls++
	if r.roleErr != nil {
		return "", r.roleErr
	}
	if role, ok := r.members[wid][uid]; ok {
		return role, nil
	}
	return "", domain.ErrNotMember
}

func (r *fakeRepo) Delete(_ context.Context, wid, actor uuid.UUID) error {
	r.deleteCalls++
	r.lastDeleted.wid, r.lastDeleted.actor = wid, actor
	if r.deleteErr != nil {
		return r.deleteErr
	}
	if r.members[wid][actor] != domain.RoleOwner {
		return domain.ErrForbidden
	}
	delete(r.workspaces, wid)
	delete(r.members, wid)
	return nil
}

func newUC(r *fakeRepo) *usecase.WorkspaceUseCase { return usecase.NewWorkspaceUseCase(r, nil) }

func strp(s string) *string { return &s }

var ctx = context.Background()

// ---------- Create ----------

func TestCreate_Success(t *testing.T) {
	repo := newFakeRepo()
	uid := uuid.New()

	got, err := newUC(repo).Create(ctx, uid.String(), "  Tim Backend ", strp("  Ruang kerja utama  "))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got.Role != domain.RoleOwner {
		t.Errorf("pembuat harus menjadi Owner, dapat %q", got.Role)
	}
	if got.Name != "Tim Backend" {
		t.Errorf("nama harus di-trim, dapat %q", got.Name)
	}
	if got.Description == nil || *got.Description != "Ruang kerja utama" {
		t.Errorf("deskripsi harus di-trim, dapat %v", got.Description)
	}
	if got.CreatedBy != uid {
		t.Errorf("CreatedBy = %v, harapannya %v", got.CreatedBy, uid)
	}
	if got.ID == uuid.Nil {
		t.Error("ID harus terisi")
	}
	if role := repo.members[got.ID][uid]; role != domain.RoleOwner {
		t.Errorf("keanggotaan Owner harus tersimpan, dapat %q", role)
	}
}

func TestCreate_CreatedByComesFromAuthenticatedUserOnly(t *testing.T) {
	repo := newFakeRepo()
	uid := uuid.New()

	_, err := newUC(repo).Create(ctx, uid.String(), "WS", nil)
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastCreated.CreatedBy != uid {
		t.Errorf("created_by harus dari identitas terautentikasi, dapat %v", repo.lastCreated.CreatedBy)
	}
}

func TestCreate_DescriptionNormalization(t *testing.T) {
	tests := []struct {
		name string
		in   *string
		want *string
	}{
		{"nil tetap nil", nil, nil},
		{"string kosong menjadi nil", strp(""), nil},
		{"spasi saja menjadi nil", strp("   \n\t "), nil},
		{"diisi", strp("halo"), strp("halo")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := newUC(newFakeRepo()).Create(ctx, uuid.NewString(), "WS", tc.in)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == nil && got.Description != nil:
				t.Errorf("harapannya nil, dapat %q", *got.Description)
			case tc.want != nil && (got.Description == nil || *got.Description != *tc.want):
				t.Errorf("deskripsi = %v, harapannya %q", got.Description, *tc.want)
			}
		})
	}
}

func TestCreate_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		ws   string
		desc *string
	}{
		{"nama kosong", "", nil},
		{"nama spasi saja", "   ", nil},
		{"nama 101 karakter", strings.Repeat("a", 101), nil},
		{"nama 101 karakter multibyte", strings.Repeat("é", 101), nil},
		{"deskripsi 2001 karakter", "WS", strp(strings.Repeat("d", 2001))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			_, err := newUC(repo).Create(ctx, uuid.NewString(), tc.ws, tc.desc)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("harapannya ErrInvalidInput, dapat %v", err)
			}
			if repo.createCalls != 0 {
				t.Error("repository tidak boleh dipanggil")
			}
		})
	}
}

func TestCreate_Boundaries(t *testing.T) {
	uc := newUC(newFakeRepo())

	if _, err := uc.Create(ctx, uuid.NewString(), strings.Repeat("a", 100), nil); err != nil {
		t.Errorf("nama 100 karakter harus valid: %v", err)
	}
	if _, err := uc.Create(ctx, uuid.NewString(), strings.Repeat("é", 100), nil); err != nil {
		t.Errorf("nama 100 karakter multibyte harus valid (dihitung per karakter): %v", err)
	}
	if _, err := uc.Create(ctx, uuid.NewString(), "WS", strp(strings.Repeat("d", 2000))); err != nil {
		t.Errorf("deskripsi 2000 karakter harus valid: %v", err)
	}
}

func TestCreate_InvalidUserID(t *testing.T) {
	repo := newFakeRepo()
	_, err := newUC(repo).Create(ctx, "bukan-uuid", "WS", nil)
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("harapannya ErrUnauthenticated, dapat %v", err)
	}
	if repo.createCalls != 0 {
		t.Error("repository tidak boleh dipanggil")
	}
}

func TestCreate_UserNoLongerExists(t *testing.T) {
	repo := newFakeRepo()
	repo.createErr = domain.ErrUserNotFound // FK created_by dilanggar

	_, err := newUC(repo).Create(ctx, uuid.NewString(), "WS", nil)
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("harapannya ErrUnauthenticated, dapat %v", err)
	}
}

func TestCreate_UnexpectedErrorIsPassedThrough(t *testing.T) {
	boom := errors.New("db mati")
	repo := newFakeRepo()
	repo.createErr = boom

	_, err := newUC(repo).Create(ctx, uuid.NewString(), "WS", nil)
	if !errors.Is(err, boom) {
		t.Fatalf("error harus diteruskan, dapat %v", err)
	}
}

// ---------- List ----------

func TestList_OnlyOwnWorkspaces(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo)
	alice, bob := uuid.New(), uuid.New()

	a1, _ := uc.Create(ctx, alice.String(), "Alice 1", nil)
	a2, _ := uc.Create(ctx, alice.String(), "Alice 2", nil)
	b1, _ := uc.Create(ctx, bob.String(), "Bob 1", nil)

	res, err := uc.List(ctx, alice.String(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	got := res.Items
	ids := map[uuid.UUID]bool{}
	for _, w := range got {
		ids[w.ID] = true
		if w.Role != domain.RoleOwner {
			t.Errorf("role = %q", w.Role)
		}
	}
	if len(got) != 2 || !ids[a1.ID] || !ids[a2.ID] {
		t.Errorf("Alice harus melihat tepat 2 workspace miliknya, dapat %d", len(got))
	}
	if ids[b1.ID] {
		t.Error("workspace Bob tidak boleh terlihat oleh Alice")
	}
}

func TestList_IncludesWorkspacesWhereUserIsPlainMember(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo)
	owner, member := uuid.New(), uuid.New()

	ws, _ := uc.Create(ctx, owner.String(), "Bersama", nil)
	repo.members[ws.ID][member] = domain.RoleMember

	res, _ := uc.List(ctx, member.String(), 0, 0)
	got := res.Items
	if len(got) != 1 || got[0].Role != domain.RoleMember {
		t.Fatalf("member harus melihat workspace dengan role member, dapat %+v", got)
	}
}

func TestList_EmptyIsNonNilSlice(t *testing.T) {
	res, err := newUC(newFakeRepo()).List(ctx, uuid.NewString(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Items == nil || len(res.Items) != 0 {
		t.Errorf("harapannya slice kosong non-nil (agar JSON menjadi []), dapat %#v", res.Items)
	}
}

func TestList_Errors(t *testing.T) {
	t.Run("user ID tidak valid", func(t *testing.T) {
		repo := newFakeRepo()
		_, err := newUC(repo).List(ctx, "x", 0, 0)
		if !errors.Is(err, domain.ErrUnauthenticated) || repo.listCalls != 0 {
			t.Fatalf("err = %v, calls = %d", err, repo.listCalls)
		}
	})
	t.Run("error repository diteruskan", func(t *testing.T) {
		boom := errors.New("db mati")
		repo := newFakeRepo()
		repo.listErr = boom
		_, err := newUC(repo).List(ctx, uuid.NewString(), 0, 0)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

// ---------- Get ----------

func TestGet_MemberCanRead(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo)
	owner := uuid.New()
	ws, _ := uc.Create(ctx, owner.String(), "WS", strp("desk"))

	got, err := uc.Get(ctx, owner.String(), ws.ID.String())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != ws.ID || got.Role != domain.RoleOwner || got.Name != "WS" {
		t.Errorf("hasil tidak sesuai: %+v", got)
	}
}

func TestGet_ReturnsCallersOwnRole(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo)
	owner, member := uuid.New(), uuid.New()
	ws, _ := uc.Create(ctx, owner.String(), "WS", nil)
	repo.members[ws.ID][member] = domain.RoleMember

	got, err := uc.Get(ctx, member.String(), ws.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != domain.RoleMember {
		t.Errorf("role harus milik peminta (member), dapat %q", got.Role)
	}
}

func TestGet_NonMemberGetsNotFound(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo)
	ws, _ := uc.Create(ctx, uuid.NewString(), "Rahasia", nil)

	_, err := uc.Get(ctx, uuid.NewString(), ws.ID.String()) // orang luar
	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("harapannya ErrWorkspaceNotFound (kebijakan 404), dapat %v", err)
	}
	if repo.findCalls != 0 {
		t.Error("data workspace tidak boleh dibaca sebelum keanggotaan terbukti")
	}
}

func TestGet_NonMemberAndNonexistentAreIndistinguishable(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo)
	ws, _ := uc.Create(ctx, uuid.NewString(), "Ada", nil)
	outsider := uuid.NewString()

	_, errExisting := uc.Get(ctx, outsider, ws.ID.String())
	_, errMissing := uc.Get(ctx, outsider, uuid.NewString())

	if errExisting == nil || errMissing == nil {
		t.Fatal("keduanya harus error")
	}
	if errExisting != errMissing {
		t.Errorf("error harus identik agar keberadaan workspace tidak bocor:\n%v\n%v", errExisting, errMissing)
	}
}

func TestGet_InvalidWorkspaceID(t *testing.T) {
	repo := newFakeRepo()
	_, err := newUC(repo).Get(ctx, uuid.NewString(), "bukan-uuid")
	if !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Fatalf("harapannya ErrWorkspaceNotFound, dapat %v", err)
	}
	if repo.roleCalls != 0 || repo.findCalls != 0 {
		t.Error("repository tidak boleh dipanggil untuk ID yang bukan UUID")
	}
}

func TestGet_InvalidUserID(t *testing.T) {
	repo := newFakeRepo()
	_, err := newUC(repo).Get(ctx, "x", uuid.NewString())
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("harapannya ErrUnauthenticated, dapat %v", err)
	}
}

func TestGet_UnexpectedErrorsArePassedThrough(t *testing.T) {
	boom := errors.New("db mati")

	t.Run("saat cek keanggotaan", func(t *testing.T) {
		repo := newFakeRepo()
		repo.roleErr = boom
		_, err := newUC(repo).Get(ctx, uuid.NewString(), uuid.NewString())
		if !errors.Is(err, boom) || errors.Is(err, domain.ErrWorkspaceNotFound) {
			t.Fatalf("error infrastruktur tidak boleh disamarkan jadi 404, dapat %v", err)
		}
	})

	t.Run("saat membaca workspace", func(t *testing.T) {
		repo := newFakeRepo()
		uc := newUC(repo)
		owner := uuid.New()
		ws, _ := uc.Create(ctx, owner.String(), "WS", nil)
		repo.findErr = boom

		_, err := uc.Get(ctx, owner.String(), ws.ID.String())
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGet_OneUserCannotReadAnotherUsersWorkspace(t *testing.T) {
	repo := newFakeRepo()
	uc := newUC(repo)
	alice, bob := uuid.New(), uuid.New()
	aliceWS, _ := uc.Create(ctx, alice.String(), "Alice", nil)
	bobWS, _ := uc.Create(ctx, bob.String(), "Bob", nil)

	if _, err := uc.Get(ctx, alice.String(), aliceWS.ID.String()); err != nil {
		t.Errorf("Alice harus bisa membaca miliknya: %v", err)
	}
	if _, err := uc.Get(ctx, alice.String(), bobWS.ID.String()); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("Alice tidak boleh membaca milik Bob, dapat %v", err)
	}
	if _, err := uc.Get(ctx, bob.String(), aliceWS.ID.String()); !errors.Is(err, domain.ErrWorkspaceNotFound) {
		t.Errorf("Bob tidak boleh membaca milik Alice, dapat %v", err)
	}
}

func TestList_PaginationDefaultsAndValidation(t *testing.T) {
	t.Run("default 20 per halaman dari offset 0", func(t *testing.T) {
		repo := newFakeRepo()
		res, err := newUC(repo).List(ctx, uuid.NewString(), 0, 0)
		if err != nil || repo.lastLimit != 20 || repo.lastOffset != 0 || res.Page != 1 || res.Limit != 20 {
			t.Fatalf("err=%v limit=%d offset=%d res=%+v", err, repo.lastLimit, repo.lastOffset, res)
		}
	})
	t.Run("offset dihitung dari page dan limit", func(t *testing.T) {
		repo := newFakeRepo()
		if _, err := newUC(repo).List(ctx, uuid.NewString(), 3, 10); err != nil ||
			repo.lastLimit != 10 || repo.lastOffset != 20 {
			t.Fatalf("err=%v limit=%d offset=%d", err, repo.lastLimit, repo.lastOffset)
		}
	})
	t.Run("input tidak valid ditolak tanpa menyentuh repository", func(t *testing.T) {
		for _, c := range [][2]int{{-1, 10}, {1, -1}, {1, 101}} {
			repo := newFakeRepo()
			_, err := newUC(repo).List(ctx, uuid.NewString(), c[0], c[1])
			if !errors.Is(err, domain.ErrInvalidInput) || repo.listCalls != 0 {
				t.Errorf("page=%d limit=%d: err=%v calls=%d", c[0], c[1], err, repo.listCalls)
			}
		}
	})
}
