// Package domain berisi entity, aturan izin, dan error modul projects.
// Tidak boleh mengimpor gin, gorm, atau package infrastruktur lain.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status project (sesuai CHECK chk_projects_status).
type Status string

const (
	StatusActive    Status = "active"
	StatusArchived  Status = "archived"
	StatusCompleted Status = "completed"
)

// ParseStatus hanya menerima nilai persis (huruf kecil).
func ParseStatus(s string) (Status, bool) {
	switch Status(s) {
	case StatusActive, StatusArchived, StatusCompleted:
		return Status(s), true
	}
	return "", false
}

// ProjectRole adalah peran user di dalam project (CHECK chk_project_members_role).
type ProjectRole string

const (
	ProjectOwner  ProjectRole = "owner"
	ProjectAdmin  ProjectRole = "admin"
	ProjectMember ProjectRole = "member"
)

// WorkspaceRole adalah peran user di workspace induk. Modul projects mendefinisikan
// tipenya sendiri agar tidak bergantung pada modul workspaces.
type WorkspaceRole string

const (
	WorkspaceOwner  WorkspaceRole = "owner"
	WorkspaceAdmin  WorkspaceRole = "admin"
	WorkspaceMember WorkspaceRole = "member"
)

type Project struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Name        string
	Description *string
	Status      Status
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Access adalah peran user terhadap sebuah project. String kosong berarti
// "bukan anggota" pada tingkat tersebut.
type Access struct {
	WorkspaceRole WorkspaceRole
	ProjectRole   ProjectRole
}

// ProjectAccess adalah project beserta akses user yang sedang meminta.
type ProjectAccess struct {
	Project
	Access
}

var (
	ErrInvalidInput = errors.New("input tidak valid")

	// ErrProjectNotFound juga dipakai bila user tidak berhak MELIHAT project
	// (kebijakan 404): orang luar tidak boleh tahu apakah project itu ada.
	ErrProjectNotFound = errors.New("project tidak ditemukan")

	// ErrWorkspaceNotFound: workspace tidak ada atau user bukan anggotanya.
	ErrWorkspaceNotFound = errors.New("workspace tidak ditemukan")

	// ErrNotWorkspaceMember dikembalikan repository; use case memetakannya ke 404.
	ErrNotWorkspaceMember = errors.New("user bukan anggota workspace")

	// ErrForbidden: user boleh melihat, tetapi perannya tidak mengizinkan aksi ini (403).
	ErrForbidden = errors.New("tidak memiliki izin untuk aksi ini")

	// ErrUnauthenticated: identitas user dari token tidak valid atau user sudah tidak ada.
	ErrUnauthenticated = errors.New("identitas user tidak valid")

	// ErrUserNotFound dikembalikan repository bila pembuat project tidak ada di database.
	ErrUserNotFound = errors.New("user tidak ditemukan")
)
