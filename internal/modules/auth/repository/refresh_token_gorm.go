package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"truss-project/internal/modules/auth/domain"
)

// refreshTokenRecord: model persistence tabel refresh_tokens. Datar (tanpa struct embedded
// tak-terekspor, yang diam-diam diabaikan GORM).
type refreshTokenRecord struct {
	ID        uuid.UUID  `gorm:"column:internal_id;type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    uuid.UUID  `gorm:"column:user_id;type:uuid;not null"`
	FamilyID  uuid.UUID  `gorm:"column:family_id;type:uuid;not null"`
	TokenHash string     `gorm:"column:token_hash;type:char(64);not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (refreshTokenRecord) TableName() string { return "refresh_tokens" }

func recordFromDomain(t *domain.RefreshToken) refreshTokenRecord {
	return refreshTokenRecord{
		ID: t.ID, UserID: t.UserID, FamilyID: t.FamilyID,
		TokenHash: t.TokenHash, ExpiresAt: t.ExpiresAt,
	}
}

type RefreshTokenGormRepository struct {
	db *gorm.DB
}

func NewRefreshTokenGormRepository(db *gorm.DB) *RefreshTokenGormRepository {
	return &RefreshTokenGormRepository{db: db}
}

// Create menyimpan token dan sekalian membuang token user ini yang sudah kedaluwarsa, agar
// tabel tidak tumbuh tanpa batas (token yang dicabut tetapi belum kedaluwarsa dipertahankan
// untuk mendeteksi pemakaian ulang).
func (r *RefreshTokenGormRepository) Create(ctx context.Context, t *domain.RefreshToken) error {
	rec := recordFromDomain(t)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"DELETE FROM refresh_tokens WHERE user_id = ? AND expires_at < NOW()", t.UserID,
		).Error; err != nil {
			return err
		}
		return tx.Create(&rec).Error
	})
	if err != nil {
		return err
	}
	t.ID, t.CreatedAt = rec.ID, rec.CreatedAt
	return nil
}

func (r *RefreshTokenGormRepository) Rotate(
	ctx context.Context,
	oldHash string,
	now time.Time,
	next *domain.RefreshToken,
) (uuid.UUID, error) {
	var (
		userID uuid.UUID
		reused bool
	)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []refreshTokenRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", oldHash).Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return domain.ErrInvalidRefreshToken
		}
		old := rows[0]

		if old.RevokedAt != nil {
			// Pemakaian ulang token yang sudah dirotasi/dicabut: cabut seluruh rantai. Transaksi
			// sengaja DI-COMMIT (return nil) agar pencabutan ini tidak ikut dibatalkan.
			reused = true
			return tx.Exec(
				"UPDATE refresh_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL",
				now, old.FamilyID,
			).Error
		}
		if !old.ExpiresAt.After(now) {
			return domain.ErrInvalidRefreshToken
		}

		if err := tx.Exec(
			"UPDATE refresh_tokens SET revoked_at = ? WHERE internal_id = ?", now, old.ID,
		).Error; err != nil {
			return err
		}
		next.UserID, next.FamilyID = old.UserID, old.FamilyID
		rec := recordFromDomain(next)
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		next.ID, next.CreatedAt = rec.ID, rec.CreatedAt
		userID = old.UserID
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	if reused {
		return uuid.Nil, domain.ErrInvalidRefreshToken
	}
	return userID, nil
}

func (r *RefreshTokenGormRepository) RevokeFamily(ctx context.Context, hash string, now time.Time) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE refresh_tokens SET revoked_at = ?
		  WHERE revoked_at IS NULL
		    AND family_id IN (SELECT family_id FROM refresh_tokens WHERE token_hash = ?)`,
		now, hash,
	).Error
}

func (r *RefreshTokenGormRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, now time.Time) error {
	return r.db.WithContext(ctx).Exec(
		"UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL",
		now, userID,
	).Error
}
