package handler_test

import (
	"context"
	"errors"
	"net/http"
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

type fakeCommentUC struct {
	err     error
	comment *domain.Comment
	list    *usecase.CommentListResult
	calls   int

	gotUser, gotTask, gotComment, gotContent string
	gotPage, gotLimit                        int
}

func (f *fakeCommentUC) Create(_ context.Context, u, t, content string) (*domain.Comment, error) {
	f.calls++
	f.gotUser, f.gotTask, f.gotContent = u, t, content
	return f.comment, f.err
}

func (f *fakeCommentUC) List(_ context.Context, u, t string, page, limit int) (*usecase.CommentListResult, error) {
	f.calls++
	f.gotUser, f.gotTask, f.gotPage, f.gotLimit = u, t, page, limit
	return f.list, f.err
}

func (f *fakeCommentUC) Update(_ context.Context, u, t, c, content string) (*domain.Comment, error) {
	f.calls++
	f.gotUser, f.gotTask, f.gotComment, f.gotContent = u, t, c, content
	return f.comment, f.err
}

func (f *fakeCommentUC) Delete(_ context.Context, u, t, c string) error {
	f.calls++
	f.gotUser, f.gotTask, f.gotComment = u, t, c
	return f.err
}

var commentID = uuid.MustParse("12345678-1234-1234-1234-123456789012")

func sampleComment(edited bool) *domain.Comment {
	c := &domain.Comment{
		ID: commentID, TaskID: taskID, AuthorID: creatorID, Author: &domain.UserRef{ID: creatorID, Name: "Ani"},
		Content: "Sudah dicek", CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}
	if edited {
		c.UpdatedAt = fixedTime.Add(time.Minute)
	}
	return c
}

func newCommentRouter(uc *fakeCommentUC) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	handler.NewCommentHandler(uc, nil).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))
	return r
}

var (
	commentsPath = taskPath + "/comments"
	commentPath  = commentsPath + "/" + commentID.String()
)

func commentRoutes() []route {
	return []route{
		{"POST", commentsPath, `{"content":"x"}`},
		{"GET", commentsPath, ""},
		{"PATCH", commentPath, `{"content":"x"}`},
		{"DELETE", commentPath, ""},
	}
}

func TestCommentRoutesRegisteredAndCoexist(t *testing.T) {
	r := gin.New()
	mw := middleware.AuthMiddleware(fakeValidator{})
	handler.NewTaskHandler(&fakeUC{}, nil).RegisterRoutes(r, mw)
	handler.NewCommentHandler(&fakeCommentUC{}, nil).RegisterRoutes(r, mw)
	got := map[string]bool{}
	for _, ri := range r.Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	for _, w := range []string{
		"POST /tasks/:id/comments", "GET /tasks/:id/comments",
		"PATCH /tasks/:id/comments/:commentId", "DELETE /tasks/:id/comments/:commentId",
	} {
		if !got[w] {
			t.Errorf("route %q harus terdaftar", w)
		}
	}
	if len(got) != 11 {
		t.Errorf("harapannya 11 route task+komentar, dapat %d", len(got))
	}
}

