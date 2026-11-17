package model

import (
	"time"

	"github.com/google/uuid"
)

type TaskActivity struct {
	InternalID   uuid.UUID `json:"internal_id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TaskID       uuid.UUID `json:"task_id" gorm:"type:uuid;not null"`
	ActorID      uuid.UUID `json:"actor_id" gorm:"type:uuid;not null"`
	ActivityType string    `json:"activity_type" gorm:"type:varchar(50);not null"`
	OldValue     *string   `json:"old_value" gorm:"type:jsonb"` // GORM bisa menggunakan string/map untuk JSONB
	NewValue     *string   `json:"new_value" gorm:"type:jsonb"`
	CreatedAt    time.Time `json:"created_at" gorm:"type:timestamptz;not null;default:now()"`

	Task  *Task `json:"task,omitempty" gorm:"foreignKey:TaskID;references:InternalID"`
	Actor *User `json:"actor,omitempty" gorm:"foreignKey:ActorID;references:InternalID"`
}
