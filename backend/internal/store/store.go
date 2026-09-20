// Package store 提供用户与刷新令牌的持久化抽象：
// PostgreSQL（生产）与内存（演示/测试）两套实现可随时切换。
package store

import (
	"context"
	"errors"
	"time"

	"zhiwellcare/backend/internal/model"
)

// 业务错误（存储层返回，handler 映射为统一错误码）。
var (
	ErrPhoneExists   = errors.New("该手机号已注册")
	ErrUserNotFound  = errors.New("用户不存在")
	ErrNotFound      = errors.New("资源不存在")
	ErrTokenNotFound = errors.New("刷新令牌不存在或已失效")
	ErrTokenRevoked  = errors.New("刷新令牌已被撤销")
	ErrTokenExpired  = errors.New("刷新令牌已过期")
)

// UserStore 用户读写接口。
type UserStore interface {
	CreateUser(ctx context.Context, params CreateUserParams) (*model.User, error)
	GetUserByPhone(ctx context.Context, phone string) (*model.User, error)
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	UpdateNickname(ctx context.Context, id, nickname string) error
	TouchLastLogin(ctx context.Context, id string) error
	// ListUsers 分页查询（keyword 匹配手机号/昵称），status=-1 表示不过滤。
	ListUsers(ctx context.Context, keyword string, status int, page, pageSize int) (items []model.User, total int64, err error)
	// SetUserStatus 启用/停用（1 正常 / 0 禁用）。
	SetUserStatus(ctx context.Context, id string, status int) error
	// SetUserRole 提升/回收角色（如 bootstrap 管理员）。
	SetUserRole(ctx context.Context, id, role string) error
	// UpdateUserPassword 覆盖用户口令哈希（入参已是哈希，任何日志都不得打印明文）。
	// 用户不存在返回 ErrNotFound（映射 404），不要用 ErrUserNotFound ——那会被 mapStoreError
	// 报成 401「手机号或密码不正确」，把「改自己密码时用户被删了」误导成密码错误。
	UpdateUserPassword(ctx context.Context, userID string, passwordHash string) error
}

// CreateUserParams 注册入参（密码已由调用方哈希）。
type CreateUserParams struct {
	Phone        string
	PasswordHash string
	Nickname     string
}

// RefreshTokenStore 刷新令牌读写接口（仅存哈希）。
type RefreshTokenStore interface {
	Save(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	FindByHash(ctx context.Context, tokenHash string) (*model.RefreshTokenRow, error)
	Revoke(ctx context.Context, tokenHash string) error
}
