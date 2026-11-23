package handler_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/modules/projects/handler"
	"truss-project/internal/platform/pagination"
)

type fakeMemberUC struct {
	addFn    func() (*domain.Member, error)
	listFn   func() ([]domain.Member, error)
	removeFn func() error

	calls                                              int
	gotPage, gotLimit                                  int
	gotActor, gotProject, gotEmail, gotRole, gotTarget string
}

func (f *fakeMemberUC) Add(_ context.Context, actor, pid, email, role string) (*domain.Member, error) {
	f.calls++
	f.gotActor, f.gotProject, f.gotEmail, f.gotRole = actor, pid, email, role
	return f.addFn()
}

func (f *fakeMemberUC) List(
	_ context.Context,
	actor, pid string,
	page, limit int,
) (*pagination.Result[domain.Member], error) {
	f.calls++
	f.gotActor, f.gotProject = actor, pid
	f.gotPage, f.gotLimit = page, limit
	items, err := f.listFn()
	if err != nil {
		return nil, err
	}
	return pagination.NewResult(items, int64(len(items)), pagination.Params{Page: 1, Limit: 20}), nil
}

func (f *fakeMemberUC) Remove(_ context.Context, actor, pid, target string) error {
	f.calls++
	f.gotActor, f.gotProject, f.gotTarget = actor, pid, target
	return f.removeFn()
}

var memberUserID = uuid.MustParse("99999999-8888-7777-6666-555555555555")

func sampleMember(role domain.ProjectRole) *domain.Member {
	return &domain.Member{
		UserID: memberUserID, Name: "Budi", Email: "budi@example.com",
		Role: role, JoinedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func newMemberRouter(uc *fakeMemberUC, log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	handler.NewMemberHandler(uc, log).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))
	return r
}

var membersPath = projectPath + "/members"

func TestMemberRoutesRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, ri := range newMemberRouter(&fakeMemberUC{}, nil).Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	want := []string{
		"POST /projects/:id/members", "GET /projects/:id/members", "DELETE /projects/:id/members/:userId",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("route %q harus terdaftar; ada %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("harapannya tepat %d route, dapat %v", len(want), got)
	}
}

// Route anggota dan route project harus bisa hidup berdampingan di satu router.
func TestMemberAndProjectRoutesCoexist(t *testing.T) {
	r := gin.New()
	mw := middleware.AuthMiddleware(fakeValidator{})
	handler.NewProjectHandler(&fakeUC{}, nil).RegisterRoutes(r, mw)
	handler.NewMemberHandler(&fakeMemberUC{}, nil).RegisterRoutes(r, mw)
	if n := len(r.Routes()); n != 9 {
		t.Errorf("harapannya 9 route, dapat %d", n)
	}
}

func TestMemberRoutesRequireAuth(t *testing.T) {
	uc := &fakeMemberUC{}
	r := newMemberRouter(uc, nil)
	for _, c := range []struct{ method, path, body string }{
		{"POST", membersPath, `{"email":"a@b.c"}`},
		{"GET", membersPath, ""},
		{"DELETE", membersPath + "/" + memberUserID.String(), ""},
	} {
		if w := do(r, c.method, c.path, c.body, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status %d", c.method, c.path, w.Code)
		}
	}
	if uc.calls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

func TestMemberAdd_Success(t *testing.T) {
	uc := &fakeMemberUC{addFn: func() (*domain.Member, error) { return sampleMember(domain.ProjectAdmin), nil }}

	w := do(newMemberRouter(uc, nil), "POST", membersPath, `{"email":"budi@example.com","role":"admin","user_id":"x"}`, authAs)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if uc.gotActor != "user-1" || uc.gotProject != projectID.String() || uc.gotEmail != "budi@example.com" || uc.gotRole != "admin" {
		t.Errorf("argumen salah: %+v", uc)
	}
	body := decode(t, w)
	data := body["data"].(map[string]any)
	want := map[string]any{
		"user_id": memberUserID.String(), "name": "Budi", "email": "budi@example.com",
		"role": "admin", "joined_at": "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data.%s = %v, harapannya %v", k, data[k], v)
		}
	}
	if body["message"] != "Anggota project berhasil ditambahkan" {
		t.Errorf("message = %v", body["message"])
	}
}

func TestMemberAdd_RoleOptional(t *testing.T) {
	uc := &fakeMemberUC{addFn: func() (*domain.Member, error) { return sampleMember(domain.ProjectMember), nil }}
	if w := do(newMemberRouter(uc, nil), "POST", membersPath, `{"email":"budi@example.com"}`, authAs); w.Code != http.StatusCreated {
		t.Fatalf("status %d", w.Code)
	}
	if uc.gotRole != "" {
		t.Errorf("role kosong diteruskan apa adanya (default ditentukan use case), dapat %q", uc.gotRole)
	}
}

