package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/workspaces/domain"
)

// userLookupRecord: pandangan baca-saja atas tabel users, hanya kolom yang dibutuhkan.
// DeletedAt membuat GORM otomatis mengabaikan user yang di-soft-delete.
type userLookupRecord struct {
	ID        uuid.UUID `gorm:"column:internal_id;primaryKey"`
	Name      string
	Email     string
	DeletedAt gorm.DeletedAt
}

func (userLookupRecord) TableName() string { return "users" }

// UserDirectoryGorm mengimplementasikan usecase.UserDirectory.
type UserDirectoryGorm struct {
	db *gorm.DB
}

func NewUserDirectoryGorm(db *gorm.DB) *UserDirectoryGorm {
	return &UserDirectoryGorm{db: db}
}

func (d *UserDirectoryGorm) FindByEmail(ctx context.Context, email string) (*domain.UserRef, error) {
	var rec userLookupRecord
	err := d.db.WithContext(ctx).Where("email = ?", email).First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return &domain.UserRef{ID: rec.ID, Name: rec.Name, Email: rec.Email}, nil
}
