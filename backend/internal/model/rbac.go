package model

import (
	"encoding/json"
	"time"
)

// 权限点编码（RBAC）：与 migrations/003_rbac_config.sql 的 permissions 种子一一对应。
const (
	PermDeviceRead     = "device:read"
	PermDeviceWrite    = "device:write"
	PermGameRead       = "game:read"
	PermGameWrite      = "game:write"
	PermRecordRead     = "record:read"
	PermUserRead       = "user:read"
	PermUserWrite      = "user:write"
	PermRoleRead       = "role:read"
	PermRoleWrite      = "role:write"
	PermConfigRead     = "config:read"
	PermConfigWrite    = "config:write"
	PermAssetRead      = "asset:read"
	PermAssetWrite     = "asset:write"
	PermWhitelistRead  = "whitelist:read"
	PermWhitelistWrite = "whitelist:write"
)

// 内置角色编码。
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// Permission 权限点（后台菜单与按钮级鉴权的原子单位）。
type Permission struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
}

// Role 角色：包含权限点集合与关联用户数（后台角色管理页直接用）。
type Role struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Builtin     bool     `json:"builtin"`
	Permissions []string `json:"permissions"`
	UserCount   int64    `json:"userCount"`
}

// AppConfigItem 配置中心条目（value 为任意 JSONB）。
type AppConfigItem struct {
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// DeviceWhitelistEntry 设备白名单条目（PostgreSQL 为唯一事实来源）。
type DeviceWhitelistEntry struct {
	DeviceKey string `json:"deviceKey"`
	ModelID   string `json:"modelId"`
	Note      string `json:"note"`
	Enabled   bool   `json:"enabled"`
}

// DeviceMappingEntry 设备标识 → 型号映射条目。
type DeviceMappingEntry struct {
	DeviceKey string `json:"deviceKey"`
	ModelID   string `json:"modelId"`
	Note      string `json:"note"`
}

// 资产类型与状态。
const (
	AssetKindWeb      = "web"
	AssetKindGame     = "game"
	AssetKindFirmware = "firmware"

	AssetStatusDraft     = "draft"
	AssetStatusPublished = "published"
	AssetStatusOffline   = "offline"
)

// Asset 资产登记：对象存储里的对象 + 版本元数据（Web 包 / 游戏资源 / 固件）。
type Asset struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	RefID       string     `json:"refId"`
	Version     string     `json:"version"`
	Filename    string     `json:"filename"`
	ObjectKey   string     `json:"objectKey"`
	Size        int64      `json:"size"`
	SHA256      string     `json:"sha256"`
	ContentType string     `json:"contentType"`
	Notes       string     `json:"notes"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// NormalizeAssetStatus 空状态按 draft 处理。
func NormalizeAssetStatus(status string) string {
	if status == "" {
		return AssetStatusDraft
	}
	return status
}

// ValidAssetKind 校验资产类型。
func ValidAssetKind(kind string) bool {
	return kind == AssetKindWeb || kind == AssetKindGame || kind == AssetKindFirmware
}

// ValidAssetStatus 校验资产状态。
func ValidAssetStatus(status string) bool {
	return status == AssetStatusDraft || status == AssetStatusPublished || status == AssetStatusOffline
}
