package store

import (
	"context"

	"zhiwellcare/backend/internal/model"
)

// ---------- 内容中心：课程 / 商城商品（PostgreSQL） ----------

const courseColumns = `course_id, title, summary, cover_url, video_url, duration_label, level, tags,
	status, sort, created_at, updated_at`

func scanCourse(row rowScanner) (*model.Course, error) {
	var item model.Course
	err := row.Scan(&item.CourseID, &item.Title, &item.Summary, &item.CoverURL, &item.VideoURL,
		&item.DurationLabel, &item.Level, &item.Tags, &item.Status, &item.Sort,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	// text[] NOT NULL DEFAULT '{}' 理论上不会为 NULL，归一避免 JSON 出现 null。
	item.Tags = emptyIfNil(item.Tags)
	return &item, nil
}

func (p *Postgres) ListCourses(ctx context.Context, includeOff bool) ([]model.Course, error) {
	query := `SELECT ` + courseColumns + ` FROM courses`
	if !includeOff {
		query += ` WHERE status = 'on'`
	}
	// sort 升序（与内存实现一致，sort 相同按 ID 升序保证稳定）
	query += ` ORDER BY sort, course_id`
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.Course
	for rows.Next() {
		item, err := scanCourse(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (p *Postgres) UpsertCourse(ctx context.Context, item model.Course) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO courses (course_id, title, summary, cover_url, video_url, duration_label, level, tags, status, sort)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE(NULLIF($9,''),'off'),$10)
		ON CONFLICT (course_id) DO UPDATE SET
			title=EXCLUDED.title, summary=EXCLUDED.summary, cover_url=EXCLUDED.cover_url,
			video_url=EXCLUDED.video_url, duration_label=EXCLUDED.duration_label, level=EXCLUDED.level,
			tags=EXCLUDED.tags, status=EXCLUDED.status, sort=EXCLUDED.sort, updated_at=now()`,
		item.CourseID, item.Title, item.Summary, item.CoverURL, item.VideoURL, item.DurationLabel,
		item.Level, emptyIfNil(item.Tags), item.Status, item.Sort,
	)
	return err
}

func (p *Postgres) DeleteCourse(ctx context.Context, courseID string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM courses WHERE course_id = $1`, courseID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetCourseStatus 上下架。目标不存在返回 ErrNotFound（映射为 404）；
// 这里不用 game 那套 ErrUserNotFound —— 那会把「内容不存在」错报成 401 登录失效。
func (p *Postgres) SetCourseStatus(ctx context.Context, courseID, status string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE courses SET status=$2, updated_at=now() WHERE course_id=$1`, courseID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const goodsColumns = `goods_id, name, summary, price_cents, price_label, cover_url, detail_url, specs,
	status, sort, created_at, updated_at`

func scanGoods(row rowScanner) (*model.MallGoods, error) {
	var item model.MallGoods
	err := row.Scan(&item.GoodsID, &item.Name, &item.Summary, &item.PriceCents, &item.PriceLabel,
		&item.CoverURL, &item.DetailURL, &item.Specs, &item.Status, &item.Sort,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.Specs = emptyIfNil(item.Specs)
	return &item, nil
}

func (p *Postgres) ListGoods(ctx context.Context, includeOff bool) ([]model.MallGoods, error) {
	query := `SELECT ` + goodsColumns + ` FROM mall_goods`
	if !includeOff {
		query += ` WHERE status = 'on'`
	}
	query += ` ORDER BY sort, goods_id`
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.MallGoods
	for rows.Next() {
		item, err := scanGoods(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (p *Postgres) UpsertGoods(ctx context.Context, item model.MallGoods) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO mall_goods (goods_id, name, summary, price_cents, price_label, cover_url, detail_url, specs, status, sort)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE(NULLIF($9,''),'off'),$10)
		ON CONFLICT (goods_id) DO UPDATE SET
			name=EXCLUDED.name, summary=EXCLUDED.summary, price_cents=EXCLUDED.price_cents,
			price_label=EXCLUDED.price_label, cover_url=EXCLUDED.cover_url, detail_url=EXCLUDED.detail_url,
			specs=EXCLUDED.specs, status=EXCLUDED.status, sort=EXCLUDED.sort, updated_at=now()`,
		item.GoodsID, item.Name, item.Summary, item.PriceCents, item.PriceLabel,
		item.CoverURL, item.DetailURL, emptyIfNil(item.Specs), item.Status, item.Sort,
	)
	return err
}

func (p *Postgres) DeleteGoods(ctx context.Context, goodsID string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM mall_goods WHERE goods_id = $1`, goodsID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SetGoodsStatus(ctx context.Context, goodsID, status string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE mall_goods SET status=$2, updated_at=now() WHERE goods_id=$1`, goodsID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
