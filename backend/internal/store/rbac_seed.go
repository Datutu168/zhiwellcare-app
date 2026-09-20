package store

import "zhiwellcare/backend/internal/model"

// 权限点目录：与 migrations/003_rbac_config.sql 的 permissions 种子保持一致。
// 内存实现（演示/测试）用它播种；PostgreSQL 实现由迁移播种。
var permissionSeeds = []model.Permission{
	{Code: model.PermDeviceRead, Name: "设备型号查看", Group: "device", Description: "查看设备型号目录"},
	{Code: model.PermDeviceWrite, Name: "设备型号维护", Group: "device", Description: "新增/修改/删除设备型号与设备映射"},
	{Code: model.PermGameRead, Name: "游戏目录查看", Group: "game", Description: "查看游戏目录"},
	{Code: model.PermGameWrite, Name: "游戏目录维护", Group: "game", Description: "新增/修改/删除游戏与上下架"},
	{Code: model.PermRecordRead, Name: "训练记录查看", Group: "record", Description: "查看训练记录与仪表盘统计"},
	{Code: model.PermUserRead, Name: "用户查看", Group: "user", Description: "查看用户列表"},
	{Code: model.PermUserWrite, Name: "用户维护", Group: "user", Description: "启用/停用用户、分配用户角色"},
	{Code: model.PermRoleRead, Name: "角色权限查看", Group: "role", Description: "查看角色与权限点"},
	{Code: model.PermRoleWrite, Name: "角色权限维护", Group: "role", Description: "新增/修改/删除角色与角色权限"},
	{Code: model.PermConfigRead, Name: "配置查看", Group: "config", Description: "查看配置中心 JSONB 配置项"},
	{Code: model.PermConfigWrite, Name: "配置维护", Group: "config", Description: "修改配置中心配置项"},
	{Code: model.PermAssetRead, Name: "资产查看", Group: "asset", Description: "查看 Web 包/游戏资源/固件资产"},
	{Code: model.PermAssetWrite, Name: "资产维护", Group: "asset", Description: "登记资产、发布/下线、删除资产"},
	{Code: model.PermWhitelistRead, Name: "设备白名单查看", Group: "whitelist", Description: "查看设备白名单"},
	{Code: model.PermWhitelistWrite, Name: "设备白名单维护", Group: "whitelist", Description: "增删设备白名单"},
}

// builtinRoleSeed 内置角色种子。
type builtinRoleSeed struct {
	Code        string
	Name        string
	Description string
	// Permissions 为空表示「全部权限」（admin）。
	Permissions []string
}

var builtinRoleSeeds = []builtinRoleSeed{
	{
		Code:        model.RoleAdmin,
		Name:        "超级管理员",
		Description: "拥有全部权限（含后续新增权限点），内置角色不可删除",
	},
	{
		Code:        model.RoleOperator,
		Name:        "运营",
		Description: "设备/游戏/白名单/资产的日常运营，可读配置与训练记录",
		Permissions: []string{
			model.PermDeviceRead, model.PermDeviceWrite, model.PermGameRead, model.PermGameWrite,
			model.PermRecordRead, model.PermUserRead, model.PermAssetRead, model.PermAssetWrite,
			model.PermWhitelistRead, model.PermWhitelistWrite, model.PermConfigRead,
		},
	},
	{
		Code:        model.RoleViewer,
		Name:        "只读观察",
		Description: "仅查看类权限，适合客服与数据查看",
		Permissions: []string{
			model.PermDeviceRead, model.PermGameRead, model.PermRecordRead, model.PermUserRead,
			model.PermRoleRead, model.PermConfigRead, model.PermAssetRead, model.PermWhitelistRead,
		},
	},
}

// allPermissionCodes 返回全部权限点编码（admin 角色用）。
func allPermissionCodes() []string {
	codes := make([]string, 0, len(permissionSeeds))
	for _, item := range permissionSeeds {
		codes = append(codes, item.Code)
	}
	return codes
}

// appConfigSeeds 配置中心种子（与迁移 003 一致）。
var appConfigSeeds = []model.AppConfigItem{
	{Key: "catalog.version", Value: []byte(`"1.0.0"`), Description: "目录配置版本号（客户端缓存失效用）"},
	{Key: "catalog.gray.enabled", Value: []byte(`false`), Description: "目录灰度开关（false 时全量下发）"},
	{Key: "feature.firmware_ota", Value: []byte(`true`), Description: "固件 OTA 开关"},
	{Key: "feature.samples_upload", Value: []byte(`true`), Description: "训练高频采样文件上传开关"},
}

// knownPermissionCode 判断权限点是否存在。
func knownPermissionCode(code string) bool {
	for _, item := range permissionSeeds {
		if item.Code == code {
			return true
		}
	}
	return false
}