func TestMemberAdd_BadBody(t *testing.T) {
	for _, body := range []string{"", "{", `{"role":"member"}`, `{"email":5}`} {
		uc := &fakeMemberUC{}
		if w := do(newMemberRouter(uc, nil), "POST", membersPath, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("body %q: use case tidak boleh dipanggil", body)
		}
	}
}

func TestMemberList_PassesPaginationAndReturnsMeta(t *testing.T) {
	uc := &fakeMemberUC{listFn: func() ([]domain.Member, error) { return nil, nil }}
	w := do(newMemberRouter(uc, nil), "GET", membersPath+"?page=3&limit=4", "", authAs)
	if w.Code != http.StatusOK || uc.gotPage != 3 || uc.gotLimit != 4 {
		t.Fatalf("status %d, page/limit = %d/%d", w.Code, uc.gotPage, uc.gotLimit)
	}
	if _, ok := decode(t, w)["meta"].(map[string]any); !ok {
		t.Errorf("respons harus memuat meta: %s", w.Body)
	}
}

func TestMemberList_NonNumericPaginationIs400(t *testing.T) {
	uc := &fakeMemberUC{}
	w := do(newMemberRouter(uc, nil), "GET", membersPath+"?limit=x", "", authAs)
	if w.Code != http.StatusBadRequest || uc.calls != 0 {
		t.Fatalf("status %d, calls %d", w.Code, uc.calls)
	}
}

func TestMemberList_EmptyArray(t *testing.T) {
	uc := &fakeMemberUC{listFn: func() ([]domain.Member, error) { return nil, nil }}
	w := do(newMemberRouter(uc, nil), "GET", membersPath, "", authAs)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
}

func TestMemberRemove_NoContentAndPassesTarget(t *testing.T) {
	uc := &fakeMemberUC{removeFn: func() error { return nil }}
	w := do(newMemberRouter(uc, nil), "DELETE", membersPath+"/"+memberUserID.String(), "", authAs)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("status %d body %q", w.Code, w.Body)
	}
	if uc.gotTarget != memberUserID.String() || uc.gotProject != projectID.String() {
		t.Errorf("argumen salah: target=%q project=%q", uc.gotTarget, uc.gotProject)
	}
}

func TestMemberErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"input", domain.ErrInvalidInput, http.StatusBadRequest},
		{"forbidden", domain.ErrForbidden, http.StatusForbidden},
		{"project tidak ada", domain.ErrProjectNotFound, http.StatusNotFound},
		{"user bukan anggota ws", domain.ErrWorkspaceUserNotFound, http.StatusNotFound},
		{"anggota tidak ada", domain.ErrMemberNotFound, http.StatusNotFound},
		{"sudah anggota", domain.ErrAlreadyMember, http.StatusConflict},
		{"owner tidak bisa dihapus", domain.ErrCannotRemoveOwner, http.StatusConflict},
		{"dibungkus", fmt.Errorf("x: %w", domain.ErrAlreadyMember), http.StatusConflict},
		{"tak terduga", errors.New("db mati"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeMemberUC{
				addFn:    func() (*domain.Member, error) { return nil, tc.err },
				listFn:   func() ([]domain.Member, error) { return nil, tc.err },
				removeFn: func() error { return tc.err },
			}
			r := newMemberRouter(uc, nil)
			for _, c := range []struct{ method, path, body string }{
				{"POST", membersPath, `{"email":"a@b.c"}`},
				{"GET", membersPath, ""},
				{"DELETE", membersPath + "/" + memberUserID.String(), ""},
			} {
				if w := do(r, c.method, c.path, c.body, authAs); w.Code != tc.want {
					t.Errorf("%s %s: status %d, harapannya %d", c.method, c.path, w.Code, tc.want)
				}
			}
		})
	}
}

func TestMemberExpectedErrorsNotLogged(t *testing.T) {
	for _, err := range []error{domain.ErrForbidden, domain.ErrAlreadyMember, domain.ErrCannotRemoveOwner, domain.ErrWorkspaceUserNotFound} {
		buf := &bytes.Buffer{}
		uc := &fakeMemberUC{removeFn: func() error { return err }}
		do(newMemberRouter(uc, slog.New(slog.NewTextHandler(buf, nil))), "DELETE", membersPath+"/"+memberUserID.String(), "", authAs)
		if buf.Len() != 0 {
			t.Errorf("%v tidak boleh dicatat: %s", err, buf)
		}
	}
}
