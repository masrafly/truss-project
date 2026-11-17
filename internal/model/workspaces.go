package model

import (
	"time"

	"github.com/google/uuid"
)

type Workspace struct {
	InternalID  uuid.UUID `json:"internal_id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name        string    `json:"name" gorm:"type:varchar(100);not null"`
	Description *string   `json:"description" gorm:"type:text"`
	CreatedBy   uuid.UUID `json:"created_by" gorm:"type:uuid;not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"not null;default:now()"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"not null;default:now()"`

	// Relasi
	Creator  *User             `json:"creator,omitempty" gorm:"foreignKey:CreatedBy;references:InternalID"`
	Members  []WorkspaceMember `json:"members,omitempty" gorm:"foreignKey:WorkspaceID"`
	Projects []Project         `json:"projects,omitempty" gorm:"foreignKey:WorkspaceID"`
}
