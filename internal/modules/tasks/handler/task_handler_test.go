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
	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/handler"
	"truss-project/internal/modules/tasks/usecase"
)

func init() { gin.SetMode(gin.TestMode) }

// ---------- fakes ----------

type fakeUC struct {
	err     error
	view    *domain.TaskView
	list    *usecase.ListResult
	acts    []domain.Activity
	summary *domain.ProjectSummary
	calls   int

	gotUser, gotProject, gotTask string
	gotCreate                    usecase.CreateInput
	gotUpdate                    usecase.UpdateInput
	gotList                      usecase.ListInput
}

func (f *fakeUC) Create(_ context.Context, u, p string, in usecase.CreateInput) (*domain.TaskView, error) {
	f.calls++
	f.gotUser, f.gotProject, f.gotCreate = u, p, in
	return f.view, f.err
}

func (f *fakeUC) List(_ context.Context, u, p string, in usecase.ListInput) (*usecase.ListResult, error) {
	f.calls++
	f.gotUser, f.gotProject, f.gotList = u, p, in
	return f.list, f.err
}

func (f *fakeUC) Get(_ context.Context, u, t string) (*domain.TaskView, error) {
	f.calls++
	f.gotUser, f.gotTask = u, t
	return f.view, f.err
}

func (f *fakeUC) Update(_ context.Context, u, t string, in usecase.UpdateInput) (*domain.TaskView, error) {
	f.calls++
	f.gotUser, f.gotTask, f.gotUpdate = u, t, in
	return f.view, f.err
}

func (f *fakeUC) Delete(_ context.Context, u, t string) error {
	f.calls++
	f.gotUser, f.gotTask = u, t
	return f.err
}

func (f *fakeUC) Activities(_ context.Context, u, t string) ([]domain.Activity, error) {
	f.calls++
	f.gotUser, f.gotTask = u, t
	return f.acts, f.err
}

type fakeValidator struct{}

func (fakeValidator) Validate(token string) (string, error) {
	if strings.HasPrefix(token, "valid-") {
		return strings.TrimPrefix(token, "valid-"), nil
	}
	return "", errors.New("invalid")
}

var (
	fixedTime  = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	projectID  = uuid.MustParse("11111111-2222-3333-4444-555555555555")
	taskID     = uuid.MustParse("66666666-7777-8888-9999-000000000000")
	creatorID  = uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	assigneeID = uuid.MustParse("99999999-8888-7777-6666-555555555555")
)

func sampleView(withAssignee bool) *domain.TaskView {
	due := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	v := &domain.TaskView{Task: domain.Task{
		ID: taskID, ProjectID: projectID, Title: "Tulis test", Status: domain.StatusInProgress,
		Priority: domain.PriorityHigh, CreatedBy: creatorID, DueDate: &due, CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}}
	if withAssignee {
		a := assigneeID
		v.AssigneeID = &a
		v.Assignee = &domain.UserRef{ID: assigneeID, Name: "Budi"}
	}
	return v
}

func newRouter(uc *fakeUC, log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	handler.NewTaskHandler(uc, log).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))
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
	projectTasksPath = "/projects/" + projectID.String() + "/tasks"
	taskPath         = "/tasks/" + taskID.String()
)

// ---------- routes & auth ----------

func TestRoutesRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, ri := range newRouter(&fakeUC{}, nil).Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	want := []string{
		"POST /projects/:id/tasks", "GET /projects/:id/tasks",
		"GET /tasks/:id", "PATCH /tasks/:id", "DELETE /tasks/:id", "GET /tasks/:id/activities",
		"GET /projects/:id/summary",
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

func TestAllRoutesRequireAuth(t *testing.T) {
	uc := &fakeUC{}
	r := newRouter(uc, nil)
	for _, c := range allRoutes() {
		if w := do(r, c.method, c.path, c.body, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status %d", c.method, c.path, w.Code)
		}
	}
	if uc.calls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

type route struct{ method, path, body string }

func allRoutes() []route {
	return []route{
		{"POST", projectTasksPath, `{"title":"T"}`},
		{"GET", projectTasksPath, ""},
		{"GET", taskPath, ""},
		{"PATCH", taskPath, `{"status":"done"}`},
		{"DELETE", taskPath, ""},
		{"GET", taskPath + "/activities", ""},
		{"GET", "/projects/" + projectID.String() + "/summary", ""},
	}
}

// ---------- Create ----------

func TestCreate_Success(t *testing.T) {
	uc := &fakeUC{view: sampleView(true)}
	body := `{"title":"Tulis test","description":"d","status":"in_progress","priority":"high",
		"assignee_id":"` + assigneeID.String() + `","due_date":"2026-03-01T17:00:00+07:00",
		"project_id":"x","created_by":"x"}`

	w := do(newRouter(uc, nil), "POST", projectTasksPath, body, authAs)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	in := uc.gotCreate
	if uc.gotUser != "user-1" || uc.gotProject != projectID.String() || in.Title != "Tulis test" ||
		in.Status != "in_progress" || in.Priority != "high" || *in.Description != "d" ||
		*in.AssigneeID != assigneeID.String() || *in.DueDate != "2026-03-01T17:00:00+07:00" {
		t.Errorf("argumen salah: user=%q project=%q in=%+v", uc.gotUser, uc.gotProject, in)
	}
	data := decode(t, w)["data"].(map[string]any)
	want := map[string]any{
		"internal_id": taskID.String(), "project_id": projectID.String(), "title": "Tulis test",
		"status": "in_progress", "priority": "high", "created_by": creatorID.String(),
		"due_date": "2026-03-01T10:00:00Z", "created_at": "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data.%s = %v, harapannya %v", k, data[k], v)
		}
	}
	assignee := data["assignee"].(map[string]any)
	if assignee["user_id"] != assigneeID.String() || assignee["name"] != "Budi" {
		t.Errorf("assignee = %v", assignee)
	}
}

