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

func (r *WorkspaceGormRepository) ListMembers(
	ctx context.Context,
	workspaceID uuid.UUID,
	limit, offset int,
) ([]domain.Member, int64, error) {
	// Total memakai JOIN yang sama agar user yang di-soft-delete tidak ikut terhitung.
	var total int64
	if err := r.db.WithContext(ctx).
		Table("workspace_members AS wm").
		Joins("JOIN users AS u ON u.internal_id = wm.user_id AND u.deleted_at IS NULL").
		Where("wm.workspace_id = ?", workspaceID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []memberRow
	err := r.db.WithContext(ctx).
		Table("workspace_members AS wm").
		Select("wm.user_id, u.name, u.email, wm.role, wm.joined_at").
		// User yang di-soft-delete tidak ditampilkan.
		Joins("JOIN users AS u ON u.internal_id = wm.user_id AND u.deleted_at IS NULL").
		Where("wm.workspace_id = ?", workspaceID).
		Order("CASE wm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, wm.joined_at, u.email").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
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
	return out, total, nil
}

// lockMemberRoles mengunci baris keanggotaan yang disebut (urut user_id: urutan seragam
// mencegah deadlock antar transaksi yang mengunci pasangan baris yang sama) dan mengembalikan
// peran masing-masing. User yang bukan anggota tidak ada di hasil.
func lockMemberRoles(tx *gorm.DB, workspaceID uuid.UUID, ids ...uuid.UUID) (map[uuid.UUID]domain.Role, error) {
	type row struct {
		UserID uuid.UUID `gorm:"column:user_id"`
		Role   string    `gorm:"column:role"`
	}
	var rows []row
	err := tx.Raw(
		`SELECT user_id, role FROM workspace_members
		  WHERE workspace_id = ? AND user_id IN ?
		  ORDER BY user_id
		  FOR UPDATE`,
		workspaceID, ids,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]domain.Role, len(rows))
	for _, r := range rows {
		out[r.UserID] = domain.Role(r.Role)
	}
	return out, nil
}

func (r *WorkspaceGormRepository) ChangeMemberRole(
	ctx context.Context,
	workspaceID, actorID, targetID uuid.UUID,
	role domain.Role,
) (*domain.Member, error) {
	var out *domain.Member
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		roles, err := lockMemberRoles(tx, workspaceID, actorID, targetID)
		if err != nil {
			return err
		}
		if !domain.CanChangeRole(roles[actorID]) {
			return domain.ErrForbidden
		}
		targetRole, ok := roles[targetID]
		if !ok {
			return domain.ErrMemberNotFound
		}
		if targetRole == domain.RoleOwner {
			return domain.ErrCannotChangeOwnerRole
		}

		if err := tx.Exec(
			"UPDATE workspace_members SET role = ? WHERE workspace_id = ? AND user_id = ?",
			string(role), workspaceID, targetID,
		).Error; err != nil {
			return err
		}

		var rows []memberRow
		if err := tx.Raw(
			`SELECT wm.user_id, u.name, u.email, wm.role, wm.joined_at
			   FROM workspace_members AS wm
			   JOIN users AS u ON u.internal_id = wm.user_id AND u.deleted_at IS NULL
			  WHERE wm.workspace_id = ? AND wm.user_id = ?`,
			workspaceID, targetID,
		).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 { // akun target sudah dihapus (soft delete): tidak tampil di daftar anggota; batalkan
			return domain.ErrMemberNotFound
		}
		m := rows[0]
		out = &domain.Member{UserID: m.UserID, Name: m.Name, Email: m.Email, Role: domain.Role(m.Role), JoinedAt: m.JoinedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *WorkspaceGormRepository) TransferOwnership(
	ctx context.Context,
	workspaceID, actorID, targetID uuid.UUID,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		roles, err := lockMemberRoles(tx, workspaceID, actorID, targetID)
		if err != nil {
			return err
		}
		if !domain.CanTransferOwnership(roles[actorID]) {
			return domain.ErrForbidden
		}
		if _, ok := roles[targetID]; !ok || actorID == targetID {
			return domain.ErrMemberNotFound
		}

		// Satu pernyataan: tidak pernah ada keadaan antara tanpa Owner atau dengan dua Owner.
		return tx.Exec(
			`UPDATE workspace_members
			    SET role = CASE WHEN user_id = ? THEN 'owner' ELSE 'admin' END
			  WHERE workspace_id = ? AND user_id IN (?, ?)`,
			targetID, workspaceID, actorID, targetID,
		).Error
	})
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
