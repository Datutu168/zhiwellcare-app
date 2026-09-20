package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"zhiwellcare/backend/internal/model"
)

// Memory 是 PostgreSQL 的演示/测试替身：结构等价、进程内存储。
type Memory struct {
	mu        sync.RWMutex
	users     map[string]*model.User // key: phone
	byID      map[string]*model.User // key: user id
	tokens    map[string]*model.RefreshTokenRow
	nextToken int64
	// 目录运营 / 训练摘要 / OAuth（演示模式支持后台接口联调）
	devices    map[string]*model.DeviceModel
	games      map[string]*model.GameCatalogItem
	courses    map[string]*model.Course
	goods      map[string]*model.MallGoods
	records    []model.TrainingSummary
	nextRecord int64
	oauth      map[string]string // provider|openid → userID
	oauthUnion map[string]string // provider|unionid → userID

	// RBAC / 配置中心 / 设备事实表 / 资产登记
	roles      map[string]*model.Role
	rolePerms  map[string]map[string]struct{} // roleCode → 权限码集合
	userRoles  map[string]map[string]struct{} // userID → 角色码集合
	configs    map[string]*model.AppConfigItem
	whitelist  map[string]*model.DeviceWhitelistEntry
	mappings   map[string]*model.DeviceMappingEntry
	assets     map[int64]*model.Asset
	assetOrder []int64
	nextAsset  int64
}

func NewMemory() *Memory {
	m := &Memory{
		users:      make(map[string]*model.User),
		byID:       make(map[string]*model.User),
		tokens:     make(map[string]*model.RefreshTokenRow),
		devices:    make(map[string]*model.DeviceModel),
		games:      make(map[string]*model.GameCatalogItem),
		courses:    make(map[string]*model.Course),
		goods:      make(map[string]*model.MallGoods),
		oauth:      make(map[string]string),
		oauthUnion: make(map[string]string),
		roles:      make(map[string]*model.Role),
		rolePerms:  make(map[string]map[string]struct{}),
		userRoles:  make(map[string]map[string]struct{}),
		configs:    make(map[string]*model.AppConfigItem),
		whitelist:  make(map[string]*model.DeviceWhitelistEntry),
		mappings:   make(map[string]*model.DeviceMappingEntry),
		assets:     make(map[int64]*model.Asset),
	}
	m.seedRBAC()
	return m
}

// seedRBAC 播种权限点 / 内置角色 / 配置中心默认值（与迁移 003 一致）。
func (m *Memory) seedRBAC() {
	all := allPermissionCodes()
	for _, seed := range builtinRoleSeeds {
		perms := seed.Permissions
		if len(perms) == 0 {
			perms = all
		}
		set := make(map[string]struct{}, len(perms))
		for _, code := range perms {
			set[code] = struct{}{}
		}
		m.rolePerms[seed.Code] = set
		m.roles[seed.Code] = &model.Role{
			Code:        seed.Code,
			Name:        seed.Name,
			Description: seed.Description,
			Builtin:     true,
			Permissions: sortedKeys(set),
			UserCount:   0,
		}
	}
	now := time.Now()
	for _, item := range appConfigSeeds {
		m.configs[item.Key] = &model.AppConfigItem{
			Key:         item.Key,
			Value:       append([]byte(nil), item.Value...),
			Description: item.Description,
			UpdatedAt:   now,
		}
	}
}

func (m *Memory) CreateUser(ctx context.Context, params CreateUserParams) (*model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.users[params.Phone]; exists {
		return nil, ErrPhoneExists
	}
	raw := sha256.Sum256([]byte(params.Phone + time.Now().String()))
	user := &model.User{
		ID:           hex.EncodeToString(raw[:12]),
		Phone:        params.Phone,
		PasswordHash: params.PasswordHash,
		Nickname:     params.Nickname,
		Role:         "user",
		Status:       1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	m.users[user.Phone] = user
	m.byID[user.ID] = user
	return user, nil
}

func (m *Memory) GetUserByPhone(_ context.Context, phone string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if user := m.users[phone]; user != nil {
		return cloneUser(user), nil
	}
	return nil, ErrUserNotFound
}

func (m *Memory) GetUserByID(_ context.Context, id string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if user := m.byID[id]; user != nil {
		return cloneUser(user), nil
	}
	return nil, ErrUserNotFound
}

func (m *Memory) UpdateNickname(_ context.Context, id, nickname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user := m.byID[id]
	if user == nil {
		return ErrUserNotFound
	}
	user.Nickname = nickname
	user.UpdatedAt = time.Now()
	return nil
}

func (m *Memory) TouchLastLogin(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user := m.byID[id]
	if user == nil {
		return ErrUserNotFound
	}
	now := time.Now()
	user.LastLoginAt = &now
	user.UpdatedAt = now
	return nil
}

func (m *Memory) ListUsers(_ context.Context, keyword string, status, page, pageSize int) ([]model.User, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matched []model.User
	for _, user := range m.users {
		if status >= 0 && user.Status != status {
			continue
		}
		if keyword != "" && !strings.Contains(user.Phone, keyword) && !strings.Contains(user.Nickname, keyword) {
			continue
		}
		matched = append(matched, *user)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CreatedAt.After(matched[j].CreatedAt) })
	page, pageSize = normalizePage(page, pageSize)
	start := (page - 1) * pageSize
	if start > len(matched) {
		start = len(matched)
	}
	end := start + pageSize
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], int64(len(matched)), nil
}

func (m *Memory) SetUserStatus(_ context.Context, id string, status int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user := m.byID[id]
	if user == nil {
		return ErrUserNotFound
	}
	user.Status = status
	user.UpdatedAt = time.Now()
	return nil
}

func (m *Memory) SetUserRole(_ context.Context, id, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user := m.byID[id]
	if user == nil {
		return ErrUserNotFound
	}
	user.Role = role
	user.UpdatedAt = time.Now()
	// 同步 user_roles：users.role 只是 admin 角色的派生标记
	if role == model.RoleAdmin {
		m.grantRoleLocked(id, model.RoleAdmin)
	} else {
		m.revokeRoleLocked(id, model.RoleAdmin)
	}
	return nil
}

func (m *Memory) Save(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextToken++
	m.tokens[tokenHash] = &model.RefreshTokenRow{
		ID:        m.nextToken,
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	return nil
}

func (m *Memory) FindByHash(_ context.Context, tokenHash string) (*model.RefreshTokenRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row := m.tokens[tokenHash]
	if row == nil {
		return nil, ErrTokenNotFound
	}
	copyRow := *row
	return &copyRow, nil
}

func (m *Memory) Revoke(_ context.Context, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.tokens[tokenHash]
	if row == nil {
		return ErrTokenNotFound
	}
	now := time.Now()
	row.RevokedAt = &now
	return nil
}

func cloneUser(user *model.User) *model.User {
	copy := *user
	return &copy
}

// Ensure Memory 实现全部接口。
var _ Combined = (*Memory)(nil)
