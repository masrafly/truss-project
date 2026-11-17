package model

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	InternalId  uuid.UUID  `json:"internal_id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ProjectID   uuid.UUID  `json:"project_id" gorm:"type:uuid;not null"`
	Title       string     `json:"title" gorm:"type:varchar(200);not null"`
	Description *string    `json:"description" gorm:"type:text"`
	Status      string     `json:"status" gorm:"type:varchar(20);not null;default:'todo'"`
	Priority    string     `json:"priority" gorm:"type:varchar(20);not null;default:'medium'"`
	AssigneeID  *uuid.UUID `json:"assignee_id" gorm:"type:uuid" `
	CreatedBy   uuid.UUID  `json:"created_by" gorm:"type:uuid;not null"`
	DueDate     *time.Time `json:"due_date" gorm:"type:timestamptz"`
	CreatedAt   time.Time  `json:"created_at" gorm:"not null;default:now()"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"not null;default:now()"`

	// Relasi
	Project    *Project       `json:"project,omitempty" gorm:"foreignKey:ProjectID;references:InternalID"`
	Assignee   *User          `json:"assignee,omitempty" gorm:"foreignKey:AssigneeID;references:InternalID"`
	Creator    *User          `json:"creator,omitempty" gorm:"foreignKey:CreatedBy;references:InternalID"`
	Comments   []TaskComment  `json:"comments,omitempty" gorm:"foreignKey:TaskID"`
	Activities []TaskActivity `json:"activities,omitempty" gorm:"foreignKey:TaskID"`
}