func TestCreate_AssigneeNullInResponse(t *testing.T) {
	uc := &fakeUC{view: sampleView(false)}
	w := do(newRouter(uc, nil), "POST", projectTasksPath, `{"title":"T"}`, authAs)
	data := decode(t, w)["data"].(map[string]any)
	if v, ok := data["assignee"]; !ok || v != nil {
		t.Errorf("assignee harus null, dapat %v (ada=%v)", v, ok)
	}
}

func TestCreate_BadBody(t *testing.T) {
	for _, body := range []string{"", "{", `{"description":"x"}`, `{"title":5}`, `{"title":"T","due_date":5}`} {
		uc := &fakeUC{}
		if w := do(newRouter(uc, nil), "POST", projectTasksPath, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("body %q: use case tidak boleh dipanggil", body)
		}
	}
}

// ---------- List ----------

func TestList_QueryParsing(t *testing.T) {
	uc := &fakeUC{list: &usecase.ListResult{Items: []domain.TaskView{*sampleView(false)}, Total: 41, Page: 2, Limit: 20}}

	w := do(newRouter(uc, nil), "GET",
		projectTasksPath+"?status=todo,in_progress&status=done&priority=high&assignee=me&q=login%20page&page=2&limit=20", "", authAs)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	in := uc.gotList
	if fmt.Sprint(in.Statuses) != "[todo in_progress done]" || fmt.Sprint(in.Priorities) != "[high]" ||
		in.Assignee != "me" || in.Query != "login page" || in.Page != 2 || in.Limit != 20 {
		t.Errorf("input list salah: %+v", in)
	}
	body := decode(t, w)
	meta := body["meta"].(map[string]any)
	if meta["total"] != float64(41) || meta["page"] != float64(2) || meta["limit"] != float64(20) {
		t.Errorf("meta = %v", meta)
	}
	if len(body["data"].([]any)) != 1 {
		t.Errorf("data = %v", body["data"])
	}
}

func TestList_DefaultsAndEmptyArray(t *testing.T) {
	uc := &fakeUC{list: &usecase.ListResult{Items: []domain.TaskView{}, Page: 1, Limit: 20}}
	w := do(newRouter(uc, nil), "GET", projectTasksPath, "", authAs)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if uc.gotList.Page != 0 || uc.gotList.Limit != 0 || uc.gotList.Statuses != nil {
		t.Errorf("parameter kosong harus diteruskan sebagai nilai nol: %+v", uc.gotList)
	}
}

func TestList_NonNumericPagination(t *testing.T) {
	for _, q := range []string{"?page=dua", "?limit=10x"} {
		uc := &fakeUC{}
		if w := do(newRouter(uc, nil), "GET", projectTasksPath+q, "", authAs); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("%s: use case tidak boleh dipanggil", q)
		}
	}
}

// ---------- Update ----------

func TestUpdate_DistinguishesAbsentNullAndValue(t *testing.T) {
	uc := &fakeUC{view: sampleView(false)}

	w := do(newRouter(uc, nil), "PATCH", taskPath,
		`{"status":"done","assignee_id":null,"due_date":"2026-05-01T00:00:00Z","project_id":"x"}`, authAs)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	in := uc.gotUpdate
	if in.Status == nil || *in.Status != "done" {
		t.Errorf("status = %v", in.Status)
	}
	if !in.AssigneeID.Set || in.AssigneeID.Value != nil {
		t.Errorf("assignee_id:null harus Set=true, Value=nil: %+v", in.AssigneeID)
	}
	if !in.DueDate.Set || in.DueDate.Value == nil || *in.DueDate.Value != "2026-05-01T00:00:00Z" {
		t.Errorf("due_date = %+v", in.DueDate)
	}
	if in.Description.Set || in.Title != nil || in.Priority != nil {
		t.Errorf("field yang tidak dikirim harus tidak diset: %+v", in)
	}
	if uc.gotTask != taskID.String() || decode(t, w)["message"] != "Task berhasil diperbarui" {
		t.Error("task/message salah")
	}
}

