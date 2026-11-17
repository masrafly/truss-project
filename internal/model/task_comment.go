package model

import (
	"time"

	"github.com/google/uuid"
)

type TaskComment struct {
	InternalID uuid.UUID `json:"internal_id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TaskID     uuid.UUID `json:"task_id" gorm:"type:uuid;not null"`
	UserID     uuid.UUID `json:"user_id" gorm:"type:uuid;not null"`
	Content    string    `json:"content" gorm:"type:text;not null"`
	CreatedAt  time.Time `json:"created_at" gorm:"not null;default:now()"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"not null;default:now()"`

	// Relasi
	Task *Task `json:"task,omitempty" gorm:"foreignKey:TaskID;references:InternalID"`
	User *User `json:"user,omitempty" gorm:"foreignKey:UserID;references:InternalID"`
}
