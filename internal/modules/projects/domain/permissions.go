package domain

// Matriks izin project. Semua fungsi murni dan diuji dengan seluruh kombinasi peran.
//
// Prinsip:
//   - Akses project selalu mensyaratkan keanggotaan workspace. Keanggotaan project
//     tanpa keanggotaan workspace (data basi) tidak memberi hak apa pun.
//   - Owner/Admin workspace adalah pengelola: mereka dapat melihat dan mengubah
//     semua project di workspace-nya walau bukan anggota project.

func (r WorkspaceRole) isManager() bool {
	return r == WorkspaceOwner || r == WorkspaceAdmin
}

func (r WorkspaceRole) valid() bool {
	return r == WorkspaceOwner || r == WorkspaceAdmin || r == WorkspaceMember
}

// CanCreateProject: hanya Owner/Admin workspace.
func CanCreateProject(ws WorkspaceRole) bool {
	return ws.isManager()
}

// CanView: anggota project (selama masih anggota workspace), atau Owner/Admin workspace.
// Yang tidak bisa melihat mendapat 404, bukan 403.
func (a Access) CanView() bool {
	if !a.WorkspaceRole.valid() {
		return false
	}
	return a.ProjectRole != "" || a.WorkspaceRole.isManager()
}

// CanUpdate: Owner/Admin project, atau Owner/Admin workspace.
func (a Access) CanUpdate() bool {
	if !a.CanView() {
		return false
	}
	return a.ProjectRole == ProjectOwner || a.ProjectRole == ProjectAdmin || a.WorkspaceRole.isManager()
}

// CanDelete: hanya Owner project atau Owner workspace (penghapusan bersifat permanen
// dan ikut menghapus semua task).
func (a Access) CanDelete() bool {
	if !a.CanView() {
		return false
	}
	return a.ProjectRole == ProjectOwner || a.WorkspaceRole == WorkspaceOwner
}
