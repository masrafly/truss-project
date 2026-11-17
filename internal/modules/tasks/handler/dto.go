package handler

import (
	"encoding/json"
	"time"

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

// optString membedakan field yang tidak dikirim (Set=false) dari yang dikirim null
// (Set=true, Value=nil). encoding/json memanggil UnmarshalJSON juga untuk null.
type optString struct {
	Set   bool
	Value *string
}

func (o *optString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	o.Value = &s
	return nil
}

func (o optString) toUsecase() usecase.Optional[string] {
	return usecase.Optional[string]{Set: o.Set, Value: o.Value}
}

// createTaskRequest sengaja tidak memuat project_id atau created_by:
// project dari path, pembuat dari token.
type createTaskRequest struct {
	Title       string  `json:"title" binding:"required"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	AssigneeID  *string `json:"assignee_id"`
	DueDate     *string `json:"due_date"` // RFC3339
}

// updateTaskRequest: field yang tidak dikirim tidak diubah. description, assignee_id,
// dan due_date dapat dikosongkan dengan null.
type updateTaskRequest struct {
	Title       *string   `json:"title"`
	Description optString `json:"description"`
	Status      *string   `json:"status"`
	Priority    *string   `json:"priority"`
	AssigneeID  optString `json:"assignee_id"`
	DueDate     optString `json:"due_date"`
}

type userRefResponse struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

func toUserRef(r *domain.UserRef) *userRefResponse {
	if r == nil {
		return nil
	}
	return &userRefResponse{UserID: r.ID.String(), Name: r.Name}
}

type taskResponse struct {
	ID          string           `json:"internal_id"`
	ProjectID   string           `json:"project_id"`
	Title       string           `json:"title"`
	Description *string          `json:"description"`
	Status      string           `json:"status"`
	Priority    string           `json:"priority"`
	Assignee    *userRefResponse `json:"assignee"` // null bila tanpa assignee
	CreatedBy   string           `json:"created_by"`
	DueDate     *time.Time       `json:"due_date"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func toTaskResponse(v *domain.TaskView) taskResponse {
	return taskResponse{
		ID:          v.ID.String(),
		ProjectID:   v.ProjectID.String(),
		Title:       v.Title,
		Description: v.Description,
		Status:      string(v.Status),
		Priority:    string(v.Priority),
		Assignee:    toUserRef(v.Assignee),
		CreatedBy:   v.CreatedBy.String(),
		DueDate:     v.DueDate,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
	}
}

func toTaskResponses(list []domain.TaskView) []taskResponse {
	out := make([]taskResponse, 0, len(list)) // selalu array, tidak pernah null
	for i := range list {
		out = append(out, toTaskResponse(&list[i]))
	}
	return out
}

type activityResponse struct {
	ID        string           `json:"internal_id"`
	Type      string           `json:"type"`
	Actor     *userRefResponse `json:"actor"`
	OldValue  *string          `json:"old_value"`
	NewValue  *string          `json:"new_value"`
	CreatedAt time.Time        `json:"created_at"`
}

func toActivityResponses(list []domain.Activity) []activityResponse {
	out := make([]activityResponse, 0, len(list))
	for _, a := range list {
		out = append(out, activityResponse{
			ID:        a.ID.String(),
			Type:      string(a.Type),
			Actor:     toUserRef(a.Actor),
			OldValue:  a.OldValue,
			NewValue:  a.NewValue,
			CreatedAt: a.CreatedAt,
		})
	}
	return out
}

type summaryResponse struct {
	Total          int64            `json:"total"`
	ByStatus       map[string]int64 `json:"by_status"`
	ByPriority     map[string]int64 `json:"by_priority"`
	Completed      int64            `json:"completed"`
	Overdue        int64            `json:"overdue"`
	Unassigned     int64            `json:"unassigned"`
	CompletionRate float64          `json:"completion_rate"` // persen, 1 desimal
	AsOf           time.Time        `json:"as_of"`
}

func toSummaryResponse(s *domain.ProjectSummary) summaryResponse {
	return summaryResponse{
		Total: s.Total,
		ByStatus: map[string]int64{
			string(domain.StatusTodo):       s.Todo,
			string(domain.StatusInProgress): s.InProgress,
			string(domain.StatusDone):       s.Done,
		},
		ByPriority: map[string]int64{
			string(domain.PriorityLow):    s.Low,
			string(domain.PriorityMedium): s.Medium,
			string(domain.PriorityHigh):   s.High,
			string(domain.PriorityUrgent): s.Urgent,
		},
		Completed:      s.Done,
		Overdue:        s.Overdue,
		Unassigned:     s.Unassigned,
		CompletionRate: s.CompletionRate(),
		AsOf:           s.AsOf,
	}
}
