// Package repository mengimplementasikan usecase.WorkspaceRepository dengan GORM.
// Semua detail GORM/SQL berhenti di sini.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/workspaces/domain"
)

// workspaceRecord: model persistence tabel workspaces.
type workspaceRecord struct {
	ID          uuid.UUID `gorm:"column:internal_id;type:uuid;primaryKey;default:gen_random_uuid()"`
	Name        string    `gorm:"type:varchar(100);not null"`
	Description *string   `gorm:"type:text"`
	CreatedBy   uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (workspaceRecord) TableName() string { return "workspaces" }

func (r workspaceRecord) toDomain() *domain.Workspace {
	return &domain.Workspace{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		CreatedBy:   r.CreatedBy,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// memberRecord: model persistence tabel workspace_members (PK gabungan).
type memberRecord struct {
	WorkspaceID uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	Role        string    `gorm:"type:varchar(20);not null"`
	JoinedAt    time.Time `gorm:"not null;default:now()"`
}

func (memberRecord) TableName() string { return "workspace_members" }

// accessRow: hasil JOIN workspaces + workspace_members untuk daftar workspace user.
type accessRow struct {
	ID          uuid.UUID `gorm:"column:internal_id"`
	Name        string    `gorm:"column:name"`
	Description *string   `gorm:"column:description"`
	CreatedBy   uuid.UUID `gorm:"column:created_by"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
	Role        string    `gorm:"column:role"`
}

type WorkspaceGormRepository struct {
	db *gorm.DB
}

func NewWorkspaceGormRepository(db *gorm.DB) *WorkspaceGormRepository {
	return &WorkspaceGormRepository{db: db}
}

// CreateWithOwner menyimpan workspace dan keanggotaan Owner dalam SATU transaksi.
func (r *WorkspaceGormRepository) CreateWithOwner(ctx context.Context, w *domain.Workspace) error {
	rec := workspaceRecord{
		ID:          w.ID,
		Name:        w.Name,
		Description: w.Description,
		CreatedBy:   w.CreatedBy,
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		return tx.Create(&memberRecord{
			WorkspaceID: rec.ID,
			UserID:      rec.CreatedBy,
			Role:        string(domain.RoleOwner),
		}).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			// Satu-satunya FK yang bisa dilanggar di sini adalah created_by -> users.
			return domain.ErrUserNotFound
		}
		return err
	}

	w.ID = rec.ID
	w.CreatedAt = rec.CreatedAt
	w.UpdatedAt = rec.UpdatedAt
	return nil
}

func (r *WorkspaceGormRepository) ListByMember(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int,
) ([]domain.WorkspaceAccess, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).
		Table("workspace_members").
		Where("user_id = ?", userID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []accessRow
	err := r.db.WithContext(ctx).
		Table("workspaces AS w").
		Select("w.internal_id, w.name, w.description, w.created_by, w.created_at, w.updated_at, wm.role").
		Joins("JOIN workspace_members AS wm ON wm.workspace_id = w.internal_id").
		Where("wm.user_id = ?", userID).
		Order("w.created_at DESC, w.internal_id").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	out := make([]domain.WorkspaceAccess, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.WorkspaceAccess{
			Workspace: domain.Workspace{
				ID:          row.ID,
				Name:        row.Name,
				Description: row.Description,
				CreatedBy:   row.CreatedBy,
				CreatedAt:   row.CreatedAt,
				UpdatedAt:   row.UpdatedAt,
			},
			Role: domain.Role(row.Role),
		})
	}
	return out, total, nil
}

func (r *WorkspaceGormRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error) {
	var rec workspaceRecord
	err := r.db.WithContext(ctx).First(&rec, "internal_id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWorkspaceNotFound
		}
		return nil, err
	}
	return rec.toDomain(), nil
}

func (r *WorkspaceGormRepository) Delete(ctx context.Context, workspaceID, actorID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		roles, err := lockMemberRoles(tx, workspaceID, actorID)
		if err != nil {
			return err
		}
		role, ok := roles[actorID]
		if !ok {
			return domain.ErrWorkspaceNotFound
		}
		if !domain.CanDeleteWorkspace(role) {
			return domain.ErrForbidden
		}
		// Anggota, project, anggota project, task, komentar, dan riwayat ikut terhapus lewat
		// ON DELETE CASCADE.
		res := tx.Exec("DELETE FROM workspaces WHERE internal_id = ?", workspaceID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrWorkspaceNotFound
		}
		return nil
	})
}

func (r *WorkspaceGormRepository) FindMemberRole(
	ctx context.Context,
	workspaceID, userID uuid.UUID,
) (domain.Role, error) {
	var rec memberRecord
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", domain.ErrNotMember
		}
		return "", err
	}
	return domain.Role(rec.Role), nil
}
