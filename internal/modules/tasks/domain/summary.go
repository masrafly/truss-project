package domain

import (
	"math"
	"time"
)

// ProjectSummary adalah ringkasan kondisi project (PRD FR-09).
type ProjectSummary struct {
	Total      int64
	Todo       int64
	InProgress int64
	Done       int64

	Low    int64
	Medium int64
	High   int64
	Urgent int64

	Unassigned int64

	// Overdue: task dengan deadline sebelum AsOf yang statusnya BUKAN done
	// (PRD FR-09 #2: task selesai tidak dihitung terlambat). Lihat IsOverdue.
	Overdue int64

	AsOf time.Time
}

// IsOverdue adalah definisi tunggal "terlambat" yang juga dipakai filter daftar task
// (repository menerjemahkannya ke SQL yang setara), agar ringkasan konsisten dengan
// data task (PRD FR-09 #3).
func IsOverdue(t Task, asOf time.Time) bool {
	return t.Status != StatusDone && t.DueDate != nil && t.DueDate.Before(asOf)
}

// CompletionRate: persentase task selesai, dibulatkan satu desimal; 0 bila belum ada task.
func (s ProjectSummary) CompletionRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return math.Round(float64(s.Done)*1000/float64(s.Total)) / 10
}
