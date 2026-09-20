// Package rbac 解析「用户 → 角色 → 权限码集合」，并提供带缓存的读取与失效。
//
// 数据事实来源是 PostgreSQL（user_roles / role_permissions），缓存只做加速：
//   - 读：未命中回源并写入 cache.Store（TTL 复用 DeviceCacheTTL，默认 5 分钟）；
//   - 写：角色分配、角色权限变更、角色删除后必须显式失效，避免权限变更不生效。
package rbac

import (
	"context"
	"time"

	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/store"
)

// DefaultTTL 权限缓存默认时长。
const DefaultTTL = 5 * time.Minute

// Set 是一个用户的角色与权限集合（也是缓存存储结构）。
type Set struct {
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

// Has 判断是否拥有权限码。
func (s *Set) Has(code string) bool {
	if s == nil {
		return false
	}
	for _, item := range s.Permissions {
		if item == code {
			return true
		}
	}
	return false
}

// Resolver 用户权限解析器。
type Resolver struct {
	data  store.Combined
	cache cache.Store
	ttl   time.Duration
}

// NewResolver 创建解析器；cache 可为 nil（此时直接回源，不做缓存）。
func NewResolver(data store.Combined, cacheStore cache.Store, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Resolver{data: data, cache: cacheStore, ttl: ttl}
}

// TTL 返回缓存时长（便于启动日志与自检）。
func (r *Resolver) TTL() time.Duration { return r.ttl }

func userKey(userID string) string {
	return cache.Key("rbac", "user", userID)
}

// Resolve 解析用户角色与权限；未命中缓存时回源并写缓存。
func (r *Resolver) Resolve(ctx context.Context, userID string) (*Set, error) {
	if userID == "" {
		return &Set{Roles: []string{}, Permissions: []string{}}, nil
	}
	if r.cache != nil {
		var cached Set
		if ok, err := cache.GetJSON(ctx, r.cache, userKey(userID), &cached); err == nil && ok {
			if cached.Roles == nil {
				cached.Roles = []string{}
			}
			if cached.Permissions == nil {
				cached.Permissions = []string{}
			}
			return &cached, nil
		}
	}
	roles, err := r.data.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	permissions, err := r.data.ListUserPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []string{}
	}
	if permissions == nil {
		permissions = []string{}
	}
	set := &Set{Roles: roles, Permissions: permissions}
	if r.cache != nil {
		// 缓存写失败不影响本次结果。
		_ = cache.SetJSON(ctx, r.cache, userKey(userID), set, r.ttl)
	}
	return set, nil
}

// Forget 失效指定用户的权限缓存（分配角色后调用）。
func (r *Resolver) Forget(ctx context.Context, userIDs ...string) error {
	if r.cache == nil || len(userIDs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if id != "" {
			keys = append(keys, userKey(id))
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return r.cache.Del(ctx, keys...)
}

// ForgetRole 失效拥有该角色的全部用户的权限缓存（角色权限变更/删除后调用）。
func (r *Resolver) ForgetRole(ctx context.Context, roleCode string) error {
	if r.cache == nil {
		return nil
	}
	userIDs, err := r.data.ListRoleUserIDs(ctx, roleCode)
	if err != nil {
		return err
	}
	return r.Forget(ctx, userIDs...)
}
