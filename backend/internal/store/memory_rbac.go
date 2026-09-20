package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"zhiwellcare/backend/internal/model"
)

// sortedKeys 把集合转成有序切片，保证接口输出稳定（便于断言与前端 diff）。
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// normalizeDeviceKey 设备标识统一去空白 + 转小写，与 cache.Devices 的缓存键保持一致。
func normalizeDeviceKey(deviceKey string) string {
	return strings.ToLower(strings.TrimSpace(deviceKey))
}

// normalizeCodeList 去空、去重并排序的编码列表（权限码 / 角色码通用）。
func normalizeCodeList(codes []string) []string {
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code != "" {
			set[code] = struct{}{}
		}
	}
	return sortedKeys(set)
}

// validatePermissionCodes 校验权限点是否存在。
func validatePermissionCodes(codes []string) error {
	for _, code := range codes {
		if !knownPermissionCode(strings.TrimSpace(code)) {
			return ErrPermissionUnknown
		}
	}
	return nil
}

func (m *Memory) roleExistsLocked(code string) bool {
	_, ok := m.roles[code]
	return ok
}

// grantRoleLocked 授予角色（调用方需持锁）。
func (m *Memory) grantRoleLocked(userID, roleCode string) {
	if m.userRoles[userID] == nil {
		m.userRoles[userID] = make(map[string]struct{})
	}
	m.userRoles[userID][roleCode] = struct{}{}
}

// revokeRoleLocked 回收角色（调用方需持锁）。
func (m *Memory) revokeRoleLocked(userID, roleCode string) {
	if set := m.userRoles[userID]; set != nil {
		delete(set, roleCode)
	}
}

// userCountLocked 统计拥有某角色的用户数（调用方需持锁）。
func (m *Memory) userCountLocked(roleCode string) int64 {
	var count int64
	for _, set := range m.userRoles {
		if _, ok := set[roleCode]; ok {
			count++
		}
	}
	return count
}

// cloneRoleLocked 返回角色副本（含权限集合与用户数）。
func (m *Memory) cloneRoleLocked(code string) *model.Role {
	role := m.roles[code]
	if role == nil {
		return nil
	}
	copyRole := *role
	copyRole.Permissions = sortedKeys(m.rolePerms[code])
	copyRole.UserCount = m.userCountLocked(code)
	return &copyRole
}

// ---------- RBAC（内存演示） ----------

func (m *Memory) ListPermissions(_ context.Context) ([]model.Permission, error) {
	items := make([]model.Permission, len(permissionSeeds))
	copy(items, permissionSeeds)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		return items[i].Code < items[j].Code
	})
	return items, nil
}

func (m *Memory) ListRoles(_ context.Context) ([]model.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.Role, 0, len(m.roles))
	for code := range m.roles {
		items = append(items, *m.cloneRoleLocked(code))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Builtin != items[j].Builtin {
			return items[i].Builtin
		}
		return items[i].Code < items[j].Code
	})
	return items, nil
}

func (m *Memory) GetRole(_ context.Context, code string) (*model.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	role := m.cloneRoleLocked(code)
	if role == nil {
		return nil, ErrNotFound
	}
	return role, nil
}

func (m *Memory) CreateRole(_ context.Context, role model.Role) (*model.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.roleExistsLocked(role.Code) {
		return nil, ErrRoleExists
	}
	if err := validatePermissionCodes(role.Permissions); err != nil {
		return nil, err
	}
	m.roles[role.Code] = &model.Role{
		Code:        role.Code,
		Name:        role.Name,
		Description: role.Description,
		Builtin:     false,
	}
	m.rolePerms[role.Code] = make(map[string]struct{})
	for _, code := range normalizeCodeList(role.Permissions) {
		m.rolePerms[role.Code][code] = struct{}{}
	}
	return m.cloneRoleLocked(role.Code), nil
}

func (m *Memory) UpdateRole(_ context.Context, code, name, description string, permissions []string) (*model.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.roleExistsLocked(code) {
		return nil, ErrNotFound
	}
	if err := validatePermissionCodes(permissions); err != nil {
		return nil, err
	}
	role := m.roles[code]
	role.Name = name
	role.Description = description
	set := make(map[string]struct{})
	for _, item := range normalizeCodeList(permissions) {
		set[item] = struct{}{}
	}
	m.rolePerms[code] = set
	return m.cloneRoleLocked(code), nil
}

