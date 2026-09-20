package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/config"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/objectstore"
	"zhiwellcare/backend/internal/store"
)

// int64String 资产 ID 拼路径用。
func int64String(value int64) string { return strconv.FormatInt(value, 10) }

// rbacTestEnv 装配带缓存与对象存储的引擎（内存存储，可操作角色）。
type rbacTestEnv struct {
	engine  *gin.Engine
	data    *store.Memory
	cache   cache.Store
	devices *cache.Devices
	assets  objectstore.Store
}

func newRBACTestEnv(t *testing.T, cdnBase string) *rbacTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cacheStore := cache.NewMemory()
	data := store.NewMemory()
	assets, err := objectstore.NewLocal(objectstore.Options{
		LocalDir:     t.TempDir(),
		LocalBaseURL: "http://127.0.0.1:8080",
		PublicBase:   cdnBase,
		SignSecret:   "upload-secret",
	})
	if err != nil {
		t.Fatalf("初始化对象存储失败: %v", err)
	}
	cfg := &config.Config{
		Addr:             ":0",
		JWTSecret:        "test-secret",
		AccessTokenTTL:   2 * time.Hour,
		RefreshTokenTTL:  30 * 24 * time.Hour,
		CORSOrigins:      []string{"http://localhost:1420"},
		DeviceCacheTTL:   time.Minute,
		UploadURLTTL:     15 * time.Minute,
		UploadSignSecret: "upload-secret",
		PublicBaseURL:    "http://127.0.0.1:8080",
		CDNBase:          cdnBase,
	}
	manager := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL)
	devices := cache.NewDevices(cacheStore, time.Minute)
	return &rbacTestEnv{
		engine: NewEngine(Deps{
			Config:    cfg,
			Store:     data,
			Manager:   manager,
			Cache:     cacheStore,
			Sessions:  cache.NewSessions(cacheStore),
			Devices:   devices,
			Assets:    assets,
			StartedAt: time.Now(),
		}),
		data:    data,
		cache:   cacheStore,
		devices: devices,
		assets:  assets,
	}
}

// grantRoles 直接给用户分配角色（等价于后台 PUT /admin/users/:id/roles）。
func (e *rbacTestEnv) grantRoles(t *testing.T, phone string, roles ...string) string {
	t.Helper()
	user, err := e.data.GetUserByPhone(context.Background(), phone)
	if err != nil {
		t.Fatalf("查询用户 %s 失败: %v", phone, err)
	}
	if _, err := e.data.SetUserRoles(context.Background(), user.ID, roles, ""); err != nil {
		t.Fatalf("分配角色失败: %v", err)
	}
	return user.ID
}

// newUserWithRoles 注册用户并分配角色，返回访问令牌与用户 ID。
func (e *rbacTestEnv) newUserWithRoles(t *testing.T, phone string, roles ...string) (string, string) {
	t.Helper()
	token := registerAndLogin(t, e.engine, phone, "pass123")
	userID := e.grantRoles(t, phone, roles...)
	return token, userID
}

