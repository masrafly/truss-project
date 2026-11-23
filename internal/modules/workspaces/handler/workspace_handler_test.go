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

func init() { gin.SetMode(gin.TestMode) }

// ---------- fakes ----------

type fakeUC struct {
	createFn func(ctx context.Context, userID, name string, desc *string) (*domain.WorkspaceAccess, error)
	listFn   func(ctx context.Context, userID string) ([]domain.WorkspaceAccess, error)
	getFn    func(ctx context.Context, userID, wsID string) (*domain.WorkspaceAccess, error)

	createCalls, listCalls, getCalls int
	gotUserID, gotName, gotWSID      string
	gotDesc                          *string
	gotPage, gotLimit                int
}

func (f *fakeUC) Create(ctx context.Context, userID, name string, desc *string) (*domain.WorkspaceAccess, error) {
	f.createCalls++
	f.gotUserID, f.gotName, f.gotDesc = userID, name, desc
	return f.createFn(ctx, userID, name, desc)
}

func (f *fakeUC) List(
	ctx context.Context,
	userID string,
	page, limit int,
) (*pagination.Result[domain.WorkspaceAccess], error) {
	f.listCalls++
	f.gotUserID = userID
	f.gotPage, f.gotLimit = page, limit
	items, err := f.listFn(ctx, userID)
	if err != nil {
		return nil, err
	}
	return pagination.NewResult(items, int64(len(items)), pagination.Params{Page: 1, Limit: 20}), nil
}

func (f *fakeUC) Get(ctx context.Context, userID, wsID string) (*domain.WorkspaceAccess, error) {
	f.getCalls++
	f.gotUserID, f.gotWSID = userID, wsID
	return f.getFn(ctx, userID, wsID)
}

type fakeValidator struct{}

func (fakeValidator) Validate(token string) (string, error) {
	if strings.HasPrefix(token, "valid-") {
		return strings.TrimPrefix(token, "valid-"), nil
	}
	return "", errors.New("invalid")
}

var (
	fixedTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	wsID      = uuid.MustParse("11111111-2222-3333-4444-555555555555")
	creatorID = uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
)

func sample(role domain.Role) *domain.WorkspaceAccess {
	desc := "deskripsi"
	return &domain.WorkspaceAccess{
		Workspace: domain.Workspace{
			ID: wsID, Name: "Tim Backend", Description: &desc,
			CreatedBy: creatorID, CreatedAt: fixedTime, UpdatedAt: fixedTime,
		},
		Role: role,
	}
}

// ---------- helpers ----------

func newRouter(uc *fakeUC, log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	handler.NewWorkspaceHandler(uc, log).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))
	return r
}

func do(r http.Handler, method, path, body, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const authAs = "Bearer valid-user-1"

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body bukan satu objek JSON valid: %v\n%s", err, w.Body.String())
	}
	return m
}

// ---------- routes & auth ----------

func TestRoutesRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, ri := range newRouter(&fakeUC{}, nil).Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{"POST /workspaces", "GET /workspaces", "GET /workspaces/:id"} {
		if !got[want] {
			t.Errorf("route %q harus terdaftar; ada: %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("harapannya tepat 3 route, dapat %v", got)
	}
}

func TestAllRoutesRequireAuthentication(t *testing.T) {
	endpoints := []struct{ method, path, body string }{
		{http.MethodPost, "/workspaces", `{"name":"WS"}`},
		{http.MethodGet, "/workspaces", ""},
		{http.MethodGet, "/workspaces/" + wsID.String(), ""},
	}

	for _, ep := range endpoints {
		for name, header := range map[string]string{
			"tanpa header": "",
			"token salah":  "Bearer salah",
			"bukan Bearer": "Basic valid-user-1",
		} {
			t.Run(ep.method+" "+ep.path+" "+name, func(t *testing.T) {
				uc := &fakeUC{}
				w := do(newRouter(uc, nil), ep.method, ep.path, ep.body, header)

				if w.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, harapannya 401", w.Code)
				}
				if uc.createCalls+uc.listCalls+uc.getCalls != 0 {
					t.Error("use case tidak boleh dipanggil tanpa autentikasi")
				}
			})
		}
	}
}

