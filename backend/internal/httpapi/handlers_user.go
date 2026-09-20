package httpapi

import (
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/store"
)

type userHandler struct {
	store store.Combined
}

type updateMeRequest struct {
	Nickname string `json:"nickname"`
}

type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// me 返回当前登录用户资料（GET /api/v1/me）。
func (h *userHandler) me(c *gin.Context) {
	userID, _ := c.Get(ctxUserID)
	user, err := h.store.GetUserByID(c, userID.(string))
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, user.ToSafe())
}

// updateMe 更新昵称（PATCH /api/v1/me）。
func (h *userHandler) updateMe(c *gin.Context) {
	var req updateMeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.Nickname = trimSpace(req.Nickname)
	if utf8.RuneCountInString(req.Nickname) == 0 {
		failBadRequest(c, "昵称不能为空")
		return
	}
	if utf8.RuneCountInString(req.Nickname) > 20 {
		failBadRequest(c, "昵称最长 20 个字符")
		return
	}
	userID, _ := c.Get(ctxUserID)
	if err := h.store.UpdateNickname(c, userID.(string), req.Nickname); err != nil {
		mapStoreError(c, err)
		return
	}
	user, err := h.store.GetUserByID(c, userID.(string))
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, user.ToSafe())
}

// changePassword 修改本人密码（PUT /api/v1/me/password）。
//
// 任何登录用户都能改自己的密码（不需要额外权限点）——这正是运维不再需要
// 把 admin-initial-password.txt 明文长期留在服务器上的前提。
// 全程只比较/落库哈希，两个密码字段都不会写进日志、也不回显到响应里。
//
// 已知限制（暂不实现）：访问令牌是无状态 JWT，claims 里没有「密码版本」，
// 因此改密成功后，旧令牌在其自身 TTL 到期前依然有效（不会被立刻踢下线）。
// 要真正做到「改密即失效」，需要在 claims 里加密码版本并在鉴权中间件里比对。
func (h *userHandler) changePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	// 复用注册时的密码规则（6–64 位），保证两处口径一致。
	if message := validatePassword(req.NewPassword); message != "" {
		failBadRequest(c, message)
		return
	}
	if req.NewPassword == req.OldPassword {
		failBadRequest(c, "新密码不能与原密码相同")
		return
	}
	userID, _ := c.Get(ctxUserID)
	user, err := h.store.GetUserByID(c, userID.(string))
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.OldPassword) {
		// 刻意返回 400 而不是 401：这里登录态是有效的，失效的只是请求里填错的 oldPassword。
		// 而客户端（后台 admin/src/api/http.ts、App src/api/http.ts）都把 401 当「登录态失效」
		// 处理并清掉本地令牌——用 401 会让用户输错一次原密码就被静默踢回登录页。
		failBadRequest(c, "原密码不正确")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if err := h.store.UpdateUserPassword(c, user.ID, hash); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"updated": true})
}
