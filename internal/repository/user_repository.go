package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// Search 按用户名或昵称模糊搜索用户，排除指定用户。
//
// 注意：LIKE '%keyword%' 无法使用索引，会全表扫描。
// 当前用户量小可以接受；数据量大后应改为前缀匹配 + 索引，或引入搜索引擎。
func (r *UserRepository) Search(
	ctx context.Context,
	keyword string,
	excludeUserID uint64,
	limit int,
) ([]model.User, error) {
	pattern := "%" + escapeLike(keyword) + "%"

	query := r.db.WithContext(ctx).
		Select("id, username, nickname, avatar").
		Where("username LIKE ? OR nickname LIKE ?", pattern, pattern)

	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}

	var users []model.User

	err := query.Order("username").Limit(limit).Find(&users).Error
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}

	return users, nil
}

// escapeLike 转义 LIKE 通配符。
//
// 用户输入 "%" 或 "_" 时如果不转义，会变成"匹配任意内容"，
// 相当于把全表数据返回给客户端。
func escapeLike(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)

	return replacer.Replace(value)
}
