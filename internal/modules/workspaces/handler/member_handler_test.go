package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/modules/workspaces/handler"
	"truss-project/internal/platform/pagination"
)

// ---------- fake ----------

type fakeMemberUC struct {
	addFn    func(ctx context.Context, actorID, wsID, email, role string) (*domain.Member, error)
	listFn   func(ctx context.Context, actorID, wsID string) ([]domain.Member, error)
	removeFn func(ctx context.Context, actorID, wsID, targetID string) error

	addCalls, listCalls, removeCalls int
	gotActor, gotWS, gotEmail        string
	gotRole, gotTarget               string
	gotPage, gotLimit                int
}

func (f *fakeMemberUC) Add(ctx context.Context, actorID, wsID, email, role string) (*domain.Member, error) {
	f.addCalls++
	f.gotActor, f.gotWS, f.gotEmail, f.gotRole = actorID, wsID, email, role
	return f.addFn(ctx, actorID, wsID, email, role)
}

func (f *fakeMemberUC) List(
	ctx context.Context,
	actorID, wsID string,
	page, limit int,
) (*pagination.Result[domain.Member], error) {
	f.listCalls++
	f.gotActor, f.gotWS = actorID, wsID
	f.gotPage, f.gotLimit = page, limit
	items, err := f.listFn(ctx, actorID, wsID)
	if err != nil {
		return nil, err
	}
	return pagination.NewResult(items, int64(len(items)), pagination.Params{Page: 1, Limit: 20}), nil
}

func (f *fakeMemberUC) Remove(ctx context.Context, actorID, wsID, targetID string) error {
	f.removeCalls++
	f.gotActor, f.gotWS, f.gotTarget = actorID, wsID, targetID
	return f.removeFn(ctx, actorID, wsID, targetID)
}

var memberID = uuid.MustParse("99999999-8888-7777-6666-555555555555")

func sampleMember(role domain.Role) *domain.Member {
	return &domain.Member{
		UserID: memberID, Name: "Budi", Email: "budi@example.com",
		Role: role, JoinedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func newMemberRouter(uc *fakeMemberUC, log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	handler.NewMemberHandler(uc, log).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))
	return r
}

var membersPath = "/workspaces/" + wsID.String() + "/members"

// ---------- routes & auth ----------

func TestMemberRoutesRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, ri := range newMemberRouter(&fakeMemberUC{}, nil).Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{
		"POST /workspaces/:id/members",
		"GET /workspaces/:id/members",
		"DELETE /workspaces/:id/members/:userId",
	} {
		if !got[want] {
			t.Errorf("route %q harus terdaftar; ada: %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("harapannya tepat 3 route, dapat %v", got)
	}
}

func TestMemberRoutesCoexistWithWorkspaceRoutes(t *testing.T) {
	// Dua handler mendaftar di bawah /workspaces; gin tidak boleh panic karena konflik wildcard.
	r := gin.New()
	mw := middleware.AuthMiddleware(fakeValidator{})
	handler.NewWorkspaceHandler(&fakeUC{}, nil).RegisterRoutes(r, mw)
	handler.NewMemberHandler(&fakeMemberUC{}, nil).RegisterRoutes(r, mw)

	if n := len(r.Routes()); n != 6 {
		t.Errorf("harapannya 6 route (3 workspace + 3 anggota), dapat %d", n)
	}
}

func TestAllMemberRoutesRequireAuthentication(t *testing.T) {
	endpoints := []struct{ method, path, body string }{
		{http.MethodPost, membersPath, `{"email":"a@b.com"}`},
		{http.MethodGet, membersPath, ""},
		{http.MethodDelete, membersPath + "/" + memberID.String(), ""},
	}

	for _, ep := range endpoints {
		for name, header := range map[string]string{
			"tanpa header": "", "token salah": "Bearer salah", "bukan Bearer": "Basic valid-user-1",
		} {
			t.Run(ep.method+" "+name, func(t *testing.T) {
				uc := &fakeMemberUC{}
				w := do(newMemberRouter(uc, nil), ep.method, ep.path, ep.body, header)

				if w.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, harapannya 401", w.Code)
				}
				if uc.addCalls+uc.listCalls+uc.removeCalls != 0 {
					t.Error("use case tidak boleh dipanggil tanpa autentikasi")
				}
			})
		}
	}
}

