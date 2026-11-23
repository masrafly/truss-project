// Package repository mengimplementasikan usecase.ProjectRepository dengan GORM.
// Semua detail GORM/SQL berhenti di sini.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/projects/domain"
)

// projectRecord: model persistence tabel projects.
type projectRecord struct {
	ID          uuid.UUID `gorm:"column:internal_id;type:uuid;primaryKey;default:gen_random_uuid()"`
	WorkspaceID uuid.UUID `gorm:"type:uuid;not null"`
	Name        string    `gorm:"type:varchar(100);not null"`
	Description *string   `gorm:"type:text"`
	Status      string    `gorm:"type:varchar(20);not null"`
	CreatedBy   uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (projectRecord) TableName() string { return "projects" }

// projectMemberRecord: model persistence tabel project_members (PK gabungan).
type projectMemberRecord struct {
	ProjectID uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Role      string    `gorm:"type:varchar(20);not null"`
	JoinedAt  time.Time `gorm:"not null;default:now()"`
}

func (projectMemberRecord) TableName() string { return "project_members" }

// accessRow: project + peran workspace + peran project user yang meminta.
type accessRow struct {
	ID            uuid.UUID `gorm:"column:internal_id"`
	WorkspaceID   uuid.UUID `gorm:"column:workspace_id"`
	Name          string    `gorm:"column:name"`
	Description   *string   `gorm:"column:description"`
	Status        string    `gorm:"column:status"`
	CreatedBy     uuid.UUID `gorm:"column:created_by"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
	WorkspaceRole string    `gorm:"column:workspace_role"`
	ProjectRole   string    `gorm:"column:project_role"`
}

func (r accessRow) toDomain() domain.ProjectAccess {
	return domain.ProjectAccess{
		Project: domain.Project{
			ID:          r.ID,
			WorkspaceID: r.WorkspaceID,
			Name:        r.Name,
			Description: r.Description,
			Status:      domain.Status(r.Status),
			CreatedBy:   r.CreatedBy,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		},
		Access: domain.Access{
			WorkspaceRole: domain.WorkspaceRole(r.WorkspaceRole),
			ProjectRole:   domain.ProjectRole(r.ProjectRole),
		},
	}
}

const accessSelect = `p.internal_id, p.workspace_id, p.name, p.description, p.status,
	p.created_by, p.created_at, p.updated_at,
	COALESCE(wm.role, '') AS workspace_role, COALESCE(pm.role, '') AS project_role`

type ProjectGormRepository struct {
	db *gorm.DB
}

func NewProjectGormRepository(db *gorm.DB) *ProjectGormRepository {
	return &ProjectGormRepository{db: db}
}

// FindWorkspaceRole membaca tabel workspace_members (baca-saja; dimiliki modul workspaces).
func (r *ProjectGormRepository) FindWorkspaceRole(
	ctx context.Context,
	workspaceID, userID uuid.UUID,
) (domain.WorkspaceRole, error) {
	var role string
	err := r.db.WithContext(ctx).
		Raw("SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?", workspaceID, userID).
		Row().Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrNotWorkspaceMember
		}
		return "", err
	}
	return domain.WorkspaceRole(role), nil
}

// CreateWithOwner menyimpan project dan keanggotaan Owner dalam SATU transaksi.
// Baris keanggotaan workspace pembuat dikunci (FOR SHARE) agar pembuat tidak dapat
// dikeluarkan dari workspace di tengah proses (penghapusan anggota mengunci FOR UPDATE).
func (r *ProjectGormRepository) CreateWithOwner(ctx context.Context, p *domain.Project) error {
	rec := projectRecord{
		ID:          p.ID,
		WorkspaceID: p.WorkspaceID,
		Name:        p.Name,
		Description: p.Description,
		Status:      string(p.Status),
		CreatedBy:   p.CreatedBy,
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var role string
		err := tx.Raw(
			"SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ? FOR SHARE",
			p.WorkspaceID, p.CreatedBy,
		).Row().Scan(&role)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrNotWorkspaceMember
			}
			return err
		}

		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		return tx.Create(&projectMemberRecord{
			ProjectID: rec.ID,
			UserID:    rec.CreatedBy,
			Role:      string(domain.ProjectOwner),
			JoinedAt:  time.Now(),
		}).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			// Praktis tak terjangkau: keanggotaan workspace terkunci menjamin user dan
			// workspace masih ada. Dipetakan agar tidak menjadi 500 yang membingungkan.
			return domain.ErrUserNotFound
		}
		return err
	}

	p.ID = rec.ID
	p.CreatedAt = rec.CreatedAt
	p.UpdatedAt = rec.UpdatedAt
	return nil
}

// ListAccessible: anggota workspace melihat project tempat ia menjadi anggota;
// Owner/Admin workspace melihat semua project di workspace tersebut.
func (r *ProjectGormRepository) ListAccessible(
	ctx context.Context,
	userID uuid.UUID,
	workspaceID *uuid.UUID,
	limit, offset int,
) ([]domain.ProjectAccess, int64, error) {
	// Aturan visibilitas dipakai bersama oleh penghitung total dan pengambil halaman.
	visible := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("projects AS p").
			Joins("JOIN workspace_members AS wm ON wm.workspace_id = p.workspace_id AND wm.user_id = ?", userID).
			Joins("LEFT JOIN project_members AS pm ON pm.project_id = p.internal_id AND pm.user_id = ?", userID).
			Where("pm.user_id IS NOT NULL OR wm.role IN ('owner', 'admin')")
		if workspaceID != nil {
			q = q.Where("p.workspace_id = ?", *workspaceID)
		}
		return q
	}

	var total int64
	if err := visible().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []accessRow
	if err := visible().
		Select(accessSelect).
		Order("p.created_at DESC, p.internal_id").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	out := make([]domain.ProjectAccess, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toDomain())
	}
	return out, total, nil
}

func (r *ProjectGormRepository) FindWithAccess(
	ctx context.Context,
	projectID, userID uuid.UUID,
) (*domain.ProjectAccess, error) {
	var rows []accessRow
	err := r.db.WithContext(ctx).
		Table("projects AS p").
		Select(accessSelect).
		Joins("LEFT JOIN workspace_members AS wm ON wm.workspace_id = p.workspace_id AND wm.user_id = ?", userID).
		Joins("LEFT JOIN project_members AS pm ON pm.project_id = p.internal_id AND pm.user_id = ?", userID).
		Where("p.internal_id = ?", projectID).
		Limit(1).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrProjectNotFound
	}
	a := rows[0].toDomain()
	return &a, nil
}

func (r *ProjectGormRepository) Update(ctx context.Context, p *domain.Project) error {
	var updatedAt time.Time
	err := r.db.WithContext(ctx).Raw(
		`UPDATE projects
		    SET name = ?, description = ?, status = ?, updated_at = NOW()
		  WHERE internal_id = ?
		RETURNING updated_at`,
		p.Name, p.Description, string(p.Status), p.ID,
	).Row().Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrProjectNotFound
		}
		return err
	}
	p.UpdatedAt = updatedAt
	return nil
}

func (r *ProjectGormRepository) Delete(ctx context.Context, projectID uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&projectRecord{}, "internal_id = ?", projectID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrProjectNotFound
	}
	return nil
}
