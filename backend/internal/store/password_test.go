package store

import (
	"context"
	"errors"
	"testing"

	"zhiwellcare/backend/internal/auth"
)

// TestMemoryUpdateUserPassword 内存实现的改密：不存在的用户 → ErrNotFound；成功 → 哈希被覆盖。
func TestMemoryUpdateUserPassword(t *testing.T) {
	ctx := context.Background()
	data := NewMemory()

	// 不存在的用户 → ErrNotFound（mapStoreError 映射为 404）
	if err := data.UpdateUserPassword(ctx, "no-such-user", "hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的用户改密应返回 ErrNotFound，实际 %v", err)
	}

	oldHash, err := auth.HashPassword("old123456")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	user, err := data.CreateUser(ctx, CreateUserParams{Phone: "13800009002", PasswordHash: oldHash, Nickname: "改密"})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	newHash, err := auth.HashPassword("new123456")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	if err := data.UpdateUserPassword(ctx, user.ID, newHash); err != nil {
		t.Fatalf("改密失败: %v", err)
	}

	found, err := data.GetUserByPhone(ctx, "13800009002")
	if err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if found.PasswordHash != newHash {
		t.Fatalf("口令哈希未更新: %q", found.PasswordHash)
	}
	if !auth.VerifyPassword(found.PasswordHash, "new123456") {
		t.Fatal("新密码校验失败")
	}
	if auth.VerifyPassword(found.PasswordHash, "old123456") {
		t.Fatal("旧密码仍然可用")
	}
}
