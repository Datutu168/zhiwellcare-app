package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/store"
)

// Envelope 是统一返回结构：code=0 成功，非 0 为业务错误码（与 HTTP 状态一致）。
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Code: 0, Message: "ok", Data: data})
}

// created 返回 201（新建资源，如资产登记）。
func created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{Code: 0, Message: "ok", Data: data})
}

func fail(c *gin.Context, status int, message string) {
	c.JSON(status, Envelope{Code: status, Message: message, Data: nil})
}

func failBadRequest(c *gin.Context, message string) { fail(c, http.StatusBadRequest, message) }
func failUnauthorized(c *gin.Context, message string) {
	fail(c, http.StatusUnauthorized, message)
}

// mapStoreError 把存储层/业务错误映射为统一 HTTP 响应。
func mapStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrPhoneExists):
		fail(c, http.StatusConflict, "该手机号已注册，请直接登录")
	case errors.Is(err, store.ErrUserNotFound):
		fail(c, http.StatusUnauthorized, "手机号或密码不正确")
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "资源不存在")
	case errors.Is(err, store.ErrTokenNotFound):
		fail(c, http.StatusUnauthorized, "登录状态已失效，请重新登录")
	case errors.Is(err, store.ErrTokenRevoked):
		fail(c, http.StatusUnauthorized, "登录状态已失效，请重新登录")
	case errors.Is(err, store.ErrTokenExpired):
		fail(c, http.StatusUnauthorized, "登录状态已过期，请重新登录")
	case errors.Is(err, auth.ErrInvalidToken):
		fail(c, http.StatusUnauthorized, "登录状态已失效，请重新登录")
	default:
		fail(c, http.StatusInternalServerError, "服务开小差了，请稍后重试")
	}
}
