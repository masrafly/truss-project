package domain_test

import (
	"testing"
	"time"

	"truss-project/internal/modules/tasks/domain"
)

func TestIsOverdue(t *testing.T) {
	asOf := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	past, future, exact := asOf.Add(-time.Second), asOf.Add(time.Hour), asOf

	cases := []struct {
		name   string
		status domain.Status
		due    *time.Time
		want   bool
	}{
		{"todo lewat deadline", domain.StatusTodo, &past, true},
		{"in_progress lewat deadline", domain.StatusInProgress, &past, true},
		{"done lewat deadline (PRD FR-09 #2)", domain.StatusDone, &past, false},
		{"belum jatuh tempo", domain.StatusTodo, &future, false},
		{"tepat pada deadline belum terlambat", domain.StatusTodo, &exact, false},
		{"tanpa deadline", domain.StatusTodo, nil, false},
	}
	for _, c := range cases {
		if got := domain.IsOverdue(domain.Task{Status: c.status, DueDate: c.due}, asOf); got != c.want {
			t.Errorf("%s: dapat %v, harapannya %v", c.name, got, c.want)
		}
	}

	// Zona waktu tidak memengaruhi hasil: yang dibandingkan adalah instan.
	wib := past.In(time.FixedZone("WIB", 7*3600))
	if !domain.IsOverdue(domain.Task{Status: domain.StatusTodo, DueDate: &wib}, asOf) {
		t.Error("instan yang sama dalam zona berbeda harus tetap terlambat")
	}
}

func TestCompletionRate(t *testing.T) {
	cases := []struct {
		done, total int64
		want        float64
	}{
		{0, 0, 0},
		{0, 5, 0},
		{5, 5, 100},
		{1, 3, 33.3},
		{2, 3, 66.7},
		{1, 8, 12.5},
	}
	for _, c := range cases {
		s := domain.ProjectSummary{Done: c.done, Total: c.total}
		if got := s.CompletionRate(); got != c.want {
			t.Errorf("%d/%d: dapat %v, harapannya %v", c.done, c.total, got, c.want)
		}
	}
}
