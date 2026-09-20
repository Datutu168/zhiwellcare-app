package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"zhiwellcare/backend/internal/model"
)

// ---------- 设备白名单 ----------

func (p *Postgres) ListDeviceWhitelist(ctx context.Context) ([]model.DeviceWhitelistEntry, error) {
	rows, err := p.pool.Query(ctx, `SELECT device_key, model_id, note, enabled FROM device_whitelist ORDER BY device_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.DeviceWhitelistEntry{}
	for rows.Next() {
		var item model.DeviceWhitelistEntry
		if err := rows.Scan(&item.DeviceKey, &item.ModelID, &item.Note, &item.Enabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) ListWhitelistKeys(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT device_key FROM device_whitelist WHERE enabled ORDER BY device_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (p *Postgres) UpsertDeviceWhitelist(ctx context.Context, entry model.DeviceWhitelistEntry) (*model.DeviceWhitelistEntry, error) {
	key := normalizeDeviceKey(entry.DeviceKey)
	if key == "" {
		return nil, ErrNotFound
	}
	var result model.DeviceWhitelistEntry
	err := p.pool.QueryRow(ctx, `
		INSERT INTO device_whitelist (device_key, model_id, note, enabled)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (device_key) DO UPDATE SET
			model_id = EXCLUDED.model_id, note = EXCLUDED.note, enabled = EXCLUDED.enabled, updated_at = now()
		RETURNING device_key, model_id, note, enabled`,
		key, entry.ModelID, entry.Note, entry.Enabled).
		Scan(&result.DeviceKey, &result.ModelID, &result.Note, &result.Enabled)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *Postgres) DeleteDeviceWhitelist(ctx context.Context, deviceKey string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM device_whitelist WHERE device_key=$1`, normalizeDeviceKey(deviceKey))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- 设备映射 ----------

func (p *Postgres) ListDeviceMappings(ctx context.Context) ([]model.DeviceMappingEntry, error) {
	rows, err := p.pool.Query(ctx, `SELECT device_key, model_id, note FROM device_mappings ORDER BY device_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.DeviceMappingEntry{}
	for rows.Next() {
		var item model.DeviceMappingEntry
		if err := rows.Scan(&item.DeviceKey, &item.ModelID, &item.Note); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) GetDeviceMapping(ctx context.Context, deviceKey string) (string, error) {
	var modelID string
	err := p.pool.QueryRow(ctx, `SELECT model_id FROM device_mappings WHERE device_key=$1`, normalizeDeviceKey(deviceKey)).Scan(&modelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return modelID, nil
}

func (p *Postgres) UpsertDeviceMapping(ctx context.Context, entry model.DeviceMappingEntry) (*model.DeviceMappingEntry, error) {
	key := normalizeDeviceKey(entry.DeviceKey)
	if key == "" || entry.ModelID == "" {
		return nil, ErrNotFound
	}
	var result model.DeviceMappingEntry
	err := p.pool.QueryRow(ctx, `
		INSERT INTO device_mappings (device_key, model_id, note)
		VALUES ($1,$2,$3)
		ON CONFLICT (device_key) DO UPDATE SET
			model_id = EXCLUDED.model_id, note = EXCLUDED.note, updated_at = now()
		RETURNING device_key, model_id, note`,
		key, entry.ModelID, entry.Note).
		Scan(&result.DeviceKey, &result.ModelID, &result.Note)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *Postgres) DeleteDeviceMapping(ctx context.Context, deviceKey string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM device_mappings WHERE device_key=$1`, normalizeDeviceKey(deviceKey))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- 资产登记 ----------

const assetColumns = `id, kind, ref_id, version, filename, object_key, size, sha256, content_type, notes, status, created_at, published_at`

func scanAsset(row rowScanner) (*model.Asset, error) {
	var item model.Asset
	err := row.Scan(&item.ID, &item.Kind, &item.RefID, &item.Version, &item.Filename, &item.ObjectKey,
		&item.Size, &item.SHA256, &item.ContentType, &item.Notes, &item.Status, &item.CreatedAt, &item.PublishedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (p *Postgres) CreateAsset(ctx context.Context, asset model.Asset) (*model.Asset, error) {
	status := model.NormalizeAssetStatus(asset.Status)
	item, err := scanAsset(p.pool.QueryRow(ctx, `
		INSERT INTO assets (kind, ref_id, version, filename, object_key, size, sha256, content_type, notes, status, published_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10, CASE WHEN $10 = 'published' THEN now() ELSE NULL END)
		RETURNING `+assetColumns,
		asset.Kind, asset.RefID, asset.Version, asset.Filename, asset.ObjectKey, asset.Size,
		asset.SHA256, asset.ContentType, asset.Notes, status))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAssetExists
		}
		return nil, err
	}
	return item, nil
}

func (p *Postgres) GetAsset(ctx context.Context, id int64) (*model.Asset, error) {
	item, err := scanAsset(p.pool.QueryRow(ctx, `SELECT `+assetColumns+` FROM assets WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (p *Postgres) ListAssets(ctx context.Context, kind, refID, status string) ([]model.Asset, error) {
	query := `SELECT ` + assetColumns + ` FROM assets WHERE ($1 = '' OR kind = $1) AND ($2 = '' OR ref_id = $2) AND ($3 = '' OR status = $3) ORDER BY id DESC`
	rows, err := p.pool.Query(ctx, query, kind, refID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Asset{}
	for rows.Next() {
		item, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (p *Postgres) SetAssetStatus(ctx context.Context, id int64, status string) (*model.Asset, error) {
	item, err := scanAsset(p.pool.QueryRow(ctx, `
		UPDATE assets SET status=$2,
			published_at = CASE WHEN $2 = 'published' THEN COALESCE(published_at, now()) ELSE published_at END
		WHERE id=$1
		RETURNING `+assetColumns, id, status))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (p *Postgres) DeleteAsset(ctx context.Context, id int64) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM assets WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) FindPublishedAsset(ctx context.Context, kind, refID, version string) (*model.Asset, error) {
	item, err := scanAsset(p.pool.QueryRow(ctx, `
		SELECT `+assetColumns+` FROM assets
		WHERE kind=$1 AND ref_id=$2 AND version=$3 AND status='published'
		ORDER BY published_at DESC NULLS LAST, id DESC LIMIT 1`, kind, refID, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (p *Postgres) LatestPublishedAsset(ctx context.Context, kind, refID string) (*model.Asset, error) {
	item, err := scanAsset(p.pool.QueryRow(ctx, `
		SELECT `+assetColumns+` FROM assets
		WHERE kind=$1 AND ref_id=$2 AND status='published'
		ORDER BY published_at DESC NULLS LAST, id DESC LIMIT 1`, kind, refID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}
