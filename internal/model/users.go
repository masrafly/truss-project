package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	InternalID   uuid.UUID      `json:"internal_id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name         string         `json:"name" gorm:"type:varchar(100);not null"`
	Email        string         `json:"email" gorm:"type:varchar(255);unique;not null" `
	PasswordHash string         `json:"-" gorm:"type:varchar(255);not null"`
	CreatedAt    time.Time      `json:"created_at" gorm:"not null;default:now()"`
	UpdatedAt    time.Time      `json:"updated_at" gorm:"not null;default:now()"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`

	// Relasi
	Workspaces       []Workspace       `json:"workspaces,omitempty" gorm:"foreignKey:CreatedBy"`
	WorkspaceMembers []WorkspaceMember ` json:"workspace_members,omitempty" gorm:"foreignKey:UserID"`
	Projects         []Project         `json:"projects,omitempty" gorm:"foreignKey:CreatedBy"`
	ProjectMembers   []ProjectMember   `json:"project_members,omitempty" gorm:"foreignKey:UserID"`
	AssignedTasks    []Task            `json:"assigned_tasks,omitempty" gorm:"foreignKey:AssigneeID"`
	CreatedTasks     []Task            `json:"created_tasks,omitempty" gorm:"foreignKey:CreatedBy"`
	TaskComments     []TaskComment     `json:"task_comments,omitempty" gorm:"foreignKey:UserID"`
	TaskActivities   []TaskActivity    `json:"task_activities,omitempty" gorm:"foreignKey:ActorID"`
}
