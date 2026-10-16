package handler

import (
	"errors"
	"net/http"

	"gochat/internal/service"
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthHandler 处理用户注册、登录等身份认证请求。
type AuthHandler struct {
	userService *service.UserService
}

// NewAuthHandler 创建身份认证处理器。
func NewAuthHandler(userService *service.UserService) *AuthHandler {
	return &AuthHandler{
		userService: userService,
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
		// 根据业务错误返回相应的 HTTP 状态码。
		switch {
		case errors.Is(err, service.ErrInvalidUsername):
			response.Error(c, http.StatusBadRequest, 4002, "invalid username")

		case errors.Is(err, service.ErrInvalidPassword):
			response.Error(c, http.StatusBadRequest, 4003, "invalid password")

		case errors.Is(err, service.ErrInvalidNickname):
			response.Error(c, http.StatusBadRequest, 4004, "invalid nickname")

		case errors.Is(err, service.ErrUsernameTaken):
			response.Error(c, http.StatusConflict, 4005, "username already exists")

		case errors.Is(err, gorm.ErrRecordNotFound):
			// 注册流程通常不会触发该错误，保留此分支没有必要。
			// 实际上，这里可以直接删除。
			response.Error(c, http.StatusInternalServerError, 5000, "internal server error")

		default:
			// 不向客户端暴露数据库错误等内部实现细节。
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