func TestMemberHandlersRejectWhenMiddlewareMissing(t *testing.T) {
	uc := &fakeMemberUC{}
	h := handler.NewMemberHandler(uc, nil)
	r := gin.New()
	r.POST("/a", h.Add)
	r.GET("/l", h.List)
	r.DELETE("/r/:userId", h.Remove)

	for _, ep := range []struct{ m, p string }{{"POST", "/a"}, {"GET", "/l"}, {"DELETE", "/r/x"}} {
		if w := do(r, ep.m, ep.p, `{"email":"a@b.com"}`, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, harapannya 401", ep.m, ep.p, w.Code)
		}
	}
	if uc.addCalls+uc.listCalls+uc.removeCalls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

// ---------- Add ----------

func TestAddMember_Success(t *testing.T) {
	uc := &fakeMemberUC{addFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
		return sampleMember(domain.RoleAdmin), nil
	}}

	w := do(newMemberRouter(uc, nil), http.MethodPost, membersPath,
		`{"email":"budi@example.com","role":"admin"}`, "Bearer valid-actor-7")

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if uc.gotActor != "actor-7" || uc.gotWS != wsID.String() ||
		uc.gotEmail != "budi@example.com" || uc.gotRole != "admin" {
		t.Errorf("argumen use case = (%q, %q, %q, %q)", uc.gotActor, uc.gotWS, uc.gotEmail, uc.gotRole)
	}

	body := decode(t, w)
	if body["message"] != "Anggota berhasil ditambahkan" {
		t.Errorf("message = %v", body["message"])
	}
	data, _ := body["data"].(map[string]any)
	want := map[string]any{
		"user_id":   memberID.String(),
		"name":      "Budi",
		"email":     "budi@example.com",
		"role":      "admin",
		"joined_at": "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data[%q] = %v, harapannya %v", k, data[k], v)
		}
	}
	if len(data) != len(want) {
		t.Errorf("DTO harus tepat %d field, dapat %d: %v", len(want), len(data), data)
	}
	for _, leak := range []string{"password", "hash"} {
		if strings.Contains(strings.ToLower(w.Body.String()), leak) {
			t.Errorf("respons tidak boleh memuat %q", leak)
		}
	}
}

func TestAddMember_RoleDefaultsToEmptyAndIsLeftToUseCase(t *testing.T) {
	uc := &fakeMemberUC{addFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
		return sampleMember(domain.RoleMember), nil
	}}

	do(newMemberRouter(uc, nil), http.MethodPost, membersPath, `{"email":"budi@example.com"}`, "Bearer valid-a")

	if uc.gotRole != "" {
		t.Errorf("role yang tidak dikirim harus diteruskan kosong (default ditentukan use case), dapat %q", uc.gotRole)
	}
}

func TestAddMember_ActorComesFromTokenNotBody(t *testing.T) {
	uc := &fakeMemberUC{addFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
		return sampleMember(domain.RoleMember), nil
	}}

	do(newMemberRouter(uc, nil), http.MethodPost, membersPath,
		`{"email":"budi@example.com","actor_id":"palsu","user_id":"palsu","workspace_id":"palsu"}`,
		"Bearer valid-asli")

	if uc.gotActor != "asli" {
		t.Errorf("actor harus dari token, dapat %q", uc.gotActor)
	}
	if uc.gotWS != wsID.String() {
		t.Errorf("workspace harus dari path, dapat %q", uc.gotWS)
	}
}

