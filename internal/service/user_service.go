package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gochat/internal/model"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// 定义注册过程中可能出现的业务错误。
var (
	ErrInvalidUsername    = errors.New("invalid username")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrInvalidNickname    = errors.New("invalid nickname")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid username or password")
)

type userRepository interface {
	// Create 创建用户记录。
	Create(ctx context.Context, user *model.User) error

	// GetByUsername 根据用户名查询用户。
	GetByUsername(ctx context.Context, username string) (*model.User, error)

	// GetByID 根据用户 ID 查询用户。

	GetByID(ctx context.Context, id uint64) (*model.User, error)
}

// UserService 负责用户相关的业务逻辑。
type UserService struct {
	userRepo userRepository
}

// NewUserService 创建用户业务服务。
func NewUserService(userRepo userRepository) *UserService {
	return &UserService{
		userRepo: userRepo,
	}
}

// usernamePattern 允许用户名由英文字母、数字和下划线组成。
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Register 注册新用户。
func (s *UserService) Register(
	ctx context.Context,
	username string,
	password string,
	nickname string,
) (*model.User, error) {
	// 1. 去除用户名两端的空白。
	username = strings.TrimSpace(username)

	// 2. 校验用户名。
	if len(username) < 3 ||
		len(username) > 32 ||
		!usernamePattern.MatchString(username) {
		return nil, ErrInvalidUsername
	}

	// 3. 校验密码。
	// bcrypt 最多处理 72 字节的密码。
	if len(password) < 8 || len(password) > 72 {
		return nil, ErrInvalidPassword
	}

	// 4. 校验昵称。
	nickname = strings.TrimSpace(nickname)
	if utf8.RuneCountInString(nickname) > 64 {
		return nil, ErrInvalidNickname
	}

	// 5. 对密码进行哈希处理。
	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// 6. 构造用户模型。
	user := &model.User{
		Username:     username,
		PasswordHash: string(passwordHash),
		Nickname:     nickname,
	}

	// 7. 保存用户。
	if err := s.userRepo.Create(ctx, user); err != nil {
		// 将数据库唯一索引冲突转换为业务错误。
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrUsernameTaken
		}

		return nil, fmt.Errorf("register user: %w", err)
	}

	return user, nil
}

// Login 验证用户身份。
// 用户名不存在和密码错误时，统一返回 ErrInvalidCredentials。
func (s *UserService) Login(
	ctx context.Context,
	username string,
	password string,
) (*model.User, error) {
	username = strings.TrimSpace(username)

	// 基本参数校验。
	if username == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	// 根据用户名查询用户。
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		// 用户不存在和密码错误使用相同的错误。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}

		// 数据库故障等其他错误应该保留，方便排查。
		return nil, fmt.Errorf("login: %w", err)
	}

	// 验证密码与数据库中的 bcrypt 哈希是否匹配。
	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(password),
	); err != nil {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// GetByID 根据用户 ID 获取用户信息。
func (s *UserService) GetByID(
	ctx context.Context,
	id uint64,
) (*model.User, error) {
	// 用户 ID 必须大于 0。
	if id == 0 {
		return nil, errors.New("invalid user id")
	}

	// 调用 Repository 查询用户。
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		// 用户不存在时，保留原始错误，交由上层处理。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}

		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return user, nil
}
