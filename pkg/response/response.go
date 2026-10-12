package response

import "github.com/gin-gonic/gin"

// Response 定义统一的 HTTP 响应结构。
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// Success 返回成功响应。
func Success(c *gin.Context, data interface{}) {
	c.JSON(200, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// Error 返回业务错误响应。
func Error(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}
