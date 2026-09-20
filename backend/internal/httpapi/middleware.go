package httpapi

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/rbac"
)

const (
	ctxUserID      = "auth.userID"
	ctxRole        = "auth.role"
	ctxRoles       = "auth.roles"
	ctxPermissions = "auth.permissions"
)

// bearerToken 从 Authorization 头提取 Bearer 令牌。
func bearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// RequireAuth 校验访问令牌、检查会话吊销名单并注入用户上下文；失败统一 401。
// sessions 为 nil 时跳过吊销检查（例如未接缓存的测试装配）。
func RequireAuth(manager *auth.TokenManager, sessions *cache.Sessions) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearerToken(c.GetHeader("Authorization"))
		if raw == "" {
			failUnauthorized(c, "请先登录")
			c.Abort()
			return
		}
		userID, role, err := manager.Parse(raw)
		if err != nil {
			mapStoreError(c, err)
			c.Abort()
			return
		}
		if sessions != nil {
			// 缓存不可用时放行（失败开放），避免缓存抖动导致全站掉线。
			if revoked, err := sessions.IsRevoked(c, cache.Fingerprint(raw)); err == nil && revoked {
				failUnauthorized(c, "登录状态已失效，请重新登录")
				c.Abort()
				return
			}
		}
		c.Set(ctxUserID, userID)
		c.Set(ctxRole, role)
		c.Next()
	}
}

// RequireRole 在 RequireAuth 之后使用：校验角色（RBAC，如 admin）。
// 保留该中间件以兼容既有装配与测试；新的后台接口请用 RequirePermission。
func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		current, exists := c.Get(ctxRole)
		if !exists || current != role {
			fail(c, http.StatusForbidden, "没有权限执行该操作")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequirePermission 在 RequireAuth 之后使用：按权限点鉴权。
//
// 权限集合来自「用户 → 角色 → 权限」的实时解析（cache.Store 缓存，TTL 5 分钟），
// 因此调整角色权限或给用户分配角色后立即生效，不依赖重新登录刷新 JWT。
// 解析失败时按拒绝处理（安全优先），避免库/缓存抖动导致越权放行。
func RequirePermission(resolver *rbac.Resolver, code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, exists := c.Get(ctxUserID)
		userID, ok := raw.(string)
		if !exists || !ok || userID == "" {
			failUnauthorized(c, "请先登录")
			c.Abort()
			return
		}
		if resolver == nil {
			fail(c, http.StatusForbidden, "没有权限执行该操作")
			c.Abort()
			return
		}
		set, err := resolver.Resolve(c.Request.Context(), userID)
		if err != nil {
			fail(c, http.StatusForbidden, "没有权限执行该操作")
			c.Abort()
			return
		}
		c.Set(ctxRoles, set.Roles)
		c.Set(ctxPermissions, set.Permissions)
		if !set.Has(code) {
			fail(c, http.StatusForbidden, "没有权限执行该操作")
			c.Abort()
			return
		}
		c.Next()
	}
}

// resolvePermissions 供 handler 复用当前请求的权限上下文（必要时回源）。
func resolvePermissions(c *gin.Context, resolver *rbac.Resolver, userID string) (*rbac.Set, error) {
	if resolver == nil {
		return &rbac.Set{Roles: []string{}, Permissions: []string{}}, nil
	}
	return resolver.Resolve(c.Request.Context(), userID)
}

// limiter 极简内存限流：按 key 每窗口最大次数。
type limiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string][]time.Time
}

func newLimiter(window time.Duration, max int) *limiter {
	return &limiter{window: window, max: max, hits: make(map[string][]time.Time)}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	recent := l.hits[key][:0]
	for _, hit := range l.hits[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= l.max {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

// RateLimit 对敏感接口按客户端 IP 限流。
//
// store 非空时用缓存计数（Redis 部署下多实例共享额度）；
// store 为空或缓存报错时退化为进程内计数，保证限流不会因缓存故障而失效。
func RateLimit(store cache.Store, window time.Duration, max int) gin.HandlerFunc {
	fallback := newLimiter(window, max)
	return func(c *gin.Context) {
		key := c.FullPath() + "|" + c.ClientIP()
		allowed := true
		if store != nil {
			if value, err := store.Allow(c, cache.Key("ratelimit", key), window, max); err == nil {
				allowed = value
			} else {
				allowed = fallback.allow(key)
			}
		} else {
			allowed = fallback.allow(key)
		}
		if !allowed {
			fail(c, 429, "操作过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}