func TestRBACPermissionEnforcement(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800009001", model.RoleAdmin)
	operatorToken, _ := env.newUserWithRoles(t, "13800009002", model.RoleOperator)
	viewerToken, _ := env.newUserWithRoles(t, "13800009003", model.RoleViewer)
	plainToken := registerAndLogin(t, env.engine, "13800009004", "pass123")

	// 未登录 → 401
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/devices", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("未登录访问后台应 401，实际 %d", code)
	}

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   any
		want   int
	}{
		{"admin 看设备", http.MethodGet, "/api/v1/admin/devices", adminToken, nil, http.StatusOK},
		{"admin 建角色", http.MethodGet, "/api/v1/admin/roles", adminToken, nil, http.StatusOK},
		{"operator 看设备", http.MethodGet, "/api/v1/admin/devices", operatorToken, nil, http.StatusOK},
		{"operator 建设备", http.MethodPost, "/api/v1/admin/devices", operatorToken,
			map[string]any{"modelId": "op-band", "name": "运营型号", "status": "on"}, http.StatusOK},
		{"operator 看训练记录", http.MethodGet, "/api/v1/admin/training-records", operatorToken, nil, http.StatusOK},
		{"operator 无角色查看权限", http.MethodGet, "/api/v1/admin/roles", operatorToken, nil, http.StatusForbidden},
		{"operator 无配置写权限", http.MethodPut, "/api/v1/admin/config/catalog.version", operatorToken,
			map[string]any{"value": "9.9.9"}, http.StatusForbidden},
		{"operator 可读配置", http.MethodGet, "/api/v1/admin/config", operatorToken, nil, http.StatusOK},
		{"viewer 看设备", http.MethodGet, "/api/v1/admin/devices", viewerToken, nil, http.StatusOK},
		{"viewer 看权限点", http.MethodGet, "/api/v1/admin/permissions", viewerToken, nil, http.StatusOK},
		{"viewer 建设备被拒", http.MethodPost, "/api/v1/admin/devices", viewerToken,
			map[string]any{"modelId": "v-band", "name": "只读型号"}, http.StatusForbidden},
		{"viewer 改白名单被拒", http.MethodPost, "/api/v1/admin/device-whitelist", viewerToken,
			map[string]any{"deviceKey": "aa:bb"}, http.StatusForbidden},
		{"普通用户看设备被拒", http.MethodGet, "/api/v1/admin/devices", plainToken, nil, http.StatusForbidden},
		{"普通用户看自己权限", http.MethodGet, "/api/v1/me/permissions", plainToken, nil, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, res := doJSON(t, env.engine, tc.method, tc.path, tc.token, tc.body)
			if code != tc.want {
				t.Fatalf("%s %s 期望 %d 实际 %d（%s）", tc.method, tc.path, tc.want, code, res.Message)
			}
		})
	}

	// /me/permissions 返回空集合（普通用户）
	_, res := doJSON(t, env.engine, http.MethodGet, "/api/v1/me/permissions", plainToken, nil)
	plain := decode[struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}](t, res.Data)
	if len(plain.Roles) != 0 || len(plain.Permissions) != 0 {
		t.Fatalf("普通用户不应有角色/权限: %+v", plain)
	}

	// viewer 的权限集合来自 role_permissions
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/me/permissions", viewerToken, nil)
	viewer := decode[struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}](t, res.Data)
	if len(viewer.Roles) != 1 || viewer.Roles[0] != model.RoleViewer {
		t.Fatalf("viewer 角色异常: %+v", viewer)
	}
	if !contains(viewer.Permissions, model.PermDeviceRead) || contains(viewer.Permissions, model.PermDeviceWrite) {
		t.Fatalf("viewer 权限集合异常: %+v", viewer.Permissions)
	}

	// 角色不存在：给用户分配不存在的角色 → 404（PUT 由 handler 映射）
	plainUser, err := env.data.GetUserByPhone(context.Background(), "13800009004")
	if err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/users/"+plainUser.ID+"/roles", adminToken,
		map[string]any{"roles": []string{"not-exist"}}); code != http.StatusNotFound {
		t.Fatalf("分配不存在的角色应 404，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/users/no-such-user/roles", adminToken,
		map[string]any{"roles": []string{model.RoleViewer}}); code != http.StatusNotFound {
		t.Fatalf("给不存在的用户分配角色应 404，实际 %d", code)
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestRBACRolesCRUDAndBuiltinProtection(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, adminUserID := env.newUserWithRoles(t, "13800009101", model.RoleAdmin)

	// 权限点清单：字段名与契约一致（code/name/group/description）
	// 数量为 17：003 的 15 个 + 004 内容中心的 content:read / content:write
	_, res := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/permissions", adminToken, nil)
	permissions := decode[struct {
		Items []model.Permission `json:"items"`
	}](t, res.Data)
	if len(permissions.Items) != 17 {
		t.Fatalf("权限点应为 17 个，实际 %d", len(permissions.Items))
	}
	// 精确断言：编码集合必须完全等于「003 的 15 个 + 004 的 content:*」，不多不少
	if got, want := permissionCodes(permissions.Items), expectedPermissionCodes(); !sameStringSet(got, want) {
		t.Fatalf("权限点编码集合与预期不一致:\n实际 %v\n预期 %v", got, want)
	}
	first := permissions.Items[0]
	if first.Code == "" || first.Name == "" || first.Group == "" {
		t.Fatalf("权限点字段缺失: %+v", first)
	}

	// 内置角色
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/roles", adminToken, nil)
	roles := decode[struct {
		Items []model.Role `json:"items"`
	}](t, res.Data)
	// admin：精确拥有全部 17 个权限点（与权限点字典逐个对齐，不是「>= 15」这类模糊判断）
	admin := findRole(roles.Items, model.RoleAdmin)
	if admin == nil || !admin.Builtin {
		t.Fatalf("admin 角色异常: %+v", admin)
	}
	if !sameStringSet(admin.Permissions, expectedPermissionCodes()) {
		t.Fatalf("admin 应精确拥有全部 17 个权限点:\n实际 %v", admin.Permissions)
	}
	if admin.UserCount != 1 {
		t.Fatalf("admin 角色用户数应为 1，实际 %d", admin.UserCount)
	}
	// operator：13 项，且必须含 content:read + content:write（004 新增）
	operator := findRole(roles.Items, model.RoleOperator)
	if operator == nil || len(operator.Permissions) != 13 {
		t.Fatalf("operator 角色异常: %+v", operator)
	}
	if !contains(operator.Permissions, model.PermContentRead) || !contains(operator.Permissions, model.PermContentWrite) {
		t.Fatalf("operator 应含 content:read + content:write: %v", operator.Permissions)
	}
	// viewer：9 项，含 content:read 但不含 content:write
	viewer := findRole(roles.Items, model.RoleViewer)
	if viewer == nil || len(viewer.Permissions) != 9 {
		t.Fatalf("viewer 角色异常: %+v", viewer)
	}
	if !contains(viewer.Permissions, model.PermContentRead) || contains(viewer.Permissions, model.PermContentWrite) {
		t.Fatalf("viewer 应只含 content:read: %v", viewer.Permissions)
	}

	// 新建角色
	code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/roles", adminToken, map[string]any{
		"code": "care", "name": "客服", "description": "客服查看用户与记录",
		"permissions": []string{model.PermUserRead, model.PermRecordRead},
	})
	if code != http.StatusOK {
		t.Fatalf("新建角色失败: %d %s", code, res.Message)
	}
	created := decode[struct {
		Role model.Role `json:"role"`
	}](t, res.Data)
	if created.Role.Code != "care" || created.Role.Builtin ||
		len(created.Role.Permissions) != 2 || created.Role.UserCount != 0 {
		t.Fatalf("新建角色返回异常: %+v", created.Role)
	}

	// 重复编码 → 409；非法编码 → 400；未知权限点 → 400
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/roles", adminToken, map[string]any{
		"code": "care", "name": "客服2", "permissions": []string{},
	}); code != http.StatusConflict {
		t.Fatalf("重复角色编码应 409，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/roles", adminToken, map[string]any{
		"code": "Bad Code", "name": "非法",
	}); code != http.StatusBadRequest {
		t.Fatalf("非法角色编码应 400，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/roles", adminToken, map[string]any{
		"code": "ghost", "name": "幽灵", "permissions": []string{"nope:read"},
	}); code != http.StatusBadRequest {
		t.Fatalf("未知权限点应 400，实际 %d", code)
	}

	// 修改角色权限
	code, res = doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/roles/care", adminToken, map[string]any{
		"name": "客服组", "description": "改后的描述", "permissions": []string{model.PermUserRead},
	})
	if code != http.StatusOK {
		t.Fatalf("修改角色失败: %d %s", code, res.Message)
	}
	updated := decode[struct {
		Role model.Role `json:"role"`
	}](t, res.Data)
	if updated.Role.Name != "客服组" || len(updated.Role.Permissions) != 1 || updated.Role.Permissions[0] != model.PermUserRead {
		t.Fatalf("角色修改未生效: %+v", updated.Role)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/roles/missing", adminToken, map[string]any{
		"name": "x", "permissions": []string{},
	}); code != http.StatusNotFound {
		t.Fatalf("修改不存在的角色应 404，实际 %d", code)
	}

	// 删除：内置 → 409；仍有用户 → 409
	if code, _ := doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/roles/admin", adminToken, nil); code != http.StatusConflict {
		t.Fatalf("删除内置角色应 409，实际 %d", code)
	}
	if _, err := env.data.SetUserRoles(context.Background(), adminUserID, []string{model.RoleAdmin, "care"}, ""); err != nil {
		t.Fatalf("分配角色失败: %v", err)
	}
	if code, _ := doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/roles/care", adminToken, nil); code != http.StatusConflict {
		t.Fatalf("删除仍有用户的角色应 409，实际 %d", code)
	}
	if _, err := env.data.SetUserRoles(context.Background(), adminUserID, []string{model.RoleAdmin}, ""); err != nil {
		t.Fatalf("回收角色失败: %v", err)
	}
	code, res = doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/roles/care", adminToken, nil)
	if code != http.StatusOK {
		t.Fatalf("删除角色失败: %d %s", code, res.Message)
	}
	deleted := decode[struct {
		Deleted bool `json:"deleted"`
	}](t, res.Data)
	if !deleted.Deleted {
		t.Fatal("删除角色应返回 deleted:true")
	}
	if code, _ := doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/roles/care", adminToken, nil); code != http.StatusNotFound {
		t.Fatalf("重复删除应 404，实际 %d", code)
	}
}

