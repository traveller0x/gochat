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
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidPassword = errors.New("invalid password")
	ErrInvalidNickname = errors.New("invalid nickname")
	ErrUsernameTaken   = errors.New("username already exists")
)

// // UserService 负责用户相关的业务逻辑。
// type UserService struct {
// 	userRepo *repository.UserRepository
// }

// // NewUserService 创建用户业务服务。
// func NewUserService(userRepo *repository.UserRepository) *UserService {
// 	return &UserService{
// 		userRepo: userRepo,
// 	}
// }

// userRepository 定义用户注册业务需要的数据库操作。
type userRepository interface {
	Create(ctx context.Context, user *model.User) error
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