func TestHandlersRejectWhenMiddlewareMissing(t *testing.T) {
	// Pertahanan berlapis: tanpa AuthMiddleware pun handler menolak.
	uc := &fakeUC{}
	h := handler.NewWorkspaceHandler(uc, nil)
	r := gin.New()
	r.POST("/c", h.Create)
	r.GET("/l", h.List)
	r.GET("/g/:id", h.Get)

	for _, ep := range []struct{ m, p string }{{"POST", "/c"}, {"GET", "/l"}, {"GET", "/g/x"}} {
		if w := do(r, ep.m, ep.p, `{"name":"WS"}`, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, harapannya 401", ep.m, ep.p, w.Code)
		}
	}
	if uc.createCalls+uc.listCalls+uc.getCalls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

// ---------- Create ----------

func TestCreate_Success(t *testing.T) {
	uc := &fakeUC{createFn: func(context.Context, string, string, *string) (*domain.WorkspaceAccess, error) {
		return sample(domain.RoleOwner), nil
	}}

	w := do(newRouter(uc, nil), http.MethodPost, "/workspaces",
		`{"name":"Tim Backend","description":"deskripsi"}`, authAs)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}

	body := decode(t, w)
	if body["message"] != "Workspace berhasil dibuat" {
		t.Errorf("message = %v", body["message"])
	}
	data, _ := body["data"].(map[string]any)
	want := map[string]any{
		"internal_id": wsID.String(),
		"name":        "Tim Backend",
		"description": "deskripsi",
		"created_by":  creatorID.String(),
		"role":        "owner",
		"created_at":  "2026-01-02T03:04:05Z",
		"updated_at":  "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data[%q] = %v, harapannya %v", k, data[k], v)
		}
	}
	if len(data) != len(want) {
		t.Errorf("DTO harus tepat %d field, dapat %d: %v", len(want), len(data), data)
	}
}

func TestCreate_UserIDComesFromTokenNotBody(t *testing.T) {
	uc := &fakeUC{createFn: func(context.Context, string, string, *string) (*domain.WorkspaceAccess, error) {
		return sample(domain.RoleOwner), nil
	}}

	// Client mencoba memalsukan created_by, role, dan id.
	w := do(newRouter(uc, nil), http.MethodPost, "/workspaces",
		`{"name":"WS","created_by":"00000000-0000-0000-0000-000000000000","role":"admin","internal_id":"x"}`,
		"Bearer valid-user-real")

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}
	if uc.gotUserID != "user-real" {
		t.Errorf("use case harus menerima user ID dari token, dapat %q", uc.gotUserID)
	}
}

func TestCreate_PassesNameAndDescriptionToUseCase(t *testing.T) {
	uc := &fakeUC{createFn: func(context.Context, string, string, *string) (*domain.WorkspaceAccess, error) {
		return sample(domain.RoleOwner), nil
	}}
	r := newRouter(uc, nil)

	do(r, http.MethodPost, "/workspaces", `{"name":"  WS  ","description":"d"}`, authAs)
	if uc.gotName != "  WS  " || uc.gotDesc == nil || *uc.gotDesc != "d" {
		t.Errorf("argumen = (%q, %v)", uc.gotName, uc.gotDesc)
	}

	do(r, http.MethodPost, "/workspaces", `{"name":"WS"}`, authAs)
	if uc.gotDesc != nil {
		t.Errorf("description yang tidak dikirim harus nil, dapat %q", *uc.gotDesc)
	}
}

