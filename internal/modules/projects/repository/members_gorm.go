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

// memberRow: hasil JOIN project_members + users.
type memberRow struct {
	UserID   uuid.UUID `gorm:"column:user_id"`
	Name     string    `gorm:"column:name"`
	Email    string    `gorm:"column:email"`
	Role     string    `gorm:"column:role"`
	JoinedAt time.Time `gorm:"column:joined_at"`
}

// FindWorkspaceMemberByEmail mencari user aktif (tidak di-soft-delete) dengan email
// tersebut yang menjadi anggota workspace pemilik project. Email tak terdaftar dan
// bukan-anggota-workspace sengaja menghasilkan error yang sama.
func (r *ProjectGormRepository) FindWorkspaceMemberByEmail(
	ctx context.Context,
	projectID uuid.UUID,
	email string,
) (*domain.UserRef, error) {
	var ref domain.UserRef
	err := r.db.WithContext(ctx).Raw(
		`SELECT u.internal_id, u.name, u.email
		   FROM users AS u
		   JOIN workspace_members AS wm ON wm.user_id = u.internal_id
		   JOIN projects AS p ON p.workspace_id = wm.workspace_id
		  WHERE p.internal_id = ? AND u.email = ? AND u.deleted_at IS NULL`,
		projectID, email,
	).Row().Scan(&ref.ID, &ref.Name, &ref.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWorkspaceUserNotFound
		}
		return nil, err
	}
	return &ref, nil
}

func (r *ProjectGormRepository) FindMemberRole(
	ctx context.Context,
	projectID, userID uuid.UUID,
) (domain.ProjectRole, error) {
	var role string
	err := r.db.WithContext(ctx).
		Raw("SELECT role FROM project_members WHERE project_id = ? AND user_id = ?", projectID, userID).
		Row().Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrMemberNotFound
		}
		return "", err
	}
	return domain.ProjectRole(role), nil
}

// AddMember menyimpan keanggotaan project dalam transaksi yang mengunci keanggotaan
// workspace target (FOR SHARE). Proses keluar workspace mengunci baris yang sama
// (FOR UPDATE), sehingga target tidak bisa menjadi anggota project setelah ia keluar.
func (r *ProjectGormRepository) AddMember(
	ctx context.Context,
	projectID, userID uuid.UUID,
	role domain.ProjectRole,
) (time.Time, error) {
	rec := projectMemberRecord{
		ProjectID: projectID,
		UserID:    userID,
		Role:      string(role),
		JoinedAt:  time.Now(),
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var one int
		err := tx.Raw(
			`SELECT 1
			   FROM workspace_members AS wm
			   JOIN projects AS p ON p.workspace_id = wm.workspace_id
			  WHERE p.internal_id = ? AND wm.user_id = ?
			  FOR SHARE OF wm`,
			projectID, userID,
		).Row().Scan(&one)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrWorkspaceUserNotFound
			}
			return err
		}
		return tx.Create(&rec).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return time.Time{}, domain.ErrAlreadyMember
		}
		return time.Time{}, err
	}
	return rec.JoinedAt, nil
}

func (r *ProjectGormRepository) ListMembers(
	ctx context.Context,
	projectID uuid.UUID,
	limit, offset int,
) ([]domain.Member, int64, error) {
	// Total memakai JOIN yang sama agar user yang di-soft-delete tidak ikut terhitung.
	var total int64
	if err := r.db.WithContext(ctx).
		Table("project_members AS pm").
		Joins("JOIN users AS u ON u.internal_id = pm.user_id AND u.deleted_at IS NULL").
		Where("pm.project_id = ?", projectID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []memberRow
	err := r.db.WithContext(ctx).
		Table("project_members AS pm").
		Select("pm.user_id, u.name, u.email, pm.role, pm.joined_at").
		// User yang di-soft-delete tidak ditampilkan.
		Joins("JOIN users AS u ON u.internal_id = pm.user_id AND u.deleted_at IS NULL").
		Where("pm.project_id = ?", projectID).
		Order("CASE pm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, pm.joined_at, u.email").
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
			Role:     domain.ProjectRole(row.Role),
			JoinedAt: row.JoinedAt,
		})
	}
	return out, total, nil
}