func TestCommentRoutesRequireAuth(t *testing.T) {
	uc := &fakeCommentUC{}
	r := newCommentRouter(uc)
	for _, c := range commentRoutes() {
		if w := do(r, c.method, c.path, c.body, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status %d", c.method, c.path, w.Code)
		}
	}
	if uc.calls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

func TestCommentCreate_Success(t *testing.T) {
	uc := &fakeCommentUC{comment: sampleComment(false)}
	w := do(newCommentRouter(uc), "POST", commentsPath, `{"content":"Sudah dicek","user_id":"x"}`, authAs)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if uc.gotUser != "user-1" || uc.gotTask != taskID.String() || uc.gotContent != "Sudah dicek" {
		t.Errorf("argumen salah: %+v", uc)
	}
	data := decode(t, w)["data"].(map[string]any)
	if data["internal_id"] != commentID.String() || data["content"] != "Sudah dicek" || data["edited"] != false ||
		data["task_id"] != taskID.String() || data["created_at"] != "2026-01-02T03:04:05Z" {
		t.Errorf("data = %v", data)
	}
	if author := data["author"].(map[string]any); author["user_id"] != creatorID.String() || author["name"] != "Ani" {
		t.Errorf("author = %v", author)
	}
}

func TestCommentBadBody(t *testing.T) {
	for _, c := range []route{
		{"POST", commentsPath, ""}, {"POST", commentsPath, `{}`}, {"POST", commentsPath, `{"content":1}`},
		{"PATCH", commentPath, `{"text":"x"}`},
	} {
		uc := &fakeCommentUC{}
		if w := do(newCommentRouter(uc), c.method, c.path, c.body, authAs); w.Code != http.StatusBadRequest {
			t.Errorf("%s %q: status %d", c.method, c.body, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("%s %q: use case tidak boleh dipanggil", c.method, c.body)
		}
	}
}

func TestCommentList_MetaAndQuery(t *testing.T) {
	uc := &fakeCommentUC{list: &usecase.CommentListResult{Items: []domain.Comment{*sampleComment(true)}, Total: 7, Page: 2, Limit: 5}}
	w := do(newCommentRouter(uc), "GET", commentsPath+"?page=2&limit=5", "", authAs)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if uc.gotPage != 2 || uc.gotLimit != 5 {
		t.Errorf("page/limit = %d/%d", uc.gotPage, uc.gotLimit)
	}
	body := decode(t, w)
	if meta := body["meta"].(map[string]any); meta["total"] != float64(7) {
		t.Errorf("meta = %v", meta)
	}
	if item := body["data"].([]any)[0].(map[string]any); item["edited"] != true {
		t.Errorf("edited harus true bila updated_at > created_at: %v", item)
	}
	if w := do(newCommentRouter(&fakeCommentUC{}), "GET", commentsPath+"?limit=x", "", authAs); w.Code != http.StatusBadRequest {
		t.Errorf("limit bukan angka: status %d", w.Code)
	}
}

func TestCommentList_EmptyArray(t *testing.T) {
	uc := &fakeCommentUC{list: &usecase.CommentListResult{Items: []domain.Comment{}, Page: 1, Limit: 50}}
	w := do(newCommentRouter(uc), "GET", commentsPath, "", authAs)
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Errorf("body = %s", w.Body)
	}
}

func TestCommentUpdateAndDelete_PassIDs(t *testing.T) {
	uc := &fakeCommentUC{comment: sampleComment(true)}
	if w := do(newCommentRouter(uc), "PATCH", commentPath, `{"content":"Revisi"}`, authAs); w.Code != http.StatusOK {
		t.Fatalf("PATCH status %d", w.Code)
	}
	if uc.gotTask != taskID.String() || uc.gotComment != commentID.String() || uc.gotContent != "Revisi" {
		t.Errorf("PATCH argumen salah: %+v", uc)
	}
	w := do(newCommentRouter(uc), "DELETE", commentPath, "", authAs)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Errorf("DELETE status %d body %q", w.Code, w.Body)
	}
}

func TestCommentErrorMapping(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{domain.ErrInvalidInput, http.StatusBadRequest},
		{domain.ErrForbidden, http.StatusForbidden},
		{domain.ErrTaskNotFound, http.StatusNotFound},
		{domain.ErrCommentNotFound, http.StatusNotFound},
		{domain.ErrProjectArchived, http.StatusConflict},
		{errors.New("db mati"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		r := newCommentRouter(&fakeCommentUC{err: tc.err})
		for _, c := range commentRoutes() {
			if w := do(r, c.method, c.path, c.body, authAs); w.Code != tc.want {
				t.Errorf("%v %s %s: status %d, harapannya %d", tc.err, c.method, c.path, w.Code, tc.want)
			}
		}
	}
}
