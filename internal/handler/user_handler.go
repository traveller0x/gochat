package handler

import (
	"errors"
	"net/http"

	"gochat/internal/service"
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UserHandler 负责处理用户资料相关的 HTTP 请求。
type UserHandler struct {
	userService *service.UserService
}

// NewUserHandler 创建用户资料处理器。
func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// userProfile 是对外返回的用户资料结构。
type userProfile struct {
	ID        uint64 `json:"id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	Avatar    string `json:"avatar"`
	CreatedAt any    `json:"created_at"`
}

// Me 获取当前登录用户的信息。
// 用户 ID 来自 JWT 中间件验证后的上下文。
func (h *UserHandler) Me(c *gin.Context) {
	// 获取 JWT 中间件写入的用户 ID。
	value, exists := c.Get("userID")
	if !exists {
		response.Error(
			c,
			http.StatusUnauthorized,
			4011,
			"unauthorized",
		)
		return
	}

	userID, ok := value.(uint64)
	if !ok || userID == 0 {
		response.Error(
			c,
			http.StatusUnauthorized,
			4011,
			"unauthorized",
		)
		return
	}

	// 查询当前用户。
	user, err := h.userService.GetByID(
		c.Request.Context(),
		userID,
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(
				c,
				http.StatusNotFound,
				4040,
				"user not found",
			)
			return
		}

		// 不向客户端暴露数据库等内部错误详情。
		response.Error(
			c,
			http.StatusInternalServerError,
			5000,
			"internal server error",
		)
		return
	}

	// 明确控制 API 返回字段，不直接返回数据库模型。
	response.Success(c, gin.H{
		"user": userProfile{
			ID:        user.ID,
			Username:  user.Username,
			Nickname:  user.Nickname,
			Avatar:    user.Avatar,
			CreatedAt: user.CreatedAt,
		},
	})
}
