package handler

import "github.com/gin-gonic/gin"

// currentUserID 从 JWT 中间件写入的上下文中取出当前用户 ID。
func currentUserID(c *gin.Context) (uint64, bool) {
	value, exists := c.Get("userID")
	if !exists {
		return 0, false
	}

	userID, ok := value.(uint64)
	if !ok || userID == 0 {
		return 0, false
	}

	return userID, true
}
