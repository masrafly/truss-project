// Package repository adalah adapter: mengimplementasikan usecase.UserRepository
// memakai GORM/PostgreSQL. Semua detail GORM berhenti di sini.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/auth/domain"
)

// userRecord adalah model persistence (bentuk tabel), terpisah dari entity domain.
type userRecord struct {
	ID           uuid.UUID `gorm:"column:internal_id;type:uuid;primaryKey;default:gen_random_uuid()"`
	Name         string    `gorm:"type:varchar(100);not null"`
	Email        string    `gorm:"type:varchar(255);unique;not null"`
	PasswordHash string    `gorm:"type:varchar(255);not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (userRecord) TableName() string { return "users" }

func (r userRecord) toDomain() *domain.User {
	return &domain.User{
		ID:           r.ID,
		Name:         r.Name,
		Email:        r.Email,
		PasswordHash: r.PasswordHash,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

type UserGormRepository struct {
	db *gorm.DB
}

func NewUserGormRepository(db *gorm.DB) *UserGormRepository {
	return &UserGormRepository{db: db}
}

func (r *UserGormRepository) Create(ctx context.Context, user *domain.User) error {
	rec := userRecord{
		ID:           user.ID,
		Name:         user.Name,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
	}

	if err := r.db.WithContext(ctx).Create(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return domain.ErrEmailAlreadyUsed
		}
		return err
	}

	// Salin nilai yang dibuat database kembali ke entity.
	user.ID = rec.ID
	user.CreatedAt = rec.CreatedAt
	user.UpdatedAt = rec.UpdatedAt
	return nil
}

func (r *UserGormRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var rec userRecord
	err := r.db.WithContext(ctx).First(&rec, "internal_id = ?", id).Error
	if err != nil {
		return nil, translate(err)
	}
	return rec.toDomain(), nil
}

func (r *UserGormRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var rec userRecord
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&rec).Error
	if err != nil {
		return nil, translate(err)
	}
	return rec.toDomain(), nil
}

// translate menerjemahkan error GORM ke error domain.
func translate(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrUserNotFound
	}
	return err
}
