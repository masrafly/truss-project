// Package domain berisi entity dan error modul workspaces.
// Tidak boleh mengimpor gin, gorm, atau package infrastruktur lain.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Role adalah peran user di dalam workspace (sesuai CHECK di tabel workspace_members).
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type Workspace struct {
	ID          uuid.UUID
	Name        string
	Description *string
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// WorkspaceAccess adalah workspace beserta peran user yang sedang meminta.
type WorkspaceAccess struct {
	Workspace
	Role Role
}

var (
	ErrInvalidInput = errors.New("input tidak valid")

	// ErrWorkspaceNotFound juga dipakai untuk user yang BUKAN anggota workspace
	// (kebijakan 404): orang luar tidak boleh tahu apakah workspace itu ada.
	ErrWorkspaceNotFound = errors.New("workspace tidak ditemukan")

	// ErrNotMember dikembalikan repository saat user bukan anggota. Use case yang
	// memutuskan bagaimana kasus ini diperlihatkan ke luar.
	ErrNotMember = errors.New("user bukan anggota workspace")

	// ErrUserNotFound dikembalikan repository saat pembuat workspace tidak ada di database.
	ErrUserNotFound = errors.New("user tidak ditemukan")

	// ErrUnauthenticated: identitas user dari token tidak valid atau user sudah tidak ada.
	ErrUnauthenticated = errors.New("identitas user tidak valid")
)
