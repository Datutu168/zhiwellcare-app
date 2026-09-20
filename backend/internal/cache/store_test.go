package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestMemoryStoreSetGetDel(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	if store.Kind() != "memory" {
		t.Fatalf("Kind() = %q, 期望 memory", store.Kind())
	}

	if _, ok, _ := store.Get(ctx, "missing"); ok {
		t.Fatal("未写入的键不应命中")
	}
	if err := store.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	value, ok, err := store.Get(ctx, "k")
	if err != nil || !ok || value != "v" {
		t.Fatalf("Get = (%q,%v,%v)，期望 (v,true,nil)", value, ok, err)
	}

	if err := store.Del(ctx, "k"); err != nil {
		t.Fatalf("Del 失败: %v", err)
	}
	if _, ok, _ := store.Get(ctx, "k"); ok {
		t.Fatal("删除后不应命中")
	}
}

func TestMemoryStoreExpires(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	if err := store.Set(ctx, "k", "v", 20*time.Millisecond); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok, _ := store.Get(ctx, "k"); ok {
		t.Fatal("TTL 到期后不应命中")
	}
}

func TestMemoryStoreAllowFixedWindow(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	window := time.Minute
	for i := 0; i < 3; i++ {
		if allowed, err := store.Allow(ctx, "ip", window, 3); err != nil || !allowed {
			t.Fatalf("第 %d 次应放行，得到 allowed=%v err=%v", i+1, allowed, err)
		}
	}
	if allowed, err := store.Allow(ctx, "ip", window, 3); err != nil || allowed {
		t.Fatalf("超过阈值应拒绝，得到 allowed=%v err=%v", allowed, err)
	}
	// 不同 key 互不影响。
	if allowed, err := store.Allow(ctx, "other", window, 3); err != nil || !allowed {
		t.Fatalf("不同 key 应独立计数，得到 allowed=%v err=%v", allowed, err)
	}
}

func newRedisStoreForTest(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	store, err := NewRedis(context.Background(), "redis://"+server.Addr()+"/0")
	if err != nil {
		server.Close()
		t.Fatalf("连接 miniredis 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
		server.Close()
	})
	return store, server
}

func TestRedisStoreSetGetDel(t *testing.T) {
	ctx := context.Background()
	store, _ := newRedisStoreForTest(t)
	if store.Kind() != "redis" {
		t.Fatalf("Kind() = %q, 期望 redis", store.Kind())
	}
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Ping 失败: %v", err)
	}
	if _, ok, err := store.Get(ctx, "missing"); ok || err != nil {
		t.Fatalf("未写入的键应返回 (false,nil)，得到 ok=%v err=%v", ok, err)
	}
	if err := store.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	value, ok, err := store.Get(ctx, "k")
	if err != nil || !ok || value != "v" {
		t.Fatalf("Get = (%q,%v,%v)，期望 (v,true,nil)", value, ok, err)
	}
	if err := store.Del(ctx, "k"); err != nil {
		t.Fatalf("Del 失败: %v", err)
	}
	if _, ok, _ := store.Get(ctx, "k"); ok {
		t.Fatal("删除后不应命中")
	}
}

func TestRedisStoreAllowFixedWindow(t *testing.T) {
	ctx := context.Background()
	store, server := newRedisStoreForTest(t)
	for i := 0; i < 2; i++ {
		if allowed, err := store.Allow(ctx, "login|ip", time.Minute, 2); err != nil || !allowed {
			t.Fatalf("第 %d 次应放行，得到 allowed=%v err=%v", i+1, allowed, err)
		}
	}
	if allowed, err := store.Allow(ctx, "login|ip", time.Minute, 2); err != nil || allowed {
		t.Fatalf("超过阈值应拒绝，得到 allowed=%v err=%v", allowed, err)
	}
	// 窗口过期后恢复放行。
	server.FastForward(2 * time.Minute)
	if allowed, err := store.Allow(ctx, "login|ip", time.Minute, 2); err != nil || !allowed {
		t.Fatalf("窗口过期后应重新放行，得到 allowed=%v err=%v", allowed, err)
	}
}