func permissionCodes(items []model.Permission) []string {
	codes := make([]string, 0, len(items))
	for _, item := range items {
		codes = append(codes, item.Code)
	}
	return codes
}

// expectedPermissionCodes 权限点全集的期望值：设备/游戏/记录/用户/角色/配置/资产/白名单
// 各权限点（003 迁移的 15 个）+ 内容中心 content:read / content:write（004 迁移新增），共 17 个。
// 用于精确断言（集合完全相等），避免「>= 15」这类会漏掉新增权限点的模糊判断。
func expectedPermissionCodes() []string {
	return []string{
		model.PermDeviceRead, model.PermDeviceWrite,
		model.PermGameRead, model.PermGameWrite,
		model.PermRecordRead,
		model.PermUserRead, model.PermUserWrite,
		model.PermRoleRead, model.PermRoleWrite,
		model.PermConfigRead, model.PermConfigWrite,
		model.PermAssetRead, model.PermAssetWrite,
		model.PermWhitelistRead, model.PermWhitelistWrite,
		model.PermContentRead, model.PermContentWrite,
	}
}

// sameStringSet 集合完全相等（忽略顺序，但长度与元素都必须一致）。
func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := make(map[string]struct{}, len(got))
	for _, item := range got {
		set[item] = struct{}{}
	}
	if len(set) != len(want) {
		return false // 有重复项
	}
	for _, item := range want {
		if _, ok := set[item]; !ok {
			return false
		}
	}
	return true
}

func findRole(items []model.Role, code string) *model.Role {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}

