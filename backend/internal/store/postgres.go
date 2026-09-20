package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"zhiwellcare/backend/internal/model"
)

// Postgres 基于 pgxpool 的 PostgreSQL 存储实现。
type Postgres struct {
	pool *pgxpool.Pool
	// partitionEnsured 记录本进程已确保存在的训练记录月分区（YYYY-MM），
	// 避免每次写入都调用一次建分区的 DDL。
	partitionEnsured sync.Map
}

func NewPostgres(ctx context.Context, dbURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool 创建失败: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL 连接失败: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// DBTarget 从连接串解析出「主机 + 库名」（不含口令），用于启动日志与事故排查。
//
// 目的：像 APP_DB_URL 被误清空、godotenv 又从 .env 回填了正式库这类事故，
// 启动日志里一眼就能看到实际连的是哪个库，而不是等到写坏数据才发现。
func DBTarget(dbURL string) (host, database string) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return "", ""
	}
	return cfg.ConnConfig.Host, cfg.ConnConfig.Database
}

// RunMigrations 幂等执行 migrations/*.sql（按文件名顺序），可在每次启动调用。
func (p *Postgres) RunMigrations(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取迁移目录 %s 失败: %w", dir, err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		path := filepath.Join(dir, name)
		script, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取迁移 %s 失败: %w", name, err)
		}
		if _, err := p.pool.Exec(ctx, string(script)); err != nil {
			return fmt.Errorf("执行迁移 %s 失败: %w", name, err)
		}
		fmt.Printf("[migrate] %s applied\n", name)
	}
	return nil
}

func (p *Postgres) CreateUser(ctx context.Context, params CreateUserParams) (*model.User, error) {
	row := p.pool.QueryRow(ctx,
		`INSERT INTO users (phone, password_hash, nickname)
		 VALUES ($1, $2, $3)
		 RETURNING id, COALESCE(phone, ''), password_hash, nickname, avatar_url, role, status, created_at, updated_at, last_login_at`,
		params.Phone, params.PasswordHash, params.Nickname,
	)
	user, err := scanUser(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrPhoneExists
		}
		return nil, err
	}
	return user, nil
}

func (p *Postgres) GetUserByPhone(ctx context.Context, phone string) (*model.User, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, COALESCE(phone, ''), password_hash, nickname, avatar_url, role, status, created_at, updated_at, last_login_at FROM users WHERE phone = $1`, phone)
	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (p *Postgres) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, COALESCE(phone, ''), password_hash, nickname, avatar_url, role, status, created_at, updated_at, last_login_at FROM users WHERE id = $1`, id)
	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (p *Postgres) UpdateNickname(ctx context.Context, id, nickname string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE users SET nickname = $2, updated_at = now() WHERE id = $1`, id, nickname)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (p *Postgres) TouchLastLogin(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, `UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1`, id)
	return err
}

func (p *Postgres) ListUsers(ctx context.Context, keyword string, status, page, pageSize int) ([]model.User, int64, error) {
	where := []string{"1=1"}
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if keyword != "" {
		where = append(where, "(COALESCE(phone,'') ILIKE '%'||"+arg(keyword)+"||'%' OR nickname ILIKE '%'||"+arg(keyword)+"||'%')")
	}
	if status >= 0 {
		where = append(where, "status = "+arg(status))
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	page, pageSize = normalizePage(page, pageSize)
	query := `SELECT id, COALESCE(phone,''), password_hash, nickname, avatar_url, role, status, created_at, updated_at, last_login_at
		FROM users WHERE ` + clause + ` ORDER BY created_at DESC LIMIT ` + arg(pageSize) + ` OFFSET ` + arg((page-1)*pageSize)
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []model.User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *user)
	}
	return items, total, rows.Err()
}

func (p *Postgres) SetUserStatus(ctx context.Context, id string, status int) error {
	tag, err := p.pool.Exec(ctx, `UPDATE users SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetUserRole 兼容旧的角色字段写入（如启动引导 bootstrapAdmin）。
// 同时同步 user_roles：users.role 只是 admin 角色的派生标记（RBAC 事实来源是 user_roles）。
func (p *Postgres) SetUserRole(ctx context.Context, id, role string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE users SET role=$2, updated_at=now() WHERE id=$1`, id, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	if role == model.RoleAdmin {
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_code) VALUES ($1,'admin') ON CONFLICT (user_id, role_code) DO NOTHING`,
			id); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx,
			`DELETE FROM user_roles WHERE user_id=$1 AND role_code='admin'`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) Save(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
	return err
}

func (p *Postgres) FindByHash(ctx context.Context, tokenHash string) (*model.RefreshTokenRow, error) {
	row := p.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at, created_at FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	)
	var result model.RefreshTokenRow
	err := row.Scan(&result.ID, &result.UserID, &result.TokenHash, &result.ExpiresAt, &result.RevokedAt, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *Postgres) Revoke(ctx context.Context, tokenHash string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTokenNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*model.User, error) {
	var user model.User
	err := row.Scan(
		&user.ID, &user.Phone, &user.PasswordHash, &user.Nickname, &user.AvatarURL,
		&user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Ensure Postgres 实现全部接口。
var _ Combined = (*Postgres)(nil)
