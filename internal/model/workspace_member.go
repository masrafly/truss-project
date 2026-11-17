package model

import (
	"time"

	"github.com/google/uuid"
)

type WorkspaceMember struct {
	WorkspaceID uuid.UUID `json:"workspace_id" gorm:"type:uuid;primaryKey"`
	UserID      uuid.UUID `json:"user_id" gorm:"type:uuid;primaryKey"`
	Role        string    `json:"role" gorm:"type:varchar(20);not null;default:'member'"`
	JoinedAt    time.Time `json:"joined_at" gorm:"not null;default:now()"`

	// Relasi
	Workspace *Workspace ` json:"workspace,omitempty" gorm:"foreignKey:WorkspaceID;references:InternalID"`
	User      *User      `json:"user,omitempty" gorm:"foreignKey:UserID;references:InternalID"`
}
