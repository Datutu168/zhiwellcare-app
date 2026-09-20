package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"zhiwellcare/backend/internal/model"
)

func TestMemoryRBACStore(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()

	// 权限点种子：15 个（与迁移 003 一致）
	permissions, err := data.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("ListPermissions 失败: %v", err)
	}
	if len(permissions) != 15 {
		t.Fatalf("权限点应为 15 个，实际 %d", len(permissions))
	}
	for _, item := range permissions {
		if item.Code == "" || item.Name == "" || item.Group == "" {
			t.Fatalf("权限点字段不完整: %+v", item)
		}
	}

	// 内置角色与权限集合
	admin, err := data.GetRole(ctx, model.RoleAdmin)
	if err != nil || !admin.Builtin || len(admin.Permissions) != 15 {
		t.Fatalf("admin 角色异常: %+v %v", admin, err)
	}
	operator, err := data.GetRole(ctx, model.RoleOperator)
	if err != nil || len(operator.Permissions) != 11 {
		t.Fatalf("operator 角色异常: %+v %v", operator, err)
	}
	viewer, err := data.GetRole(ctx, model.RoleViewer)
	if err != nil || len(viewer.Permissions) != 8 {
		t.Fatalf("viewer 角色异常: %+v %v", viewer, err)
	}
	if _, err := data.GetRole(ctx, "not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的角色应返回 ErrNotFound，实际 %v", err)
	}

	// 角色 CRUD
	created, err := data.CreateRole(ctx, model.Role{
		Code: "care", Name: "客服", Permissions: []string{model.PermUserRead, model.PermRecordRead},
	})
	if err != nil {
		t.Fatalf("CreateRole 失败: %v", err)
	}
	if created.Builtin || len(created.Permissions) != 2 || created.UserCount != 0 {
		t.Fatalf("CreateRole 返回异常: %+v", created)
	}
	if _, err := data.CreateRole(ctx, model.Role{Code: "care", Name: "重复"}); !errors.Is(err, ErrRoleExists) {
		t.Fatalf("重复编码应返回 ErrRoleExists，实际 %v", err)
	}
	if _, err := data.CreateRole(ctx, model.Role{Code: "ghost", Permissions: []string{"nope"}}); !errors.Is(err, ErrPermissionUnknown) {
		t.Fatalf("未知权限点应返回 ErrPermissionUnknown，实际 %v", err)
	}
	updated, err := data.UpdateRole(ctx, "care", "客服组", "描述", []string{model.PermUserRead})
	if err != nil || updated.Name != "客服组" || len(updated.Permissions) != 1 {
		t.Fatalf("UpdateRole 异常: %+v %v", updated, err)
	}
	if _, err := data.UpdateRole(ctx, "missing", "x", "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("更新不存在的角色应返回 ErrNotFound，实际 %v", err)
	}
	if err := data.DeleteRole(ctx, model.RoleAdmin); !errors.Is(err, ErrRoleBuiltin) {
		t.Fatalf("删除内置角色应返回 ErrRoleBuiltin，实际 %v", err)
	}
	if err := data.DeleteRole(ctx, "care"); err != nil {
		t.Fatalf("DeleteRole 失败: %v", err)
	}
	if err := data.DeleteRole(ctx, "care"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}

	// 用户角色：无角色 → 无权限；有权限/无权限判定
	user, err := data.CreateUser(ctx, CreateUserParams{Phone: "13800000001", PasswordHash: "x", Nickname: "甲"})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	roles, err := data.ListUserRoles(ctx, user.ID)
	if err != nil || len(roles) != 0 {
		t.Fatalf("初始角色应为空: %v %v", roles, err)
	}
	perms, err := data.ListUserPermissions(ctx, user.ID)
	if err != nil || len(perms) != 0 {
		t.Fatalf("初始权限应为空: %v %v", perms, err)
	}

	if _, err := data.SetUserRoles(ctx, user.ID, []string{model.RoleOperator}, ""); err != nil {
		t.Fatalf("SetUserRoles 失败: %v", err)
	}
	perms, _ = data.ListUserPermissions(ctx, user.ID)
	if !containsString(perms, model.PermDeviceWrite) || containsString(perms, model.PermRoleWrite) {
		t.Fatalf("operator 权限集合异常: %v", perms)
	}
	stored, _ := data.GetUserByID(ctx, user.ID)
	if stored.Role != "user" {
		t.Fatalf("仅 operator 角色时 users.role 应为 user，实际 %q", stored.Role)
	}
	roleUserIDs, err := data.ListRoleUserIDs(ctx, model.RoleOperator)
	if err != nil || len(roleUserIDs) != 1 || roleUserIDs[0] != user.ID {
		t.Fatalf("ListRoleUserIDs 异常: %v %v", roleUserIDs, err)
	}
	// 仍有用户的自定义角色不可删除（内置角色有单独的 ErrRoleBuiltin）
	care, err := data.CreateRole(ctx, model.Role{Code: "care", Name: "客服", Permissions: []string{model.PermUserRead}})
	if err != nil {
		t.Fatalf("CreateRole 失败: %v", err)
	}
	if _, err := data.SetUserRoles(ctx, user.ID, []string{model.RoleOperator, care.Code}, ""); err != nil {
		t.Fatalf("SetUserRoles 失败: %v", err)
	}
	if err := data.DeleteRole(ctx, care.Code); !errors.Is(err, ErrRoleInUse) {
		t.Fatalf("删除仍有用户的角色应返回 ErrRoleInUse，实际 %v", err)
	}
	if _, err := data.SetUserRoles(ctx, user.ID, []string{model.RoleOperator}, ""); err != nil {
		t.Fatalf("SetUserRoles 失败: %v", err)
	}
	if err := data.DeleteRole(ctx, care.Code); err != nil {
		t.Fatalf("删除自定义角色失败: %v", err)
	}

	// 叠加 admin → users.role 同步为 admin
	if _, err := data.SetUserRoles(ctx, user.ID, []string{model.RoleOperator, model.RoleAdmin}, ""); err != nil {
		t.Fatalf("SetUserRoles 失败: %v", err)
	}
	stored, _ = data.GetUserByID(ctx, user.ID)
	if stored.Role != model.RoleAdmin {
		t.Fatalf("拥有 admin 角色后 users.role 应为 admin，实际 %q", stored.Role)
	}
	perms, _ = data.ListUserPermissions(ctx, user.ID)
	if !containsString(perms, model.PermRoleWrite) || len(perms) != 15 {
		t.Fatalf("admin 权限集合应为 15 个: %v", perms)
	}

	// 回收全部角色 → users.role 回退、权限清空
	if _, err := data.SetUserRoles(ctx, user.ID, nil, ""); err != nil {
		t.Fatalf("清空角色失败: %v", err)
	}
	stored, _ = data.GetUserByID(ctx, user.ID)
	if stored.Role != "user" {
		t.Fatalf("清空角色后 users.role 应为 user，实际 %q", stored.Role)
	}
	if perms, _ = data.ListUserPermissions(ctx, user.ID); len(perms) != 0 {
		t.Fatalf("清空角色后权限应为空: %v", perms)
	}

	// 分配不存在的角色 → ErrNotFound；不存在的用户 → ErrUserNotFound
	if _, err := data.SetUserRoles(ctx, user.ID, []string{"ghost-role"}, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("分配不存在的角色应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := data.SetUserRoles(ctx, "no-such-user", nil, ""); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("不存在的用户应返回 ErrUserNotFound，实际 %v", err)
	}

	// SetUserRole 兼容路径同样写 user_roles
	if err := data.SetUserRole(ctx, user.ID, model.RoleAdmin); err != nil {
		t.Fatalf("SetUserRole 失败: %v", err)
	}
	if roles, _ = data.ListUserRoles(ctx, user.ID); len(roles) != 1 || roles[0] != model.RoleAdmin {
		t.Fatalf("SetUserRole 未同步 user_roles: %v", roles)
	}
	if err := data.SetUserRole(ctx, user.ID, "user"); err != nil {
		t.Fatalf("SetUserRole 失败: %v", err)
	}
	if roles, _ = data.ListUserRoles(ctx, user.ID); len(roles) != 0 {
		t.Fatalf("回收 admin 后 user_roles 应为空: %v", roles)
	}
}