func TestUserRoleAssignmentSyncsLegacyRoleAndInvalidatesCache(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800009201", model.RoleAdmin)
	targetToken := registerAndLogin(t, env.engine, "13800009202", "pass123")
	target, err := env.data.GetUserByPhone(context.Background(), "13800009202")
	if err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}

	// 初始无角色：权限缓存被写入「空集合」
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/devices", targetToken, nil); code != http.StatusForbidden {
		t.Fatalf("无角色用户应 403，实际 %d", code)
	}
	_, res := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/users/"+target.ID+"/roles", adminToken, nil)
	initial := decode[struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}](t, res.Data)
	if len(initial.Roles) != 0 || len(initial.Permissions) != 0 {
		t.Fatalf("初始角色/权限应为空: %+v", initial)
	}

	// 分配 operator：users.role 仍为 user（未拥有 admin）
	code, res := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/users/"+target.ID+"/roles", adminToken,
		map[string]any{"roles": []string{model.RoleOperator}})
	if code != http.StatusOK {
		t.Fatalf("分配角色失败: %d %s", code, res.Message)
	}
	assigned := decode[struct {
		Roles []string `json:"roles"`
	}](t, res.Data)
	if len(assigned.Roles) != 1 || assigned.Roles[0] != model.RoleOperator {
		t.Fatalf("分配结果异常: %+v", assigned)
	}
	stored, _ := env.data.GetUserByID(context.Background(), target.ID)
	if stored.Role != "user" {
		t.Fatalf("operator 角色的 users.role 应为 user，实际 %q", stored.Role)
	}
	// 权限缓存立即失效：用同一个令牌即可访问（无需重新登录）
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/devices", targetToken, nil); code != http.StatusOK {
		t.Fatalf("分配角色后应立即有权限，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/roles", targetToken, nil); code != http.StatusForbidden {
		t.Fatalf("operator 不应有 role:read，实际 %d", code)
	}

	// 叠加 admin：users.role 同步为 admin
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/users/"+target.ID+"/roles", adminToken,
		map[string]any{"roles": []string{model.RoleOperator, model.RoleAdmin}}); code != http.StatusOK {
		t.Fatalf("分配 admin 失败: %d", code)
	}
	stored, _ = env.data.GetUserByID(context.Background(), target.ID)
	if stored.Role != model.RoleAdmin {
		t.Fatalf("拥有 admin 角色时 users.role 应为 admin，实际 %q", stored.Role)
	}
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/roles", targetToken, nil); code != http.StatusOK {
		t.Fatalf("admin 角色应立即获得 role:read，实际 %d", code)
	}

	// 清空角色：users.role 回退 user，权限缓存再次失效
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/users/"+target.ID+"/roles", adminToken,
		map[string]any{"roles": []string{}}); code != http.StatusOK {
		t.Fatalf("清空角色失败: %d", code)
	}
	stored, _ = env.data.GetUserByID(context.Background(), target.ID)
	if stored.Role != "user" {
		t.Fatalf("清空角色后 users.role 应为 user，实际 %q", stored.Role)
	}
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/devices", targetToken, nil); code != http.StatusForbidden {
		t.Fatalf("清空角色后应 403，实际 %d", code)
	}
}

func TestRolePermissionChangeInvalidatesUserCache(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800009301", model.RoleAdmin)
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/roles", adminToken, map[string]any{
		"code": "reporter", "name": "报表", "permissions": []string{model.PermRecordRead},
	}); code != http.StatusOK {
		t.Fatal("创建角色失败")
	}
	token, _ := env.newUserWithRoles(t, "13800009302", "reporter")

	// 命中缓存：有 record:read，无 device:read
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/training-records", token, nil); code != http.StatusOK {
		t.Fatal("应可读训练记录")
	}
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/devices", token, nil); code != http.StatusForbidden {
		t.Fatal("不应可读设备")
	}

	// 角色权限变更 → 该角色下用户缓存失效
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/roles/reporter", adminToken, map[string]any{
		"name": "报表", "description": "", "permissions": []string{model.PermDeviceRead},
	}); code != http.StatusOK {
		t.Fatal("修改角色权限失败")
	}
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/devices", token, nil); code != http.StatusOK {
		t.Fatalf("权限变更后应立即生效（device:read），实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/training-records", token, nil); code != http.StatusForbidden {
		t.Fatalf("权限变更后应收敛（record:read 已移除），实际 %d", code)
	}
}

func TestConfigCenterJSONBRoundTrip(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800009401", model.RoleAdmin)

	_, res := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/config", adminToken, nil)
	list := decode[struct {
		Items []model.AppConfigItem `json:"items"`
	}](t, res.Data)
	if len(list.Items) != 4 {
		t.Fatalf("配置项应有 4 个种子，实际 %d", len(list.Items))
	}
	keys := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		keys = append(keys, item.Key)
		if item.Key == "" || item.UpdatedAt.IsZero() {
			t.Fatalf("配置项字段缺失: %+v", item)
		}
	}
	for _, want := range []string{"catalog.version", "catalog.gray.enabled", "feature.firmware_ota", "feature.samples_upload"} {
		if !contains(keys, want) {
			t.Fatalf("缺少配置项 %s: %v", want, keys)
		}
	}

	// 单键读取（字符串 JSON）
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/config/catalog.version", adminToken, nil)
	version := decode[model.AppConfigItem](t, res.Data)
	if string(version.Value) != `"1.0.0"` {
		t.Fatalf("catalog.version 初值异常: %s", version.Value)
	}

	// 写入布尔值
	code, res := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/config/catalog.gray.enabled", adminToken,
		map[string]any{"value": true, "description": "打开灰度"})
	if code != http.StatusOK {
		t.Fatalf("写配置失败: %d %s", code, res.Message)
	}
	written := decode[model.AppConfigItem](t, res.Data)
	if string(written.Value) != "true" || written.Description != "打开灰度" {
		t.Fatalf("写配置返回异常: %+v", written)
	}

	// 写入对象（任意 JSON）
	code, res = doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/config/catalog.thresholds", adminToken,
		map[string]any{"value": map[string]any{"score": 80, "tags": []string{"a", "b"}}})
	if code != http.StatusOK {
		t.Fatalf("写新配置失败: %d %s", code, res.Message)
	}
	createdItem := decode[model.AppConfigItem](t, res.Data)
	var thresholds struct {
		Score int      `json:"score"`
		Tags  []string `json:"tags"`
	}
	if err := json.Unmarshal(createdItem.Value, &thresholds); err != nil {
		t.Fatalf("解析配置值失败: %v", err)
	}
	if thresholds.Score != 80 || len(thresholds.Tags) != 2 {
		t.Fatalf("配置值往返不一致: %+v", thresholds)
	}

	// 回读
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/config/catalog.gray.enabled", adminToken, nil)
	if string(decode[model.AppConfigItem](t, res.Data).Value) != "true" {
		t.Fatal("配置未持久化")
	}
	// 不存在的键 → 404
	if code, _ := doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/config/not.exists", adminToken, nil); code != http.StatusNotFound {
		t.Fatalf("不存在的配置键应 404，实际 %d", code)
	}
	// 不传 description 时保留原描述
	if code, res = doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/config/catalog.gray.enabled", adminToken,
		map[string]any{"value": false}); code != http.StatusOK {
		t.Fatalf("写配置失败: %d", code)
	}
	if got := decode[model.AppConfigItem](t, res.Data); got.Description != "打开灰度" {
		t.Fatalf("未传 description 应保留原值，实际 %q", got.Description)
	}
}