// TestDeletePrefixMemoryOnlyRemovesMatchingKeys 内存实现：前缀删除只清命中的键。
//
// 场景即生产事故：迁移直接写库授予了新权限，重启后必须能按 zwkl:rbac 前缀
// 一次性清掉所有用户权限缓存，同时不能误伤其它命名空间（如设备映射）。
func TestDeletePrefixMemoryOnlyRemovesMatchingKeys(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	rbacKeys := []string{
		Key("rbac", "user", "x"),
		Key("rbac", "user", "y"),
	}
	otherKeys := []string{
		Key("device", "mapping", "y"),
		Key("session", "revoked", "z"),
		Key("ratelimit", "login|ip"),
	}
	for _, key := range append(append([]string{}, rbacKeys...), otherKeys...) {
		if err := store.Set(ctx, key, "v", time.Minute); err != nil {
			t.Fatalf("Set(%s) 失败: %v", key, err)
		}
	}
	// 限流计数也带上 rbac 前缀，确认 DeletePrefix 一并清理（避免残留旧计数）。
	if _, err := store.Allow(ctx, Key("rbac", "user", "x"), time.Minute, 5); err != nil {
		t.Fatalf("Allow 失败: %v", err)
	}

	if err := store.DeletePrefix(ctx, Key("rbac")); err != nil {
		t.Fatalf("DeletePrefix 失败: %v", err)
	}

	for _, key := range rbacKeys {
		if _, ok, _ := store.Get(ctx, key); ok {
			t.Fatalf("rbac 键 %s 应被前缀删除", key)
		}
	}
	for _, key := range otherKeys {
		if _, ok, err := store.Get(ctx, key); err != nil || !ok {
			t.Fatalf("无关键 %s 不应被删除（ok=%v err=%v）", key, ok, err)
		}
	}
	// 前缀删除是幂等的：再删一次不报错，且无关键仍在。
	if err := store.DeletePrefix(ctx, Key("rbac")); err != nil {
		t.Fatalf("重复 DeletePrefix 应成功: %v", err)
	}
	if _, ok, _ := store.Get(ctx, Key("device", "mapping", "y")); !ok {
		t.Fatal("重复前缀删除不应影响无关键")
	}
	// 未命中任何键的前缀同样不报错。
	if err := store.DeletePrefix(ctx, Key("not", "used")); err != nil {
		t.Fatalf("空前缀匹配应成功: %v", err)
	}
	// 防呆：空前缀会清整个缓存，必须报错且不删任何键。
	if err := store.DeletePrefix(ctx, ""); err == nil {
		t.Fatal("空前缀应返回错误（防止误清整个缓存）")
	}
	if _, ok, _ := store.Get(ctx, Key("device", "mapping", "y")); !ok {
		t.Fatal("空前缀报错后不应删除任何键")
	}
}

// TestDeletePrefixRedisOnlyRemovesMatchingKeys Redis 实现：SCAN+DEL 语义与内存实现一致。
func TestDeletePrefixRedisOnlyRemovesMatchingKeys(t *testing.T) {
	ctx := context.Background()
	store, server := newRedisStoreForTest(t)

	rbacKeys := []string{
		Key("rbac", "user", "x"),
		Key("rbac", "user", "y"),
	}
	// 外加一批同前缀键，确保跨多轮 SCAN 游标也能全部清掉（批量 200 不足以一轮扫完）。
	for i := 0; i < 250; i++ {
		rbacKeys = append(rbacKeys, Key("rbac", "user", "bulk-"+string(rune('a'+i%26))+string(rune('0'+i%10))+string(rune('0'+i/10))))
	}
	otherKeys := []string{
		Key("device", "mapping", "y"),
		Key("session", "revoked", "z"),
	}
	for _, key := range append(append([]string{}, rbacKeys...), otherKeys...) {
		if err := store.Set(ctx, key, "v", time.Minute); err != nil {
			t.Fatalf("Set(%s) 失败: %v", key, err)
		}
	}

	if err := store.DeletePrefix(ctx, Key("rbac")); err != nil {
		t.Fatalf("DeletePrefix 失败: %v", err)
	}

	for _, key := range rbacKeys {
		if _, ok, _ := store.Get(ctx, key); ok {
			t.Fatalf("rbac 键 %s 应被前缀删除", key)
		}
	}
	for _, key := range otherKeys {
		if _, ok, err := store.Get(ctx, key); err != nil || !ok {
			t.Fatalf("无关键 %s 不应被删除（ok=%v err=%v）", key, ok, err)
		}
	}
	// 清理后 Redis 里不应再有 rbac 前缀键（用 SCAN 核对，不复用被测方法）。
	cursor := uint64(0)
	remaining := 0
	for {
		keys, next, err := store.Client().Scan(ctx, cursor, Key("rbac")+"*", 100).Result()
		if err != nil {
			t.Fatalf("SCAN 核对失败: %v", err)
		}
		remaining += len(keys)
		if next == 0 {
			break
		}
		cursor = next
	}
	if remaining != 0 {
		t.Fatalf("清理后仍残留 %d 个 rbac 键", remaining)
	}
	// 既有键数量不受影响（确认没有误删）。
	if server.Exists(Key("device", "mapping", "y")) != true {
		t.Fatal("设备映射键应保留")
	}
	// 防呆：空前缀必须报错（否则会清掉整个 keyspace）。
	if err := store.DeletePrefix(ctx, ""); err == nil {
		t.Fatal("空前缀应返回错误")
	}
	if server.Exists(Key("device", "mapping", "y")) != true {
		t.Fatal("空前缀报错后不应删除任何键")
	}
}

