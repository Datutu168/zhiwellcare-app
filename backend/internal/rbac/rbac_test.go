package rbac

import (
	"context"
	"testing"
	"time"

	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/store"
)

// TestPurgeCacheRecoversFromStalePersistedPermissions 复刻生产事故（004 迁移升级后管理员 403）：
//
//	① 用户是 admin，DB 侧权限集合已是最新（17 项，含 content:read/write）；
//	② 但缓存里是升级前写入的旧集合（15 项，无 content:*）；
//	   Redis 开 AOF 时这份旧缓存**跨重启存活**，服务重启也不会自动清；
//	③ 不做处理 → Resolve 返回旧集合 → content:write 判定失败 → /admin/courses 403；
//	④ 启动时执行 PurgeCache（main.go 在「迁移/bootstrap 之后、开始服务之前」调用）后，
//	   Resolve 回源拿到 17 项 → 新权限立即生效，不必等 5 分钟 TTL。
func TestPurgeCacheRecoversFromStalePersistedPermissions(t *testing.T) {
	ctx := context.Background()
	data := store.NewMemory()
	cacheStore := cache.NewMemory()
	resolver := NewResolver(data, cacheStore, time.Minute)

	user, err := data.CreateUser(ctx, store.CreateUserParams{Phone: "13800013001", PasswordHash: "x", Nickname: "管理员"})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if _, err := data.SetUserRoles(ctx, user.ID, []string{model.RoleAdmin}, ""); err != nil {
		t.Fatalf("分配 admin 角色失败: %v", err)
	}

	// ① DB 侧（事实来源）应包含 content:*
	fresh, err := resolver.Resolve(ctx, user.ID)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if !fresh.Has(model.PermContentWrite) {
		t.Fatalf("admin 应拥有 content:write: %v", fresh.Permissions)
	}

	// ② 模拟 AOF 里残留的升级前缓存（只有旧的 15 个权限点）
	stale := &Set{Roles: []string{model.RoleAdmin}, Permissions: []string{
		model.PermDeviceRead, model.PermDeviceWrite, model.PermGameRead, model.PermGameWrite,
		model.PermRecordRead, model.PermUserRead, model.PermUserWrite, model.PermRoleRead,
		model.PermRoleWrite, model.PermConfigRead, model.PermConfigWrite, model.PermAssetRead,
		model.PermAssetWrite, model.PermWhitelistRead, model.PermWhitelistWrite,
	}}
	if err := cache.SetJSON(ctx, cacheStore, CachePrefix()+":user:"+user.ID, stale, time.Minute); err != nil {
		t.Fatalf("写入旧缓存失败: %v", err)
	}
	// 同时放一个无关键，验证前缀清理不误伤。
	otherKey := cache.Key("device", "mapping", "keep-me")
	if err := cacheStore.Set(ctx, otherKey, "wobble-wrist-band", time.Minute); err != nil {
		t.Fatalf("写入无关键失败: %v", err)
	}

	// ③ 清缓存之前：读到旧集合 → content:write 判定失败（正是线上 403 的成因）
	before, err := resolver.Resolve(ctx, user.ID)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if before.Has(model.PermContentWrite) {
		t.Fatal("用例前提不成立：旧缓存里不该有 content:write")
	}
	if len(before.Permissions) != 15 {
		t.Fatalf("旧缓存应为 15 项，实际 %d", len(before.Permissions))
	}

	// ④ 启动清理（等价 main.go 里的 purgeRBACCache）
	if err := PurgeCache(ctx, cacheStore); err != nil {
		t.Fatalf("PurgeCache 失败: %v", err)
	}
	if _, ok, _ := cacheStore.Get(ctx, CachePrefix()+":user:"+user.ID); ok {
		t.Fatal("权限缓存应已被清理")
	}
	if _, ok, _ := cacheStore.Get(ctx, otherKey); !ok {
		t.Fatal("前缀清理不应删除其它命名空间的键")
	}

	after, err := resolver.Resolve(ctx, user.ID)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if !after.Has(model.PermContentWrite) || !after.Has(model.PermContentRead) {
		t.Fatalf("清理后应回源拿到最新权限（含 content:*）: %v", after.Permissions)
	}
	if len(after.Permissions) != len(fresh.Permissions) {
		t.Fatalf("清理后权限集合应等于 DB 事实来源: %d != %d", len(after.Permissions), len(fresh.Permissions))
	}

	// PurgeCache(nil) 不应 panic（cache 未配置时同样安全）。
	if err := PurgeCache(ctx, nil); err != nil {
		t.Fatalf("cache 为 nil 时应静默成功: %v", err)
	}
	// 幂等：再清一次仍成功。
	if err := PurgeCache(ctx, cacheStore); err != nil {
		t.Fatalf("重复 PurgeCache 应成功: %v", err)
	}
}

// TestCachePrefixIsRBACNamespace 前缀必须落在 zwkl:rbac（与 userKey 一致），
// 否则启动清理会清错命名空间或漏清。
func TestCachePrefixIsRBACNamespace(t *testing.T) {
	if CachePrefix() != "zwkl:rbac" {
		t.Fatalf("权限缓存前缀应为 zwkl:rbac，实际 %q", CachePrefix())
	}
	if userKey("u1") != CachePrefix()+":user:u1" {
		t.Fatalf("用户缓存键应在前缀之下: %q", userKey("u1"))
	}
	if len(CachePrefix()) >= len(userKey("u1")) {
		t.Fatal("前缀必须比具体键短，DeletePrefix 才有效")
	}
}
