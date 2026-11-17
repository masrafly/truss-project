package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/tasks/domain"
)

// commentRecord: model persistence tabel task_comments. Kolom penulis bernama user_id.
type commentRecord struct {
	ID        uuid.UUID `gorm:"column:internal_id;type:uuid;primaryKey;default:gen_random_uuid()"`
	TaskID    uuid.UUID `gorm:"type:uuid;not null"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	Content   string    `gorm:"type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (commentRecord) TableName() string { return "task_comments" }

type commentRow struct {
	ID         uuid.UUID `gorm:"column:internal_id"`
	TaskID     uuid.UUID `gorm:"column:task_id"`
	UserID     uuid.UUID `gorm:"column:user_id"`
	AuthorName *string   `gorm:"column:author_name"`
	Content    string    `gorm:"column:content"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (r commentRow) toDomain() domain.Comment {
	author := domain.UserRef{ID: r.UserID}
	if r.AuthorName != nil {
		author.Name = *r.AuthorName
	}
	return domain.Comment{
		ID: r.ID, TaskID: r.TaskID, AuthorID: r.UserID, Author: &author,
		Content: r.Content, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

const commentSelect = `c.internal_id, c.task_id, c.user_id, u.name AS author_name,
	c.content, c.created_at, c.updated_at`

func (r *TaskGormRepository) CreateComment(ctx context.Context, c *domain.Comment) error {
	rec := commentRecord{TaskID: c.TaskID, UserID: c.AuthorID, Content: c.Content}
	if err := r.db.WithContext(ctx).Create(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return domain.ErrTaskNotFound // task terhapus di tengah proses
		}
		return err
	}
	saved, err := r.FindComment(ctx, c.TaskID, rec.ID)
	if err != nil {
		return err
	}
	*c = *saved
	return nil
}

func (r *TaskGormRepository) ListComments(
	ctx context.Context,
	taskID uuid.UUID,
	limit, offset int,
) ([]domain.Comment, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&commentRecord{}).Where("task_id = ?", taskID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []commentRow
	err := r.db.WithContext(ctx).
		Table("task_comments AS c").
		Select(commentSelect).
		Joins("LEFT JOIN users AS u ON u.internal_id = c.user_id").
		Where("c.task_id = ?", taskID).
		Order("c.created_at, c.internal_id").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]domain.Comment, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toDomain())
	}
	return out, total, nil
}

// FindComment hanya menemukan komentar yang benar-benar milik taskID, sehingga
// ID komentar dari task lain tidak bisa dipakai lewat path task yang berbeda.
func (r *TaskGormRepository) FindComment(ctx context.Context, taskID, commentID uuid.UUID) (*domain.Comment, error) {
	var rows []commentRow
	err := r.db.WithContext(ctx).
		Table("task_comments AS c").
		Select(commentSelect).
		Joins("LEFT JOIN users AS u ON u.internal_id = c.user_id").
		Where("c.internal_id = ? AND c.task_id = ?", commentID, taskID).
		Limit(1).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrCommentNotFound
	}
	c := rows[0].toDomain()
	return &c, nil
}

func (r *TaskGormRepository) UpdateComment(ctx context.Context, c *domain.Comment) error {
	var updatedAt time.Time
	err := r.db.WithContext(ctx).Raw(
		`UPDATE task_comments SET content = ?, updated_at = NOW()
		  WHERE internal_id = ? AND task_id = ?
		RETURNING updated_at`,
		c.Content, c.ID, c.TaskID,
	).Row().Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrCommentNotFound
		}
		return err
	}
	c.UpdatedAt = updatedAt
	return nil
}

func (r *TaskGormRepository) DeleteComment(ctx context.Context, taskID, commentID uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&commentRecord{}, "internal_id = ? AND task_id = ?", commentID, taskID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrCommentNotFound
	}
	return nil
}
