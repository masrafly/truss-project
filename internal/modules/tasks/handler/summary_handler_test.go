package handler_test

import (
	"net/http"
	"testing"

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

var summaryPath = "/projects/" + projectID.String() + "/summary"

func TestSummary_Response(t *testing.T) {
	uc := &fakeUC{summary: &domain.ProjectSummary{
		Total: 8, Todo: 3, InProgress: 4, Done: 1,
		Low: 1, Medium: 2, High: 3, Urgent: 2,
		Unassigned: 5, Overdue: 2, AsOf: fixedTime,
	}}

	w := do(newRouter(uc, nil), "GET", summaryPath, "", authAs)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if uc.gotUser != "user-1" || uc.gotProject != projectID.String() {
		t.Errorf("argumen salah: %q %q", uc.gotUser, uc.gotProject)
	}
	data := decode(t, w)["data"].(map[string]any)
	want := map[string]any{
		"total": 8.0, "completed": 1.0, "overdue": 2.0, "unassigned": 5.0,
		"completion_rate": 12.5, "as_of": "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data.%s = %v, harapannya %v", k, data[k], v)
		}
	}
	bs := data["by_status"].(map[string]any)
	if bs["todo"] != 3.0 || bs["in_progress"] != 4.0 || bs["done"] != 1.0 || len(bs) != 3 {
		t.Errorf("by_status = %v", bs)
	}
	bp := data["by_priority"].(map[string]any)
	if bp["low"] != 1.0 || bp["medium"] != 2.0 || bp["high"] != 3.0 || bp["urgent"] != 2.0 || len(bp) != 4 {
		t.Errorf("by_priority = %v", bp)
	}
}

func TestSummary_EmptyProjectHasZeroesNotNulls(t *testing.T) {
	uc := &fakeUC{summary: &domain.ProjectSummary{AsOf: fixedTime}}
	data := decode(t, do(newRouter(uc, nil), "GET", summaryPath, "", authAs))["data"].(map[string]any)
	if data["total"] != 0.0 || data["completion_rate"] != 0.0 {
		t.Errorf("data = %v", data)
	}
	if bs := data["by_status"].(map[string]any); bs["todo"] != 0.0 || bs["done"] != 0.0 {
		t.Errorf("status tanpa task tetap tampil dengan 0: %v", bs)
	}
}

func TestList_OverdueQuery(t *testing.T) {
	for q, want := range map[string]bool{"": false, "?overdue=true": true, "?overdue=false": false} {
		uc := &fakeUC{list: emptyList()}
		if w := do(newRouter(uc, nil), "GET", projectTasksPath+q, "", authAs); w.Code != http.StatusOK {
			t.Fatalf("%q: status %d", q, w.Code)
		}
		if uc.gotList.Overdue != want {
			t.Errorf("%q: Overdue = %v", q, uc.gotList.Overdue)
		}
	}
	for _, q := range []string{"?overdue=1", "?overdue=yes", "?overdue=TRUE"} {
		uc := &fakeUC{}
		if w := do(newRouter(uc, nil), "GET", projectTasksPath+q, "", authAs); w.Code != http.StatusBadRequest {
			t.Errorf("%q: status %d, harapannya 400", q, w.Code)
		}
		if uc.calls != 0 {
			t.Errorf("%q: use case tidak boleh dipanggil", q)
		}
	}
}

func emptyList() *usecase.ListResult {
	return &usecase.ListResult{Items: []domain.TaskView{}, Page: 1, Limit: 20}
}