func TestMemoryConfigStore(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()

	items, err := data.ListAppConfig(ctx)
	if err != nil || len(items) != 4 {
		t.Fatalf("配置种子应为 4 个: %d %v", len(items), err)
	}
	item, err := data.GetAppConfig(ctx, "catalog.version")
	if err != nil || string(item.Value) != `"1.0.0"` {
		t.Fatalf("catalog.version 初值异常: %+v %v", item, err)
	}
	if _, err := data.GetAppConfig(ctx, "not.exists"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的配置应返回 ErrNotFound，实际 %v", err)
	}

	// 对象型 JSON 往返
	written, err := data.SetAppConfig(ctx, "catalog.thresholds", []byte(`{"score":80,"tags":["a"]}`), nil, "tester")
	if err != nil {
		t.Fatalf("SetAppConfig 失败: %v", err)
	}
	var decoded struct {
		Score int      `json:"score"`
		Tags  []string `json:"tags"`
	}
	if err := json.Unmarshal(written.Value, &decoded); err != nil || decoded.Score != 80 || len(decoded.Tags) != 1 {
		t.Fatalf("配置值往返异常: %+v %v", decoded, err)
	}

	// description 为 nil 保留原描述；显式传入则更新
	description := "新描述"
	if _, err := data.SetAppConfig(ctx, "catalog.thresholds", []byte(`true`), nil, ""); err != nil {
		t.Fatalf("SetAppConfig 失败: %v", err)
	}
	if got, _ := data.GetAppConfig(ctx, "catalog.thresholds"); got.Description != "" {
		t.Fatalf("未传描述时应保持空: %q", got.Description)
	}
	if _, err := data.SetAppConfig(ctx, "catalog.thresholds", []byte(`false`), &description, ""); err != nil {
		t.Fatalf("SetAppConfig 失败: %v", err)
	}
	got, _ := data.GetAppConfig(ctx, "catalog.thresholds")
	if got.Description != "新描述" || string(got.Value) != "false" {
		t.Fatalf("配置更新异常: %+v", got)
	}

	// 空值按 {} 落库；非法 JSON 报错
	if _, err := data.SetAppConfig(ctx, "catalog.empty", nil, nil, ""); err != nil {
		t.Fatalf("SetAppConfig 失败: %v", err)
	}
	if empty, _ := data.GetAppConfig(ctx, "catalog.empty"); string(empty.Value) != "{}" {
		t.Fatalf("空值应落为 {}，实际 %s", empty.Value)
	}
	if _, err := data.SetAppConfig(ctx, "catalog.bad", []byte(`{oops`), nil, ""); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestMemoryDeviceRegistryStore(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()

	entry, err := data.UpsertDeviceWhitelist(ctx, model.DeviceWhitelistEntry{
		DeviceKey: " BT91-ABC ", ModelID: "wobble-wrist-band", Note: "样机", Enabled: true,
	})
	if err != nil || entry.DeviceKey != "bt91-abc" {
		t.Fatalf("白名单写入异常: %+v %v", entry, err)
	}
	// 停用的白名单不出现在生效集合里
	if _, err := data.UpsertDeviceWhitelist(ctx, model.DeviceWhitelistEntry{DeviceKey: "off-1", Enabled: false}); err != nil {
		t.Fatalf("白名单写入失败: %v", err)
	}
	keys, err := data.ListWhitelistKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0] != "bt91-abc" {
		t.Fatalf("生效白名单异常: %v %v", keys, err)
	}
	if items, _ := data.ListDeviceWhitelist(ctx); len(items) != 2 {
		t.Fatalf("白名单列表应为 2 条: %+v", items)
	}
	if err := data.DeleteDeviceWhitelist(ctx, "BT91-abc"); err != nil {
		t.Fatalf("删除白名单失败: %v", err)
	}
	if err := data.DeleteDeviceWhitelist(ctx, "bt91-abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}

	// 映射：大小写归一
	if _, err := data.UpsertDeviceMapping(ctx, model.DeviceMappingEntry{DeviceKey: "BT91-ABC", ModelID: "wobble-wrist-band"}); err != nil {
		t.Fatalf("映射写入失败: %v", err)
	}
	modelID, err := data.GetDeviceMapping(ctx, "bt91-abc")
	if err != nil || modelID != "wobble-wrist-band" {
		t.Fatalf("映射读取异常: %q %v", modelID, err)
	}
	if _, err := data.GetDeviceMapping(ctx, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的映射应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := data.UpsertDeviceMapping(ctx, model.DeviceMappingEntry{DeviceKey: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺 modelId 应报错，实际 %v", err)
	}
	if err := data.DeleteDeviceMapping(ctx, "bt91-abc"); err != nil {
		t.Fatalf("删除映射失败: %v", err)
	}
	if err := data.DeleteDeviceMapping(ctx, "bt91-abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除映射应返回 ErrNotFound，实际 %v", err)
	}
}

func TestMemoryAssetStore(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()

	draft, err := data.CreateAsset(ctx, model.Asset{
		Kind: model.AssetKindGame, RefID: "target-reach", Version: "1.0.0",
		Filename: "res.zip", ObjectKey: "games/target-reach/1.0.0/res.zip", Size: 10, SHA256: "aa",
	})
	if err != nil || draft.Status != model.AssetStatusDraft || draft.PublishedAt != nil {
		t.Fatalf("资产登记异常: %+v %v", draft, err)
	}
	if _, err := data.CreateAsset(ctx, model.Asset{
		Kind: model.AssetKindGame, RefID: "target-reach", Version: "1.0.0", Filename: "res.zip",
	}); !errors.Is(err, ErrAssetExists) {
		t.Fatalf("重复登记应返回 ErrAssetExists，实际 %v", err)
	}

	// draft 不进入 published 查询
	if _, err := data.FindPublishedAsset(ctx, model.AssetKindGame, "target-reach", "1.0.0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft 不应命中 published 查询，实际 %v", err)
	}

	published, err := data.SetAssetStatus(ctx, draft.ID, model.AssetStatusPublished)
	if err != nil || published.Status != model.AssetStatusPublished || published.PublishedAt == nil {
		t.Fatalf("发布失败: %+v %v", published, err)
	}
	found, err := data.FindPublishedAsset(ctx, model.AssetKindGame, "target-reach", "1.0.0")
	if err != nil || found.ID != draft.ID {
		t.Fatalf("published 查询失败: %+v %v", found, err)
	}
	latest, err := data.LatestPublishedAsset(ctx, model.AssetKindGame, "target-reach")
	if err != nil || latest.ID != draft.ID {
		t.Fatalf("最新 published 查询失败: %+v %v", latest, err)
	}
	if _, err := data.LatestPublishedAsset(ctx, model.AssetKindWeb, "android"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("无 published 资产应返回 ErrNotFound，实际 %v", err)
	}

	// 状态过滤
	if items, _ := data.ListAssets(ctx, model.AssetKindGame, "target-reach", model.AssetStatusPublished); len(items) != 1 {
		t.Fatalf("published 过滤异常: %+v", items)
	}
	if items, _ := data.ListAssets(ctx, model.AssetKindGame, "target-reach", model.AssetStatusDraft); len(items) != 0 {
		t.Fatalf("draft 过滤异常: %+v", items)
	}
	if _, err := data.SetAssetStatus(ctx, 999, model.AssetStatusDraft); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的资产应返回 ErrNotFound，实际 %v", err)
	}
	if err := data.DeleteAsset(ctx, draft.ID); err != nil {
		t.Fatalf("删除资产失败: %v", err)
	}
	if err := data.DeleteAsset(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}

// TestMemoryTrainingRecordIdempotent 重复上报（同 recordId + completedAt）只落 1 行。
func TestMemoryTrainingRecordIdempotent(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()
	completed := time.Now().Truncate(time.Millisecond)

	record := model.TrainingSummary{RecordID: "rec-dup", GameID: "target-reach", CompletedAt: completed}
	if err := data.SaveTrainingRecord(ctx, record); err != nil {
		t.Fatalf("首次上报失败: %v", err)
	}
	if err := data.SaveTrainingRecord(ctx, record); err != nil {
		t.Fatalf("重复上报失败: %v", err)
	}
	items, total, err := data.ListTrainingRecords(ctx, TrainingRecordFilter{})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("重复上报应只落 1 行: total=%d items=%d err=%v", total, len(items), err)
	}

	// 同一 recordId、不同完成时间属于两次训练 → 2 行
	record2 := record
	record2.CompletedAt = completed.Add(time.Hour)
	if err := data.SaveTrainingRecord(ctx, record2); err != nil {
		t.Fatalf("上报失败: %v", err)
	}
	if _, total, _ = data.ListTrainingRecords(ctx, TrainingRecordFilter{}); total != 2 {
		t.Fatalf("不同完成时间应落 2 行，实际 %d", total)
	}
	// GetTrainingRecord 返回最新一条
	got, err := data.GetTrainingRecord(ctx, "rec-dup")
	if err != nil || !got.CompletedAt.Equal(record2.CompletedAt) {
		t.Fatalf("应返回最新记录: %+v %v", got, err)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
