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
	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/modules/projects/handler"
	"truss-project/internal/modules/projects/usecase"
)

func init() { gin.SetMode(gin.TestMode) }

// ---------- fakes ----------

type fakeUC struct {
	createFn  func() (*domain.ProjectAccess, error)
	listWSFn  func() ([]domain.ProjectAccess, error)
	listAllFn func() ([]domain.ProjectAccess, error)
	getFn     func() (*domain.ProjectAccess, error)
	updateFn  func() (*domain.ProjectAccess, error)
	deleteFn  func() error

	calls                               int
	gotUser, gotWS, gotProject, gotName string
	gotDesc                             *string
	gotUpdate                           usecase.UpdateInput
}

func (f *fakeUC) Create(_ context.Context, userID, wsID, name string, desc *string) (*domain.ProjectAccess, error) {
	f.calls++
	f.gotUser, f.gotWS, f.gotName, f.gotDesc = userID, wsID, name, desc
	return f.createFn()
}

func (f *fakeUC) ListInWorkspace(_ context.Context, userID, wsID string) ([]domain.ProjectAccess, error) {
	f.calls++
	f.gotUser, f.gotWS = userID, wsID
	return f.listWSFn()
}

func (f *fakeUC) ListAll(_ context.Context, userID string) ([]domain.ProjectAccess, error) {
	f.calls++
	f.gotUser = userID
	return f.listAllFn()
}

func (f *fakeUC) Get(_ context.Context, userID, pid string) (*domain.ProjectAccess, error) {
	f.calls++
	f.gotUser, f.gotProject = userID, pid
	return f.getFn()
}

func (f *fakeUC) Update(_ context.Context, userID, pid string, in usecase.UpdateInput) (*domain.ProjectAccess, error) {
	f.calls++
	f.gotUser, f.gotProject, f.gotUpdate = userID, pid, in
	return f.updateFn()
}

func (f *fakeUC) Delete(_ context.Context, userID, pid string) error {
	f.calls++
	f.gotUser, f.gotProject = userID, pid
	return f.deleteFn()
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
	projectID = uuid.MustParse("66666666-7777-8888-9999-000000000000")
	creatorID = uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
)

func sample(ws domain.WorkspaceRole, p domain.ProjectRole) *domain.ProjectAccess {
	desc := "deskripsi"
	return &domain.ProjectAccess{
		Project: domain.Project{
			ID: projectID, WorkspaceID: wsID, Name: "Proyek", Description: &desc,
			Status: domain.StatusActive, CreatedBy: creatorID, CreatedAt: fixedTime, UpdatedAt: fixedTime,
		},
		Access: domain.Access{WorkspaceRole: ws, ProjectRole: p},
	}
}

func newRouter(uc *fakeUC, log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	handler.NewProjectHandler(uc, log).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))
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
		t.Fatalf("body bukan objek JSON valid: %v\n%s", err, w.Body.String())
	}
	return m
}

var (
	wsProjectsPath = "/workspaces/" + wsID.String() + "/projects"
	projectPath    = "/projects/" + projectID.String()
)

// ---------- routes & auth ----------

func TestRoutesRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, ri := range newRouter(&fakeUC{}, nil).Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	want := []string{
		"POST /workspaces/:id/projects", "GET /workspaces/:id/projects",
		"GET /projects", "GET /projects/:id", "PATCH /projects/:id", "DELETE /projects/:id",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("route %q harus terdaftar; ada: %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("harapannya tepat %d route, dapat %v", len(want), got)
	}
}

func TestAllRoutesRequireAuth(t *testing.T) {
	uc := &fakeUC{}
	r := newRouter(uc, nil)
	cases := []struct{ method, path, body string }{
		{"POST", wsProjectsPath, `{"name":"X"}`},
		{"GET", wsProjectsPath, ""},
		{"GET", "/projects", ""},
		{"GET", projectPath, ""},
		{"PATCH", projectPath, `{"name":"X"}`},
		{"DELETE", projectPath, ""},
	}
	for _, c := range cases {
		for _, auth := range []string{"", "Bearer salah", "Basic abc"} {
			if w := do(r, c.method, c.path, c.body, auth); w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s auth=%q: status %d, harapannya 401", c.method, c.path, auth, w.Code)
			}
		}
	}
	if uc.calls != 0 {
		t.Error("use case tidak boleh dipanggil tanpa autentikasi")
	}
}