func TestDeviceWhitelistAndMappingCacheInvalidation(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800009501", model.RoleAdmin)
	ctx := context.Background()

	// 预热带计数回源的白名单缓存
	loads := 0
	load := func(context.Context) ([]string, error) {
		loads++
		return env.data.ListWhitelistKeys(ctx)
	}
	if _, err := env.devices.Whitelist(ctx, load); err != nil {
		t.Fatalf("预热白名单缓存失败: %v", err)
	}
	if _, err := env.devices.Whitelist(ctx, load); err != nil {
		t.Fatalf("读取白名单失败: %v", err)
	}
	if loads != 1 {
		t.Fatalf("第二次读取应命中缓存，回源次数 %d", loads)
	}

	// 新增白名单 → 缓存必须失效（deviceKey 归一为小写）
	code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/device-whitelist", adminToken, map[string]any{
		"deviceKey": "BT91-ABC", "modelId": "wobble-wrist-band", "note": "首批样机",
	})
	if code != http.StatusOK {
		t.Fatalf("新增白名单失败: %d %s", code, res.Message)
	}
	entry := decode[model.DeviceWhitelistEntry](t, res.Data)
	if entry.DeviceKey != "bt91-abc" || entry.ModelID != "wobble-wrist-band" || !entry.Enabled {
		t.Fatalf("白名单返回异常: %+v", entry)
	}
	set, err := env.devices.Whitelist(ctx, load)
	if err != nil {
		t.Fatalf("读取白名单失败: %v", err)
	}
	if loads != 2 {
		t.Fatalf("新增后应回源一次（缓存已失效），回源次数 %d", loads)
	}
	if _, ok := set["bt91-abc"]; !ok {
		t.Fatalf("白名单缓存未生效: %+v", set)
	}
	// 列表接口与契约字段一致
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/device-whitelist", adminToken, nil)
	whitelist := decode[struct {
		Items []model.DeviceWhitelistEntry `json:"items"`
	}](t, res.Data)
	if len(whitelist.Items) != 1 || whitelist.Items[0].DeviceKey != "bt91-abc" {
		t.Fatalf("白名单列表异常: %+v", whitelist.Items)
	}

	// 缺 deviceKey → 400
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/device-whitelist", adminToken,
		map[string]any{"modelId": "x"}); code != http.StatusBadRequest {
		t.Fatalf("缺 deviceKey 应 400，实际 %d", code)
	}

	// 删除白名单 → 缓存失效
	code, res = doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/device-whitelist/BT91-ABC", adminToken, nil)
	if code != http.StatusOK || !decode[struct {
		Deleted bool `json:"deleted"`
	}](t, res.Data).Deleted {
		t.Fatalf("删除白名单失败: %d", code)
	}
	set, _ = env.devices.Whitelist(ctx, load)
	if loads != 3 || len(set) != 0 {
		t.Fatalf("删除后缓存未失效: loads=%d set=%+v", loads, set)
	}
	if code, _ := doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/device-whitelist/bt91-abc", adminToken, nil); code != http.StatusNotFound {
		t.Fatalf("重复删除白名单应 404，实际 %d", code)
	}

	// 设备映射：先写脏缓存，再通过接口更新，缓存必须失效并回源新值
	if err := env.devices.SaveMapping(ctx, "bt91-abc", "stale-model"); err != nil {
		t.Fatalf("预热映射缓存失败: %v", err)
	}
	code, res = doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/device-mappings", adminToken, map[string]any{
		"deviceKey": "BT91-ABC", "modelId": "wobble-wrist-band", "note": "映射",
	})
	if code != http.StatusOK {
		t.Fatalf("新增映射失败: %d %s", code, res.Message)
	}
	mapped, ok, err := env.devices.Mapping(ctx, "bt91-abc", func(ctx context.Context) (string, error) {
		modelID, err := env.data.GetDeviceMapping(ctx, "bt91-abc")
		if err != nil {
			return "", nil
		}
		return modelID, nil
	})
	if err != nil || !ok || mapped != "wobble-wrist-band" {
		t.Fatalf("映射缓存未失效: mapped=%q ok=%v err=%v", mapped, ok, err)
	}
	// 缺 modelId → 400
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/device-mappings", adminToken,
		map[string]any{"deviceKey": "bt91-abc"}); code != http.StatusBadRequest {
		t.Fatalf("缺 modelId 应 400，实际 %d", code)
	}
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/device-mappings", adminToken, nil)
	mappings := decode[struct {
		Items []model.DeviceMappingEntry `json:"items"`
	}](t, res.Data)
	if len(mappings.Items) != 1 || mappings.Items[0].ModelID != "wobble-wrist-band" {
		t.Fatalf("映射列表异常: %+v", mappings.Items)
	}
	if code, _ := doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/device-mappings/bt91-abc", adminToken, nil); code != http.StatusOK {
		t.Fatalf("删除映射失败: %d", code)
	}
	if _, err := env.data.GetDeviceMapping(ctx, "bt91-abc"); err == nil {
		t.Fatal("映射应已删除")
	}
}

