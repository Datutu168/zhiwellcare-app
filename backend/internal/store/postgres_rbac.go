package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"zhiwellcare/backend/internal/model"
)

// isForeignKeyViolation 外键冲突（23503）：如角色引用了不存在的权限点。
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// ---------- RBAC：角色 / 权限 ----------

const roleSelect = `
	SELECT r.code, r.name, r.description, r.builtin,
	       COALESCE((SELECT array_agg(rp.permission_code ORDER BY rp.permission_code)
	                 FROM role_permissions rp WHERE rp.role_code = r.code), '{}'::text[]) AS permissions,
	       (SELECT count(*) FROM user_roles ur WHERE ur.role_code = r.code) AS user_count
	FROM roles r`

func scanRole(row rowScanner) (*model.Role, error) {
	var role model.Role
	if err := row.Scan(&role.Code, &role.Name, &role.Description, &role.Builtin, &role.Permissions, &role.UserCount); err != nil {
		return nil, err
	}
	if role.Permissions == nil {
		role.Permissions = []string{}
	}
	return &role, nil
}

func (p *Postgres) ListPermissions(ctx context.Context) ([]model.Permission, error) {
	rows, err := p.pool.Query(ctx, `SELECT code, name, group_name, description FROM permissions ORDER BY group_name, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Permission{}
	for rows.Next() {
		var item model.Permission
		if err := rows.Scan(&item.Code, &item.Name, &item.Group, &item.Description); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) ListRoles(ctx context.Context) ([]model.Role, error) {
	rows, err := p.pool.Query(ctx, roleSelect+` ORDER BY r.builtin DESC, r.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Role{}
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *role)
	}
	return items, rows.Err()
}

func (p *Postgres) GetRole(ctx context.Context, code string) (*model.Role, error) {
	role, err := scanRole(p.pool.QueryRow(ctx, roleSelect+` WHERE r.code = $1`, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return role, err
}

// ensurePermissionsExist 校验权限点编码全部存在（避免外键错误难以解读）。
func (p *Postgres) ensurePermissionsExist(ctx context.Context, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	var count int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM permissions WHERE code = ANY($1::text[])`, codes).Scan(&count); err != nil {
		return err
	}
	if count != len(codes) {
		return ErrPermissionUnknown
	}
	return nil
}

func (p *Postgres) CreateRole(ctx context.Context, role model.Role) (*model.Role, error) {
	permissions := normalizeCodeList(role.Permissions)
	if err := p.ensurePermissionsExist(ctx, permissions); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO roles (code, name, description, builtin) VALUES ($1,$2,$3,false)`,
		role.Code, role.Name, role.Description); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrRoleExists
		}
		if isForeignKeyViolation(err) {
			return nil, ErrPermissionUnknown
		}
		return nil, err
	}
	if err := insertRolePermissions(ctx, tx, role.Code, permissions); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p.GetRole(ctx, role.Code)
}

func (p *Postgres) UpdateRole(ctx context.Context, code, name, description string, permissions []string) (*model.Role, error) {
	normalized := normalizeCodeList(permissions)
	if err := p.ensurePermissionsExist(ctx, normalized); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE roles SET name=$2, description=$3, updated_at=now() WHERE code=$1`, code, name, description)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_code=$1`, code); err != nil {
		return nil, err
	}
	if err := insertRolePermissions(ctx, tx, code, normalized); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p.GetRole(ctx, code)
}

// insertRolePermissions 批量写入角色权限（幂等去重）。
func insertRolePermissions(ctx context.Context, tx pgx.Tx, roleCode string, permissions []string) error {
	if len(permissions) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO role_permissions (role_code, permission_code)
		SELECT $1, code FROM unnest($2::text[]) AS code
		ON CONFLICT (role_code, permission_code) DO NOTHING`, roleCode, permissions)
	if err != nil && isForeignKeyViolation(err) {
		return ErrPermissionUnknown
	}
	return err
}

func (p *Postgres) DeleteRole(ctx context.Context, code string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var builtin bool
	if err := tx.QueryRow(ctx, `SELECT builtin FROM roles WHERE code=$1`, code).Scan(&builtin); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if builtin {
		return ErrRoleBuiltin
	}
	var users int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_roles WHERE role_code=$1`, code).Scan(&users); err != nil {
		return err
	}
	if users > 0 {
		return ErrRoleInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE code=$1`, code); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) ListUserRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT role_code FROM user_roles WHERE user_id=$1 ORDER BY role_code`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		roles = append(roles, code)
	}
	return roles, rows.Err()
}

