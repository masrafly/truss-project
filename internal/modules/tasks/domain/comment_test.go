package domain_test

import (
	"testing"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

// Matriks izin komentar: peran x (penulis / bukan) -> "CED" (Comment, Edit, Delete).
func TestCommentAccessMatrix(t *testing.T) {
	actor := uuid.New()
	for _, ws := range wsRoles {
		for _, p := range pRoles {
			for _, isAuthor := range []bool{true, false} {
				a := domain.ProjectAccess{WorkspaceRole: ws, ProjectRole: p}
				c := domain.Comment{AuthorID: uuid.New()}
				if isAuthor {
					c.AuthorID = actor
				}
				got := flag(a.CanComment(), "C") + flag(a.CanEditComment(c, actor), "E") + flag(a.CanDeleteComment(c, actor), "D")

				want := "---"
				if a.CanView() {
					want = "C" + flag(isAuthor, "E") + flag(isAuthor || a.IsManager(), "D")
				}
				if got != want {
					t.Errorf("ws=%q p=%q penulis=%v: dapat %s, harapannya %s", ws, p, isAuthor, got, want)
				}
			}
		}
	}

	// Titik eksplisit agar aturan di atas tidak tautologis.
	actorC := domain.Comment{AuthorID: actor}
	other := domain.Comment{AuthorID: uuid.New()}
	manager := domain.ProjectAccess{WorkspaceRole: domain.WorkspaceAdmin}
	member := domain.ProjectAccess{WorkspaceRole: domain.WorkspaceMember, ProjectRole: domain.ProjectMember}
	stale := domain.ProjectAccess{ProjectRole: domain.ProjectOwner}
	switch {
	case manager.CanEditComment(other, actor):
		t.Error("pengelola tidak boleh mengubah kata-kata orang lain")
	case !manager.CanDeleteComment(other, actor):
		t.Error("pengelola boleh menghapus (moderasi)")
	case member.CanDeleteComment(other, actor):
		t.Error("anggota biasa tidak boleh menghapus komentar orang lain")
	case !member.CanEditComment(actorC, actor) || !member.CanDeleteComment(actorC, actor):
		t.Error("penulis boleh mengubah dan menghapus komentarnya")
	case stale.CanComment() || stale.CanEditComment(actorC, actor) || stale.CanDeleteComment(actorC, actor):
		t.Error("bukan anggota workspace: tidak ada hak sama sekali, walau penulis")
	}
}