func TestAddMember_InvalidBodyRejectedBeforeUseCase(t *testing.T) {
	tests := []struct{ name, body string }{
		{"bukan JSON", `xxx`},
		{"body kosong", ``},
		{"email hilang", `{"role":"member"}`},
		{"email kosong", `{"email":""}`},
		{"email tipe salah", `{"email":123}`},
		{"role tipe salah", `{"email":"a@b.com","role":5}`},
		{"array", `[]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeMemberUC{}
			w := do(newMemberRouter(uc, nil), http.MethodPost, membersPath, tc.body, "Bearer valid-a")

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, harapannya 400", w.Code)
			}
			if uc.addCalls != 0 {
				t.Error("use case tidak boleh dipanggil")
			}
		})
	}
}

// Semua error domain -> status HTTP, termasuk 403 dan 409 yang baru.
func TestMemberErrorMapping_AllEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"input tidak valid", domain.ErrInvalidInput, http.StatusBadRequest},
		{"tidak terautentikasi", domain.ErrUnauthenticated, http.StatusUnauthorized},
		{"peran tidak cukup", domain.ErrForbidden, http.StatusForbidden},
		{"workspace tidak ada / non-anggota", domain.ErrWorkspaceNotFound, http.StatusNotFound},
		{"user tidak terdaftar", domain.ErrUserNotFound, http.StatusNotFound},
		{"anggota tidak ada", domain.ErrMemberNotFound, http.StatusNotFound},
		{"sudah anggota", domain.ErrAlreadyMember, http.StatusConflict},
		{"owner tidak bisa dihapus", domain.ErrCannotRemoveOwner, http.StatusConflict},
		{"masih owner project", domain.ErrMemberOwnsProjects, http.StatusConflict},
		{"error dibungkus", fmt.Errorf("x: %w", domain.ErrForbidden), http.StatusForbidden},
		{"error tak terduga", errors.New("db mati"), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		uc := &fakeMemberUC{
			addFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
				return nil, tc.err
			},
			listFn:   func(context.Context, string, string) ([]domain.Member, error) { return nil, tc.err },
			removeFn: func(context.Context, string, string, string) error { return tc.err },
		}
		r := newMemberRouter(uc, nil)

		t.Run("POST "+tc.name, func(t *testing.T) {
			w := do(r, http.MethodPost, membersPath, `{"email":"a@b.com"}`, "Bearer valid-a")
			if w.Code != tc.status {
				t.Fatalf("status = %d, harapannya %d", w.Code, tc.status)
			}
		})
		t.Run("GET "+tc.name, func(t *testing.T) {
			w := do(r, http.MethodGet, membersPath, "", "Bearer valid-a")
			if w.Code != tc.status {
				t.Fatalf("status = %d, harapannya %d", w.Code, tc.status)
			}
		})
		t.Run("DELETE "+tc.name, func(t *testing.T) {
			w := do(r, http.MethodDelete, membersPath+"/"+memberID.String(), "", "Bearer valid-a")
			if w.Code != tc.status {
				t.Fatalf("status = %d, harapannya %d", w.Code, tc.status)
			}
		})
	}
}

// Kebijakan inti: 403 hanya untuk anggota tanpa izin, 404 untuk non-anggota.
func TestForbiddenAndNotFoundAreDistinctStatuses(t *testing.T) {
	forbidden := &fakeMemberUC{addFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
		return nil, domain.ErrForbidden
	}}
	notFound := &fakeMemberUC{addFn: func(context.Context, string, string, string, string) (*domain.Member, error) {
		return nil, domain.ErrWorkspaceNotFound
	}}

	a := do(newMemberRouter(forbidden, nil), http.MethodPost, membersPath, `{"email":"a@b.com"}`, "Bearer valid-a")
	b := do(newMemberRouter(notFound, nil), http.MethodPost, membersPath, `{"email":"a@b.com"}`, "Bearer valid-a")

	if a.Code != http.StatusForbidden || b.Code != http.StatusNotFound {
		t.Fatalf("status = %d dan %d, harapannya 403 dan 404", a.Code, b.Code)
	}
}

// ---------- List ----------

func TestListMembers_PassesPaginationAndReturnsMeta(t *testing.T) {
	uc := &fakeMemberUC{listFn: func(context.Context, string, string) ([]domain.Member, error) {
		return []domain.Member{*sampleMember(domain.RoleOwner)}, nil
	}}

	w := do(newMemberRouter(uc, nil), http.MethodGet, membersPath+"?page=2&limit=5", "", "Bearer valid-a")

	if w.Code != http.StatusOK || uc.gotPage != 2 || uc.gotLimit != 5 {
		t.Fatalf("status = %d, page/limit = %d/%d", w.Code, uc.gotPage, uc.gotLimit)
	}
	if _, ok := decode(t, w)["meta"].(map[string]any); !ok {
		t.Errorf("respons harus memuat meta: %s", w.Body.String())
	}
}

func TestListMembers_NonNumericPaginationIs400(t *testing.T) {
	uc := &fakeMemberUC{}
	w := do(newMemberRouter(uc, nil), http.MethodGet, membersPath+"?limit=x", "", "Bearer valid-a")

	if w.Code != http.StatusBadRequest || uc.listCalls != 0 {
		t.Fatalf("status = %d, listCalls = %d", w.Code, uc.listCalls)
	}
}

func TestListMembers_Success(t *testing.T) {
	uc := &fakeMemberUC{listFn: func(context.Context, string, string) ([]domain.Member, error) {
		return []domain.Member{*sampleMember(domain.RoleOwner), *sampleMember(domain.RoleMember)}, nil
	}}

	w := do(newMemberRouter(uc, nil), http.MethodGet, membersPath, "", "Bearer valid-viewer")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if uc.gotActor != "viewer" || uc.gotWS != wsID.String() {
		t.Errorf("argumen = (%q, %q)", uc.gotActor, uc.gotWS)
	}
	data, _ := decode(t, w)["data"].([]any)
	if len(data) != 2 || data[0].(map[string]any)["role"] != "owner" || data[1].(map[string]any)["role"] != "member" {
		t.Errorf("data tidak sesuai: %v", data)
	}
}

func TestListMembers_EmptyIsJSONArrayNotNull(t *testing.T) {
	for name, ret := range map[string][]domain.Member{"nil": nil, "kosong": {}} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeMemberUC{listFn: func(context.Context, string, string) ([]domain.Member, error) { return ret, nil }}
			w := do(newMemberRouter(uc, nil), http.MethodGet, membersPath, "", "Bearer valid-a")

			if !bytes.Contains(w.Body.Bytes(), []byte(`"data":[]`)) {
				t.Errorf("data harus berupa [] bukan null: %s", w.Body.String())
			}
		})
	}
}

// ---------- Remove ----------

func TestRemoveMember_SuccessReturns204WithEmptyBody(t *testing.T) {
	uc := &fakeMemberUC{removeFn: func(context.Context, string, string, string) error { return nil }}

	w := do(newMemberRouter(uc, nil), http.MethodDelete, membersPath+"/"+memberID.String(), "", "Bearer valid-actor-3")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, harapannya 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body harus kosong, dapat %q", w.Body.String())
	}
	if uc.gotActor != "actor-3" || uc.gotWS != wsID.String() || uc.gotTarget != memberID.String() {
		t.Errorf("argumen = (%q, %q, %q)", uc.gotActor, uc.gotWS, uc.gotTarget)
	}
}

func TestRemoveMember_PathParamsAreNotConfused(t *testing.T) {
	// :id (workspace) dan :userId (target) harus tidak tertukar.
	uc := &fakeMemberUC{removeFn: func(context.Context, string, string, string) error { return nil }}
	other := uuid.New().String()

	do(newMemberRouter(uc, nil), http.MethodDelete, "/workspaces/"+other+"/members/"+memberID.String(), "", "Bearer valid-a")

	if uc.gotWS != other || uc.gotTarget != memberID.String() {
		t.Errorf("workspace = %q, target = %q", uc.gotWS, uc.gotTarget)
	}
}

// ---------- logging ----------

func TestMemberUnexpectedError_GenericBodyAndLogged(t *testing.T) {
	uc := &fakeMemberUC{removeFn: func(context.Context, string, string, string) error {
		return errors.New("connection refused: db://user:pw@10.0.0.5")
	}}
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, nil))

	req := httptest.NewRequest(http.MethodDelete, membersPath+"/"+memberID.String(), nil)
	req.Header.Set("Authorization", "Bearer valid-a")
	req.Header.Set("X-Request-ID", "rid-mem")
	w := httptest.NewRecorder()
	newMemberRouter(uc, log).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	if msg := decode(t, w)["message"]; msg != "Terjadi kesalahan pada server" {
		t.Errorf("message = %v", msg)
	}
	if strings.Contains(w.Body.String(), "db://") {
		t.Errorf("detail internal bocor: %s", w.Body.String())
	}

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log harus satu baris JSON: %v\n%s", err, buf.String())
	}
	if rec["level"] != "ERROR" || rec["request_id"] != "rid-mem" {
		t.Errorf("rekaman log tidak sesuai: %v", rec)
	}
}

func TestMemberExpectedErrors_NotLoggedByHandler(t *testing.T) {
	for _, err := range []error{domain.ErrForbidden, domain.ErrAlreadyMember, domain.ErrCannotRemoveOwner, domain.ErrMemberNotFound, domain.ErrMemberOwnsProjects} {
		uc := &fakeMemberUC{removeFn: func(context.Context, string, string, string) error { return err }}
		buf := &bytes.Buffer{}

		do(newMemberRouter(uc, slog.New(slog.NewJSONHandler(buf, nil))),
			http.MethodDelete, membersPath+"/"+memberID.String(), "", "Bearer valid-a")

		if strings.TrimSpace(buf.String()) != "" {
			t.Errorf("%v: handler tidak boleh mencatat error yang diharapkan: %s", err, buf.String())
		}
	}
}
