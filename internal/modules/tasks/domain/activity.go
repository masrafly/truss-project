package domain

import (
	"time"

	"github.com/google/uuid"
)

// ActivityType: jenis aktivitas yang dicatat (PRD FR-08, aktivitas minimum).
type ActivityType string

const (
	ActivityCreated         ActivityType = "created"
	ActivityStatusChanged   ActivityType = "status_changed"
	ActivityAssigneeChanged ActivityType = "assignee_changed"
	ActivityPriorityChanged ActivityType = "priority_changed"
	ActivityDueDateChanged  ActivityType = "due_date_changed"
)

// Activity adalah satu entri riwayat. OldValue/NewValue berupa nilai skalar
// (nil = kosong): status/prioritas apa adanya, assignee berupa UUID user,
// deadline berupa RFC3339 UTC, dan judul untuk ActivityCreated.
type Activity struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	ActorID   uuid.UUID
	Actor     *UserRef // diisi saat dibaca
	Type      ActivityType
	OldValue  *string
	NewValue  *string
	CreatedAt time.Time
}

func strPtr(s string) *string { return &s }

func uuidValue(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	return strPtr(id.String())
}

func timeValue(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return strPtr(t.UTC().Format(time.RFC3339))
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// CreatedActivity: entri riwayat untuk task baru.
func CreatedActivity(t Task, actor uuid.UUID) Activity {
	return Activity{TaskID: t.ID, ActorID: actor, Type: ActivityCreated, NewValue: strPtr(t.Title)}
}

// DiffActivities menghasilkan entri riwayat untuk setiap perubahan penting antara
// before dan after. Perubahan judul/deskripsi tidak dicatat (di luar aktivitas minimum PRD).
func DiffActivities(before, after Task, actor uuid.UUID) []Activity {
	var out []Activity
	add := func(t ActivityType, old, new *string) {
		out = append(out, Activity{TaskID: before.ID, ActorID: actor, Type: t, OldValue: old, NewValue: new})
	}
	if before.Status != after.Status {
		add(ActivityStatusChanged, strPtr(string(before.Status)), strPtr(string(after.Status)))
	}
	if !sameUUID(before.AssigneeID, after.AssigneeID) {
		add(ActivityAssigneeChanged, uuidValue(before.AssigneeID), uuidValue(after.AssigneeID))
	}
	if before.Priority != after.Priority {
		add(ActivityPriorityChanged, strPtr(string(before.Priority)), strPtr(string(after.Priority)))
	}
	if !sameTime(before.DueDate, after.DueDate) {
		add(ActivityDueDateChanged, timeValue(before.DueDate), timeValue(after.DueDate))
	}
	return out
}