func (p *Postgres) ListUserPermissions(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT rp.permission_code
		FROM user_roles ur JOIN role_permissions rp ON rp.role_code = ur.role_code
		WHERE ur.user_id = $1
		ORDER BY rp.permission_code`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	permissions := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		permissions = append(permissions, code)
	}
	return permissions, rows.Err()
}

func (p *Postgres) SetUserRoles(ctx context.Context, userID string, roles []string, grantedBy string) ([]string, error) {
	normalized := normalizeCodeList(roles)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM users WHERE id=$1`, userID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if len(normalized) > 0 {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM roles WHERE code = ANY($1::text[])`, normalized).Scan(&count); err != nil {
			return nil, err
		}
		if count != len(normalized) {
			return nil, ErrNotFound
		}
	}
	var granted any
	if grantedBy != "" {
		granted = grantedBy
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	if len(normalized) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_code, granted_by)
			SELECT $1, code, $3 FROM unnest($2::text[]) AS code
			ON CONFLICT (user_id, role_code) DO NOTHING`, userID, normalized, granted); err != nil {
			if isForeignKeyViolation(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	// users.role 只是派生标记：拥有 admin 角色 → 'admin'，否则 'user'
	derived := "user"
	for _, code := range normalized {
		if code == model.RoleAdmin {
			derived = model.RoleAdmin
			break
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET role=$2, updated_at=now() WHERE id=$1`, userID, derived); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return normalized, nil
}

func (p *Postgres) ListRoleUserIDs(ctx context.Context, roleCode string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT user_id::text FROM user_roles WHERE role_code=$1 ORDER BY 1`, roleCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ---------- 配置中心（JSONB） ----------

const appConfigColumns = `key, value, description, updated_at`

func scanAppConfig(row rowScanner) (*model.AppConfigItem, error) {
	var item model.AppConfigItem
	if err := row.Scan(&item.Key, &item.Value, &item.Description, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (p *Postgres) ListAppConfig(ctx context.Context) ([]model.AppConfigItem, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+appConfigColumns+` FROM app_config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.AppConfigItem{}
	for rows.Next() {
		item, err := scanAppConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (p *Postgres) GetAppConfig(ctx context.Context, key string) (*model.AppConfigItem, error) {
	item, err := scanAppConfig(p.pool.QueryRow(ctx, `SELECT `+appConfigColumns+` FROM app_config WHERE key=$1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (p *Postgres) SetAppConfig(ctx context.Context, key string, value []byte, description *string, updatedBy string) (*model.AppConfigItem, error) {
	normalized, err := normalizeConfigValue(value)
	if err != nil {
		return nil, err
	}
	var updated any
	if updatedBy != "" {
		updated = updatedBy
	}
	// description 为 nil 时保留原描述（新增时落空串）。
	item, err := scanAppConfig(p.pool.QueryRow(ctx, `
		INSERT INTO app_config (key, value, description, updated_by)
		VALUES ($1, $2::jsonb, COALESCE($3::text, ''), $4)
		ON CONFLICT (key) DO UPDATE SET
			value = EXCLUDED.value,
			description = CASE WHEN $3::text IS NULL THEN app_config.description ELSE EXCLUDED.description END,
			updated_at = now(),
			updated_by = EXCLUDED.updated_by
		RETURNING `+appConfigColumns, key, normalized, description, updated))
	if err != nil {
		return nil, err
	}
	return item, nil
}
