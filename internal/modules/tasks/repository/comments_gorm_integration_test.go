package repository_test

// Integration test komentar task. Memakai env/setup dari task_gorm_integration_test.go.
// Di-skip otomatis jika TEST_DB_HOST tidak diisi.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

func (e *env) comment(t *testing.T, taskID, author uuid.UUID, content string) *domain.Comment {
	t.Helper()
	c := &domain.Comment{TaskID: taskID, AuthorID: author, Content: content}
	if err := e.repo.CreateComment(ctx, c); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	return c
}

func TestCreateComment_PersistsWithAuthor(t *testing.T) {
	e := setup(t)
	task := e.create(t, "T", nil)

	c := e.comment(t, task.ID, e.member, "Sudah dicek")

	if c.ID == uuid.Nil || c.CreatedAt.IsZero() || time.Since(c.CreatedAt) > time.Minute || !c.UpdatedAt.Equal(c.CreatedAt) {
		t.Errorf("ID/timestamp salah: %+v", c)
	}
	if c.Author == nil || c.Author.ID != e.member || c.Author.Name != "Member" || c.Content != "Sudah dicek" {
		t.Errorf("penulis/isi salah: %+v %+v", c, c.Author)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM task_comments WHERE task_id = ? AND user_id = ?", task.ID, e.member); n != 1 {
		t.Errorf("baris tersimpan = %d", n)
	}
}

func TestCreateComment_UnknownTaskIsNotFound(t *testing.T) {
	e := setup(t)
	err := e.repo.CreateComment(ctx, &domain.Comment{TaskID: uuid.New(), AuthorID: e.owner, Content: "x"})
	if !errors.Is(err, domain.ErrTaskNotFound) {
		t.Fatalf("dapat %v", err)
	}
}

func TestListComments_OrderPaginationAndIsolation(t *testing.T) {
	e := setup(t)
	task, other := e.create(t, "T", nil), e.create(t, "Lain", nil)
	var ids []uuid.UUID
	for _, s := range []string{"satu", "dua", "tiga"} {
		ids = append(ids, e.comment(t, task.ID, e.owner, s).ID)
		time.Sleep(10 * time.Millisecond)
	}
	e.comment(t, other.ID, e.owner, "task lain")

	all, total, err := e.repo.ListComments(ctx, task.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("total=%d len=%d", total, len(all))
	}
	for i, id := range ids {
		if all[i].ID != id {
			t.Errorf("urutan[%d] salah (harus terlama dulu)", i)
		}
	}
	if all[0].Author == nil || all[0].Author.Name != "Owner" {
		t.Errorf("penulis = %+v", all[0].Author)
	}

	page2, total, _ := e.repo.ListComments(ctx, task.ID, 2, 2)
	if total != 3 || len(page2) != 1 || page2[0].ID != ids[2] {
		t.Errorf("halaman 2: total=%d %+v", total, page2)
	}
	empty, total, err := e.repo.ListComments(ctx, uuid.New(), 10, 0)
	if err != nil || total != 0 || empty == nil || len(empty) != 0 {
		t.Errorf("task tak ada: %#v total=%d err=%v", empty, total, err)
	}
}

func TestFindComment_MustBelongToTask(t *testing.T) {
	e := setup(t)
	task, other := e.create(t, "T", nil), e.create(t, "Lain", nil)
	c := e.comment(t, task.ID, e.owner, "x")

	if got, err := e.repo.FindComment(ctx, task.ID, c.ID); err != nil || got.ID != c.ID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := e.repo.FindComment(ctx, other.ID, c.ID); !errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("lewat task lain harus ErrCommentNotFound, dapat %v", err)
	}
	if _, err := e.repo.FindComment(ctx, task.ID, uuid.New()); !errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("dapat %v", err)
	}
}

func TestUpdateComment(t *testing.T) {
	e := setup(t)
	task, other := e.create(t, "T", nil), e.create(t, "Lain", nil)
	c := e.comment(t, task.ID, e.owner, "awal")
	time.Sleep(15 * time.Millisecond)

	c.Content = "revisi"
	if err := e.repo.UpdateComment(ctx, c); err != nil {
		t.Fatal(err)
	}
	if !c.UpdatedAt.After(c.CreatedAt) {
		t.Errorf("updated_at harus maju: %v -> %v", c.CreatedAt, c.UpdatedAt)
	}
	if got, _ := e.repo.FindComment(ctx, task.ID, c.ID); got.Content != "revisi" {
		t.Errorf("isi = %q", got.Content)
	}

	wrong := *c
	wrong.TaskID = other.ID
	wrong.Content = "dibajak"
	if err := e.repo.UpdateComment(ctx, &wrong); !errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("update lewat task lain harus ErrCommentNotFound, dapat %v", err)
	}
	if got, _ := e.repo.FindComment(ctx, task.ID, c.ID); got.Content != "revisi" {
		t.Error("isi tidak boleh berubah lewat task lain")
	}
}

func TestDeleteComment(t *testing.T) {
	e := setup(t)
	task, other := e.create(t, "T", nil), e.create(t, "Lain", nil)
	c, keep := e.comment(t, task.ID, e.owner, "hapus"), e.comment(t, task.ID, e.owner, "simpan")

	if err := e.repo.DeleteComment(ctx, other.ID, c.ID); !errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("hapus lewat task lain harus ErrCommentNotFound, dapat %v", err)
	}
	if err := e.repo.DeleteComment(ctx, task.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DeleteComment(ctx, task.ID, c.ID); !errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("hapus kedua kali: dapat %v", err)
	}
	if _, err := e.repo.FindComment(ctx, task.ID, keep.ID); err != nil {
		t.Error("komentar lain harus tetap")
	}
}

func TestComments_CascadeWithTask(t *testing.T) {
	e := setup(t)
	task := e.create(t, "T", nil)
	e.comment(t, task.ID, e.owner, "x")
	if err := e.repo.Delete(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, e.db, "SELECT COUNT(*) FROM task_comments WHERE task_id = ?", task.ID); n != 0 {
		t.Error("komentar harus ikut terhapus bersama task")
	}
}

func TestComments_CancelledContext(t *testing.T) {
	e := setup(t)
	task := e.create(t, "T", nil)
	c := e.comment(t, task.ID, e.owner, "x")
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := e.repo.CreateComment(cctx, &domain.Comment{TaskID: task.ID, AuthorID: e.owner, Content: "y"}); err == nil || errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("CreateComment: %v", err)
	}
	if _, _, err := e.repo.ListComments(cctx, task.ID, 10, 0); err == nil {
		t.Error("ListComments harus error")
	}
	if _, err := e.repo.FindComment(cctx, task.ID, c.ID); err == nil || errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("FindComment: %v", err)
	}
	if err := e.repo.UpdateComment(cctx, c); err == nil || errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("UpdateComment: %v", err)
	}
	if err := e.repo.DeleteComment(cctx, task.ID, c.ID); err == nil || errors.Is(err, domain.ErrCommentNotFound) {
		t.Errorf("DeleteComment: %v", err)
	}
}
