package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"truss-project/internal/modules/workspaces/domain"
)

// memberRow: hasil JOIN workspace_members + users.
type memberRow struct {
	UserID   uuid.UUID `gorm:"column:user_id"`
	Name     string    `gorm:"column:name"`
	Email    string    `gorm:"column:email"`
	Role     string    `gorm:"column:role"`
	JoinedAt time.Time `gorm:"column:joined_at"`
}

func (r *WorkspaceGormRepository) AddMember(
	ctx context.Context,
	workspaceID, userID uuid.UUID,
	role domain.Role,
) (time.Time, error) {
	rec := memberRecord{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        string(role),
		JoinedAt:    time.Now(),
	}

	if err := r.db.WithContext(ctx).Create(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return time.Time{}, domain.ErrAlreadyMember
		}
		return time.Time{}, err
	}
	return rec.JoinedAt, nil
}

func (r *WorkspaceGormRepository) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.Member, error) {
	var rows []memberRow

	err := r.db.WithContext(ctx).
		Table("workspace_members AS wm").
		Select("wm.user_id, u.name, u.email, wm.role, wm.joined_at").
		// User yang di-soft-delete tidak ditampilkan.
		Joins("JOIN users AS u ON u.internal_id = wm.user_id AND u.deleted_at IS NULL").
		Where("wm.workspace_id = ?", workspaceID).
		Order("CASE wm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, wm.joined_at, u.email").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]domain.Member, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Member{
			UserID:   row.UserID,
			Name:     row.Name,
			Email:    row.Email,
			Role:     domain.Role(row.Role),
			JoinedAt: row.JoinedAt,
		})
	}
	return out, nil
}

// RemoveMember menghapus keanggotaan workspace beserta seluruh keanggotaan project
// user tersebut di workspace ini, dan melepasnya dari task yang ditugaskan kepadanya
// (tercatat sebagai aktivitas oleh actorID), dalam SATU transaksi.
//
// Jika user masih Owner suatu project di workspace ini, tidak ada yang dihapus dan
// hasilnya ErrMemberOwnsProjects. Baris keanggotaan dikunci (FOR UPDATE) lebih dulu;
// pembuatan project mengunci baris yang sama (FOR SHARE), sehingga user tidak bisa
// menjadi Owner project baru di tengah proses keluar (lihat modul projects).
//
// Catatan: repository ini sengaja menyentuh tabel projects/project_members agar
// pembersihan atomik dengan penghapusan keanggotaan workspace.
func (r *WorkspaceGormRepository) RemoveMember(ctx context.Context, workspaceID, userID, actorID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec memberRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
			First(&rec).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrMemberNotFound
			}
			return err
		}

		var owned int64
		err = tx.Table("project_members AS pm").
			Joins("JOIN projects AS p ON p.internal_id = pm.project_id").
			Where("p.workspace_id = ? AND pm.user_id = ? AND pm.role = 'owner'", workspaceID, userID).
			Count(&owned).Error
		if err != nil {
			return err
		}
		if owned > 0 {
			return domain.ErrMemberOwnsProjects
		}

		// Lepas user dari task di workspace ini (assignee harus anggota project) dan catat
		// sebagai aktivitas atas nama actorID.
		err = tx.Exec(
			`WITH unassigned AS (
			     UPDATE tasks SET assignee_id = NULL, updated_at = NOW()
			      WHERE assignee_id = ?
			        AND project_id IN (SELECT internal_id FROM projects WHERE workspace_id = ?)
			  RETURNING internal_id
			 )
			 INSERT INTO task_activities (task_id, actor_id, activity_type, old_value, new_value, created_at)
			 SELECT internal_id, ?, 'assignee_changed', to_jsonb(CAST(? AS text)), NULL, clock_timestamp()
			   FROM unassigned`,
			userID, workspaceID, actorID, userID.String(),
		).Error
		if err != nil {
			return err
		}

		err = tx.Exec(
			`DELETE FROM project_members
			 WHERE user_id = ?
			   AND project_id IN (SELECT internal_id FROM projects WHERE workspace_id = ?)`,
			userID, workspaceID,
		).Error
		if err != nil {
			return err
		}

		return tx.Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
			Delete(&memberRecord{}).Error
	})
}
