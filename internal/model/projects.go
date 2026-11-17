package model

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	InternalID  uuid.UUID `json:"internal_id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	WorkspaceID uuid.UUID `json:"workspace_id" gorm:"type:uuid;not null"`
	Name        string    `json:"name" gorm:"type:varchar(100);not null"`
	Description *string   `json:"description" gorm:"type:text"`
	Status      string    `json:"status" gorm:"type:varchar(20);not null;default:'active'"`
	CreatedBy   uuid.UUID `json:"created_by" gorm:"type:uuid;not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"not null;default:now()"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"not null;default:now()"`

	// Relasi
	Workspace *Workspace      `json:"workspace,omitempty" gorm:"foreignKey:WorkspaceID;references:InternalID"`
	Creator   *User           `json:"creator,omitempty" gorm:"foreignKey:CreatedBy;references:InternalID"`
	Members   []ProjectMember `json:"members,omitempty" gorm:"foreignKey:ProjectID"`
	Tasks     []Task          `json:"tasks,omitempty" gorm:"foreignKey:ProjectID"`
}
