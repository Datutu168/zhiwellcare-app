package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/rbac"
	"zhiwellcare/backend/internal/store"
)

// roleCodePattern 角色编码：小写字母开头，允许小写字母/数字/下划线/中划线。
var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

// rbacHandler 承载后台角色权限与配置中心接口。
type rbacHandler struct {
	store store.Combined
	perms *rbac.Resolver
}

// mapRBACError 把 RBAC/配置相关错误映射为统一响应。
func mapRBACError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrRoleExists):
		fail(c, http.StatusConflict, "角色编码已存在")
	case errors.Is(err, store.ErrRoleBuiltin):
		fail(c, http.StatusConflict, "内置角色不可删除")
	case errors.Is(err, store.ErrRoleInUse):
		fail(c, http.StatusConflict, "该角色下仍有用户，请先解除分配")
	case errors.Is(err, store.ErrPermissionUnknown):
		fail(c, http.StatusBadRequest, "存在无效的权限点编码")
	case errors.Is(err, store.ErrAssetExists):
		fail(c, http.StatusConflict, "同类型/版本/文件名的资产已存在")
	case errors.Is(err, store.ErrUserNotFound):
		fail(c, http.StatusNotFound, "用户不存在")
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "资源不存在")
	default:
		mapStoreError(c, err)
	}
}

// ---------- 权限点 / 角色 ----------

// listPermissions GET /api/v1/admin/permissions
func (h *rbacHandler) listPermissions(c *gin.Context) {
	items, err := h.store.ListPermissions(c)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"items": items})
}

// listRoles GET /api/v1/admin/roles
func (h *rbacHandler) listRoles(c *gin.Context) {
	items, err := h.store.ListRoles(c)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"items": items})
}

type roleRequest struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// createRole POST /api/v1/admin/roles
func (h *rbacHandler) createRole(c *gin.Context) {
	var req roleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if !roleCodePattern.MatchString(req.Code) {
		failBadRequest(c, "角色编码需为 2-32 位小写字母开头（可含数字/下划线/中划线）")
		return
	}
	role, err := h.store.CreateRole(c, model.Role{
		Code:        req.Code,
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		Permissions: req.Permissions,
	})
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"role": role})
}

// updateRole PUT /api/v1/admin/roles/:code
func (h *rbacHandler) updateRole(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	var req roleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	existing, err := h.store.GetRole(c, code)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		// 未传名称时保留原名，避免前端只改权限时被清空。
		name = existing.Name
	}
	role, err := h.store.UpdateRole(c, code, name, req.Description, req.Permissions)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	// 角色权限变更 → 失效该角色下所有用户的权限缓存
	if h.perms != nil {
		_ = h.perms.ForgetRole(c.Request.Context(), code)
	}
	ok(c, gin.H{"role": role})
}

// deleteRole DELETE /api/v1/admin/roles/:code
func (h *rbacHandler) deleteRole(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	userIDs, err := h.store.ListRoleUserIDs(c, code)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	if err := h.store.DeleteRole(c, code); err != nil {
		mapRBACError(c, err)
		return
	}
	if h.perms != nil {
		_ = h.perms.Forget(c.Request.Context(), userIDs...)
	}
	ok(c, gin.H{"deleted": true})
}

// ---------- 用户角色 ----------

// getUserRoles GET /api/v1/admin/users/:id/roles
func (h *rbacHandler) getUserRoles(c *gin.Context) {
	userID := c.Param("id")
	if _, err := h.store.GetUserByID(c, userID); err != nil {
		mapRBACError(c, err)
		return
	}
	roles, err := h.store.ListUserRoles(c, userID)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	permissions, err := h.store.ListUserPermissions(c, userID)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"roles": roles, "permissions": permissions})
}

type userRolesRequest struct {
	Roles []string `json:"roles"`
}

// setUserRoles PUT /api/v1/admin/users/:id/roles
func (h *rbacHandler) setUserRoles(c *gin.Context) {
	userID := c.Param("id")
	var req userRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	operator, _ := c.Get(ctxUserID)
	operatorID, _ := operator.(string)
	roles, err := h.store.SetUserRoles(c, userID, req.Roles, operatorID)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	// 用户角色变更 → 立即失效其权限缓存（不必等 TTL 或重新登录）
	if h.perms != nil {
		_ = h.perms.Forget(c.Request.Context(), userID)
	}
	ok(c, gin.H{"roles": roles})
}

// myPermissions GET /api/v1/me/permissions（后台前端控制菜单用）
func (h *rbacHandler) myPermissions(c *gin.Context) {
	userID, _ := c.Get(ctxUserID)
	current, _ := userID.(string)
	set, err := resolvePermissions(c, h.perms, current)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"roles": set.Roles, "permissions": set.Permissions})
}

// ---------- 配置中心 ----------

// listConfig GET /api/v1/admin/config
func (h *rbacHandler) listConfig(c *gin.Context) {
	items, err := h.store.ListAppConfig(c)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"items": items})
}

// getConfig GET /api/v1/admin/config/:key
func (h *rbacHandler) getConfig(c *gin.Context) {
	item, err := h.store.GetAppConfig(c, c.Param("key"))
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, item)
}

type configRequest struct {
	Value       json.RawMessage `json:"value"`
	Description *string         `json:"description"`
}

// setConfig PUT /api/v1/admin/config/:key（value 为任意 JSON）
func (h *rbacHandler) setConfig(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		failBadRequest(c, "配置键不能为空")
		return
	}
	var req configRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	userID, _ := c.Get(ctxUserID)
	operator, _ := userID.(string)
	item, err := h.store.SetAppConfig(c, key, req.Value, req.Description, operator)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, item)
}
