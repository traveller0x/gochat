package handler

import (
	"errors"
	"net/http"
	"time"

	"gochat/internal/auth"
	"gochat/internal/service"
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
)

// AuthHandler 处理用户注册、登录等身份认证请求。
type AuthHandler struct {
	userService    *service.UserService
	jwtSecret      string
	jwtExpireHours int
}

// NewAuthHandler 创建身份认证处理器。
func NewAuthHandler(
	userService *service.UserService,
	jwtSecret string,
	jwtExpireHours int,
) *AuthHandler {
	return &AuthHandler{
		userService:    userService,
		jwtSecret:      jwtSecret,
		jwtExpireHours: jwtExpireHours,
	}
}

// RegisterRequest 定义用户注册请求参数。
type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Nickname string `json:"nickname"`
}

// RegisterResponse 定义注册成功后返回的数据。
type RegisterResponse struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// LoginRequest 定义用户登录请求参数。
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 定义登录成功后返回的数据。
type LoginResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int64     `json:"expires_in"`
	User        LoginUser `json:"user"`
}

// LoginUser 定义登录响应中允许返回的用户信息。
type LoginUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// Register 处理用户注册请求。
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest

	// 1. 解析 JSON 请求体。
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(
			c,
			http.StatusBadRequest,
			4001,
			"invalid request body",
		)
		return
	}

	// 2. 调用 Service 执行注册业务。
	user, err := h.userService.Register(
		c.Request.Context(),
		req.Username,
		req.Password,
		req.Nickname,
	)
	if err != nil {
		// 根据业务错误返回对应的 HTTP 状态码。
		switch {
		case errors.Is(err, service.ErrInvalidUsername):
			response.Error(c, http.StatusBadRequest, 4002, "invalid username")

		case errors.Is(err, service.ErrInvalidPassword):
			response.Error(c, http.StatusBadRequest, 4003, "invalid password")

		case errors.Is(err, service.ErrInvalidNickname):
			response.Error(c, http.StatusBadRequest, 4004, "invalid nickname")

		case errors.Is(err, service.ErrUsernameTaken):
			response.Error(c, http.StatusConflict, 4005, "username already exists")

		default:
			response.Error(c, http.StatusInternalServerError, 5000, "internal server error")
		}

		return
	}

	// 3. 只返回允许客户端看到的用户信息。
	data := RegisterResponse{
		ID:       uint64(user.ID),
		Username: user.Username,
		Nickname: user.Nickname,
	}

	// 4. 注册成功，返回 HTTP 201。
	c.JSON(http.StatusCreated, response.Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// Login 处理用户登录请求。
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest

	// 1. 解析 JSON 请求体。
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(
			c,
			http.StatusBadRequest,
			4001,
			"invalid request body",
		)
		return
	}

	// 2. 验证用户名和密码。
	user, err := h.userService.Login(
		c.Request.Context(),
		req.Username,
		req.Password,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			// 不区分用户不存在和密码错误，避免泄露账号信息。
			response.Error(
				c,
				http.StatusUnauthorized,
				4010,
				"invalid username or password",
			)

		default:
			response.Error(
				c,
				http.StatusInternalServerError,
				5000,
				"internal server error",
			)
		}

		return
	}

	// 3. 生成 JWT。
	ttl := time.Duration(h.jwtExpireHours) * time.Hour

	token, err := auth.GenerateToken(
		uint64(user.ID),
		user.Username,
		h.jwtSecret,
		ttl,
	)
	if err != nil {
		response.Error(
			c,
			http.StatusInternalServerError,
			5000,
			"internal server error",
		)
		return
	}

	// 4. 返回 Token 和用户基本信息。
	data := LoginResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(ttl.Seconds()),
		User: LoginUser{
			ID:       uint64(user.ID),
			Username: user.Username,
			Nickname: user.Nickname,
		},
	}

	c.JSON(http.StatusOK, response.Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}
