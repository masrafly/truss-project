package usecase

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
)

const (
	maxCommentLength    = 5000 // karakter
	defaultCommentLimit = 50
	maxCommentPageLimit = 100
)

type CommentListResult struct {
	Items []domain.Comment
	Total int64
	Page  int
	Limit int
}

type CommentUseCase struct {
	repo CommentRepository
	log  *slog.Logger
}

// log boleh nil (mis. pada unit test); dalam kasus itu log dibuang.
func NewCommentUseCase(repo CommentRepository, log *slog.Logger) *CommentUseCase {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &CommentUseCase{repo: repo, log: log.With("module", "tasks")}
}

// Create: dapat melihat task (404) -> isi valid (400) -> izin (403) -> tidak diarsipkan (409).
// Penulis selalu diambil dari identitas terautentikasi (PRD FR-07 #2).
func (u *CommentUseCase) Create(ctx context.Context, userID, taskID, content string) (*domain.Comment, error) {
	uid, task, access, err := u.loadTask(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	content, err = validateContent(content)
	if err != nil {
		return nil, err
	}
	if !access.CanComment() {
		return nil, domain.ErrForbidden
	}
	if err := access.CheckWritable(); err != nil {
		return nil, err
	}

	c := &domain.Comment{TaskID: task.ID, AuthorID: uid, Content: content}
	if err := u.repo.CreateComment(ctx, c); err != nil {
		return nil, err
	}
	u.log.InfoContext(ctx, "komentar dibuat", "comment_id", c.ID, "task_id", task.ID, "user_id", uid)
	return c, nil
}

// List: komentar task, terlama lebih dulu, dengan pagination.
func (u *CommentUseCase) List(ctx context.Context, userID, taskID string, page, limit int) (*CommentListResult, error) {
	_, task, _, err := u.loadTask(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = defaultCommentLimit
	}
	if page < 1 || limit < 1 || limit > maxCommentPageLimit {
		return nil, domain.ErrInvalidInput
	}

	items, total, err := u.repo.ListComments(ctx, task.ID, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.Comment{}
	}
	return &CommentListResult{Items: items, Total: total, Page: page, Limit: limit}, nil
}

// Update: dapat melihat task (404) -> komentar milik task (404) -> isi valid (400)
// -> hanya penulis (403) -> tidak diarsipkan (409).
func (u *CommentUseCase) Update(ctx context.Context, userID, taskID, commentID, content string) (*domain.Comment, error) {
	uid, task, access, err := u.loadTask(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	c, err := u.findComment(ctx, task.ID, commentID)
	if err != nil {
		return nil, err
	}
	content, err = validateContent(content)
	if err != nil {
		return nil, err
	}
	if !access.CanEditComment(*c, uid) {
		u.log.WarnContext(ctx, "perubahan komentar ditolak: bukan penulis",
			"comment_id", c.ID, "task_id", task.ID, "user_id", uid)
		return nil, domain.ErrForbidden
	}
	if err := access.CheckWritable(); err != nil {
		return nil, err
	}

	c.Content = content
	if err := u.repo.UpdateComment(ctx, c); err != nil {
		return nil, err
	}
	u.log.InfoContext(ctx, "komentar diperbarui", "comment_id", c.ID, "task_id", task.ID, "user_id", uid)
	return c, nil
}

// Delete: dapat melihat task (404) -> komentar milik task (404) -> penulis atau
// pengelola (403) -> tidak diarsipkan (409).
func (u *CommentUseCase) Delete(ctx context.Context, userID, taskID, commentID string) error {
	uid, task, access, err := u.loadTask(ctx, userID, taskID)
	if err != nil {
		return err
	}
	c, err := u.findComment(ctx, task.ID, commentID)
	if err != nil {
		return err
	}
	if !access.CanDeleteComment(*c, uid) {
		u.log.WarnContext(ctx, "penghapusan komentar ditolak",
			"comment_id", c.ID, "task_id", task.ID, "user_id", uid,
			"workspace_role", access.WorkspaceRole, "project_role", access.ProjectRole)
		return domain.ErrForbidden
	}
	if err := access.CheckWritable(); err != nil {
		return err
	}
	if err := u.repo.DeleteComment(ctx, task.ID, c.ID); err != nil {
		return err
	}
	u.log.InfoContext(ctx, "komentar dihapus",
		"comment_id", c.ID, "task_id", task.ID, "user_id", uid, "by_author", c.AuthorID == uid)
	return nil
}

// ---------- helpers ----------

func (u *CommentUseCase) loadTask(
	ctx context.Context,
	userID, taskID string,
) (uuid.UUID, *domain.TaskView, *domain.ProjectAccess, error) {
	return loadVisibleTask(ctx, u.repo, u.log, userID, taskID)
}

func (u *CommentUseCase) findComment(ctx context.Context, taskID uuid.UUID, commentID string) (*domain.Comment, error) {
	cid, err := uuid.Parse(commentID)
	if err != nil {
		return nil, domain.ErrCommentNotFound
	}
	return u.repo.FindComment(ctx, taskID, cid)
}

// validateContent: di-trim, wajib, maksimal 5000 karakter (PRD FR-07 #4: komentar kosong ditolak).
func validateContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > maxCommentLength {
		return "", domain.ErrInvalidInput
	}
	return content, nil
}
