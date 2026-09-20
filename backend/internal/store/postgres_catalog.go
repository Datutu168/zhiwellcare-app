package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"zhiwellcare/backend/internal/model"
)

// jsonOrEmptyObject 空 JSON 归一为 {}（列类型 jsonb NOT NULL）。
func jsonOrEmptyObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// emptyIfNil 数组列（NOT NULL DEFAULT '{}'）不接受 NULL：nil 归一为空切片。
func emptyIfNil(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

// emptyIntsIfNil 同上，用于 int[] 列。
func emptyIntsIfNil(items []int) []int {
	if items == nil {
		return []int{}
	}
	return items
}

// ---------- 设备型号 ----------

const deviceModelColumns = `model_id, name, manufacturer, category, description, capability_tags, supported_handle_tags,
	name_patterns, protocol, min_app_version, firmware_updatable, icon, status, config, created_at, updated_at`

func scanDeviceModel(row rowScanner) (*model.DeviceModel, error) {
	var item model.DeviceModel
	err := row.Scan(&item.ModelID, &item.Name, &item.Manufacturer, &item.Category, &item.Description,
		&item.CapabilityTags, &item.SupportedHandleTags, &item.NamePatterns, &item.Protocol, &item.MinAppVersion,
		&item.FirmwareUpdatable, &item.Icon, &item.Status, &item.Config, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.Config = jsonOrEmptyObject(item.Config)
	return &item, nil
}

func (p *Postgres) ListDeviceModels(ctx context.Context, includeOff bool) ([]model.DeviceModel, error) {
	query := `SELECT ` + deviceModelColumns + ` FROM device_models`
	if !includeOff {
		query += ` WHERE status = 'on'`
	}
	query += ` ORDER BY created_at`
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.DeviceModel
	for rows.Next() {
		item, err := scanDeviceModel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (p *Postgres) GetDeviceModel(ctx context.Context, modelID string) (*model.DeviceModel, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+deviceModelColumns+` FROM device_models WHERE model_id = $1`, modelID)
	item, err := scanDeviceModel(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (p *Postgres) UpsertDeviceModel(ctx context.Context, item model.DeviceModel) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO device_models (model_id, name, manufacturer, category, description, capability_tags,
			supported_handle_tags, name_patterns, protocol, min_app_version, firmware_updatable, icon, status, config)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,COALESCE(NULLIF($13,''),'on'),$14::jsonb)
		ON CONFLICT (model_id) DO UPDATE SET
			name=EXCLUDED.name, manufacturer=EXCLUDED.manufacturer, category=EXCLUDED.category,
			description=EXCLUDED.description, capability_tags=EXCLUDED.capability_tags,
			supported_handle_tags=EXCLUDED.supported_handle_tags, name_patterns=EXCLUDED.name_patterns,
			protocol=EXCLUDED.protocol, min_app_version=EXCLUDED.min_app_version,
			firmware_updatable=EXCLUDED.firmware_updatable, icon=EXCLUDED.icon,
			status=EXCLUDED.status, config=EXCLUDED.config, updated_at=now()`,
		item.ModelID, item.Name, item.Manufacturer, item.Category, item.Description, emptyIfNil(item.CapabilityTags),
		emptyIfNil(item.SupportedHandleTags), emptyIfNil(item.NamePatterns), item.Protocol, item.MinAppVersion,
		item.FirmwareUpdatable, item.Icon, item.Status, jsonOrEmptyObject(item.Config),
	)
	return err
}

func (p *Postgres) DeleteDeviceModel(ctx context.Context, modelID string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM device_models WHERE model_id = $1`, modelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SetDeviceModelStatus(ctx context.Context, modelID, status string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE device_models SET status=$2, updated_at=now() WHERE model_id=$1`, modelID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ---------- 游戏目录 ----------

const gameColumns = `game_id, name, summary, required_tags, duration_presets_min, resource_version, resource_url,
	status, gray_batch, gray_ratio, category_label, play_mode, config, created_at, updated_at`

func scanGame(row rowScanner) (*model.GameCatalogItem, error) {
	var item model.GameCatalogItem
	err := row.Scan(&item.GameID, &item.Name, &item.Summary, &item.RequiredTags, &item.DurationPresetsMin,
		&item.ResourceVersion, &item.ResourceURL, &item.Status, &item.GrayBatch, &item.GrayRatio,
		&item.CategoryLabel, &item.PlayMode, &item.Config, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.Config = jsonOrEmptyObject(item.Config)
	return &item, nil
}

func (p *Postgres) ListGames(ctx context.Context, includeOff bool) ([]model.GameCatalogItem, error) {
	query := `SELECT ` + gameColumns + ` FROM game_catalog`
	if !includeOff {
		query += ` WHERE status = 'on'`
	}
	query += ` ORDER BY created_at`
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.GameCatalogItem
	for rows.Next() {
		item, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (p *Postgres) GetGame(ctx context.Context, gameID string) (*model.GameCatalogItem, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+gameColumns+` FROM game_catalog WHERE game_id = $1`, gameID)
	item, err := scanGame(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (p *Postgres) UpsertGame(ctx context.Context, item model.GameCatalogItem) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO game_catalog (game_id, name, summary, required_tags, duration_presets_min, resource_version,
			resource_url, status, gray_batch, gray_ratio, category_label, play_mode, config)
		VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE(NULLIF($8,''),'off'),$9,$10,$11,$12,$13::jsonb)
		ON CONFLICT (game_id) DO UPDATE SET
			name=EXCLUDED.name, summary=EXCLUDED.summary, required_tags=EXCLUDED.required_tags,
			duration_presets_min=EXCLUDED.duration_presets_min, resource_version=EXCLUDED.resource_version,
			resource_url=EXCLUDED.resource_url, status=EXCLUDED.status, gray_batch=EXCLUDED.gray_batch,
			gray_ratio=EXCLUDED.gray_ratio, category_label=EXCLUDED.category_label,
			play_mode=EXCLUDED.play_mode, config=EXCLUDED.config, updated_at=now()`,
		item.GameID, item.Name, item.Summary, emptyIfNil(item.RequiredTags), emptyIntsIfNil(item.DurationPresetsMin),
		item.ResourceVersion, item.ResourceURL, item.Status, item.GrayBatch, item.GrayRatio,
		item.CategoryLabel, item.PlayMode, jsonOrEmptyObject(item.Config),
	)
	return err
}

func (p *Postgres) DeleteGame(ctx context.Context, gameID string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM game_catalog WHERE game_id = $1`, gameID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SetGameStatus(ctx context.Context, gameID, status string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE game_catalog SET status=$2, updated_at=now() WHERE game_id=$1`, gameID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ---------- 训练摘要 ----------

// SaveTrainingRecord 落训练摘要。
//
// training_records 是按 completed_at 的 RANGE 分区表（见 migrations/003），
// 分区表的唯一索引必须包含分区键，因此幂等键为 (record_id, completed_at)：
// 客户端重试时 recordId 与 completedAt 不变 → ON CONFLICT DO NOTHING 只落 1 行。
func (p *Postgres) SaveTrainingRecord(ctx context.Context, record model.TrainingSummary) error {
	stats := record.Statistics
	if len(stats) == 0 {
		stats = json.RawMessage(`{}`)
	}
	// 写入前保证该月份分区存在（进程内按年月去重，正常只多一次往返）。
	p.ensureRecordPartition(ctx, record.CompletedAt)
	_, err := p.pool.Exec(ctx, `
		INSERT INTO training_records (record_id, user_id, game_id, game_name, device_model_id, device_model_name,
			capability_tags, statistics, client_version, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (record_id, completed_at) DO NOTHING`,
		record.RecordID, record.UserID, record.GameID, record.GameName, record.DeviceModelID,
		record.DeviceModelName, emptyIfNil(record.CapabilityTags), stats, record.ClientVersion, record.CompletedAt,
	)
	return err
}

// ensureRecordPartition 按需创建月分区；失败不阻断写入（DEFAULT 分区兜底）。
func (p *Postgres) ensureRecordPartition(ctx context.Context, at time.Time) {
	if at.IsZero() {
		return
	}
	month := at.UTC().Format("2006-01")
	if _, loaded := p.partitionEnsured.LoadOrStore(month, struct{}{}); loaded {
		return
	}
	if _, err := p.pool.Exec(ctx, `SELECT training_records_ensure_partition($1)`, at); err != nil {
		// 允许下次重试；DEFAULT 分区保证本次写入不会失败。
		p.partitionEnsured.Delete(month)
	}
}

func (p *Postgres) GetTrainingRecord(ctx context.Context, recordID string) (*model.TrainingSummary, error) {
	var record model.TrainingSummary
	var userID *string
	err := p.pool.QueryRow(ctx, `SELECT id, record_id, user_id::text, game_id, game_name, device_model_id, device_model_name,
		capability_tags, statistics, client_version, completed_at, uploaded_at
		FROM training_records WHERE record_id = $1 ORDER BY completed_at DESC LIMIT 1`, recordID).
		Scan(&record.ID, &record.RecordID, &userID, &record.GameID, &record.GameName,
			&record.DeviceModelID, &record.DeviceModelName, &record.CapabilityTags, &record.Statistics,
			&record.ClientVersion, &record.CompletedAt, &record.UploadedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	record.UserID = userID
	return &record, nil
}

func (p *Postgres) ListTrainingRecords(ctx context.Context, filter TrainingRecordFilter) ([]model.TrainingSummary, int64, error) {
	where := []string{"1=1"}
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.UserID != "" {
		where = append(where, "user_id = "+arg(filter.UserID))
	}
	if filter.GameID != "" {
		where = append(where, "game_id = "+arg(filter.GameID))
	}
	if filter.ModelID != "" {
		where = append(where, "device_model_id = "+arg(filter.ModelID))
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM training_records WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(filter.Page, filter.PageSize)
	query := `SELECT id, record_id, user_id::text, game_id, game_name, device_model_id, device_model_name,
		capability_tags, statistics, client_version, completed_at, uploaded_at
		FROM training_records WHERE ` + clause + ` ORDER BY completed_at DESC LIMIT ` + arg(size) + ` OFFSET ` + arg((page-1)*size)
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []model.TrainingSummary
	for rows.Next() {
		var record model.TrainingSummary
		var userID *string
		if err := rows.Scan(&record.ID, &record.RecordID, &userID, &record.GameID, &record.GameName,
			&record.DeviceModelID, &record.DeviceModelName, &record.CapabilityTags, &record.Statistics,
			&record.ClientVersion, &record.CompletedAt, &record.UploadedAt); err != nil {
			return nil, 0, err
		}
		record.UserID = userID
		items = append(items, record)
	}
	return items, total, rows.Err()
}

func (p *Postgres) DashboardStats(ctx context.Context) (*model.DashboardStats, error) {
	stats := &model.DashboardStats{}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM device_models`).Scan(&stats.DeviceModels); err != nil {
		return nil, err
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM game_catalog`).Scan(&stats.Games); err != nil {
		return nil, err
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&stats.Users); err != nil {
		return nil, err
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM training_records`).Scan(&stats.TrainingRecs); err != nil {
		return nil, err
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM training_records WHERE completed_at >= date_trunc('day', now())`).Scan(&stats.TodayRecs); err != nil {
		return nil, err
	}
	return stats, nil
}

// ---------- OAuth 身份 ----------

func (p *Postgres) FindUserByOpenID(ctx context.Context, provider, openid string) (*model.User, error) {
	return p.userByOAuthQuery(ctx,
		`SELECT u.id, COALESCE(u.phone,''), u.password_hash, u.nickname, u.avatar_url, u.role, u.status,
			u.created_at, u.updated_at, u.last_login_at
		 FROM oauth_identities oi JOIN users u ON u.id = oi.user_id
		 WHERE oi.provider=$1 AND oi.openid=$2`, provider, openid)
}

func (p *Postgres) FindUserByUnionID(ctx context.Context, provider, unionid string) (*model.User, error) {
	return p.userByOAuthQuery(ctx,
		`SELECT u.id, COALESCE(u.phone,''), u.password_hash, u.nickname, u.avatar_url, u.role, u.status,
			u.created_at, u.updated_at, u.last_login_at
		 FROM oauth_identities oi JOIN users u ON u.id = oi.user_id
		 WHERE oi.provider=$1 AND oi.unionid=$2`, provider, unionid)
}

func (p *Postgres) userByOAuthQuery(ctx context.Context, query string, args ...any) (*model.User, error) {
	user, err := scanUser(p.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (p *Postgres) GetOrCreateByOAuth(ctx context.Context, params OAuthParams) (*model.User, bool, error) {
	// 优先 openid；有 unionid 时优先 unionid（同一开放平台账号打通）。
	if params.UnionID != nil && *params.UnionID != "" {
		if user, err := p.FindUserByUnionID(ctx, params.Provider, *params.UnionID); err == nil {
			return user, false, nil
		} else if !errors.Is(err, ErrUserNotFound) {
			return nil, false, err
		}
	}
	if user, err := p.FindUserByOpenID(ctx, params.Provider, params.OpenID); err == nil {
		return user, false, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, false, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var user model.User
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (nickname, password_hash) VALUES ($1,'') 
		 RETURNING id, COALESCE(phone,''), password_hash, nickname, avatar_url, role, status, created_at, updated_at, last_login_at`,
		params.Nickname,
	).Scan(&user.ID, &user.Phone, &user.PasswordHash, &user.Nickname, &user.AvatarURL, &user.Role, &user.Status,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt); err != nil {
		return nil, false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO oauth_identities (provider, openid, unionid, user_id) VALUES ($1,$2,$3,$4)`,
		params.Provider, params.OpenID, params.UnionID, user.ID); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return &user, true, nil
}

func (p *Postgres) BindPhoneToUser(ctx context.Context, userID, phone string) error {
	_, err := p.pool.Exec(ctx, `UPDATE users SET phone=$2, updated_at=now() WHERE id=$1`, userID, phone)
	if isUniqueViolation(err) {
		return ErrPhoneExists
	}
	return err
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