func (m *Memory) DeleteRole(_ context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	role := m.roles[code]
	if role == nil {
		return ErrNotFound
	}
	if role.Builtin {
		return ErrRoleBuiltin
	}
	if m.userCountLocked(code) > 0 {
		return ErrRoleInUse
	}
	delete(m.roles, code)
	delete(m.rolePerms, code)
	return nil
}

func (m *Memory) ListUserRoles(_ context.Context, userID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return sortedKeys(m.userRoles[userID]), nil
}

func (m *Memory) ListUserPermissions(_ context.Context, userID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	set := make(map[string]struct{})
	for roleCode := range m.userRoles[userID] {
		for code := range m.rolePerms[roleCode] {
			set[code] = struct{}{}
		}
	}
	return sortedKeys(set), nil
}

func (m *Memory) SetUserRoles(_ context.Context, userID string, roles []string, _ string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user := m.byID[userID]
	if user == nil {
		return nil, ErrUserNotFound
	}
	next := normalizeCodeList(roles)
	for _, code := range next {
		if !m.roleExistsLocked(code) {
			return nil, ErrNotFound
		}
	}
	set := make(map[string]struct{}, len(next))
	for _, code := range next {
		set[code] = struct{}{}
	}
	m.userRoles[userID] = set
	// users.role 为派生标记：拥有 admin 角色 → 'admin'，否则 'user'
	if _, isAdmin := set[model.RoleAdmin]; isAdmin {
		user.Role = model.RoleAdmin
	} else {
		user.Role = "user"
	}
	user.UpdatedAt = time.Now()
	return sortedKeys(set), nil
}

