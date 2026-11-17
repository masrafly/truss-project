package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

// fakeCommentRepo memperluas fakeRepo (task_usecase_test.go) dengan penyimpanan komentar.
type fakeCommentRepo struct {
	*fakeRepo
	comments map[uuid.UUID]domain.Comment

	createErr, listErr, updateErr, deleteErr error
	createCalls, updateCalls, deleteCalls    int
	lastLimit, lastOffset                    int
}

func (r *fakeCommentRepo) CreateComment(_ context.Context, c *domain.Comment) error {
	r.createCalls++
	if r.createErr != nil {
		return r.createErr
	}
	c.ID = uuid.New()
	c.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c.UpdatedAt = c.CreatedAt
	c.Author = &domain.UserRef{ID: c.AuthorID, Name: "Penulis"}
	r.comments[c.ID] = *c
	return nil
}

func (r *fakeCommentRepo) ListComments(_ context.Context, tid uuid.UUID, limit, offset int) ([]domain.Comment, int64, error) {
	r.lastLimit, r.lastOffset = limit, offset
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	var n int64
	for _, c := range r.comments {
		if c.TaskID == tid {
			n++
		}
	}
	return nil, n, nil
}

func (r *fakeCommentRepo) FindComment(_ context.Context, tid, cid uuid.UUID) (*domain.Comment, error) {
	c, ok := r.comments[cid]
	if !ok || c.TaskID != tid {
		return nil, domain.ErrCommentNotFound
	}
	return &c, nil
}

func (r *fakeCommentRepo) UpdateComment(_ context.Context, c *domain.Comment) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	c.UpdatedAt = c.CreatedAt.Add(time.Hour)
	r.comments[c.ID] = *c
	return nil
}

func (r *fakeCommentRepo) DeleteComment(_ context.Context, tid, cid uuid.UUID) error {
	r.deleteCalls++
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.comments, cid)
	return nil
}

type commentFixture struct {
	*fixture
	crepo *fakeCommentRepo
	cuc   *usecase.CommentUseCase
	cid   uuid.UUID // komentar milik bystander pada task fixture
}

func newCommentFixture() *commentFixture {
	f := &commentFixture{fixture: newFixture(), cid: uuid.New()}
	f.crepo = &fakeCommentRepo{fakeRepo: f.repo, comments: map[uuid.UUID]domain.Comment{
		f.cid: {ID: f.cid, TaskID: f.tid, AuthorID: f.bystander, Content: "Awal", CreatedAt: time.Now()},
	}}
	f.cuc = usecase.NewCommentUseCase(f.crepo, nil)
	return f
}

// ---------- Create ----------

func TestCommentCreate_AuthorFromIdentityAndTrimmed(t *testing.T) {
	f := newCommentFixture()

	got, err := f.cuc.Create(ctx, f.wsOwner.String(), f.tid.String(), "  Sudah dicek  ")

	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorID != f.wsOwner || got.TaskID != f.tid || got.Content != "Sudah dicek" || got.ID == uuid.Nil {
		t.Errorf("hasil = %+v", got)
	}
}

func TestCommentCreate_AccessAndValidation(t *testing.T) {
	f := newCommentFixture()
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.plain, "orang luar": f.outsider} {
		if _, err := f.cuc.Create(ctx, uid.String(), f.tid.String(), "x"); !errors.Is(err, domain.ErrTaskNotFound) {
			t.Errorf("%s harus 404, dapat %v", name, err)
		}
	}
	for name, content := range map[string]string{"kosong": "", "spasi": "  \n\t ", "5001": strings.Repeat("k", 5001)} {
		if _, err := f.cuc.Create(ctx, f.bystander.String(), f.tid.String(), content); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: harapannya ErrInvalidInput, dapat %v", name, err)
		}
	}
	if _, err := f.cuc.Create(ctx, f.bystander.String(), f.tid.String(), strings.Repeat("é", 5000)); err != nil {
		t.Errorf("5000 karakter harus diterima: %v", err)
	}
	if f.crepo.createCalls != 1 {
		t.Errorf("hanya satu penyimpanan yang sah, dapat %d", f.crepo.createCalls)
	}
}

func TestCommentCreate_ArchivedAndErrors(t *testing.T) {
	f := newCommentFixture()
	f.repo.archived = true
	if _, err := f.cuc.Create(ctx, f.bystander.String(), f.tid.String(), "x"); !errors.Is(err, domain.ErrProjectArchived) {
		t.Errorf("dapat %v", err)
	}
	f.repo.archived = false
	boom := errors.New("db mati")
	f.crepo.createErr = boom
	if _, err := f.cuc.Create(ctx, f.bystander.String(), f.tid.String(), "x"); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.cuc.Create(ctx, "x", f.tid.String(), "x"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.cuc.Create(ctx, f.bystander.String(), "x", "x"); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("dapat %v", err)
	}
}

// ---------- List ----------

