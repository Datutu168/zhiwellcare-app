// Package cache 提供 Redis 缓存能力（映射 / 白名单 / 会话状态 / 分布式限流）。
//
// 设计目标：
//   - 生产多实例部署用 Redis，让限流计数、设备映射与白名单、会话吊销状态跨实例共享；
//   - 未配置 APP_REDIS_URL 或 Redis 连不上时，自动回退到进程内实现，
//     保证本地开发、单实例部署无需额外中间件即可运行（行为一致，仅不跨实例共享）。
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// 键前缀统一收敛，便于运维按前缀清理与排查。
const (
	keyPrefix = "zwkl"
)

// Key 拼接带命名空间的缓存键。
func Key(parts ...string) string {
	out := keyPrefix
	for _, part := range parts {
		out += ":" + part
	}
	return out
}

// Store 是最小缓存接口：字符串读写、删除、固定窗口限流与健康探测。
type Store interface {
	// Kind 返回实现类型：redis | memory
	Kind() string
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	// Allow 固定窗口限流：窗口内第 max+1 次调用返回 false。
	Allow(ctx context.Context, key string, window time.Duration, max int) (bool, error)
	Ping(ctx context.Context) error
	Close() error
}

// ---------- 工厂 ----------

// New 按连接串创建缓存；url 为空则直接使用内存实现。
// Redis 连接失败时打印告警并回退内存实现，避免因中间件不可用导致服务起不来。
func New(ctx context.Context, url string, logger *slog.Logger) Store {
	if url == "" {
		if logger != nil {
			logger.Info("未配置 APP_REDIS_URL，缓存使用进程内实现（单实例可用，多实例请接 Redis）")
		}
		return NewMemory()
	}
	store, err := NewRedis(ctx, url)
	if err != nil {
		if logger != nil {
			logger.Warn("Redis 连接失败，回退进程内缓存", "err", err)
		}
		return NewMemory()
	}
	if logger != nil {
		logger.Info("Redis 缓存已启用")
	}
	return store
}

// ---------- JSON 辅助 ----------

// SetJSON 以 JSON 形式写入缓存。
func SetJSON(ctx context.Context, store Store, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("缓存序列化失败: %w", err)
	}
	return store.Set(ctx, key, string(raw), ttl)
}

// GetJSON 读取并反序列化；未命中返回 false。
func GetJSON(ctx context.Context, store Store, key string, out any) (bool, error) {
	raw, ok, err := store.Get(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		// 脏数据不应让业务失败：当作未命中，交由上层回源。
		return false, nil
	}
	return true, nil
}

// GetOrLoad 穿透读缓存：未命中时调用 load 回源并写入。
// load 出错时直接返回错误（不回写缓存）；load 返回空串时不写缓存。
func GetOrLoad(ctx context.Context, store Store, key string, ttl time.Duration, load func(context.Context) (string, error)) (string, error) {
	if value, ok, err := store.Get(ctx, key); err == nil && ok {
		return value, nil
	}
	value, err := load(ctx)
	if err != nil {
		return "", err
	}
	if value != "" {
		if err := store.Set(ctx, key, value, ttl); err != nil {
			// 缓存写失败不影响业务结果。
			return value, nil
		}
	}
	return value, nil
}

// ---------- 内存实现 ----------

type memoryItem struct {
	value     string
	expiresAt time.Time
}

// MemoryStore 是进程内实现：单实例部署或本地开发使用。
type MemoryStore struct {
	mu    sync.Mutex
	items map[string]memoryItem
	hits  map[string][]time.Time
}

func NewMemory() *MemoryStore {
	return &MemoryStore{items: make(map[string]memoryItem), hits: make(map[string][]time.Time)}
}

func (m *MemoryStore) Kind() string { return "memory" }

func (m *MemoryStore) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[key]
	if !ok {
		return "", false, nil
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		delete(m.items, key)
		return "", false, nil
	}
	return item.value, true, nil
}

func (m *MemoryStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := memoryItem{value: value}
	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}
	m.items[key] = item
	return nil
}

func (m *MemoryStore) Del(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range keys {
		delete(m.items, key)
	}
	return nil
}

func (m *MemoryStore) Allow(_ context.Context, key string, window time.Duration, max int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)
	recent := m.hits[key][:0]
	for _, hit := range m.hits[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= max {
		m.hits[key] = recent
		return false, nil
	}
	m.hits[key] = append(recent, now)
	return true, nil
}

func (m *MemoryStore) Ping(context.Context) error { return nil }

func (m *MemoryStore) Close() error { return nil }

// ---------- Redis 实现 ----------

// allowScript 固定窗口计数：首次自增时设置过期，避免窗口被无限续期。
var allowScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return current
`)

// RedisStore 是 Redis 实现（多实例共享）。
type RedisStore struct {
	client *redis.Client
}

// NewRedis 建立连接并做一次 Ping 探测。
func NewRedis(ctx context.Context, url string) (*RedisStore, error) {
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("APP_REDIS_URL 无效: %w", err)
	}
	client := redis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &RedisStore{client: client}, nil
}

func (r *RedisStore) Kind() string { return "redis" }

func (r *RedisStore) Get(ctx context.Context, key string) (string, bool, error) {
	value, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (r *RedisStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisStore) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}

func (r *RedisStore) Allow(ctx context.Context, key string, window time.Duration, max int) (bool, error) {
	count, err := allowScript.Run(ctx, r.client, []string{key}, window.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return count <= int64(max), nil
}

func (r *RedisStore) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

func (r *RedisStore) Close() error { return r.client.Close() }

// Client 暴露底层客户端，供需要 Lua/管道的扩展场景使用。
func (r *RedisStore) Client() *redis.Client { return r.client }