func (m *Memory) ListRoleUserIDs(_ context.Context, roleCode string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for userID, set := range m.userRoles {
		if _, ok := set[roleCode]; ok {
			ids = append(ids, userID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ---------- 配置中心（内存演示） ----------

func (m *Memory) ListAppConfig(_ context.Context) ([]model.AppConfigItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.AppConfigItem, 0, len(m.configs))
	for _, item := range m.configs {
		items = append(items, cloneAppConfig(item))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func (m *Memory) GetAppConfig(_ context.Context, key string) (*model.AppConfigItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item := m.configs[key]
	if item == nil {
		return nil, ErrNotFound
	}
	copyItem := cloneAppConfig(item)
	return &copyItem, nil
}

func (m *Memory) SetAppConfig(_ context.Context, key string, value []byte, description *string, _ string) (*model.AppConfigItem, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrNotFound
	}
	normalized, err := normalizeConfigValue(value)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.configs[key]
	if item == nil {
		item = &model.AppConfigItem{Key: key}
		m.configs[key] = item
	}
	item.Value = normalized
	if description != nil {
		item.Description = *description
	}
	item.UpdatedAt = time.Now()
	copyItem := cloneAppConfig(item)
	return &copyItem, nil
}

func cloneAppConfig(item *model.AppConfigItem) model.AppConfigItem {
	copyItem := *item
	copyItem.Value = append([]byte(nil), item.Value...)
	return copyItem
}

// normalizeConfigValue 空值按 {} 处理，并校验必须是合法 JSON（列类型为 jsonb）。
func normalizeConfigValue(value []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" {
		return []byte(`{}`), nil
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, errors.New("value 必须是合法 JSON")
	}
	return []byte(trimmed), nil
}

// ---------- 设备白名单 / 映射（内存演示） ----------

func (m *Memory) ListDeviceWhitelist(_ context.Context) ([]model.DeviceWhitelistEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.DeviceWhitelistEntry, 0, len(m.whitelist))
	for _, item := range m.whitelist {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DeviceKey < items[j].DeviceKey })
	return items, nil
}

func (m *Memory) ListWhitelistKeys(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for key, item := range m.whitelist {
		if item.Enabled {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *Memory) UpsertDeviceWhitelist(_ context.Context, entry model.DeviceWhitelistEntry) (*model.DeviceWhitelistEntry, error) {
	key := normalizeDeviceKey(entry.DeviceKey)
	if key == "" {
		return nil, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := &model.DeviceWhitelistEntry{
		DeviceKey: key,
		ModelID:   strings.TrimSpace(entry.ModelID),
		Note:      entry.Note,
		Enabled:   entry.Enabled,
	}
	m.whitelist[key] = stored
	copyEntry := *stored
	return &copyEntry, nil
}

func (m *Memory) DeleteDeviceWhitelist(_ context.Context, deviceKey string) error {
	key := normalizeDeviceKey(deviceKey)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.whitelist[key]; !exists {
		return ErrNotFound
	}
	delete(m.whitelist, key)
	return nil
}

func (m *Memory) ListDeviceMappings(_ context.Context) ([]model.DeviceMappingEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.DeviceMappingEntry, 0, len(m.mappings))
	for _, item := range m.mappings {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DeviceKey < items[j].DeviceKey })
	return items, nil
}

func (m *Memory) GetDeviceMapping(_ context.Context, deviceKey string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item := m.mappings[normalizeDeviceKey(deviceKey)]
	if item == nil {
		return "", ErrNotFound
	}
	return item.ModelID, nil
}

func (m *Memory) UpsertDeviceMapping(_ context.Context, entry model.DeviceMappingEntry) (*model.DeviceMappingEntry, error) {
	key := normalizeDeviceKey(entry.DeviceKey)
	if key == "" || strings.TrimSpace(entry.ModelID) == "" {
		return nil, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := &model.DeviceMappingEntry{DeviceKey: key, ModelID: strings.TrimSpace(entry.ModelID), Note: entry.Note}
	m.mappings[key] = stored
	copyEntry := *stored
	return &copyEntry, nil
}

func (m *Memory) DeleteDeviceMapping(_ context.Context, deviceKey string) error {
	key := normalizeDeviceKey(deviceKey)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.mappings[key]; !exists {
		return ErrNotFound
	}
	delete(m.mappings, key)
	return nil
}

// ---------- 资产登记（内存演示） ----------

func (m *Memory) CreateAsset(_ context.Context, asset model.Asset) (*model.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.assets {
		if existing.Kind == asset.Kind && existing.RefID == asset.RefID &&
			existing.Version == asset.Version && existing.Filename == asset.Filename {
			return nil, ErrAssetExists
		}
	}
	m.nextAsset++
	asset.ID = m.nextAsset
	asset.Status = model.NormalizeAssetStatus(asset.Status)
	asset.CreatedAt = time.Now()
	if asset.Status == model.AssetStatusPublished {
		published := asset.CreatedAt
		asset.PublishedAt = &published
	}
	stored := asset
	m.assets[stored.ID] = &stored
	m.assetOrder = append(m.assetOrder, stored.ID)
	copyAsset := stored
	return &copyAsset, nil
}

func (m *Memory) GetAsset(_ context.Context, id int64) (*model.Asset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	asset := m.assets[id]
	if asset == nil {
		return nil, ErrNotFound
	}
	copyAsset := *asset
	return &copyAsset, nil
}

func (m *Memory) ListAssets(_ context.Context, kind, refID, status string) ([]model.Asset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.Asset, 0, len(m.assets))
	for _, id := range m.assetOrder {
		asset := m.assets[id]
		if asset == nil {
			continue
		}
		if kind != "" && asset.Kind != kind {
			continue
		}
		if refID != "" && asset.RefID != refID {
			continue
		}
		if status != "" && asset.Status != status {
			continue
		}
		items = append(items, *asset)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func (m *Memory) SetAssetStatus(_ context.Context, id int64, status string) (*model.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	asset := m.assets[id]
	if asset == nil {
		return nil, ErrNotFound
	}
	asset.Status = status
	if status == model.AssetStatusPublished {
		now := time.Now()
		asset.PublishedAt = &now
	}
	copyAsset := *asset
	return &copyAsset, nil
}

func (m *Memory) DeleteAsset(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.assets[id]; !exists {
		return ErrNotFound
	}
	delete(m.assets, id)
	return nil
}

func (m *Memory) FindPublishedAsset(_ context.Context, kind, refID, version string) (*model.Asset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, asset := range m.assets {
		if asset.Kind == kind && asset.RefID == refID && asset.Version == version &&
			asset.Status == model.AssetStatusPublished {
			copyAsset := *asset
			return &copyAsset, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) LatestPublishedAsset(_ context.Context, kind, refID string) (*model.Asset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var latest *model.Asset
	for _, asset := range m.assets {
		if asset.Kind != kind || asset.RefID != refID || asset.Status != model.AssetStatusPublished {
			continue
		}
		if latest == nil || asset.ID > latest.ID {
			copyAsset := *asset
			latest = &copyAsset
		}
	}
	if latest == nil {
		return nil, ErrNotFound
	}
	return latest, nil
}