// ---------- Create ----------

func TestCreate_Success(t *testing.T) {
	uc := &fakeUC{createFn: func() (*domain.ProjectAccess, error) {
		return sample(domain.WorkspaceAdmin, domain.ProjectOwner), nil
	}}

	w := do(newRouter(uc, nil), "POST", wsProjectsPath,
		`{"name":"Proyek","description":"deskripsi","workspace_id":"x","created_by":"x","status":"archived"}`, authAs)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if uc.gotUser != "user-1" || uc.gotWS != wsID.String() || uc.gotName != "Proyek" {
		t.Errorf("argumen salah: user=%q ws=%q name=%q", uc.gotUser, uc.gotWS, uc.gotName)
	}
	if uc.gotDesc == nil || *uc.gotDesc != "deskripsi" {
		t.Errorf("description = %v", uc.gotDesc)
	}
	body := decode(t, w)
	data := body["data"].(map[string]any)
	want := map[string]any{
		"internal_id": projectID.String(), "workspace_id": wsID.String(), "name": "Proyek",
		"description": "deskripsi", "status": "active", "created_by": creatorID.String(),
		"role": "owner", "workspace_role": "admin", "created_at": "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data.%s = %v, harapannya %v", k, data[k], v)
		}
	}
	if body["message"] != "Project berhasil dibuat" {
		t.Errorf("message = %v", body["message"])
	}
}

func TestCreate_BadBody(t *testing.T) {
	for _, body := range []string{"", "{", `{"description":"x"}`, `{"name":123}`, `[]`} {
		uc := &fakeUC{}
		w := do(newRouter(uc, nil), "POST", wsProjectsPath, body, authAs)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("body %q: use case tidak boleh dipanggil", body)
		}
	}
}

// ---------- List ----------

func TestListInWorkspace_EmptyIsArray(t *testing.T) {
	uc := &fakeUC{listWSFn: func() ([]domain.ProjectAccess, error) { return nil, nil }}
	w := do(newRouter(uc, nil), "GET", wsProjectsPath, "", authAs)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Errorf("data harus array kosong, dapat %s", w.Body)
	}
	if uc.gotWS != wsID.String() {
		t.Errorf("workspace = %q", uc.gotWS)
	}
}

func TestListAll_ProjectRoleNullWhenNotProjectMember(t *testing.T) {
	uc := &fakeUC{listAllFn: func() ([]domain.ProjectAccess, error) {
		return []domain.ProjectAccess{*sample(domain.WorkspaceOwner, "")}, nil
	}}
	w := do(newRouter(uc, nil), "GET", "/projects", "", authAs)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	list := decode(t, w)["data"].([]any)
	item := list[0].(map[string]any)
	if v, ok := item["role"]; !ok || v != nil {
		t.Errorf("role harus null untuk non-anggota project, dapat %v (ada=%v)", v, ok)
	}
	if item["workspace_role"] != "owner" {
		t.Errorf("workspace_role = %v", item["workspace_role"])
	}
}

// ---------- Get / Update / Delete ----------

func TestGet_PassesIDs(t *testing.T) {
	uc := &fakeUC{getFn: func() (*domain.ProjectAccess, error) {
		return sample(domain.WorkspaceMember, domain.ProjectMember), nil
	}}
	w := do(newRouter(uc, nil), "GET", projectPath, "", authAs)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if uc.gotUser != "user-1" || uc.gotProject != projectID.String() {
		t.Errorf("argumen salah: %q %q", uc.gotUser, uc.gotProject)
	}
}

func TestUpdate_PassesOnlyProvidedFields(t *testing.T) {
	uc := &fakeUC{updateFn: func() (*domain.ProjectAccess, error) { return sample(domain.WorkspaceOwner, ""), nil }}

	w := do(newRouter(uc, nil), "PATCH", projectPath, `{"status":"archived","workspace_id":"x"}`, authAs)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if uc.gotUpdate.Status == nil || *uc.gotUpdate.Status != "archived" {
		t.Errorf("status = %v", uc.gotUpdate.Status)
	}
	if uc.gotUpdate.Name != nil || uc.gotUpdate.Description != nil {
		t.Errorf("field yang tidak dikirim harus nil: %+v", uc.gotUpdate)
	}
	if decode(t, w)["message"] != "Project berhasil diperbarui" {
		t.Error("message salah")
	}
}