func TestCreate_InvalidBodyRejectedBeforeUseCase(t *testing.T) {
	tests := []struct{ name, body string }{
		{"bukan JSON", `xxx`},
		{"body kosong", ``},
		{"JSON rusak", `{"name":`},
		{"name hilang", `{"description":"d"}`},
		{"name kosong", `{"name":""}`},
		{"name tipe salah", `{"name":123}`},
		{"description tipe salah", `{"name":"WS","description":123}`},
		{"array", `[]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUC{}
			w := do(newRouter(uc, nil), http.MethodPost, "/workspaces", tc.body, authAs)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, harapannya 400", w.Code)
			}
			if uc.createCalls != 0 {
				t.Error("use case tidak boleh dipanggil")
			}
		})
	}
}

func TestCreate_ErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"input tidak valid", domain.ErrInvalidInput, http.StatusBadRequest},
		{"identitas tidak valid", domain.ErrUnauthenticated, http.StatusUnauthorized},
		{"workspace tidak ada", domain.ErrWorkspaceNotFound, http.StatusNotFound},
		{"error dibungkus", fmt.Errorf("x: %w", domain.ErrInvalidInput), http.StatusBadRequest},
		{"error tak terduga", errors.New("db mati"), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUC{createFn: func(context.Context, string, string, *string) (*domain.WorkspaceAccess, error) {
				return nil, tc.err
			}}
			w := do(newRouter(uc, nil), http.MethodPost, "/workspaces", `{"name":"WS"}`, authAs)

			if w.Code != tc.status {
				t.Fatalf("status = %d, harapannya %d", w.Code, tc.status)
			}
		})
	}
}

// ---------- List ----------

func TestList_PassesPaginationAndReturnsMeta(t *testing.T) {
	uc := &fakeUC{listFn: func(context.Context, string) ([]domain.WorkspaceAccess, error) {
		return []domain.WorkspaceAccess{*sample(domain.RoleOwner)}, nil
	}}

	w := do(newRouter(uc, nil), http.MethodGet, "/workspaces?page=3&limit=7", "", "Bearer valid-u")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if uc.gotPage != 3 || uc.gotLimit != 7 {
		t.Errorf("page/limit ke use case = %d/%d, harapannya 3/7", uc.gotPage, uc.gotLimit)
	}
	if _, ok := decode(t, w)["meta"].(map[string]any); !ok {
		t.Errorf("respons harus memuat meta: %s", w.Body.String())
	}
}

func TestList_NonNumericPaginationIs400WithoutCallingUseCase(t *testing.T) {
	uc := &fakeUC{}
	w := do(newRouter(uc, nil), http.MethodGet, "/workspaces?page=abc", "", "Bearer valid-u")

	if w.Code != http.StatusBadRequest || uc.listCalls != 0 {
		t.Fatalf("status = %d, listCalls = %d", w.Code, uc.listCalls)
	}
}

func TestList_Success(t *testing.T) {
	uc := &fakeUC{listFn: func(context.Context, string) ([]domain.WorkspaceAccess, error) {
		return []domain.WorkspaceAccess{*sample(domain.RoleOwner), *sample(domain.RoleMember)}, nil
	}}

	w := do(newRouter(uc, nil), http.MethodGet, "/workspaces", "", "Bearer valid-user-9")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if uc.gotUserID != "user-9" {
		t.Errorf("use case harus menerima user ID dari token, dapat %q", uc.gotUserID)
	}
	data, _ := decode(t, w)["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("harapannya 2 item, dapat %d", len(data))
	}
	if data[1].(map[string]any)["role"] != "member" {
		t.Errorf("role item kedua = %v", data[1].(map[string]any)["role"])
	}
}

func TestList_EmptyIsJSONArrayNotNull(t *testing.T) {
	for name, ret := range map[string][]domain.WorkspaceAccess{"nil": nil, "kosong": {}} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeUC{listFn: func(context.Context, string) ([]domain.WorkspaceAccess, error) { return ret, nil }}

			w := do(newRouter(uc, nil), http.MethodGet, "/workspaces", "", authAs)

			if !bytes.Contains(w.Body.Bytes(), []byte(`"data":[]`)) {
				t.Errorf("data harus berupa [] bukan null: %s", w.Body.String())
			}
		})
	}
}

func TestList_ErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{domain.ErrUnauthenticated, http.StatusUnauthorized},
		{errors.New("db mati"), http.StatusInternalServerError},
	} {
		uc := &fakeUC{listFn: func(context.Context, string) ([]domain.WorkspaceAccess, error) { return nil, tc.err }}
		if w := do(newRouter(uc, nil), http.MethodGet, "/workspaces", "", authAs); w.Code != tc.status {
			t.Errorf("err %v: status = %d, harapannya %d", tc.err, w.Code, tc.status)
		}
	}
}

// ---------- Get ----------

func TestGet_Success(t *testing.T) {
	uc := &fakeUC{getFn: func(context.Context, string, string) (*domain.WorkspaceAccess, error) {
		return sample(domain.RoleAdmin), nil
	}}

	w := do(newRouter(uc, nil), http.MethodGet, "/workspaces/"+wsID.String(), "", "Bearer valid-user-5")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if uc.gotUserID != "user-5" || uc.gotWSID != wsID.String() {
		t.Errorf("argumen use case = (%q, %q)", uc.gotUserID, uc.gotWSID)
	}
	data, _ := decode(t, w)["data"].(map[string]any)
	if data["role"] != "admin" || data["name"] != "Tim Backend" {
		t.Errorf("data tidak sesuai: %v", data)
	}
}

func TestGet_NotFoundForNonMember(t *testing.T) {
	uc := &fakeUC{getFn: func(context.Context, string, string) (*domain.WorkspaceAccess, error) {
		return nil, domain.ErrWorkspaceNotFound
	}}

	w := do(newRouter(uc, nil), http.MethodGet, "/workspaces/"+wsID.String(), "", authAs)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, harapannya 404 (bukan 403)", w.Code)
	}
	if strings.Contains(w.Body.String(), "Tim Backend") {
		t.Error("tidak boleh ada data workspace di respons 404")
	}
}

func TestGet_ErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{domain.ErrWorkspaceNotFound, http.StatusNotFound},
		{domain.ErrUnauthenticated, http.StatusUnauthorized},
		{errors.New("db mati"), http.StatusInternalServerError},
	} {
		uc := &fakeUC{getFn: func(context.Context, string, string) (*domain.WorkspaceAccess, error) { return nil, tc.err }}
		if w := do(newRouter(uc, nil), http.MethodGet, "/workspaces/"+wsID.String(), "", authAs); w.Code != tc.status {
			t.Errorf("err %v: status = %d, harapannya %d", tc.err, w.Code, tc.status)
		}
	}
}

// ---------- logging & error leakage ----------

func TestUnexpectedError_GenericBodyAndLogged(t *testing.T) {
	uc := &fakeUC{getFn: func(context.Context, string, string) (*domain.WorkspaceAccess, error) {
		return nil, errors.New("connection refused: db://user:pw@10.0.0.5")
	}}
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, nil))

	req := httptest.NewRequest(http.MethodGet, "/workspaces/"+wsID.String(), nil)
	req.Header.Set("Authorization", authAs)
	req.Header.Set("X-Request-ID", "rid-ws")
	w := httptest.NewRecorder()
	newRouter(uc, log).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	if msg := decode(t, w)["message"]; msg != "Terjadi kesalahan pada server" {
		t.Errorf("message = %v", msg)
	}
	if strings.Contains(w.Body.String(), "db://") || strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Errorf("detail internal bocor: %s", w.Body.String())
	}

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log harus satu baris JSON: %v\n%s", err, buf.String())
	}
	if rec["level"] != "ERROR" || rec["request_id"] != "rid-ws" || rec["module"] != "workspaces" {
		t.Errorf("rekaman log tidak sesuai: %v", rec)
	}
}

func TestExpectedErrors_NotLoggedByHandler(t *testing.T) {
	uc := &fakeUC{getFn: func(context.Context, string, string) (*domain.WorkspaceAccess, error) {
		return nil, domain.ErrWorkspaceNotFound
	}}
	buf := &bytes.Buffer{}

	do(newRouter(uc, slog.New(slog.NewJSONHandler(buf, nil))), http.MethodGet, "/workspaces/"+wsID.String(), "", authAs)

	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("handler tidak boleh mencatat error yang diharapkan: %s", buf.String())
	}
}
