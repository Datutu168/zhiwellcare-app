package store

import (
	"context"
	"errors"

	"zhiwellcare/backend/internal/model"
)

// RBAC / 配置中心 / 设备事实表 / 资产登记相关的业务错误。
var (
	// ErrRoleExists 角色编码重复。
	ErrRoleExists = errors.New("角色编码已存在")
	// ErrRoleInUse 角色下仍有用户，禁止删除。
	ErrRoleInUse = errors.New("角色下仍有用户")
	// ErrRoleBuiltin 内置角色禁止删除。
	ErrRoleBuiltin = errors.New("内置角色不可删除")
	// ErrPermissionUnknown 引用了不存在的权限点。
	ErrPermissionUnknown = errors.New("权限点不存在")
	// ErrAssetExists 同 (kind, refId, version, filename) 的资产已登记。
	ErrAssetExists = errors.New("资产已存在")
)

// RBACStore 角色 / 权限 / 用户角色。
//
// 设计：user_roles 是「用户实际拥有哪些角色」的唯一事实来源；
// users.role 仅作为兼容旧逻辑的派生标记（拥有 admin 角色 → 'admin'，否则 'user'），
// 由 SetUserRoles / SetUserRole 同步维护。
type RBACStore interface {
	ListPermissions(ctx context.Context) ([]model.Permission, error)
	ListRoles(ctx context.Context) ([]model.Role, error)
	GetRole(ctx context.Context, code string) (*model.Role, error)
	CreateRole(ctx context.Context, role model.Role) (*model.Role, error)
	UpdateRole(ctx context.Context, code, name, description string, permissions []string) (*model.Role, error)
	DeleteRole(ctx context.Context, code string) error
	// ListUserRoles 返回用户拥有的角色编码（升序）。
	ListUserRoles(ctx context.Context, userID string) ([]string, error)
	// ListUserPermissions 返回用户经角色聚合后的权限点编码（升序、去重）。
	ListUserPermissions(ctx context.Context, userID string) ([]string, error)
	// SetUserRoles 全量覆盖用户角色，并同步 users.role。
	SetUserRoles(ctx context.Context, userID string, roles []string, grantedBy string) ([]string, error)
	// ListRoleUserIDs 返回拥有该角色的用户 ID（角色权限变更后失效权限缓存用）。
	ListRoleUserIDs(ctx context.Context, roleCode string) ([]string, error)
}

// ConfigStore 配置中心（JSONB）。
type ConfigStore interface {
	ListAppConfig(ctx context.Context) ([]model.AppConfigItem, error)
	GetAppConfig(ctx context.Context, key string) (*model.AppConfigItem, error)
	// SetAppConfig upsert 配置项；description 为 nil 时保留原描述。
	SetAppConfig(ctx context.Context, key string, value []byte, description *string, updatedBy string) (*model.AppConfigItem, error)
}

// DeviceRegistryStore 设备白名单与「设备标识 → 型号」映射的事实来源。
type DeviceRegistryStore interface {
	ListDeviceWhitelist(ctx context.Context) ([]model.DeviceWhitelistEntry, error)
	// ListWhitelistKeys 仅返回生效（enabled=true）的设备标识，供 cache.Devices 回源。
	ListWhitelistKeys(ctx context.Context) ([]string, error)
	UpsertDeviceWhitelist(ctx context.Context, entry model.DeviceWhitelistEntry) (*model.DeviceWhitelistEntry, error)
	DeleteDeviceWhitelist(ctx context.Context, deviceKey string) error

	ListDeviceMappings(ctx context.Context) ([]model.DeviceMappingEntry, error)
	// GetDeviceMapping 返回设备标识对应型号；不存在返回 ErrNotFound。
	GetDeviceMapping(ctx context.Context, deviceKey string) (string, error)
	UpsertDeviceMapping(ctx context.Context, entry model.DeviceMappingEntry) (*model.DeviceMappingEntry, error)
	DeleteDeviceMapping(ctx context.Context, deviceKey string) error
}

// AssetStore 资产登记（Web 包 / 游戏资源 / 固件）。
type AssetStore interface {
	CreateAsset(ctx context.Context, asset model.Asset) (*model.Asset, error)
	GetAsset(ctx context.Context, id int64) (*model.Asset, error)
	ListAssets(ctx context.Context, kind, refID, status string) ([]model.Asset, error)
	SetAssetStatus(ctx context.Context, id int64, status string) (*model.Asset, error)
	DeleteAsset(ctx context.Context, id int64) error
	// FindPublishedAsset 按 kind + refId + version 命中已发布资产；不存在返回 ErrNotFound。
	FindPublishedAsset(ctx context.Context, kind, refID, version string) (*model.Asset, error)
	// LatestPublishedAsset 取某 kind + refId 下最新发布的资产（按发布时间倒序）；不存在返回 ErrNotFound。
	LatestPublishedAsset(ctx context.Context, kind, refID string) (*model.Asset, error)
}