func TestAssetRegistrationAndPublicRelease(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800009601", model.RoleAdmin)

	// 1) 申请直传地址（Web 包）
	code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets/upload-url", adminToken, map[string]any{
		"kind": "web", "refId": "android", "version": "0.2.0", "filename": "app.zip", "contentType": "application/zip",
	})
	if code != http.StatusOK {
		t.Fatalf("申请直传地址失败: %d %s", code, res.Message)
	}
	upload := decode[struct {
		Key              string `json:"key"`
		UploadURL        string `json:"uploadUrl"`
		Method           string `json:"method"`
		ExpiresInSeconds int64  `json:"expiresInSeconds"`
	}](t, res.Data)
	if upload.Key != "web/android/0.2.0/app.zip" || upload.Method != http.MethodPut ||
		upload.UploadURL == "" || upload.ExpiresInSeconds <= 0 {
		t.Fatalf("直传地址返回异常: %+v", upload)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets/upload-url", adminToken, map[string]any{
		"kind": "sample", "refId": "x", "version": "1", "filename": "a.bin",
	}); code != http.StatusBadRequest {
		t.Fatalf("非法 kind 应 400，实际 %d", code)
	}

	// 2) 登记资产（draft）→ 公开接口应报告「无更新」
	code, res = doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets", adminToken, map[string]any{
		"kind": "web", "refId": "android", "version": "0.2.0", "filename": "app.zip",
		"objectKey": upload.Key, "size": 1024, "sha256": "AABBCC", "contentType": "application/zip",
		"notes": "修复闪退\n新增腕带校准",
	})
	if code != http.StatusCreated {
		t.Fatalf("登记资产应 201，实际 %d %s", code, res.Message)
	}
	asset := decode[model.Asset](t, res.Data)
	if asset.ID == 0 || asset.Status != model.AssetStatusDraft || asset.SHA256 != "aabbcc" {
		t.Fatalf("资产登记返回异常: %+v", asset)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets", adminToken, map[string]any{
		"kind": "web", "refId": "android", "version": "0.2.0", "filename": "app.zip", "objectKey": upload.Key,
	}); code != http.StatusConflict {
		t.Fatalf("重复登记应 409，实际 %d", code)
	}
	if code, _ := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets", adminToken, map[string]any{
		"kind": "web", "refId": "android", "version": "0.3.0", "filename": "app.zip", "objectKey": "../etc/passwd",
	}); code != http.StatusBadRequest {
		t.Fatalf("非法 objectKey 应 400，实际 %d", code)
	}

	code, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/app/web-bundle?platform=android&currentVersion=0.1.0", "", nil)
	if code != http.StatusOK {
		t.Fatalf("公开接口应 200，实际 %d", code)
	}
	noUpdate := decode[struct {
		UpToDate bool   `json:"upToDate"`
		Version  string `json:"version"`
	}](t, res.Data)
	if !noUpdate.UpToDate || noUpdate.Version != "" {
		t.Fatalf("draft 资产不应下发: %+v", noUpdate)
	}

	// 3) 发布 → 有更新
	code, res = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/assets/"+int64String(asset.ID)+"/status", adminToken,
		map[string]any{"status": model.AssetStatusPublished})
	if code != http.StatusOK {
		t.Fatalf("发布资产失败: %d %s", code, res.Message)
	}
	published := decode[model.Asset](t, res.Data)
	if published.Status != model.AssetStatusPublished || published.PublishedAt == nil {
		t.Fatalf("发布结果异常: %+v", published)
	}

	code, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/app/web-bundle?platform=android&currentVersion=0.1.0", "", nil)
	if code != http.StatusOK {
		t.Fatalf("公开接口应 200，实际 %d", code)
	}
	update := decode[struct {
		UpToDate    bool       `json:"upToDate"`
		Version     string     `json:"version"`
		URL         string     `json:"url"`
		SHA256      string     `json:"sha256"`
		Size        int64      `json:"size"`
		Notes       []string   `json:"notes"`
		PublishedAt *time.Time `json:"publishedAt"`
	}](t, res.Data)
	if update.UpToDate || update.Version != "0.2.0" {
		t.Fatalf("应报告有更新: %+v", update)
	}
	if !strings.Contains(update.URL, "web/android/0.2.0/app.zip") {
		t.Fatalf("下发地址异常: %s", update.URL)
	}
	if update.SHA256 != "aabbcc" || update.Size != 1024 || len(update.Notes) != 2 || update.PublishedAt == nil {
		t.Fatalf("下发元数据异常: %+v", update)
	}

	// 版本一致 → 无更新
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/app/web-bundle?platform=android&currentVersion=0.2.0", "", nil)
	if same := decode[struct {
		UpToDate bool   `json:"upToDate"`
		Version  string `json:"version"`
	}](t, res.Data); !same.UpToDate || same.Version != "0.2.0" {
		t.Fatalf("版本一致应无更新: %+v", same)
	}

	// 4) 固件：无发布 → 无更新；发布后 → 有更新
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/device/wobble-wrist-band/firmware?currentVersion=1.0.0", "", nil)
	if none := decode[struct {
		UpToDate bool   `json:"upToDate"`
		Version  string `json:"version"`
	}](t, res.Data); !none.UpToDate || none.Version != "" {
		t.Fatalf("无固件资产应无更新: %+v", none)
	}
	code, res = doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets/upload-url", adminToken, map[string]any{
		"kind": "firmware", "refId": "wobble-wrist-band", "version": "1.1.0", "filename": "fw.bin",
	})
	if code != http.StatusOK {
		t.Fatalf("固件直传地址失败: %d", code)
	}
	firmwareKey := decode[struct {
		Key string `json:"key"`
	}](t, res.Data).Key
	if firmwareKey != "firmware/wobble-wrist-band/1.1.0/fw.bin" {
		t.Fatalf("固件对象键异常: %s", firmwareKey)
	}
	code, res = doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets", adminToken, map[string]any{
		"kind": "firmware", "refId": "wobble-wrist-band", "version": "1.1.0", "filename": "fw.bin",
		"objectKey": firmwareKey, "size": 2048, "sha256": "ff00",
	})
	if code != http.StatusCreated {
		t.Fatalf("登记固件失败: %d", code)
	}
	firmwareAsset := decode[model.Asset](t, res.Data)
	// draft 状态下固件接口仍无更新
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/device/wobble-wrist-band/firmware?currentVersion=1.0.0", "", nil)
	if none := decode[struct {
		UpToDate bool `json:"upToDate"`
	}](t, res.Data); !none.UpToDate {
		t.Fatal("draft 固件不应下发")
	}
	// 发布后固件接口有更新
	if code, _ = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/assets/"+int64String(firmwareAsset.ID)+"/status", adminToken,
		map[string]any{"status": model.AssetStatusPublished}); code != http.StatusOK {
		t.Fatal("发布固件失败")
	}
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/device/wobble-wrist-band/firmware?currentVersion=1.0.0", "", nil)
	firmwareUpdate := decode[struct {
		UpToDate bool   `json:"upToDate"`
		Version  string `json:"version"`
		Size     int64  `json:"size"`
	}](t, res.Data)
	if firmwareUpdate.UpToDate || firmwareUpdate.Version != "1.1.0" || firmwareUpdate.Size != 2048 {
		t.Fatalf("固件下发异常: %+v", firmwareUpdate)
	}

	// 5) 列表与过滤
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/assets?kind=web&refId=android", adminToken, nil)
	all := decode[struct {
		Items []model.Asset `json:"items"`
	}](t, res.Data)
	if len(all.Items) != 1 || all.Items[0].ID != asset.ID {
		t.Fatalf("资产列表异常: %+v", all.Items)
	}
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/admin/assets?kind=web&refId=android&status=draft", adminToken, nil)
	if drafts := decode[struct {
		Items []model.Asset `json:"items"`
	}](t, res.Data); len(drafts.Items) != 0 {
		t.Fatalf("published 过滤异常: %+v", drafts.Items)
	}

	// 6) 下线 → 公开接口回到无更新；非法状态 400；删除
	if code, _ = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/assets/"+int64String(asset.ID)+"/status", adminToken,
		map[string]any{"status": model.AssetStatusOffline}); code != http.StatusOK {
		t.Fatal("下线失败")
	}
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/app/web-bundle?platform=android&currentVersion=0.1.0", "", nil)
	if offline := decode[struct {
		UpToDate bool   `json:"upToDate"`
		Version  string `json:"version"`
	}](t, res.Data); !offline.UpToDate || offline.Version != "" {
		t.Fatalf("下线后不应下发: %+v", offline)
	}
	if code, _ = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/assets/"+int64String(asset.ID)+"/status", adminToken,
		map[string]any{"status": "bogus"}); code != http.StatusBadRequest {
		t.Fatalf("非法状态应 400，实际 %d", code)
	}
	if code, _ = doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/assets/"+int64String(asset.ID), adminToken, nil); code != http.StatusOK {
		t.Fatalf("删除资产失败: %d", code)
	}
	if code, _ = doJSON(t, env.engine, http.MethodDelete, "/api/v1/admin/assets/"+int64String(asset.ID), adminToken, nil); code != http.StatusNotFound {
		t.Fatalf("重复删除应 404，实际 %d", code)
	}
	if code, _ = doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/assets/abc/status", adminToken,
		map[string]any{"status": "draft"}); code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应 400，实际 %d", code)
	}
}