func TestCommentList_Pagination(t *testing.T) {
	f := newCommentFixture()
	res, err := f.cuc.List(ctx, f.bystander.String(), f.tid.String(), 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	if f.crepo.lastLimit != 10 || f.crepo.lastOffset != 20 || res.Total != 1 || res.Items == nil {
		t.Errorf("limit=%d offset=%d res=%+v", f.crepo.lastLimit, f.crepo.lastOffset, res)
	}
	res, _ = f.cuc.List(ctx, f.bystander.String(), f.tid.String(), 0, 0)
	if res.Page != 1 || res.Limit != 50 || f.crepo.lastOffset != 0 {
		t.Errorf("default salah: %+v", res)
	}
	for _, pl := range [][2]int{{-1, 10}, {1, -1}, {1, 101}} {
		if _, err := f.cuc.List(ctx, f.bystander.String(), f.tid.String(), pl[0], pl[1]); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("page=%d limit=%d: dapat %v", pl[0], pl[1], err)
		}
	}
	if _, err := f.cuc.List(ctx, f.outsider.String(), f.tid.String(), 0, 0); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("orang luar harus 404 (PRD FR-07 #3), dapat %v", err)
	}
}

// ---------- Update ----------

func TestCommentUpdate_OnlyAuthor(t *testing.T) {
	f := newCommentFixture()

	for name, uid := range map[string]uuid.UUID{"pengelola": f.pAdmin, "owner ws": f.wsOwner, "anggota lain": f.creator} {
		if _, err := f.cuc.Update(ctx, uid.String(), f.tid.String(), f.cid.String(), "diubah"); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s harus 403, dapat %v", name, err)
		}
	}
	if f.crepo.updateCalls != 0 {
		t.Fatal("tidak boleh ada penyimpanan")
	}

	got, err := f.cuc.Update(ctx, f.bystander.String(), f.tid.String(), f.cid.String(), "  Revisi ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "Revisi" || !got.UpdatedAt.After(got.CreatedAt) {
		t.Errorf("hasil = %+v", got)
	}
}

func TestCommentUpdate_NotFoundAndValidation(t *testing.T) {
	f := newCommentFixture()
	otherTask := uuid.New()
	f.repo.tasks[otherTask] = domain.Task{ID: otherTask, ProjectID: f.pid, CreatedBy: f.creator, Title: "Lain"}

	cases := map[string]struct {
		task, comment, content string
		want                   error
	}{
		"komentar tidak ada":         {f.tid.String(), uuid.NewString(), "x", domain.ErrCommentNotFound},
		"ID komentar rusak":          {f.tid.String(), "x", "x", domain.ErrCommentNotFound},
		"komentar milik task lain":   {otherTask.String(), f.cid.String(), "x", domain.ErrCommentNotFound},
		"isi kosong":                 {f.tid.String(), f.cid.String(), " ", domain.ErrInvalidInput},
		"task tidak terlihat (luar)": {f.tid.String(), f.cid.String(), "x", domain.ErrTaskNotFound},
	}
	for name, c := range cases {
		uid := f.bystander
		if name == "task tidak terlihat (luar)" {
			uid = f.outsider
		}
		if _, err := f.cuc.Update(ctx, uid.String(), c.task, c.comment, c.content); !errors.Is(err, c.want) {
			t.Errorf("%s: harapannya %v, dapat %v", name, c.want, err)
		}
	}
	f.repo.archived = true
	if _, err := f.cuc.Update(ctx, f.bystander.String(), f.tid.String(), f.cid.String(), "x"); !errors.Is(err, domain.ErrProjectArchived) {
		t.Errorf("archived: dapat %v", err)
	}
	if f.crepo.updateCalls != 0 {
		t.Error("tidak boleh ada penyimpanan")
	}
}

// ---------- Delete ----------

func TestCommentDelete_AuthorOrManager(t *testing.T) {
	for name, pick := range map[string]func(*commentFixture) uuid.UUID{
		"penulis":       func(f *commentFixture) uuid.UUID { return f.bystander },
		"admin project": func(f *commentFixture) uuid.UUID { return f.pAdmin },
		"owner ws":      func(f *commentFixture) uuid.UUID { return f.wsOwner },
	} {
		f := newCommentFixture()
		if err := f.cuc.Delete(ctx, pick(f).String(), f.tid.String(), f.cid.String()); err != nil {
			t.Errorf("%s harus boleh menghapus: %v", name, err)
		}
		if _, ok := f.crepo.comments[f.cid]; ok {
			t.Errorf("%s: komentar harus terhapus", name)
		}
	}

	f := newCommentFixture()
	// Pembuat TASK bukan penulis KOMENTAR: tidak boleh menghapus komentar orang lain.
	for name, uid := range map[string]uuid.UUID{"pembuat task": f.creator, "assignee task": f.assignee} {
		if err := f.cuc.Delete(ctx, uid.String(), f.tid.String(), f.cid.String()); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s harus 403, dapat %v", name, err)
		}
	}
	if err := f.cuc.Delete(ctx, f.outsider.String(), f.tid.String(), f.cid.String()); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("orang luar harus 404, dapat %v", err)
	}
	f.repo.archived = true
	if err := f.cuc.Delete(ctx, f.bystander.String(), f.tid.String(), f.cid.String()); !errors.Is(err, domain.ErrProjectArchived) {
		t.Errorf("archived: dapat %v", err)
	}
	if f.crepo.deleteCalls != 0 {
		t.Error("tidak boleh ada penghapusan")
	}
}