func TestUpdate_EmptyStringDescriptionIsPassed(t *testing.T) {
	uc := &fakeUC{updateFn: func() (*domain.ProjectAccess, error) { return sample(domain.WorkspaceOwner, ""), nil }}
	do(newRouter(uc, nil), "PATCH", projectPath, `{"description":""}`, authAs)
	if uc.gotUpdate.Description == nil || *uc.gotUpdate.Description != "" {
		t.Errorf("description \"\" harus diteruskan (untuk menghapus), dapat %v", uc.gotUpdate.Description)
	}
}

func TestUpdate_BadBody(t *testing.T) {
	for _, body := range []string{"", "{", `{"name":5}`, `"x"`} {
		uc := &fakeUC{}
		if w := do(newRouter(uc, nil), "PATCH", projectPath, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("body %q: use case tidak boleh dipanggil", body)
		}
	}
}

func TestDelete_NoContent(t *testing.T) {
	uc := &fakeUC{deleteFn: func() error { return nil }}
	w := do(newRouter(uc, nil), "DELETE", projectPath, "", authAs)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("204 tidak boleh punya body: %q", w.Body)
	}
	if uc.gotProject != projectID.String() {
		t.Errorf("project = %q", uc.gotProject)
	}
}

// ---------- error mapping ----------

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"input", domain.ErrInvalidInput, http.StatusBadRequest},
		{"unauthenticated", domain.ErrUnauthenticated, http.StatusUnauthorized},
		{"forbidden", domain.ErrForbidden, http.StatusForbidden},
		{"project tidak ada", domain.ErrProjectNotFound, http.StatusNotFound},
		{"workspace tidak ada", domain.ErrWorkspaceNotFound, http.StatusNotFound},
		{"dibungkus", fmt.Errorf("x: %w", domain.ErrForbidden), http.StatusForbidden},
		{"tak terduga", errors.New("db mati"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errP := func() (*domain.ProjectAccess, error) { return nil, tc.err }
			errL := func() ([]domain.ProjectAccess, error) { return nil, tc.err }
			uc := &fakeUC{
				createFn: errP, getFn: errP, updateFn: errP,
				listWSFn: errL, listAllFn: errL,
				deleteFn: func() error { return tc.err },
			}
			r := newRouter(uc, nil)
			for _, c := range []struct{ method, path, body string }{
				{"POST", wsProjectsPath, `{"name":"X"}`},
				{"GET", wsProjectsPath, ""},
				{"GET", "/projects", ""},
				{"GET", projectPath, ""},
				{"PATCH", projectPath, `{"name":"X"}`},
				{"DELETE", projectPath, ""},
			} {
				w := do(r, c.method, c.path, c.body, authAs)
				if w.Code != tc.want {
					t.Errorf("%s %s: status %d, harapannya %d", c.method, c.path, w.Code, tc.want)
				}
			}
		})
	}
}

func TestUnexpectedErrorIsLoggedButNotLeaked(t *testing.T) {
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(buf, nil))
	uc := &fakeUC{getFn: func() (*domain.ProjectAccess, error) {
		return nil, errors.New("pq: password authentication failed for user rahasia")
	}}

	w := do(newRouter(uc, log), "GET", projectPath, "", authAs)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "rahasia") {
		t.Error("detail error tidak boleh bocor ke client")
	}
	if !strings.Contains(buf.String(), "rahasia") || !strings.Contains(buf.String(), "request_id") {
		t.Errorf("error harus dicatat beserta request_id: %s", buf)
	}
}

func TestExpectedErrorsNotLogged(t *testing.T) {
	for _, err := range []error{domain.ErrForbidden, domain.ErrProjectNotFound, domain.ErrInvalidInput} {
		buf := &bytes.Buffer{}
		uc := &fakeUC{getFn: func() (*domain.ProjectAccess, error) { return nil, err }}
		do(newRouter(uc, slog.New(slog.NewTextHandler(buf, nil))), "GET", projectPath, "", authAs)
		if buf.Len() != 0 {
			t.Errorf("%v tidak boleh dicatat sebagai error: %s", err, buf)
		}
	}
}
