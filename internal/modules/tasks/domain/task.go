// Package domain berisi entity, aturan izin, dan error modul tasks.
// Tidak boleh mengimpor gin, gorm, atau package infrastruktur lain.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status task (CHECK chk_tasks_status).
type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

func ParseStatus(s string) (Status, bool) {
	switch Status(s) {
	case StatusTodo, StatusInProgress, StatusDone:
		return Status(s), true
	}
	return "", false
}

// Priority task (CHECK chk_tasks_priority).
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func ParsePriority(s string) (Priority, bool) {
	switch Priority(s) {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent:
		return Priority(s), true
	}
	return "", false
}

type Task struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	Title       string
	Description *string
	Status      Status
	Priority    Priority
	AssigneeID  *uuid.UUID
	CreatedBy   uuid.UUID
	DueDate     *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UserRef: profil singkat user (assignee, aktor aktivitas).
type UserRef struct {
	ID   uuid.UUID
	Name string
}

// TaskView adalah task beserta profil assignee untuk ditampilkan.
type TaskView struct {
	Task
	Assignee *UserRef
}

// TaskFilter: filter daftar task. Field kosong berarti tidak difilter.
type TaskFilter struct {
	Statuses   []Status
	Priorities []Priority
	AssigneeID *uuid.UUID
	Unassigned bool
	Query      string // pencarian judul (mengandung, tidak peka huruf besar)
	// OverdueAsOf, bila diisi, hanya menyertakan task yang terlambat per waktu tersebut
	// (definisi sama dengan IsOverdue dan ringkasan project).
	OverdueAsOf *time.Time
	Limit       int
	Offset      int
}

var (
	ErrInvalidInput = errors.New("input tidak valid")

	// ErrProjectNotFound / ErrTaskNotFound juga dipakai bila user tidak berhak melihat (404).
	ErrProjectNotFound = errors.New("project tidak ditemukan")
	ErrTaskNotFound    = errors.New("task tidak ditemukan")

	ErrForbidden       = errors.New("tidak memiliki izin untuk aksi ini")
	ErrUnauthenticated = errors.New("identitas user tidak valid")

	// ErrAssigneeNotMember: assignee harus anggota project (PRD FR-05 #3).
	ErrAssigneeNotMember = errors.New("assignee harus anggota project")

	// ErrProjectArchived: project diarsipkan sehingga task-nya read-only (409).
	ErrProjectArchived = errors.New("project diarsipkan; task tidak dapat diubah")
)