func TestPublicReleaseUsesCDNWhenConfigured(t *testing.T) {
	env := newRBACTestEnv(t, "https://cdn.example.com")
	adminToken, _ := env.newUserWithRoles(t, "13800009701", model.RoleAdmin)

	code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets", adminToken, map[string]any{
		"kind": "web", "refId": "android", "version": "0.5.0", "filename": "app.zip",
		"objectKey": "web/android/0.5.0/app.zip", "size": 10, "sha256": "abc",
	})
	if code != http.StatusCreated {
		t.Fatalf("登记资产失败: %d %s", code, res.Message)
	}
	asset := decode[model.Asset](t, res.Data)
	if code, _ := doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/assets/"+int64String(asset.ID)+"/status", adminToken,
		map[string]any{"status": model.AssetStatusPublished}); code != http.StatusOK {
		t.Fatal("发布失败")
	}
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/app/web-bundle?platform=android&currentVersion=0.1.0", "", nil)
	update := decode[struct {
		URL string `json:"url"`
	}](t, res.Data)
	if update.URL != "https://cdn.example.com/web/android/0.5.0/app.zip" {
		t.Fatalf("配置 CDN 时应下发 CDN 地址，实际 %s", update.URL)
	}
}

func TestCatalogResourceURLBackfill(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800009801", model.RoleAdmin)

	if code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/devices", adminToken, map[string]any{
		"modelId": "wobble-wrist-band", "name": "不倒翁手腕训练仪", "capabilityTags": []string{"posture-sensor"},
		"status": "on",
	}); code != http.StatusOK {
		t.Fatalf("建设备失败: %s", res.Message)
	}
	if code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/games", adminToken, map[string]any{
		"gameId": "target-reach", "name": "四方挥腕挑战", "requiredTags": []string{"posture-sensor"},
		"resourceVersion": "1.0.0", "status": "on", "config": map[string]any{"difficulty": map[string]any{"level": 2}},
	}); code != http.StatusOK {
		t.Fatalf("建游戏失败: %s", res.Message)
	}

	// 未登记资源：resourceUrl 为空、可选字段不出现
	_, res := doJSON(t, env.engine, http.MethodGet, "/api/v1/catalog/games", "", nil)
	games := decode[[]model.GameCatalogItem](t, res.Data)
	if len(games) != 1 || games[0].ResourceURL != "" {
		t.Fatalf("初始目录异常: %+v", games)
	}
	if string(games[0].Config) == "" || string(games[0].Config) == "{}" {
		t.Fatalf("游戏 JSONB 扩展配置未往返: %s", games[0].Config)
	}

	// 登记并发布 1.0.0 资源包
	code, res := doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets", adminToken, map[string]any{
		"kind": "game", "refId": "target-reach", "version": "1.0.0", "filename": "res.zip",
		"objectKey": "games/target-reach/1.0.0/res.zip", "size": 2048, "sha256": "DEADBEEF",
	})
	if code != http.StatusCreated {
		t.Fatalf("登记游戏资源失败: %d %s", code, res.Message)
	}
	gameAsset := decode[model.Asset](t, res.Data)
	// 另登记一个不匹配版本的 published 资产，确认按 resourceVersion 精确命中
	code, res = doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/assets", adminToken, map[string]any{
		"kind": "game", "refId": "target-reach", "version": "1.1.0", "filename": "res.zip",
		"objectKey": "games/target-reach/1.1.0/res.zip", "size": 4096, "sha256": "cafebabe",
	})
	if code != http.StatusCreated {
		t.Fatalf("登记游戏资源失败: %d", code)
	}
	other := decode[model.Asset](t, res.Data)
	for _, id := range []int64{gameAsset.ID, other.ID} {
		if code, _ := doJSON(t, env.engine, http.MethodPatch, "/api/v1/admin/assets/"+int64String(id)+"/status", adminToken,
			map[string]any{"status": model.AssetStatusPublished}); code != http.StatusOK {
			t.Fatal("发布游戏资源失败")
		}
	}

	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/catalog/games", "", nil)
	games = decode[[]model.GameCatalogItem](t, res.Data)
	if len(games) != 1 || !strings.Contains(games[0].ResourceURL, "games/target-reach/1.0.0/res.zip") {
		t.Fatalf("公开目录未回填 resourceUrl: %+v", games)
	}
	if games[0].ResourceSha256 != "deadbeef" || games[0].ResourceSize != 2048 {
		t.Fatalf("回填 sha256/size 异常: %+v", games[0])
	}

	// 设备维度同样回填
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/device/wobble-wrist-band/games", "", nil)
	deviceGames := decode[[]model.GameCatalogItem](t, res.Data)
	if len(deviceGames) != 1 || deviceGames[0].ResourceURL == "" || deviceGames[0].ResourceSize != 2048 {
		t.Fatalf("设备目录未回填: %+v", deviceGames)
	}

	// 已手工配置 resourceUrl 时以数据库值为准（不被资产覆盖）
	if code, _ := doJSON(t, env.engine, http.MethodPut, "/api/v1/admin/games/target-reach", adminToken, map[string]any{
		"gameId": "target-reach", "name": "四方挥腕挑战", "requiredTags": []string{"posture-sensor"},
		"resourceVersion": "1.0.0", "resourceUrl": "https://cdn.example.com/manual.zip", "status": "on",
	}); code != http.StatusOK {
		t.Fatal("更新游戏失败")
	}
	_, res = doJSON(t, env.engine, http.MethodGet, "/api/v1/catalog/games", "", nil)
	games = decode[[]model.GameCatalogItem](t, res.Data)
	if games[0].ResourceURL != "https://cdn.example.com/manual.zip" {
		t.Fatalf("手工配置的 resourceUrl 不应被覆盖: %+v", games[0])
	}
}