func TestNewFallsBackToMemoryWhenRedisUnavailable(t *testing.T) {
	ctx := context.Background()
	// 端口 1 上不会有 Redis：应回退内存实现而不是报错。
	store := New(ctx, "redis://127.0.0.1:1/0", nil)
	defer store.Close()
	if store.Kind() != "memory" {
		t.Fatalf("Redis 不可用时应回退 memory，得到 %q", store.Kind())
	}
	// 非法连接串同样回退。
	bad := New(ctx, "not-a-url", nil)
	defer bad.Close()
	if bad.Kind() != "memory" {
		t.Fatalf("非法连接串应回退 memory，得到 %q", bad.Kind())
	}
	// 空连接串直接内存实现。
	empty := New(ctx, "", nil)
	defer empty.Close()
	if empty.Kind() != "memory" {
		t.Fatalf("空连接串应使用 memory，得到 %q", empty.Kind())
	}
}

func TestGetOrLoadCachesAndPropagatesError(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	calls := 0
	load := func(context.Context) (string, error) {
		calls++
		return "model-x", nil
	}
	for i := 0; i < 3; i++ {
		value, err := GetOrLoad(ctx, store, "k", time.Minute, load)
		if err != nil || value != "model-x" {
			t.Fatalf("第 %d 次 GetOrLoad = (%q,%v)", i+1, value, err)
		}
	}
	if calls != 1 {
		t.Fatalf("回源次数 = %d，期望 1（后两次命中缓存）", calls)
	}

	boom := errors.New("db down")
	if _, err := GetOrLoad(ctx, store, "k2", time.Minute, func(context.Context) (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Fatalf("错误应向上传递，得到 %v", err)
	}
	// 空结果不写缓存：每次都会回源。
	emptyCalls := 0
	emptyLoad := func(context.Context) (string, error) { emptyCalls++; return "", nil }
	_, _ = GetOrLoad(ctx, store, "k3", time.Minute, emptyLoad)
	_, _ = GetOrLoad(ctx, store, "k3", time.Minute, emptyLoad)
	if emptyCalls != 2 {
		t.Fatalf("空结果不应写缓存，回源次数 = %d，期望 2", emptyCalls)
	}
}

func TestGetJSONIgnoresCorruptedValue(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	if err := store.Set(ctx, "bad", "{不是合法 JSON", time.Minute); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	var out []string
	ok, err := GetJSON(ctx, store, "bad", &out)
	if err != nil {
		t.Fatalf("脏数据不应返回错误，得到 %v", err)
	}
	if ok {
		t.Fatal("脏数据应视为未命中")
	}
}

func TestSessionsRevokeAndRestore(t *testing.T) {
	ctx := context.Background()
	sessions := NewSessions(NewMemory())
	fingerprint := Fingerprint("raw-access-token")

	if revoked, err := sessions.IsRevoked(ctx, fingerprint); err != nil || revoked {
		t.Fatalf("初始不应被吊销，得到 revoked=%v err=%v", revoked, err)
	}
	if err := sessions.Revoke(ctx, fingerprint, time.Minute); err != nil {
		t.Fatalf("吊销失败: %v", err)
	}
	if revoked, err := sessions.IsRevoked(ctx, fingerprint); err != nil || !revoked {
		t.Fatalf("吊销后应为 true，得到 revoked=%v err=%v", revoked, err)
	}
	// 已过期令牌（ttl<=0）不登记。
	if err := sessions.Revoke(ctx, Fingerprint("expired"), 0); err != nil {
		t.Fatalf("过期令牌吊销应静默成功: %v", err)
	}
	if revoked, _ := sessions.IsRevoked(ctx, Fingerprint("expired")); revoked {
		t.Fatal("已过期令牌不应登记进吊销名单")
	}
	if err := sessions.Restore(ctx, fingerprint); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if revoked, _ := sessions.IsRevoked(ctx, fingerprint); revoked {
		t.Fatal("恢复后不应再被判为吊销")
	}
	// 指纹只存哈希：缓存里不应出现明文令牌。
	if Fingerprint("raw-access-token") == "raw-access-token" {
		t.Fatal("指纹必须是哈希")
	}
}

func TestSessionsWorksOnRedis(t *testing.T) {
	ctx := context.Background()
	store, _ := newRedisStoreForTest(t)
	sessions := NewSessions(store)
	fingerprint := Fingerprint("token-on-redis")
	if err := sessions.Revoke(ctx, fingerprint, time.Minute); err != nil {
		t.Fatalf("Redis 吊销失败: %v", err)
	}
	if revoked, err := sessions.IsRevoked(ctx, fingerprint); err != nil || !revoked {
		t.Fatalf("Redis 上应判定为已吊销，得到 revoked=%v err=%v", revoked, err)
	}
}

func TestDevicesMappingReadThrough(t *testing.T) {
	ctx := context.Background()
	devices := NewDevices(NewMemory(), time.Minute)
	calls := 0
	load := func(context.Context) (string, error) {
		calls++
		return "wobble-wrist-band", nil
	}
	// 大小写与空白应归一到同一缓存键。
	for _, key := range []string{"BT91-ABC", " bt91-abc "} {
		model, ok, err := devices.Mapping(ctx, key, load)
		if err != nil || !ok || model != "wobble-wrist-band" {
			t.Fatalf("Mapping(%q) = (%q,%v,%v)", key, model, ok, err)
		}
	}
	if calls != 1 {
		t.Fatalf("回源次数 = %d，期望 1", calls)
	}

	// 无映射关系：ok=false 且不写空值缓存。
	missing, ok, err := devices.Mapping(ctx, "unknown-device", func(context.Context) (string, error) { return "", nil })
	if err != nil || ok || missing != "" {
		t.Fatalf("无映射应返回 (\"\",false,nil)，得到 (%q,%v,%v)", missing, ok, err)
	}

	// 显式写入后立即生效；失效后重新回源。
	if err := devices.SaveMapping(ctx, "BT91-ABC", "desk-torque-base"); err != nil {
		t.Fatalf("SaveMapping 失败: %v", err)
	}
	model, _, _ := devices.Mapping(ctx, "bt91-abc", load)
	if model != "desk-torque-base" {
		t.Fatalf("写入映射后应命中新值，得到 %q", model)
	}
	if err := devices.ForgetMapping(ctx, "BT91-ABC"); err != nil {
		t.Fatalf("ForgetMapping 失败: %v", err)
	}
	model, _, _ = devices.Mapping(ctx, "bt91-abc", load)
	if model != "wobble-wrist-band" {
		t.Fatalf("失效后应重新回源，得到 %q", model)
	}
}

func TestDevicesWhitelist(t *testing.T) {
	ctx := context.Background()
	devices := NewDevices(NewMemory(), time.Minute)
	calls := 0
	load := func(context.Context) ([]string, error) {
		calls++
		return []string{"AA:BB", " cc:dd "}, nil
	}

	allowed, err := devices.IsWhitelisted(ctx, "aa:bb", load)
	if err != nil || !allowed {
		t.Fatalf("白名单内设备应放行，得到 allowed=%v err=%v", allowed, err)
	}
	allowed, err = devices.IsWhitelisted(ctx, "CC:DD", load)
	if err != nil || !allowed {
		t.Fatalf("白名单应忽略大小写与空白，得到 allowed=%v err=%v", allowed, err)
	}
	allowed, err = devices.IsWhitelisted(ctx, "ee:ff", load)
	if err != nil || allowed {
		t.Fatalf("白名单外设备应拒绝，得到 allowed=%v err=%v", allowed, err)
	}
	if calls != 1 {
		t.Fatalf("白名单回源次数 = %d，期望 1（缓存生效）", calls)
	}

	if err := devices.ForgetWhitelist(ctx); err != nil {
		t.Fatalf("ForgetWhitelist 失败: %v", err)
	}
	if _, err := devices.IsWhitelisted(ctx, "aa:bb", load); err != nil {
		t.Fatalf("失效后重新回源失败: %v", err)
	}
	if calls != 2 {
		t.Fatalf("失效后应重新回源，回源次数 = %d，期望 2", calls)
	}

	// 空白名单 = 不限制。
	empty := NewDevices(NewMemory(), time.Minute)
	allowed, err = empty.IsWhitelisted(ctx, "any-device", func(context.Context) ([]string, error) { return nil, nil })
	if err != nil || !allowed {
		t.Fatalf("空白名单应放行所有设备，得到 allowed=%v err=%v", allowed, err)
	}
}
