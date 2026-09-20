package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Sessions 管理访问令牌的吊销状态（会话缓存）。
//
// JWT 本身无状态，登出时无法撤回已签发的访问令牌；
// 这里以「令牌指纹 + 剩余有效期」写入缓存形成吊销名单，
// Redis 部署时多实例共享，内存部署时仅本实例生效。
type Sessions struct {
	store Store
}

func NewSessions(store Store) *Sessions {
	return &Sessions{store: store}
}

// Fingerprint 计算令牌指纹：只存哈希，缓存泄库也无法重放。
func Fingerprint(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func revokedKey(fingerprint string) string {
	return Key("session", "revoked", fingerprint)
}

// Revoke 吊销令牌，ttl 应取该令牌的剩余有效期（过期后自动清理）。
func (s *Sessions) Revoke(ctx context.Context, fingerprint string, ttl time.Duration) error {
	if fingerprint == "" {
		return nil
	}
	if ttl <= 0 {
		// 令牌已过期，无需登记。
		return nil
	}
	return s.store.Set(ctx, revokedKey(fingerprint), "1", ttl)
}

// IsRevoked 判断令牌是否已被吊销；缓存不可用时返回 false（失败开放，避免全站无法登录）。
func (s *Sessions) IsRevoked(ctx context.Context, fingerprint string) (bool, error) {
	if fingerprint == "" {
		return false, nil
	}
	_, ok, err := s.store.Get(ctx, revokedKey(fingerprint))
	if err != nil {
		return false, err
	}
	return ok, nil
}

// Restore 从吊销名单移除（用户重新登录或运维恢复）。
func (s *Sessions) Restore(ctx context.Context, fingerprint string) error {
	return s.store.Del(ctx, revokedKey(fingerprint))
}

// Kind 便于启动日志与自检输出当前后端。
func (s *Sessions) Kind() string { return s.store.Kind() }
