package repository

import (
	"context"
	"errors"
	"fmt"

	"gochat/internal/model"

	"gorm.io/gorm"
)

// UserRepository 负责用户数据的持久化操作。
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户数据访问对象。
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

// Create 创建用户记录。
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("username already exists: %w", err)
		}

		return fmt.Errorf("create user: %w", err)
	}

	return nil
}

// GetByUsername 根据用户名查询用户。
func (r *UserRepository) GetByUsername(
	ctx context.Context,
	username string,
) (*model.User, error) {
	var user model.User

	err := r.db.WithContext(ctx).
		Where("username = ?", username).
		First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}

		return nil, fmt.Errorf("get user by username: %w", err)
	}

	return &user, nil
}

// GetByID 根据用户 ID 查询用户。
func (r *UserRepository) GetByID(
	ctx context.Context,
	id uint64,
) (*model.User, error) {
	var user model.User

	err := r.db.WithContext(ctx).First(&user, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}

		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return &user, nil
}
