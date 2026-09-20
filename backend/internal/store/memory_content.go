package store

import (
	"context"
	"sort"
	"time"

	"zhiwellcare/backend/internal/model"
)

// ---------- 内容中心：课程 / 商城商品（内存演示） ----------

// sortCourses 与 PostgreSQL 的 ORDER BY sort, course_id 保持一致，保证两套实现输出同序。
func sortCourses(items []model.Course) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Sort != items[j].Sort {
			return items[i].Sort < items[j].Sort
		}
		return items[i].CourseID < items[j].CourseID
	})
}

// sortGoods 同上，对应 ORDER BY sort, goods_id。
func sortGoods(items []model.MallGoods) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Sort != items[j].Sort {
			return items[i].Sort < items[j].Sort
		}
		return items[i].GoodsID < items[j].GoodsID
	})
}

func (m *Memory) ListCourses(_ context.Context, includeOff bool) ([]model.Course, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.Course, 0, len(m.courses))
	for _, item := range m.courses {
		if !includeOff && item.Status != "on" {
			continue
		}
		items = append(items, *item)
	}
	sortCourses(items)
	return items, nil
}

func (m *Memory) UpsertCourse(_ context.Context, item model.Course) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item.Status == "" {
		item.Status = "off"
	}
	// 与 PostgreSQL 的 text[] NOT NULL DEFAULT '{}' 行为保持一致：nil 归一为空切片。
	if item.Tags == nil {
		item.Tags = []string{}
	}
	now := time.Now()
	if existing := m.courses[item.CourseID]; existing != nil {
		// upsert 不改变创建时间（与 PG 的 ON CONFLICT DO UPDATE 一致）
		item.CreatedAt = existing.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.courses[item.CourseID] = &item
	return nil
}

func (m *Memory) DeleteCourse(_ context.Context, courseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.courses[courseID]; !exists {
		return ErrNotFound
	}
	delete(m.courses, courseID)
	return nil
}

func (m *Memory) SetCourseStatus(_ context.Context, courseID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.courses[courseID]
	if item == nil {
		return ErrNotFound
	}
	item.Status = status
	item.UpdatedAt = time.Now()
	return nil
}

func (m *Memory) ListGoods(_ context.Context, includeOff bool) ([]model.MallGoods, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.MallGoods, 0, len(m.goods))
	for _, item := range m.goods {
		if !includeOff && item.Status != "on" {
			continue
		}
		items = append(items, *item)
	}
	sortGoods(items)
	return items, nil
}

func (m *Memory) UpsertGoods(_ context.Context, item model.MallGoods) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item.Status == "" {
		item.Status = "off"
	}
	if item.Specs == nil {
		item.Specs = []string{}
	}
	now := time.Now()
	if existing := m.goods[item.GoodsID]; existing != nil {
		item.CreatedAt = existing.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.goods[item.GoodsID] = &item
	return nil
}

func (m *Memory) DeleteGoods(_ context.Context, goodsID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.goods[goodsID]; !exists {
		return ErrNotFound
	}
	delete(m.goods, goodsID)
	return nil
}

func (m *Memory) SetGoodsStatus(_ context.Context, goodsID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.goods[goodsID]
	if item == nil {
		return ErrNotFound
	}
	item.Status = status
	item.UpdatedAt = time.Now()
	return nil
}
