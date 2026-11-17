package domain

import "github.com/google/uuid"

// WorkspaceRole dan ProjectRole didefinisikan ulang di modul ini agar tasks tidak
// bergantung pada modul workspaces/projects. String kosong = bukan anggota.
type WorkspaceRole string
type ProjectRole string

const (
	WorkspaceOwner  WorkspaceRole = "owner"
	WorkspaceAdmin  WorkspaceRole = "admin"
	WorkspaceMember WorkspaceRole = "member"

	ProjectOwner  ProjectRole = "owner"
	ProjectAdmin  ProjectRole = "admin"
	ProjectMember ProjectRole = "member"
)

// ProjectAccess: konteks project dan peran user yang meminta.
type ProjectAccess struct {
	ProjectID     uuid.UUID
	WorkspaceID   uuid.UUID
	Archived      bool
	WorkspaceRole WorkspaceRole
	ProjectRole   ProjectRole
}

// Matriks izin task. Semua fungsi murni.
//
//	Lihat & buat      : siapa pun yang dapat melihat project
//	Ubah              : pengelola, ATAU pembuat task, ATAU assignee task
//	Hapus             : pengelola, ATAU pembuat task
//
// Pengelola = Owner/Admin project, atau Owner/Admin workspace.
// Project yang diarsipkan bersifat read-only (CheckWritable).

func (a ProjectAccess) workspaceMember() bool {
	switch a.WorkspaceRole {
	case WorkspaceOwner, WorkspaceAdmin, WorkspaceMember:
		return true
	}
	return false
}

func (a ProjectAccess) CanView() bool {
	if !a.workspaceMember() {
		return false
	}
	return a.ProjectRole != "" || a.WorkspaceRole == WorkspaceOwner || a.WorkspaceRole == WorkspaceAdmin
}

// IsManager: pengelola project (hanya bermakna bila CanView).
func (a ProjectAccess) IsManager() bool {
	if !a.CanView() {
		return false
	}
	return a.ProjectRole == ProjectOwner || a.ProjectRole == ProjectAdmin ||
		a.WorkspaceRole == WorkspaceOwner || a.WorkspaceRole == WorkspaceAdmin
}

func (a ProjectAccess) CanCreateTask() bool {
	return a.CanView()
}

func (a ProjectAccess) CanEditTask(t Task, actor uuid.UUID) bool {
	if !a.CanView() {
		return false
	}
	if a.IsManager() || t.CreatedBy == actor {
		return true
	}
	return t.AssigneeID != nil && *t.AssigneeID == actor
}

func (a ProjectAccess) CanDeleteTask(t Task, actor uuid.UUID) bool {
	if !a.CanView() {
		return false
	}
	return a.IsManager() || t.CreatedBy == actor
}

// CheckWritable: penulisan task ditolak bila project diarsipkan.
func (a ProjectAccess) CheckWritable() error {
	if a.Archived {
		return ErrProjectArchived
	}
	return nil
}