func TestUpdate_BadBody(t *testing.T) {
	for _, body := range []string{"", "{", `{"assignee_id":5}`, `{"description":true}`, `{"status":1}`} {
		uc := &fakeUC{}
		if w := do(newRouter(uc, nil), "PATCH", taskPath, body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("body %q: use case tidak boleh dipanggil", body)
		}
	}
}

// ---------- Delete & Activities ----------

func TestDelete_NoContent(t *testing.T) {
	uc := &fakeUC{}
	w := do(newRouter(uc, nil), "DELETE", taskPath, "", authAs)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("status %d body %q", w.Code, w.Body)
	}
	if uc.gotTask != taskID.String() {
		t.Errorf("task = %q", uc.gotTask)
	}
}

func TestActivities_Response(t *testing.T) {
	old := "todo"
	uc := &fakeUC{acts: []domain.Activity{{
		ID: uuid.New(), Type: domain.ActivityStatusChanged, Actor: &domain.UserRef{ID: creatorID, Name: "Ani"},
		OldValue: &old, NewValue: nil, CreatedAt: fixedTime,
	}}}
	w := do(newRouter(uc, nil), "GET", taskPath+"/activities", "", authAs)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	item := decode(t, w)["data"].([]any)[0].(map[string]any)
	if item["type"] != "status_changed" || item["old_value"] != "todo" || item["new_value"] != nil {
		t.Errorf("item = %v", item)
	}
	if actor := item["actor"].(map[string]any); actor["user_id"] != creatorID.String() || actor["name"] != "Ani" {
		t.Errorf("actor = %v", actor)
	}
}

func TestActivities_EmptyArray(t *testing.T) {
	uc := &fakeUC{acts: []domain.Activity{}}
	w := do(newRouter(uc, nil), "GET", taskPath+"/activities", "", authAs)
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Errorf("body = %s", w.Body)
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
		{"assignee bukan anggota", domain.ErrAssigneeNotMember, http.StatusBadRequest},
		{"unauthenticated", domain.ErrUnauthenticated, http.StatusUnauthorized},
		{"forbidden", domain.ErrForbidden, http.StatusForbidden},
		{"project tak ada", domain.ErrProjectNotFound, http.StatusNotFound},
		{"task tak ada", domain.ErrTaskNotFound, http.StatusNotFound},
		{"diarsipkan", domain.ErrProjectArchived, http.StatusConflict},
		{"dibungkus", fmt.Errorf("x: %w", domain.ErrProjectArchived), http.StatusConflict},
		{"tak terduga", errors.New("db mati"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(&fakeUC{err: tc.err}, nil)
			for _, c := range allRoutes() {
				if w := do(r, c.method, c.path, c.body, authAs); w.Code != tc.want {
					t.Errorf("%s %s: status %d, harapannya %d", c.method, c.path, w.Code, tc.want)
				}
			}
		})
	}
}

func TestUnexpectedErrorLoggedNotLeaked(t *testing.T) {
	buf := &bytes.Buffer{}
	uc := &fakeUC{err: errors.New("pq: kolom rahasia")}
	w := do(newRouter(uc, slog.New(slog.NewTextHandler(buf, nil))), "GET", taskPath, "", authAs)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "rahasia") {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if !strings.Contains(buf.String(), "rahasia") || !strings.Contains(buf.String(), "request_id") {
		t.Errorf("log = %s", buf)
	}
}

func TestExpectedErrorsNotLogged(t *testing.T) {
	for _, err := range []error{domain.ErrForbidden, domain.ErrTaskNotFound, domain.ErrProjectArchived, domain.ErrAssigneeNotMember} {
		buf := &bytes.Buffer{}
		do(newRouter(&fakeUC{err: err}, slog.New(slog.NewTextHandler(buf, nil))), "GET", taskPath, "", authAs)
		if buf.Len() != 0 {
			t.Errorf("%v tidak boleh dicatat: %s", err, buf)
		}
	}
}

func (f *fakeUC) Summary(_ context.Context, u, p string) (*domain.ProjectSummary, error) {
	f.calls++
	f.gotUser, f.gotProject = u, p
	return f.summary, f.err
}