func (r *ProjectGormRepository) ChangeMemberRole(
	ctx context.Context,
	projectID, userID uuid.UUID,
	role domain.ProjectRole,
) (*domain.Member, error) {
	var out *domain.Member
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current string
		err := tx.Raw(
			"SELECT role FROM project_members WHERE project_id = ? AND user_id = ? FOR UPDATE",
			projectID, userID,
		).Row().Scan(&current)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrMemberNotFound
			}
			return err
		}
		if domain.ProjectRole(current) == domain.ProjectOwner {
			return domain.ErrCannotChangeOwnerRole
		}

		if err := tx.Exec(
			"UPDATE project_members SET role = ? WHERE project_id = ? AND user_id = ?",
			string(role), projectID, userID,
		).Error; err != nil {
			return err
		}

		var rows []memberRow
		if err := tx.Raw(
			`SELECT pm.user_id, u.name, u.email, pm.role, pm.joined_at
			   FROM project_members AS pm
			   JOIN users AS u ON u.internal_id = pm.user_id AND u.deleted_at IS NULL
			  WHERE pm.project_id = ? AND pm.user_id = ?`,
			projectID, userID,
		).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 { // akun target sudah dihapus (soft delete): tidak tampil di daftar anggota; batalkan
			return domain.ErrMemberNotFound
		}
		m := rows[0]
		out = &domain.Member{UserID: m.UserID, Name: m.Name, Email: m.Email, Role: domain.ProjectRole(m.Role), JoinedAt: m.JoinedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ProjectGormRepository) TransferOwnership(ctx context.Context, projectID, newOwnerID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Kunci keanggotaan workspace target (FOR SHARE), urutan yang sama dengan AddMember.
		//    Proses keluar workspace mengunci baris ini FOR UPDATE lebih dulu lalu memeriksa
		//    apakah ia Owner project: bila kita menang, ia menjadi Owner dan keluarnya ditolak
		//    (409); bila ia menang, target sudah bukan anggota dan kita berhenti di sini.
		var one int
		err := tx.Raw(
			`SELECT 1
			   FROM workspace_members AS wm
			   JOIN projects AS p ON p.workspace_id = wm.workspace_id
			  WHERE p.internal_id = ? AND wm.user_id = ?
			  FOR SHARE OF wm`,
			projectID, newOwnerID,
		).Row().Scan(&one)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrMemberNotFound
			}
			return err
		}

		// 2. Kunci baris Owner saat ini dan target (urut user_id: urutan seragam anti-deadlock).
		type row struct {
			UserID uuid.UUID `gorm:"column:user_id"`
			Role   string    `gorm:"column:role"`
		}
		var rows []row
		if err := tx.Raw(
			`SELECT user_id, role FROM project_members
			  WHERE project_id = ? AND (role = 'owner' OR user_id = ?)
			  ORDER BY user_id
			  FOR UPDATE`,
			projectID, newOwnerID,
		).Scan(&rows).Error; err != nil {
			return err
		}
		var targetRole string
		found := false
		for _, r := range rows {
			if r.UserID == newOwnerID {
				targetRole, found = r.Role, true
			}
		}
		if !found {
			return domain.ErrMemberNotFound
		}
		if domain.ProjectRole(targetRole) == domain.ProjectOwner {
			return domain.ErrInvalidInput // sudah menjadi Owner
		}

		// 3. Satu pernyataan: tidak pernah ada keadaan antara tanpa Owner atau dengan dua Owner.
		return tx.Exec(
			`UPDATE project_members
			    SET role = CASE WHEN user_id = ? THEN 'owner' ELSE 'admin' END
			  WHERE project_id = ? AND (user_id = ? OR role = 'owner')`,
			newOwnerID, projectID, newOwnerID,
		).Error
	})
}

// RemoveMember menghapus keanggotaan project dan, dalam transaksi yang sama, melepas
// user dari semua task yang ditugaskan kepadanya di project ini (dicatat sebagai
// aktivitas assignee_changed oleh actorID). Owner tidak pernah dihapus: filter
// role <> 'owner' adalah pertahanan terakhir bila pemeriksaan di use case terlewat.
func (r *ProjectGormRepository) RemoveMember(ctx context.Context, projectID, userID, actorID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.
			Where("project_id = ? AND user_id = ? AND role <> ?", projectID, userID, string(domain.ProjectOwner)).
			Delete(&projectMemberRecord{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrMemberNotFound
		}
		return tx.Exec(
			`WITH unassigned AS (
			     UPDATE tasks SET assignee_id = NULL, updated_at = NOW()
			      WHERE assignee_id = ? AND project_id = ?
			  RETURNING internal_id
			 )
			 INSERT INTO task_activities (task_id, actor_id, activity_type, old_value, new_value, created_at)
			 SELECT internal_id, ?, 'assignee_changed', to_jsonb(CAST(? AS text)), NULL, clock_timestamp()
			   FROM unassigned`,
			userID, projectID, actorID, userID.String(),
		).Error
	})
}
